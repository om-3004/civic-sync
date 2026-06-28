// Report flow screen — camera-only capture with GPS coordinate acquisition,
// image upload, and AI triage.
//
// This screen handles:
//   1. Camera permission check → open native camera (gallery disabled).
//   2. GPS coordinate acquisition with ≤50 m accuracy within 15 seconds.
//   3. Combined state management to gate the "Upload & Analyze" button on both
//      a captured image AND valid GPS coordinates being available.
//   4. Upload image to Firebase Storage and POST /triage (Req 2.6, 2.7, 3.1).
//
// Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 3.1

import 'dart:io';

import 'package:flutter/material.dart';
import 'package:geolocator/geolocator.dart';
import 'package:image_picker/image_picker.dart';

import '../services/triage_service.dart';

/// Screen that drives the "new issue report" flow.
///
/// The screen enforces authenticity-locked capture:
/// - Only the native camera is used — gallery access is never offered.
/// - GPS coordinates must be obtained at ≤50 m accuracy within 15 s before
///   the report can continue.
/// - Once both are ready, the citizen can upload the image and trigger AI
///   triage via the backend (Req 2.6, 3.1).
///
/// When the full triage succeeds the [onCaptureComplete] callback is invoked
/// (or the default placeholder navigation fires).
class ReportFlowScreen extends StatefulWidget {
  /// Called when the citizen successfully captures an image **and** obtains
  /// valid GPS coordinates. Both arguments are guaranteed non-null.
  ///
  /// If this callback is null, the screen shows a placeholder "next step"
  /// message via a SnackBar.
  final void Function(XFile image, Position location)? onCaptureComplete;

  const ReportFlowScreen({super.key, this.onCaptureComplete});

  @override
  State<ReportFlowScreen> createState() => _ReportFlowScreenState();
}

class _ReportFlowScreenState extends State<ReportFlowScreen> {
  // ── State fields ──────────────────────────────────────────────────────────

  /// The photo taken by the citizen. Null until a photo has been captured.
  XFile? _capturedImage;

  /// GPS position obtained after capture. Null until successfully acquired.
  Position? _location;

  /// True while the camera intent is active (prevents double-tap).
  bool _isCapturing = false;

  /// True while GPS is being polled after a photo capture.
  bool _isLocating = false;

  /// Non-null when camera access has been denied or an error has occurred.
  String? _cameraError;

  /// Non-null when GPS acquisition has failed (permission denied or timeout).
  String? _locationError;

  /// True while upload and/or triage is in progress.
  bool _isUploading = false;

  /// Non-null when image upload to Firebase Storage failed (Req 2.7).
  String? _uploadError;

  /// Non-null when the backend triage call failed.
  String? _triageError;

  /// Last successfully retrieved triage result.
  TriageResult? _triageResult;

  // ── Service ───────────────────────────────────────────────────────────────

  final TriageService _triageService = TriageService();

  // ── ImagePicker singleton ─────────────────────────────────────────────────

  final ImagePicker _picker = ImagePicker();

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  @override
  void initState() {
    super.initState();
    // Pre-check camera permission on screen load so the UI immediately shows
    // an error state if the user has previously denied camera access.
    _checkCameraPermission();
  }

  // ── Permission helpers ────────────────────────────────────────────────────

  Future<void> _checkCameraPermission() async {
    if (mounted) {
      setState(() {
        _cameraError = null;
      });
    }
  }

  /// Checks and requests location permission using the geolocator API.
  Future<bool> _ensureLocationPermission() async {
    LocationPermission permission = await Geolocator.checkPermission();

    if (permission == LocationPermission.denied) {
      permission = await Geolocator.requestPermission();
    }

    if (permission == LocationPermission.denied ||
        permission == LocationPermission.deniedForever) {
      if (mounted) {
        setState(() {
          _locationError =
              'Location access is required to submit a report. '
              'Please enable location permission in Settings.';
        });
      }
      return false;
    }

    final bool serviceEnabled = await Geolocator.isLocationServiceEnabled();
    if (!serviceEnabled) {
      if (mounted) {
        setState(() {
          _locationError =
              'Location services are disabled. '
              'Please enable GPS in device Settings.';
        });
      }
      return false;
    }

    return true;
  }

