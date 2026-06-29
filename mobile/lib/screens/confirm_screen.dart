// AI confirmation screen — review and confirm AI-generated triage results
// before submitting as a civic issue ticket.
//
// This screen is pushed with named route '/triage-confirm' and expects
// arguments: { 'triageResult': TriageResult?, 'image': XFile, 'location':
// Position, 'imageUrl': String }.
//
// When triageResult is null (AI error path), the screen renders in fallback
// manual-entry mode with an amber warning banner (Req 3.6).
//
// On submission, POST /tickets is called. A 201 response navigates to
// /citizen-feed. A 200 (duplicate) prompts the citizen to upvote the existing
// ticket (Req 4.3).
//
// Requirements: 3.3, 3.4, 3.6, 4.3

import 'dart:io';

import 'package:firebase_auth/firebase_auth.dart';
import 'package:flutter/material.dart';
import 'package:geolocator/geolocator.dart';
import 'package:image_picker/image_picker.dart';

import '../services/ticket_service.dart';
import '../services/triage_service.dart';

/// Allowed civic issue categories (Req 3.7).
const List<String> _kCategories = [
  'Pothole',
  'Water Clogging',
  'Drain Overflow',
  'Electrical Hazard',
  'Street Light Out',
  'Garbage Dumping',
  'Broken Road',
  'Tree Fallen',
  'Sewage Overflow',
  'Other',
];

/// Review & Confirm screen shown after successful AI triage or as a fallback
/// manual-entry form when triage is unavailable (Req 3.6).
///
/// Route: `/triage-confirm`
/// Arguments:
///   - `triageResult` (TriageResult?): AI result; null triggers fallback mode.
///   - `image` (XFile): captured photo for preview.
///   - `location` (Position): GPS coordinates; displayed read-only.
///   - `imageUrl` (String): Firebase Storage URL for the uploaded image.
class ConfirmScreen extends StatefulWidget {
  const ConfirmScreen({super.key});

  @override
  State<ConfirmScreen> createState() => _ConfirmScreenState();
}

class _ConfirmScreenState extends State<ConfirmScreen> {
  // ── Route arguments ───────────────────────────────────────────────────────

  TriageResult? _triageResult;
  XFile? _image;
  Position? _location;
  String? _imageUrl;

  /// True when this screen is operating in fallback manual-entry mode (Req 3.6).
  bool get _isFallback => _triageResult == null;

  // ── Form state ────────────────────────────────────────────────────────────

  String? _selectedCategory;
  final TextEditingController _titleController = TextEditingController();
  final TextEditingController _descriptionController = TextEditingController();
  final GlobalKey<FormState> _formKey = GlobalKey<FormState>();

  // ── UI state ──────────────────────────────────────────────────────────────

  bool _isSubmitting = false;
  bool _argumentsParsed = false;

  // ── Services ──────────────────────────────────────────────────────────────

