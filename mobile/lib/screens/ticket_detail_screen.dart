// Ticket detail screen — full ticket information and upvote action.
//
// Displays all ticket fields: category, title, description, image_url,
// location (map), status, upvotes, created_at, updated_at.
//
// Includes an upvote button that calls POST /tickets/:id/upvote.
// Displays a 409 conflict message when the citizen has already upvoted.
//
// Requirements: 7.3, 5.1, 5.3

import 'package:firebase_auth/firebase_auth.dart';
import 'package:flutter/material.dart';
import 'package:geocoding/geocoding.dart';
import 'package:google_maps_flutter/google_maps_flutter.dart';

import '../services/ticket_service.dart';
import 'citizen_feed_screen.dart' show Ticket;

// ── TicketDetailScreen ────────────────────────────────────────────────────────

/// Full detail view for a single civic issue ticket (Req 7.3).
///
/// The [ticket] argument carries the ticket data loaded from the feed.
class TicketDetailScreen extends StatefulWidget {
  final Ticket ticket;

  const TicketDetailScreen({super.key, required this.ticket});

  @override
  State<TicketDetailScreen> createState() => _TicketDetailScreenState();
}

class _TicketDetailScreenState extends State<TicketDetailScreen> {
  // ── Upvote state ──────────────────────────────────────────────────────────

  bool _isUpvoting = false;
  String? _upvoteMessage;
  bool _upvoteSuccess = false;

  /// Current displayed upvote count (may be incremented on success).
  late int _upvotes;

  // ── Service ───────────────────────────────────────────────────────────────

  final TicketService _ticketService = TicketService();

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  @override
  void initState() {
    super.initState();
    _upvotes = widget.ticket.upvotes;
  }

  // ── Upvote action (Req 5.1, 5.3) ─────────────────────────────────────────