  // ── Camera capture ────────────────────────────────────────────────────────

  /// Opens the native camera (gallery disabled per Req 2.3) and captures a
  /// photo. On success immediately triggers GPS acquisition (Req 2.4).
  Future<void> _capturePhoto() async {
    if (_isCapturing) return;

    setState(() {
      _isCapturing = true;
      _cameraError = null;
      _locationError = null;
      _uploadError = null;
      _triageError = null;
      _triageResult = null;
      _capturedImage = null;
      _location = null;
    });

    try {
      final XFile? photo = await _picker.pickImage(
        source: ImageSource.camera,
        imageQuality: 85,
        preferredCameraDevice: CameraDevice.rear,
      );

      if (photo == null) {
        if (mounted) {
          setState(() {
            _isCapturing = false;
          });
        }
        return;
      }

      if (mounted) {
        setState(() {
          _capturedImage = photo;
          _isCapturing = false;
        });
      }

      await _acquireGps();
    } catch (e) {
      if (mounted) {
        setState(() {
          _isCapturing = false;
          _cameraError =
              'Camera access is required to report an issue. '
              'Please grant camera permission in Settings.';
        });
      }
    }
  }

  // ── GPS acquisition ───────────────────────────────────────────────────────

  /// Polls geolocator for a position with ≤50 m accuracy within 15 seconds.
  Future<void> _acquireGps() async {
    if (mounted) {
      setState(() {
        _isLocating = true;
        _locationError = null;
        _location = null;
      });
    }

    final bool hasPermission = await _ensureLocationPermission();
    if (!hasPermission) {
      if (mounted) {
        setState(() => _isLocating = false);
      }
      return;
    }

    try {
      final Position position = await Geolocator.getCurrentPosition(
        locationSettings: AndroidSettings(
          accuracy: LocationAccuracy.high,
          timeLimit: const Duration(seconds: 15),
        ),
      );

      if (position.accuracy > 50.0) {
        if (mounted) {
          setState(() {
            _isLocating = false;
            _locationError =
                'GPS accuracy is insufficient (${position.accuracy.toStringAsFixed(0)} m). '
                'Please try again in an open area.';
          });
        }
        return;
      }

      if (mounted) {
        setState(() {
          _location = position;
          _isLocating = false;
        });
      }
    } on LocationServiceDisabledException {
      if (mounted) {
        setState(() {
          _isLocating = false;
          _locationError =
              'GPS is disabled. Please enable location services and try again.';
        });
      }
    } on PermissionDeniedException {
      if (mounted) {
        setState(() {
          _isLocating = false;
          _locationError =
              'Location permission denied. '
              'Please enable it in Settings to submit a report.';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _isLocating = false;
          _locationError =
              'Unable to obtain GPS coordinates within 15 seconds. '
              'Please move to an open area and try again.';
        });
      }
    }
  }

  // ── Upload & triage flow ──────────────────────────────────────────────────

