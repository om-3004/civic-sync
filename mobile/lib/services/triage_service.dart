// Triage service — image upload to Firebase Storage and AI triage backend call.
//
// Responsibilities:
//   1. Upload a captured XFile to Cloud Storage for Firebase under
//      tickets/<uuid>/<filename> and return the download URL.
//   2. POST /triage with the Storage URL and GPS coordinates, returning a
//      structured TriageResult parsed from the JSON response.
//
// Requirements: 2.6, 2.7, 3.1

import 'dart:convert';
import 'dart:io';

import 'package:firebase_auth/firebase_auth.dart';
import 'package:firebase_storage/firebase_storage.dart';
import 'package:http/http.dart' as http;
import 'package:image_picker/image_picker.dart';

// Backend base URL — override at build time via --dart-define=BACKEND_URL=...
// Defaults to the Android emulator loopback to the host machine.
const String _backendBaseUrl = String.fromEnvironment(
  'BACKEND_URL',
  defaultValue: 'http://10.0.2.2:8080',
);

// ── Custom exceptions ─────────────────────────────────────────────────────────

/// Thrown when uploading an image to Cloud Storage for Firebase fails.
class StorageUploadException implements Exception {
  final String message;
  const StorageUploadException(this.message);

  @override
  String toString() => 'StorageUploadException: $message';
}

/// Thrown when the backend /triage call returns a non-200 status or a network
/// error occurs.
class TriageException implements Exception {
  final String message;
  const TriageException(this.message);

  @override
  String toString() => 'TriageException: $message';
}

// ── TriageResult model ────────────────────────────────────────────────────────

/// Structured result returned by the backend AI triage endpoint.
class TriageResult {
  final String category;
  final String title;
  final String description;

  const TriageResult({
    required this.category,
    required this.title,
    required this.description,
  });

  factory TriageResult.fromJson(Map<String, dynamic> json) {
    return TriageResult(
      category: (json['category'] as String?) ?? '',
      title: (json['title'] as String?) ?? '',
      description: (json['description'] as String?) ?? '',
    );
  }

  @override
  String toString() =>
      'TriageResult(category: $category, title: $title, description: $description)';
}

// ── TriageService ─────────────────────────────────────────────────────────────

/// Service encapsulating image upload and AI triage operations.
///
/// Usage:
/// ```dart
/// final service = TriageService();
/// final url = await service.uploadImage(imageFile);          // Req 2.6
/// final result = await service.triage(                       // Req 3.1
///   imageUrl: url,
///   latitude: position.latitude,
///   longitude: position.longitude,
/// );
/// ```
class TriageService {
  final FirebaseStorage _storage;
  final FirebaseAuth _auth;
  final http.Client _httpClient;

  TriageService({
    FirebaseStorage? storage,
    FirebaseAuth? auth,
    http.Client? httpClient,
  })  : _storage = storage ?? FirebaseStorage.instance,
        _auth = auth ?? FirebaseAuth.instance,
        _httpClient = httpClient ?? http.Client();

  /// Uploads [image] to Cloud Storage at `tickets/<uuid>/<filename>` and
  /// returns the publicly accessible download URL.
  ///
  /// Throws [StorageUploadException] on any upload failure (Req 2.7).
  Future<String> uploadImage(XFile image) async {
    try {
      // Generate a UUID-like unique path segment using timestamp + hashCode
      // to avoid collisions without adding a uuid dependency.
      final String uniqueId =
          '${DateTime.now().millisecondsSinceEpoch}_${image.name.hashCode.abs()}';
      final String fileName = image.name;
      final String storagePath = 'tickets/$uniqueId/$fileName';

      final Reference ref = _storage.ref().child(storagePath);

      final File file = File(image.path);
      await ref.putFile(file);

      final String downloadUrl = await ref.getDownloadURL();
      return downloadUrl;
    } on FirebaseException catch (e) {
      throw StorageUploadException(
        e.message ?? 'Failed to upload image to Cloud Storage.',
      );
    } catch (e) {
      throw StorageUploadException(
        'An unexpected error occurred during image upload: $e',
      );
    }
  }

  /// POSTs to `$backendBaseUrl/triage` with [imageUrl] and GPS coordinates,
  /// returning a parsed [TriageResult] on success (HTTP 200).
  ///
  /// The current Firebase Auth user's ID token is included as the
  /// `Authorization: Bearer <token>` header.
  ///
  /// Throws [TriageException] on non-200 responses or network errors (Req 3.1).
  Future<TriageResult> triage({
    required String imageUrl,
    required double latitude,
    required double longitude,
  }) async {
    try {
      // Retrieve current user's Firebase ID token for authenticated request.
      final String? idToken =
          await _auth.currentUser?.getIdToken();

      final Map<String, dynamic> body = {
        'image_url': imageUrl,
        'location': {
          'latitude': latitude,
          'longitude': longitude,
        },
      };

      final http.Response response = await _httpClient.post(
        Uri.parse('$_backendBaseUrl/triage'),
        headers: {
          'Content-Type': 'application/json',
          if (idToken != null) 'Authorization': 'Bearer $idToken',
        },
        body: jsonEncode(body),
      );

      if (response.statusCode == 200) {
        final Map<String, dynamic> json =
            jsonDecode(response.body) as Map<String, dynamic>;
        return TriageResult.fromJson(json);
      } else {
        throw TriageException(
          'Triage request failed with status ${response.statusCode}.',
        );
      }
    } on TriageException {
      rethrow;
    } on http.ClientException catch (e) {
      throw TriageException('Network error during triage: ${e.message}');
    } catch (e) {
      throw TriageException('An unexpected error occurred during triage: $e');
    }
  }
}
