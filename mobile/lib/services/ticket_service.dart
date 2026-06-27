// Ticket service — submit new civic issue tickets and upvote duplicates.
//
// Responsibilities:
//   1. POST /tickets with category, title, description, image URL, and GPS
//      coordinates; returns a TicketSubmitResult indicating whether the
//      backend found a duplicate (Req 3.3, 3.4, 4.3).
//   2. POST /tickets/:id/upvote to increment an existing ticket's upvote count.
//
// Both calls use the Firebase Auth ID token as a Bearer token, matching the
// pattern established in triage_service.dart.
//
// Requirements: 3.3, 3.4, 4.3

import 'dart:convert';

import 'package:firebase_auth/firebase_auth.dart';
import 'package:http/http.dart' as http;

// Backend base URL — override at build time via --dart-define=BACKEND_URL=...
// Defaults to the Android emulator loopback to the host machine.
const String _backendBaseUrl = String.fromEnvironment(
  'BACKEND_URL',
  defaultValue: 'http://10.0.2.2:8080',
);

// ── Custom exceptions ─────────────────────────────────────────────────────────

/// Thrown when POST /tickets returns a non-200/201 status or a network error
/// occurs.
class TicketSubmitException implements Exception {
  final String message;
  final int? statusCode;

  const TicketSubmitException(this.message, {this.statusCode});

  @override
  String toString() => 'TicketSubmitException: $message';
}

/// Thrown when POST /tickets/:id/upvote returns a non-200 status or a network
/// error occurs.
class UpvoteException implements Exception {
  final String message;
  final int? statusCode;

  const UpvoteException(this.message, {this.statusCode});

  @override
  String toString() => 'UpvoteException: $message';
}

// ── TicketSubmitResult model ──────────────────────────────────────────────────

/// Result returned by [TicketService.submitTicket].
///
/// [isDuplicate] is true when the backend responded with HTTP 200, meaning a
/// nearby ticket already exists. [isDuplicate] is false on HTTP 201 (new
/// ticket created).
class TicketSubmitResult {
  /// Raw ticket JSON object from the backend response.
  final Map<String, dynamic> ticket;

  /// True when the backend found a nearby duplicate (HTTP 200).
  /// False when a new ticket was created (HTTP 201).
  final bool isDuplicate;

  const TicketSubmitResult({
    required this.ticket,
    required this.isDuplicate,
  });
}

// ── TicketService ─────────────────────────────────────────────────────────────

/// Service encapsulating ticket submission and upvote operations.
///
/// Usage:
/// ```dart
/// final service = TicketService();
///
/// // Submit a new ticket (Req 3.3, 3.4):
/// final result = await service.submitTicket(
///   category: 'Pothole',
///   title: 'Large pothole on Main St',
///   description: 'Deep pothole causing damage to vehicles.',
///   imageUrl: 'https://storage.googleapis.com/...',
///   latitude: 23.0225,
///   longitude: 72.5714,
/// );
///
/// // Upvote a duplicate ticket (Req 4.3):
/// final upvotes = await service.upvoteTicket(result.ticket['id']);
/// ```
class TicketService {
  final FirebaseAuth _auth;
  final http.Client _httpClient;

  TicketService({
    FirebaseAuth? auth,
    http.Client? httpClient,
  })  : _auth = auth ?? FirebaseAuth.instance,
        _httpClient = httpClient ?? http.Client();