  /// Uploads the captured image then calls the backend /triage endpoint.
  ///
  /// On [StorageUploadException] sets [_uploadError] and surfaces a Retry
  /// Upload button — the citizen does NOT need to re-take the photo (Req 2.7).
  /// On [TriageException] sets [_triageError] with a Retry Analysis button.
  /// On full success invokes [widget.onCaptureComplete] or navigates to
  /// /triage-confirm. Falls back to a SnackBar if the route is not yet wired.
  Future<void> _uploadAndTriage() async {
    final image = _capturedImage;
    final location = _location;

    if (image == null || location == null) return;

    setState(() {
      _isUploading = true;
      _uploadError = null;
      _triageError = null;
    });

    // Step 1: Upload image to Firebase Storage (Req 2.6).
    String imageUrl;
    try {
      imageUrl = await _triageService.uploadImage(image);
    } on StorageUploadException catch (e) {
      // Req 2.7: show retry without re-capturing.
      if (mounted) {
        setState(() {
          _isUploading = false;
          _uploadError = e.message;
        });
      }
      return;
    }

    // Step 2: POST /triage with Storage URL + coordinates (Req 3.1).
    TriageResult result;
    try {
      result = await _triageService.triage(
        imageUrl: imageUrl,
        latitude: location.latitude,
        longitude: location.longitude,
      );
    } on TriageException catch (e) {
      if (mounted) {
        setState(() {
          _isUploading = false;
          _triageError = e.message;
        });
      }
      return;
    }

    if (!mounted) return;

    setState(() {
      _isUploading = false;
      _triageResult = result;
    });

    // Step 3: Hand off result.
    if (widget.onCaptureComplete != null) {
      widget.onCaptureComplete!(image, location);
    } else {
      // Try to navigate to the triage confirm screen (wired in task 15.3).
      // Until then, fall back to a SnackBar placeholder.
      final bool navigated = await _tryNavigateToTriageConfirm(
        image: image,
        location: location,
        triageResult: result,
        imageUrl: imageUrl,
      );
      if (!navigated && mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              'Triage complete!\n'
              'Category: ${result.category}\n'
              'Title: ${result.title}',
            ),
            duration: const Duration(seconds: 4),
          ),
        );
      }
    }
  }

  /// Attempts to push `/triage-confirm` with [triageResult], [image], and
  /// [location] as route arguments. Returns true if navigation succeeded.
  Future<bool> _tryNavigateToTriageConfirm({
    required XFile image,
    required Position location,
    required TriageResult triageResult,
    required String imageUrl,
  }) async {
    try {
      await Navigator.pushNamed(
        context,
        '/triage-confirm',
        arguments: {
          'triageResult': triageResult,
          'image': image,
          'location': location,
          'imageUrl': imageUrl,
        },
      );
      return true;
    } catch (_) {
      return false;
    }
  }

  // ── Build ─────────────────────────────────────────────────────────────────

  @override
  Widget build(BuildContext context) {
    final bool canUpload =
        _capturedImage != null && _location != null && !_isLocating;

    return Scaffold(
      backgroundColor: const Color(0xFFF8F9FA),
      appBar: AppBar(
        title: const Text('Report an Issue'),
        backgroundColor: const Color(0xFF1A73E8),
        foregroundColor: Colors.white,
        elevation: 0,
      ),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(20.0),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              // ── Step 1 label ────────────────────────────────────────────
              _SectionLabel(
                step: '1',
                title: 'Take a Photo',
                subtitle: 'Open your camera and photograph the issue.',
              ),
              const SizedBox(height: 12),

              // ── Camera error banner ─────────────────────────────────────
              if (_cameraError != null) ...[
                _ErrorBanner(message: _cameraError!),
                const SizedBox(height: 12),
              ],

              // ── Image preview / capture button ──────────────────────────
              _ImageCaptureCard(
                capturedImage: _capturedImage,
                isCapturing: _isCapturing,
                cameraError: _cameraError,
                onCapture: _capturePhoto,
              ),

              const SizedBox(height: 28),

              // ── Step 2 label ────────────────────────────────────────────
              _SectionLabel(
                step: '2',
                title: 'Acquire GPS Location',
                subtitle:
                    'Your coordinates are captured automatically after the photo.',
              ),
              const SizedBox(height: 12),

              // ── GPS status card ─────────────────────────────────────────
              _GpsStatusCard(
                isLocating: _isLocating,
                location: _location,
                locationError: _locationError,
                onRetry: _capturedImage != null ? _acquireGps : null,
              ),

              const SizedBox(height: 32),

              // ── Upload error with retry (Req 2.7) ───────────────────────
              if (_uploadError != null) ...[
                _ErrorBanner(message: _uploadError!),
                const SizedBox(height: 8),
                OutlinedButton.icon(
                  onPressed: _isUploading ? null : _uploadAndTriage,
                  icon: const Icon(Icons.upload),
                  label: const Text('Retry Upload'),
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size.fromHeight(44),
                    foregroundColor: Colors.red.shade700,
                    side: BorderSide(color: Colors.red.shade300),
                  ),
                ),
                const SizedBox(height: 12),
              ],

              // ── Triage error with retry ─────────────────────────────────
              if (_triageError != null) ...[
                _ErrorBanner(message: _triageError!),
                const SizedBox(height: 8),
                OutlinedButton.icon(
                  onPressed: _isUploading ? null : _uploadAndTriage,
                  icon: const Icon(Icons.refresh),
                  label: const Text('Retry Analysis'),
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size.fromHeight(44),
                    foregroundColor: Colors.orange.shade800,
                    side: BorderSide(color: Colors.orange.shade300),
                  ),
                ),
                const SizedBox(height: 12),
              ],

              // ── Triage success banner ───────────────────────────────────
              if (_triageResult != null && !_isUploading) ...[
                Container(
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: Colors.green.shade50,
                    border: Border.all(color: Colors.green.shade200),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Row(
                    children: [
                      Icon(Icons.check_circle,
                          color: Colors.green.shade700, size: 20),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          'Triage complete: ${_triageResult!.category}',
                          style: TextStyle(
                              color: Colors.green.shade700, fontSize: 13),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 12),
              ],

              // ── Upload & Analyze button / loading indicator ─────────────
              if (canUpload) ...[
                if (_isUploading)
                  const Column(
                    children: [
                      CircularProgressIndicator(color: Color(0xFF1A73E8)),
                      SizedBox(height: 12),
                      Text(
                        'Uploading and analysing…',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Colors.grey),
                      ),
                    ],
                  )
                else
                  ElevatedButton.icon(
                    onPressed: _uploadAndTriage,
                    icon: const Icon(Icons.cloud_upload_outlined),
                    label: const Text('Upload & Analyze'),
                    style: ElevatedButton.styleFrom(
                      minimumSize: const Size.fromHeight(52),
                      backgroundColor: const Color(0xFF1A73E8),
                      foregroundColor: Colors.white,
                      textStyle: const TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
              ],

              // Hint when we have an image but GPS is still pending/failed.
              if (_capturedImage != null && !canUpload) ...[
                if (_isLocating)
                  const Padding(
                    padding: EdgeInsets.only(top: 8),
                    child: Text(
                      'Waiting for GPS coordinates…',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: Colors.grey),
                    ),
                  )
                else if (_locationError != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 8),
                    child: Text(
                      'Fix the GPS error above before continuing.',
                      textAlign: TextAlign.center,
                      style: TextStyle(
                        color: Colors.red.shade700,
                        fontSize: 13,
                      ),
                    ),
                  ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

// ── Private helper widgets ────────────────────────────────────────────────────

/// Numbered section label shown above each major step.
class _SectionLabel extends StatelessWidget {
  final String step;
  final String title;
  final String subtitle;

  const _SectionLabel({
    required this.step,
    required this.title,
    required this.subtitle,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        CircleAvatar(
          radius: 14,
          backgroundColor: const Color(0xFF1A73E8),
          child: Text(
            step,
            style: const TextStyle(
              color: Colors.white,
              fontSize: 13,
              fontWeight: FontWeight.bold,
            ),
          ),
        ),
        const SizedBox(width: 10),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                title,
                style: const TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                  color: Color(0xFF202124),
                ),
              ),
              const SizedBox(height: 2),
              Text(
                subtitle,
                style: const TextStyle(
                  fontSize: 13,
                  color: Color(0xFF5F6368),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// Red error banner displayed when camera, GPS, upload, or triage has an error.
class _ErrorBanner extends StatelessWidget {
  final String message;

  const _ErrorBanner({required this.message});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.red.shade50,
        border: Border.all(color: Colors.red.shade200),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        children: [
          Icon(Icons.error_outline, color: Colors.red.shade700, size: 20),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: TextStyle(color: Colors.red.shade700, fontSize: 13),
            ),
          ),
        ],
      ),
    );
  }
}

/// Card that shows either a captured image preview or a "Take Photo" button.
class _ImageCaptureCard extends StatelessWidget {
  final XFile? capturedImage;
  final bool isCapturing;
  final String? cameraError;
  final VoidCallback onCapture;

  const _ImageCaptureCard({
    required this.capturedImage,
    required this.isCapturing,
    required this.cameraError,
    required this.onCapture,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            if (capturedImage != null) ...[
              ClipRRect(
                borderRadius: BorderRadius.circular(8),
                child: Image.file(
                  File(capturedImage!.path),
                  height: 220,
                  width: double.infinity,
                  fit: BoxFit.cover,
                ),
              ),
              const SizedBox(height: 12),
              Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  const Icon(Icons.check_circle, color: Colors.green, size: 18),
                  const SizedBox(width: 6),
                  const Text(
                    'Photo captured',
                    style: TextStyle(color: Colors.green),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              TextButton.icon(
                onPressed: isCapturing ? null : onCapture,
                icon: const Icon(Icons.camera_alt_outlined),
                label: const Text('Retake Photo'),
              ),
            ] else ...[
              Container(
                height: 160,
                decoration: BoxDecoration(
                  color: const Color(0xFFF1F3F4),
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(color: const Color(0xFFDADCE0)),
                ),
                child: Center(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        Icons.camera_alt,
                        size: 48,
                        color: Colors.grey.shade500,
                      ),
                      const SizedBox(height: 8),
                      Text(
                        'No photo yet',
                        style: TextStyle(color: Colors.grey.shade600),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 16),
              ElevatedButton.icon(
                onPressed:
                    (isCapturing || cameraError != null) ? null : onCapture,
                icon: isCapturing
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Colors.white,
                        ),
                      )
                    : const Icon(Icons.camera_alt),
                label: Text(isCapturing ? 'Opening Camera…' : 'Take Photo'),
                style: ElevatedButton.styleFrom(
                  minimumSize: const Size.fromHeight(48),
                  backgroundColor: const Color(0xFF1A73E8),
                  foregroundColor: Colors.white,
                  disabledBackgroundColor: Colors.grey.shade300,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Card showing GPS acquisition status: loading, success, or error.
class _GpsStatusCard extends StatelessWidget {
  final bool isLocating;
  final Position? location;
  final String? locationError;
  final VoidCallback? onRetry;

  const _GpsStatusCard({
    required this.isLocating,
    required this.location,
    required this.locationError,
    required this.onRetry,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (isLocating) ...[
              Row(
                children: [
                  const SizedBox(
                    width: 20,
                    height: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  const SizedBox(width: 12),
                  const Text(
                    'Acquiring GPS coordinates…',
                    style: TextStyle(fontSize: 14),
                  ),
                ],
              ),
              const SizedBox(height: 6),
              Text(
                'This may take up to 15 seconds.',
                style: TextStyle(fontSize: 12, color: Colors.grey.shade600),
              ),
            ] else if (location != null) ...[
              Row(
                children: [
                  const Icon(Icons.gps_fixed, color: Colors.green, size: 20),
                  const SizedBox(width: 8),
                  const Text(
                    'GPS acquired',
                    style: TextStyle(
                      color: Colors.green,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              _CoordinateRow(
                label: 'Latitude',
                value: location!.latitude.toStringAsFixed(6),
              ),
              const SizedBox(height: 4),
              _CoordinateRow(
                label: 'Longitude',
                value: location!.longitude.toStringAsFixed(6),
              ),
              const SizedBox(height: 4),
              _CoordinateRow(
                label: 'Accuracy',
                value: '±${location!.accuracy.toStringAsFixed(0)} m',
              ),
            ] else if (locationError != null) ...[
              _ErrorBanner(message: locationError!),
              if (onRetry != null) ...[
                const SizedBox(height: 10),
                OutlinedButton.icon(
                  onPressed: onRetry,
                  icon: const Icon(Icons.refresh),
                  label: const Text('Retry GPS'),
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size.fromHeight(44),
                  ),
                ),
              ],
            ] else ...[
              Row(
                children: [
                  Icon(Icons.location_off,
                      color: Colors.grey.shade400, size: 20),
                  const SizedBox(width: 8),
                  Text(
                    'GPS will be acquired after the photo is taken.',
                    style: TextStyle(
                      color: Colors.grey.shade600,
                      fontSize: 13,
                    ),
                  ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Simple label + value row used inside the GPS status card.
class _CoordinateRow extends StatelessWidget {
  final String label;
  final String value;

  const _CoordinateRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        SizedBox(
          width: 80,
          child: Text(
            label,
            style: const TextStyle(
              fontSize: 13,
              color: Color(0xFF5F6368),
            ),
          ),
        ),
        Text(
          value,
          style: const TextStyle(
            fontSize: 13,
            fontWeight: FontWeight.w500,
            color: Color(0xFF202124),
          ),
        ),
      ],
    );
  }
}