  final TicketService _ticketService = TicketService();

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (!_argumentsParsed) {
      _parseArguments();
      _argumentsParsed = true;
    }
  }

  void _parseArguments() {
    final args =
        ModalRoute.of(context)?.settings.arguments as Map<String, dynamic>?;

    if (args == null) return;

    _triageResult = args['triageResult'] as TriageResult?;
    _image = args['image'] as XFile?;
    _location = args['location'] as Position?;
    _imageUrl = args['imageUrl'] as String?;

    // Pre-fill fields from triage result (Req 3.3, 3.4).
    if (_triageResult != null) {
      final category = _triageResult!.category;
      _selectedCategory =
          _kCategories.contains(category) ? category : _kCategories.first;
      _titleController.text = _triageResult!.title;
      _descriptionController.text = _triageResult!.description;
    }
  }

  @override
  void dispose() {
    _titleController.dispose();
    _descriptionController.dispose();
    super.dispose();
  }

  // ── Submission ────────────────────────────────────────────────────────────

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) return;

    final location = _location;
    final imageUrl = _imageUrl ?? '';

    if (location == null) {
      _showErrorSnackBar('GPS location is unavailable. Please go back and try again.');
      return;
    }

    if (mounted) {
      setState(() => _isSubmitting = true);
    }

    try {
      final result = await _ticketService.submitTicket(
        category: _selectedCategory!,
        title: _titleController.text.trim(),
        description: _descriptionController.text.trim(),
        imageUrl: imageUrl,
        latitude: location.latitude,
        longitude: location.longitude,
      );

      if (!mounted) return;

      if (result.isDuplicate) {
        // Check if the duplicate was reported by the current user.
        final String? currentUid = FirebaseAuth.instance.currentUser?.uid;
        final String reportedBy = (result.ticket['reported_by'] as String?) ?? '';

        if (reportedBy.isNotEmpty && reportedBy == currentUid) {
          // User is trying to re-report their own issue — show a dialog.
          if (!mounted) return;
          await showDialog<void>(
            context: context,
            barrierDismissible: false,
            builder: (ctx) => AlertDialog(
              icon: const Icon(Icons.info_outline, color: Color(0xFF1A73E8), size: 40),
              title: const Text('Already Reported'),
              content: const Text(
                'You have already reported this issue. It has been logged and is being tracked.',
                textAlign: TextAlign.center,
              ),
              actionsAlignment: MainAxisAlignment.center,
              actions: [
                ElevatedButton(
                  onPressed: () => Navigator.of(ctx).pop(),
                  style: ElevatedButton.styleFrom(
                    backgroundColor: const Color(0xFF1A73E8),
                    foregroundColor: Colors.white,
                    minimumSize: const Size(120, 44),
                  ),
                  child: const Text('Okay'),
                ),
              ],
            ),
          );
          if (mounted) _navigateToFeed();
        } else {
          // Req 4.3: duplicate from another user — prompt to upvote.
          await _showDuplicateDialog(result.ticket);
        }
      } else {
        // HTTP 201: new ticket created successfully.
        _navigateToFeedWithSuccess('Issue reported successfully!');
      }
    } on TicketSubmitException catch (e) {
      if (!mounted) return;
      _showErrorSnackBar(e.message);
    } catch (e) {
      if (!mounted) return;
      _showErrorSnackBar('An unexpected error occurred. Please try again.');
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  /// Shows the duplicate-found dialog and handles the Upvote / Dismiss choice.
  Future<void> _showDuplicateDialog(Map<String, dynamic> ticket) async {
    final String ticketId = (ticket['id'] as String?) ?? '';

    final bool? shouldUpvote = await showDialog<bool>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => AlertDialog(
        title: const Text('Similar Issue Found'),
        content: const Text(
          'A similar issue already exists nearby. Would you like to upvote it to increase its visibility?',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text(
              'Dismiss',
              style: TextStyle(color: Color(0xFF5F6368)),
            ),
          ),
          ElevatedButton(
            onPressed: () => Navigator.pop(ctx, true),
            style: ElevatedButton.styleFrom(
              backgroundColor: const Color(0xFF1A73E8),
              foregroundColor: Colors.white,
            ),
            child: const Text('Upvote'),
          ),
        ],
      ),
    );

    if (!mounted) return;

    if (shouldUpvote == true && ticketId.isNotEmpty) {
      await _upvoteAndNavigate(ticketId);
    } else {
      _navigateToFeed();
    }
  }

  /// Calls POST /tickets/:id/upvote then navigates to the feed.
  Future<void> _upvoteAndNavigate(String ticketId) async {
    try {
      await _ticketService.upvoteTicket(ticketId);
      if (!mounted) return;
      _navigateToFeedWithSuccess('Issue upvoted successfully!');
    } on UpvoteException catch (e) {
      if (!mounted) return;
      if (e.statusCode == 409) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('You have already upvoted this issue.'),
          ),
        );
      } else {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(e.message)),
        );
      }
      _navigateToFeed();
    } catch (_) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Failed to upvote. Please try again.')),
      );
      _navigateToFeed();
    }
  }

  // ── Navigation helpers ────────────────────────────────────────────────────

  void _navigateToFeedWithSuccess(String message) {
    Navigator.pushNamedAndRemoveUntil(
      context,
      '/citizen-feed',
      (route) => false,
    );
    // Show snackbar after navigation settles.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) return; // screen is gone — handled by the new route
    });
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message)),
    );
  }

  void _navigateToFeed() {
    Navigator.pushNamedAndRemoveUntil(
      context,
      '/citizen-feed',
      (route) => false,
    );
  }

  void _showErrorSnackBar(String message) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(message),
        action: SnackBarAction(
          label: 'Retry',
          onPressed: _submit,
        ),
        duration: const Duration(seconds: 6),
      ),
    );
  }

  // ── Build ─────────────────────────────────────────────────────────────────

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFFF8F9FA),
      appBar: AppBar(
        title: Text(_isFallback ? 'Report Issue' : 'Review & Confirm'),
        backgroundColor: const Color(0xFF1A73E8),
        foregroundColor: Colors.white,
        elevation: 0,
      ),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(20.0),
          child: Form(
            key: _formKey,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // ── Fallback banner (Req 3.6) ───────────────────────────
                if (_isFallback) ...[
                  _FallbackBanner(),
                  const SizedBox(height: 16),
                ],

                // ── Image preview ───────────────────────────────────────
                if (_image != null) ...[
                  _ImagePreviewCard(image: _image!),
                  const SizedBox(height: 20),
                ],

                // ── GPS coordinates (read-only) ─────────────────────────
                if (_location != null) ...[
                  _LocationCard(location: _location!),
                  const SizedBox(height: 20),
                ],

                // ── Issue details form ──────────────────────────────────
                Card(
                  elevation: 2,
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const Text(
                          'Issue Details',
                          style: TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w600,
                            color: Color(0xFF202124),
                          ),
                        ),
                        const SizedBox(height: 16),

                        // ── Category dropdown (Req 3.3) ─────────────────
                        _CategoryDropdown(
                          value: _selectedCategory,
                          onChanged: _isSubmitting
                              ? null
                              : (val) {
                                  if (mounted) {
                                    setState(() => _selectedCategory = val);
                                  }
                                },
                        ),
                        const SizedBox(height: 16),

                        // ── Title field (Req 3.4, max 100 chars) ────────
                        _TitleField(
                          controller: _titleController,
                          enabled: !_isSubmitting,
                        ),
                        const SizedBox(height: 16),

                        // ── Description field (Req 3.4, max 500 chars) ──
                        _DescriptionField(
                          controller: _descriptionController,
                          enabled: !_isSubmitting,
                        ),
                      ],
                    ),
                  ),
                ),

                const SizedBox(height: 28),

                // ── Submit button / loading indicator ───────────────────
                if (_isSubmitting)
                  const Column(
                    children: [
                      CircularProgressIndicator(color: Color(0xFF1A73E8)),
                      SizedBox(height: 12),
                      Text(
                        'Submitting your report…',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Color(0xFF5F6368)),
                      ),
                    ],
                  )
                else
                  ElevatedButton.icon(
                    onPressed: _submit,
                    icon: const Icon(Icons.send_outlined),
                    label: const Text('Submit Report'),
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

                const SizedBox(height: 16),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

// ── Private helper widgets ────────────────────────────────────────────────────

/// Amber warning banner shown in fallback manual-entry mode (Req 3.6).
class _FallbackBanner extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.amber.shade50,
        border: Border.all(color: Colors.amber.shade300),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.warning_amber_rounded,
              color: Colors.amber.shade800, size: 22),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              'AI classification unavailable — please fill in the details manually.',
              style: TextStyle(
                color: Colors.amber.shade900,
                fontSize: 13,
                height: 1.4,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Small non-editable image thumbnail preview.
class _ImagePreviewCard extends StatelessWidget {
  final XFile image;

  const _ImagePreviewCard({required this.image});

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Captured Photo',
              style: TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w600,
                color: Color(0xFF202124),
              ),
            ),
            const SizedBox(height: 10),
            ClipRRect(
              borderRadius: BorderRadius.circular(8),
              child: Image.file(
                File(image.path),
                height: 160,
                width: double.infinity,
                fit: BoxFit.cover,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Non-editable GPS coordinates card.
class _LocationCard extends StatelessWidget {
  final Position location;

  const _LocationCard({required this.location});

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
            Row(
              children: [
                const Icon(Icons.gps_fixed,
                    color: Color(0xFF1A73E8), size: 18),
                const SizedBox(width: 8),
                const Text(
                  'GPS Location',
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                    color: Color(0xFF202124),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            _CoordRow(
              label: 'Latitude',
              value: location.latitude.toStringAsFixed(6),
            ),
            const SizedBox(height: 4),
            _CoordRow(
              label: 'Longitude',
              value: location.longitude.toStringAsFixed(6),
            ),
            const SizedBox(height: 4),
            _CoordRow(
              label: 'Accuracy',
              value: '±${location.accuracy.toStringAsFixed(0)} m',
            ),
          ],
        ),
      ),
    );
  }
}