  Future<void> _upvote() async {
    if (_isUpvoting) return;

    setState(() {
      _isUpvoting = true;
      _upvoteMessage = null;
    });

    try {
      final int newCount = await _ticketService.upvoteTicket(widget.ticket.id);
      if (mounted) {
        setState(() {
          _isUpvoting = false;
          _upvotes = newCount;
          _upvoteSuccess = true;
          _upvoteMessage = 'Upvoted! This issue now has $_upvotes vote(s).';
        });
      }
    } on UpvoteException catch (e) {
      if (!mounted) return;
      if (e.statusCode == 409) {
        // Req 5.3: duplicate upvote — show 409 conflict message.
        setState(() {
          _isUpvoting = false;
          _upvoteSuccess = false;
          _upvoteMessage = 'You have already upvoted this issue.';
        });
      } else {
        setState(() {
          _isUpvoting = false;
          _upvoteSuccess = false;
          _upvoteMessage = e.message;
        });
      }
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _isUpvoting = false;
        _upvoteSuccess = false;
        _upvoteMessage = 'Failed to upvote. Please try again.';
      });
    }
  }

  // ── Build ─────────────────────────────────────────────────────────────────

  @override
  Widget build(BuildContext context) {
    final ticket = widget.ticket;
    final bool isResolved = ticket.status == 'Done';

    return Scaffold(
      backgroundColor: const Color(0xFFF8F9FA),
      appBar: AppBar(
        title: const Text('Issue Details'),
        backgroundColor: const Color(0xFF1A73E8),
        foregroundColor: Colors.white,
        elevation: 0,
      ),
      body: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // ── Image ───────────────────────────────────────────────────────
            if (ticket.imageUrl.isNotEmpty)
              _TicketImage(imageUrl: ticket.imageUrl),

            Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // ── Category + Status row ─────────────────────────────────
                  Row(
                    children: [
                      _CategoryChip(category: ticket.category),
                      const SizedBox(width: 8),
                      if (isResolved)
                        _ResolvedBadge()
                      else
                        _StatusBadge(status: ticket.status),
                    ],
                  ),
                  const SizedBox(height: 12),

                  // ── Title ─────────────────────────────────────────────────
                  Text(
                    ticket.title,
                    style: const TextStyle(
                      fontSize: 20,
                      fontWeight: FontWeight.bold,
                      color: Color(0xFF202124),
                    ),
                  ),
                  const SizedBox(height: 8),

                  // ── Description ───────────────────────────────────────────
                  if (ticket.description.isNotEmpty) ...[
                    Text(
                      ticket.description,
                      style: const TextStyle(
                        fontSize: 14,
                        color: Color(0xFF5F6368),
                        height: 1.5,
                      ),
                    ),
                    const SizedBox(height: 16),
                  ],

                  // ── Upvote card ───────────────────────────────────────────
                  if (widget.ticket.reportedBy != FirebaseAuth.instance.currentUser?.uid)
                    _UpvoteCard(
                      upvotes: _upvotes,
                      isUpvoting: _isUpvoting,
                      message: _upvoteMessage,
                      isSuccess: _upvoteSuccess,
                      onUpvote: _upvote,
                    )
                  else
                    _UpvoteCard(
                      upvotes: _upvotes,
                      isUpvoting: false,
                      message: null,
                      isSuccess: false,
                      onUpvote: null,
                    ),

                  const SizedBox(height: 16),

                  // ── Metadata card ─────────────────────────────────────────
                  _MetadataCard(ticket: ticket),

                  const SizedBox(height: 16),

                  // ── Location map ──────────────────────────────────────────
                  if (ticket.latitude != 0.0 || ticket.longitude != 0.0) ...[
                    _LocationMapCard(
                      latitude: ticket.latitude,
                      longitude: ticket.longitude,
                      title: ticket.title,
                    ),
                    const SizedBox(height: 16),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ── Private widgets ───────────────────────────────────────────────────────────

/// Full-width image at top of detail (Req 7.3).
class _TicketImage extends StatelessWidget {
  final String imageUrl;

  const _TicketImage({required this.imageUrl});

  @override
  Widget build(BuildContext context) {
    return AspectRatio(
      aspectRatio: 16 / 9,
      child: Image.network(
        imageUrl,
        fit: BoxFit.cover,
        errorBuilder: (context, error, stackTrace) => Container(
          color: const Color(0xFFF1F3F4),
          child: const Center(
            child: Icon(
              Icons.broken_image_outlined,
              size: 48,
              color: Color(0xFF5F6368),
            ),
          ),
        ),
        loadingBuilder: (context, child, progress) {
          if (progress == null) return child;
          return Container(
            color: const Color(0xFFF1F3F4),
            child: const Center(
              child: CircularProgressIndicator(color: Color(0xFF1A73E8)),
            ),
          );
        },
      ),
    );
  }
}

/// Upvote count display and upvote button (Req 5.1, 5.3).
class _UpvoteCard extends StatelessWidget {
  final int upvotes;
  final bool isUpvoting;
  final String? message;
  final bool isSuccess;
  final VoidCallback? onUpvote;

  const _UpvoteCard({
    required this.upvotes,
    required this.isUpvoting,
    required this.message,
    required this.isSuccess,
    required this.onUpvote,
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
            Row(
              children: [
                // Upvote count display.
                const Icon(
                  Icons.thumb_up_alt_outlined,
                  color: Color(0xFF1A73E8),
                  size: 22,
                ),
                const SizedBox(width: 8),
                Text(
                  '$upvotes upvote${upvotes == 1 ? '' : 's'}',
                  style: const TextStyle(
                    fontSize: 16,
                    fontWeight: FontWeight.w600,
                    color: Color(0xFF202124),
                  ),
                ),
                const Spacer(),
                // Upvote button.
                if (isUpvoting)
                  const SizedBox(
                    width: 24,
                    height: 24,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: Color(0xFF1A73E8),
                    ),
                  )
                else if (onUpvote == null)
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                    decoration: BoxDecoration(
                      color: const Color(0xFFE8F0FE),
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: const Text(
                      'Your issue',
                      style: TextStyle(
                        fontSize: 12,
                        color: Color(0xFF1A73E8),
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  )
                else
                  ElevatedButton.icon(
                    onPressed: onUpvote,
                    icon: const Icon(Icons.thumb_up_alt, size: 18),
                    label: const Text('Upvote'),
                    style: ElevatedButton.styleFrom(
                      backgroundColor: const Color(0xFF1A73E8),
                      foregroundColor: Colors.white,
                      padding: const EdgeInsets.symmetric(
                          horizontal: 14, vertical: 8),
                      textStyle: const TextStyle(
                          fontSize: 13, fontWeight: FontWeight.w600),
                    ),
                  ),
              ],
            ),

            // ── Upvote feedback message ─────────────────────────────────
            if (message != null && message!.isNotEmpty) ...[
              const SizedBox(height: 10),
              Container(
                padding:
                    const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                decoration: BoxDecoration(
                  color: isSuccess
                      ? Colors.green.shade50
                      : Colors.orange.shade50,
                  border: Border.all(
                    color: isSuccess
                        ? Colors.green.shade300
                        : Colors.orange.shade300,
                  ),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Row(
                  children: [
                    Icon(
                      isSuccess
                          ? Icons.check_circle_outline
                          : Icons.info_outline,
                      size: 16,
                      color: isSuccess
                          ? Colors.green.shade700
                          : Colors.orange.shade800,
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        message!,
                        style: TextStyle(
                          fontSize: 13,
                          color: isSuccess
                              ? Colors.green.shade700
                              : Colors.orange.shade800,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Read-only metadata card showing all ticket timestamps and reporter info.
class _MetadataCard extends StatefulWidget {
  final Ticket ticket;

  const _MetadataCard({required this.ticket});

  @override
  State<_MetadataCard> createState() => _MetadataCardState();
}

class _MetadataCardState extends State<_MetadataCard> {
  String? _areaName;

  @override
  void initState() {
    super.initState();
    _reverseGeocode();
  }

  Future<void> _reverseGeocode() async {
    final lat = widget.ticket.latitude;
    final lng = widget.ticket.longitude;
    if (lat == 0.0 && lng == 0.0) return;
    try {
      final placemarks = await placemarkFromCoordinates(lat, lng);
      if (placemarks.isNotEmpty && mounted) {
        final p = placemarks.first;
        final parts = <String>[
          if (p.subLocality?.isNotEmpty == true) p.subLocality!,
          if (p.locality?.isNotEmpty == true) p.locality!,
          if (p.administrativeArea?.isNotEmpty == true) p.administrativeArea!,
        ];
        setState(() => _areaName = parts.isNotEmpty ? parts.join(', ') : null);
      }
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    final ticket = widget.ticket;
    return Card(
      elevation: 2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Details',
              style: TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w600,
                color: Color(0xFF202124),
              ),
            ),
            const SizedBox(height: 12),
            _MetaRow(
              label: 'Reported',
              value: _formatDateTime(ticket.createdAt),
            ),
            const SizedBox(height: 6),
            _MetaRow(
              label: 'Updated',
              value: _formatDateTime(ticket.updatedAt),
            ),
            if (ticket.resolvedAt != null) ...[
              const SizedBox(height: 6),
              _MetaRow(
                label: 'Resolved',
                value: _formatDateTime(ticket.resolvedAt!),
              ),
            ],
            const SizedBox(height: 6),
            _MetaRow(label: 'Status', value: ticket.status),
            const SizedBox(height: 6),
            _MetaRow(
              label: 'Location',
              value: _areaName ??
                  '${ticket.latitude.toStringAsFixed(5)}, '
                  '${ticket.longitude.toStringAsFixed(5)}',
            ),
          ],
        ),
      ),
    );
  }

  String _formatDateTime(DateTime dt) {
    String pad(int n) => n.toString().padLeft(2, '0');
    return '${dt.day}/${dt.month}/${dt.year} '
        '${pad(dt.hour)}:${pad(dt.minute)}';
  }
}

class _MetaRow extends StatelessWidget {
  final String label;
  final String value;

  const _MetaRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 90,
          child: Text(
            label,
            style: const TextStyle(
              fontSize: 13,
              color: Color(0xFF5F6368),
            ),
          ),
        ),
        Expanded(
          child: Text(
            value,
            style: const TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w500,
              color: Color(0xFF202124),
            ),
          ),
        ),
      ],
    );
  }
}

/// Small embedded GoogleMap pinning the ticket's exact location (Req 7.3).
class _LocationMapCard extends StatelessWidget {
  final double latitude;
  final double longitude;
  final String title;

  const _LocationMapCard({
    required this.latitude,
    required this.longitude,
    required this.title,
  });

  @override
  Widget build(BuildContext context) {
    final target = LatLng(latitude, longitude);

    return Card(
      elevation: 2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Padding(
            padding: EdgeInsets.fromLTRB(16, 14, 16, 8),
            child: Text(
              'Location',
              style: TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w600,
                color: Color(0xFF202124),
              ),
            ),
          ),
          SizedBox(
            height: 200,
            child: GoogleMap(
              initialCameraPosition: CameraPosition(
                target: target,
                zoom: 16,
              ),
              markers: {
                Marker(
                  markerId: const MarkerId('detail'),
                  position: target,
                  infoWindow: InfoWindow(title: title),
                ),
              },
              scrollGesturesEnabled: false,
              zoomGesturesEnabled: false,
              rotateGesturesEnabled: false,
              tiltGesturesEnabled: false,
              myLocationButtonEnabled: false,
              zoomControlsEnabled: false,
              mapToolbarEnabled: false,
            ),
          ),
        ],
      ),
    );
  }
}

// ── Shared badge widgets ──────────────────────────────────────────────────────

class _CategoryChip extends StatelessWidget {
  final String category;
  const _CategoryChip({required this.category});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: const Color(0xFFE8F0FE),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Text(
        category,
        style: const TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w600,
          color: Color(0xFF1A73E8),
        ),
      ),
    );
  }
}

class _ResolvedBadge extends StatelessWidget {
  const _ResolvedBadge();

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: Colors.green.shade50,
        border: Border.all(color: Colors.green.shade300),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.check_circle, size: 13, color: Colors.green.shade700),
          const SizedBox(width: 4),
          Text(
            'Resolved',
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w600,
              color: Colors.green.shade700,
            ),
          ),
        ],
      ),
    );
  }
}

class _StatusBadge extends StatelessWidget {
  final String status;
  const _StatusBadge({required this.status});

  Color get _color {
    switch (status) {
      case 'In Progress':
        return Colors.orange;
      case 'To Do':
      default:
        return Colors.blue;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: _color.withValues(alpha: 0.1),
        border: Border.all(color: _color.withValues(alpha: 0.4)),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Text(
        status,
        style: TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w600,
          color: _color,
        ),
      ),
    );
  }
}