  /// Submits a new civic issue ticket via POST /tickets.
  ///
  /// Returns a [TicketSubmitResult] with [isDuplicate] set to true when the
  /// backend finds a nearby existing ticket (HTTP 200), or false for a newly
  /// created ticket (HTTP 201).
  ///
  /// Throws [TicketSubmitException] on:
  ///   - HTTP 400: missing or invalid coordinates / body fields.
  ///   - HTTP 401: unauthenticated or expired token.
  ///   - HTTP 5xx or network error.
  Future<TicketSubmitResult> submitTicket({
    required String category,
    required String title,
    required String description,
    required String imageUrl,
    required double latitude,
    required double longitude,
  }) async {
    try {
      final String? idToken = await _auth.currentUser?.getIdToken();

      final Map<String, dynamic> body = {
        'category': category,
        'title': title,
        'description': description,
        'image_url': imageUrl,
        'location': {
          'latitude': latitude,
          'longitude': longitude,
        },
      };

      final http.Response response = await _httpClient.post(
        Uri.parse('$_backendBaseUrl/tickets'),
        headers: {
          'Content-Type': 'application/json',
          if (idToken != null) 'Authorization': 'Bearer $idToken',
        },
        body: jsonEncode(body),
      );

      if (response.statusCode == 201 || response.statusCode == 200) {
        final Map<String, dynamic> json =
            jsonDecode(response.body) as Map<String, dynamic>;
        final Map<String, dynamic> ticketJson =
            (json['ticket'] as Map<String, dynamic>?) ?? {};
        final bool isDuplicate = (json['duplicate'] as bool?) ?? false;

        return TicketSubmitResult(
          ticket: ticketJson,
          isDuplicate: isDuplicate,
        );
      } else if (response.statusCode == 400) {
        throw TicketSubmitException(
          'Invalid report data. Please check all fields and try again.',
          statusCode: 400,
        );
      } else if (response.statusCode == 401) {
        throw TicketSubmitException(
          'Your session has expired. Please sign in again.',
          statusCode: 401,
        );
      } else {
        throw TicketSubmitException(
          'Failed to submit report (error ${response.statusCode}). Please try again.',
          statusCode: response.statusCode,
        );
      }
    } on TicketSubmitException {
      rethrow;
    } on http.ClientException catch (e) {
      throw TicketSubmitException(
        'Network error. Please check your connection and try again: ${e.message}',
      );
    } catch (e) {
      throw TicketSubmitException(
        'An unexpected error occurred while submitting: $e',
      );
    }
  }

  /// Upvotes an existing ticket via POST /tickets/[ticketId]/upvote.
  ///
  /// Returns the updated upvote count on success (HTTP 200).
  ///
  /// Throws [UpvoteException] on:
  ///   - HTTP 409: already upvoted or ticket is Archived.
  ///   - HTTP 401: unauthenticated.
  ///   - HTTP 404: ticket not found.
  ///   - Network errors.
  Future<int> upvoteTicket(String ticketId) async {
    try {
      final String? idToken = await _auth.currentUser?.getIdToken();

      final http.Response response = await _httpClient.post(
        Uri.parse('$_backendBaseUrl/tickets/$ticketId/upvote'),
        headers: {
          'Content-Type': 'application/json',
          if (idToken != null) 'Authorization': 'Bearer $idToken',
        },
      );

      if (response.statusCode == 200) {
        final Map<String, dynamic> json =
            jsonDecode(response.body) as Map<String, dynamic>;
        return (json['upvotes'] as int?) ?? 0;
      } else if (response.statusCode == 409) {
        throw UpvoteException(
          'You have already upvoted this issue.',
          statusCode: 409,
        );
      } else if (response.statusCode == 401) {
        throw UpvoteException(
          'Your session has expired. Please sign in again.',
          statusCode: 401,
        );
      } else if (response.statusCode == 404) {
        throw UpvoteException(
          'Issue not found.',
          statusCode: 404,
        );
      } else {
        throw UpvoteException(
          'Failed to upvote (error ${response.statusCode}). Please try again.',
          statusCode: response.statusCode,
        );
      }
    } on UpvoteException {
      rethrow;
    } on http.ClientException catch (e) {
      throw UpvoteException(
        'Network error while upvoting: ${e.message}',
      );
    } catch (e) {
      throw UpvoteException(
        'An unexpected error occurred while upvoting: $e',
      );
    }
  }
}