/// Label/value pair row used inside the location card.
class _CoordRow extends StatelessWidget {
  final String label;
  final String value;

  const _CoordRow({required this.label, required this.value});

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

/// Category dropdown — shows the 5 allowed categories (Req 3.3).
class _CategoryDropdown extends StatelessWidget {
  final String? value;
  final ValueChanged<String?>? onChanged;

  const _CategoryDropdown({
    required this.value,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    return DropdownButtonFormField<String>(
      value: value,
      decoration: const InputDecoration(
        labelText: 'Category',
        border: OutlineInputBorder(),
        contentPadding: EdgeInsets.symmetric(horizontal: 12, vertical: 12),
      ),
      hint: const Text('Select a category'),
      items: _kCategories
          .map(
            (cat) => DropdownMenuItem(
              value: cat,
              child: Text(cat),
            ),
          )
          .toList(),
      onChanged: onChanged,
      validator: (val) =>
          (val == null || val.isEmpty) ? 'Please select a category.' : null,
    );
  }
}

/// Title text field — max 100 characters, shows counter (Req 3.4).
class _TitleField extends StatelessWidget {
  final TextEditingController controller;
  final bool enabled;

  const _TitleField({
    required this.controller,
    required this.enabled,
  });

  @override
  Widget build(BuildContext context) {
    return TextFormField(
      controller: controller,
      enabled: enabled,
      maxLength: 100,
      textCapitalization: TextCapitalization.sentences,
      decoration: const InputDecoration(
        labelText: 'Title',
        border: OutlineInputBorder(),
        contentPadding: EdgeInsets.symmetric(horizontal: 12, vertical: 12),
        helperText: 'Brief summary of the issue',
      ),
      validator: (val) {
        if (val == null || val.trim().isEmpty) {
          return 'Please enter a title.';
        }
        return null;
      },
    );
  }
}

/// Description text field — max 500 characters, shows counter (Req 3.4).
class _DescriptionField extends StatelessWidget {
  final TextEditingController controller;
  final bool enabled;

  const _DescriptionField({
    required this.controller,
    required this.enabled,
  });

  @override
  Widget build(BuildContext context) {
    return TextFormField(
      controller: controller,
      enabled: enabled,
      maxLength: 500,
      maxLines: 4,
      textCapitalization: TextCapitalization.sentences,
      decoration: const InputDecoration(
        labelText: 'Description',
        border: OutlineInputBorder(),
        contentPadding: EdgeInsets.symmetric(horizontal: 12, vertical: 12),
        helperText: 'Describe the issue in detail',
        alignLabelWithHint: true,
      ),
      validator: (val) {
        if (val == null || val.trim().isEmpty) {
          return 'Please enter a description.';
        }
        return null;
      },
    );
  }
}
