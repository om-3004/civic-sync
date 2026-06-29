// Official Management Dashboard — Kanban view for government officials.
//
// Subscribes to a Firestore snapshot listener:
//   status IN ['To Do', 'In Progress', 'Done'], ordered by upvotes desc,
//   limit 200. (Req 8.2)
//
// Displays tickets in three Kanban columns: "To Do", "In Progress", "Done".
// Each ticket card shows full details (Req 8.3): title, category, description,
// upvotes, status, location, submission timestamp, image thumbnail, and
// reporter email.
//
// Status change controls on each card call PUT /tickets/:id/status (Req 8.4).
// Backend errors are surfaced as snackbars:
//   - HTTP 400 → "Invalid status transition"   (Req 8.5, 8.6)
//   - HTTP 409 → "Ticket is archived and cannot be modified"  (Req 9.4)
//
// Firestore snapshot delivery propagates status changes to citizen feeds within
// 5 seconds (Req 8.7).
//
// Requirements: 8.1, 8.2, 8.3, 8.4, 8.7

import 'dart:async';
import 'dart:convert';

import 'package:cloud_firestore/cloud_firestore.dart';
import 'package:firebase_auth/firebase_auth.dart';
import 'package:flutter/material.dart';
import 'package:geocoding/geocoding.dart';
import 'package:http/http.dart' as http;

import 'citizen_feed_screen.dart' show Ticket;

// Backend base URL — override at build time via --dart-define=BACKEND_URL=...
const String _backendBaseUrl = String.fromEnvironment(
  'BACKEND_URL',
  defaultValue: 'http://10.0.2.2:8080',
);

// ── Kanban column definitions ─────────────────────────────────────────────────

/// The three KanbanStatus values displayed in the dashboard (Req 8.2).
const List<String> _kColumns = ['To Do', 'In Progress', 'Done'];

/// Allowed forward-only status transitions (Req 8.5):
///   To Do → In Progress → Done.
const Map<String, String?> _kNextStatus = {
  'To Do': 'In Progress',
  'In Progress': 'Done',
  'Done': null, // no further manual transition
};

// ── OfficialDashboardScreen ───────────────────────────────────────────────────

/// Kanban management dashboard shown to users with `role == "official"` (Req 8.1).
///
/// Route: `/official-dash`
class OfficialDashboardScreen extends StatefulWidget {
  const OfficialDashboardScreen({super.key});

  @override
  State<OfficialDashboardScreen> createState() =>
      _OfficialDashboardScreenState();
}

class _OfficialDashboardScreenState extends State<OfficialDashboardScreen> {
  // ── Feed state ────────────────────────────────────────────────────────────

  /// Full ticket list as delivered by the Firestore snapshot listener.
  List<Ticket> _tickets = [];

  /// True while waiting for the first snapshot event.
  bool _isLoading = true;

  /// Non-null when the Firestore listener has emitted an error.
  String? _errorMessage;

  // ── In-flight status update tracking ──────────────────────────────────────

  /// Set of ticket IDs currently being updated (prevents double-tap).
  final Set<String> _updatingIds = {};

  // ── Firestore subscription ────────────────────────────────────────────────

  StreamSubscription<QuerySnapshot<Map<String, dynamic>>>? _subscription;

  // ── HTTP client ───────────────────────────────────────────────────────────

  final http.Client _httpClient = http.Client();

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  @override
  void initState() {
    super.initState();
    _subscribeToTickets();
  }

  @override
  void dispose() {
    _subscription?.cancel();
    _httpClient.close();
    super.dispose();
  }

  // ── Firestore subscription ────────────────────────────────────────────────

  /// Subscribes to the official dashboard Firestore query (Req 8.2).
  ///
  /// Filter:  status IN ['To Do', 'In Progress', 'Done']
  /// Order:   upvotes descending
  /// Limit:   200
  void _subscribeToTickets() {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    _subscription?.cancel();

    _subscription = FirebaseFirestore.instance
        .collection('tickets')
        .where('status', whereIn: ['To Do', 'In Progress', 'Done'])
        .orderBy('upvotes', descending: true)
        .limit(200)
        .snapshots()
        .listen(
          _onSnapshot,
          onError: _onError,
        );
  }

  void _onSnapshot(QuerySnapshot<Map<String, dynamic>> snapshot) {
    final tickets =
        snapshot.docs.map((d) => Ticket.fromFirestore(d)).toList();

    if (mounted) {
      setState(() {
        _tickets = tickets;
        _isLoading = false;
        _errorMessage = null;
      });
    }
  }

  void _onError(Object error) {
    if (mounted) {
      setState(() {
        _isLoading = false;
        _errorMessage =
            'Unable to load dashboard. Please check your connection.';
      });
    }
  }

  // ── Status update ─────────────────────────────────────────────────────────

  /// Calls PUT /tickets/:id/status with the next allowed status.
  ///
  /// On success the Firestore listener automatically delivers the updated
  /// document within 5 seconds, propagating the change to citizen feeds
  /// (Req 8.7).
  ///
  /// Handles:
  ///   HTTP 400 → snackbar "Invalid status transition"
  ///   HTTP 409 → snackbar "Ticket is archived and cannot be modified"
  Future<void> _advanceStatus(Ticket ticket) async {
    final String? nextStatus = _kNextStatus[ticket.status];
    if (nextStatus == null) return; // 'Done' has no further transition.

    if (_updatingIds.contains(ticket.id)) return; // already in-flight.

    setState(() => _updatingIds.add(ticket.id));

    try {
      final String? idToken =
          await FirebaseAuth.instance.currentUser?.getIdToken();

      final response = await _httpClient.put(
        Uri.parse('$_backendBaseUrl/tickets/${ticket.id}/status'),
        headers: {
          'Content-Type': 'application/json',
          if (idToken != null) 'Authorization': 'Bearer $idToken',
        },
        body: jsonEncode({'status': nextStatus}),
      );

      if (!mounted) return;

      if (response.statusCode == 200) {
        // Firestore snapshot listener delivers the update automatically.
        _showSnackBar('Status updated to "$nextStatus".');
      } else if (response.statusCode == 400) {
        _showSnackBar('Invalid status transition');
      } else if (response.statusCode == 409) {
        _showSnackBar('Ticket is archived and cannot be modified');
      } else if (response.statusCode == 401) {
        _showSnackBar('Session expired. Please sign in again.');
      } else if (response.statusCode == 403) {
        _showSnackBar('You do not have permission to update ticket status.');
      } else {
        _showSnackBar(
            'Failed to update status (error ${response.statusCode}). Please try again.');
      }
    } on http.ClientException catch (e) {
      if (mounted) {
        _showSnackBar('Network error: ${e.message}');
      }
    } catch (_) {
      if (mounted) {
        _showSnackBar('An unexpected error occurred. Please try again.');
      }
    } finally {
      if (mounted) {
        setState(() => _updatingIds.remove(ticket.id));
      }
    }
  }

  // ── Helpers ───────────────────────────────────────────────────────────────

  void _showSnackBar(String message) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(message),
        duration: const Duration(seconds: 4),
      ),
    );
  }

  /// Tickets filtered to a single Kanban column.
  List<Ticket> _ticketsForColumn(String status) =>
      _tickets.where((t) => t.status == status).toList();

  // ── Build ─────────────────────────────────────────────────────────────────

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFFF1F3F4),
      appBar: AppBar(
        title: const Text('Official Dashboard'),
        backgroundColor: const Color(0xFF0D47A1),
        foregroundColor: Colors.white,
        elevation: 0,
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _subscribeToTickets,
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    // Error state: show message + retry button.
    if (_errorMessage != null) {
      return _ErrorRetryPanel(
        message: _errorMessage!,
        onRetry: _subscribeToTickets,
      );
    }

    // Loading state: spinner.
    if (_isLoading) {
      return const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(color: Color(0xFF0D47A1)),
            SizedBox(height: 16),
            Text(
              'Loading dashboard…',
              style: TextStyle(color: Color(0xFF5F6368)),
            ),
          ],
        ),
      );
    }

    // Normal state: horizontal Kanban board.
    return _KanbanBoard(
      columns: _kColumns,
      ticketsForColumn: _ticketsForColumn,
      updatingIds: _updatingIds,
      onAdvanceStatus: _advanceStatus,
    );
  }
}

// ── Error + retry panel ───────────────────────────────────────────────────────

class _ErrorRetryPanel extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _ErrorRetryPanel({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.cloud_off, size: 64, color: Color(0xFF5F6368)),
            const SizedBox(height: 16),
            Text(
              message,
              textAlign: TextAlign.center,
              style: const TextStyle(
                fontSize: 15,
                color: Color(0xFF5F6368),
              ),
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              onPressed: onRetry,
              icon: const Icon(Icons.refresh),
              label: const Text('Retry'),
              style: ElevatedButton.styleFrom(
                backgroundColor: const Color(0xFF0D47A1),
                foregroundColor: Colors.white,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ── Kanban board ──────────────────────────────────────────────────────────────

/// Horizontal scrollable Kanban board with one column per status.
class _KanbanBoard extends StatelessWidget {
  final List<String> columns;
  final List<Ticket> Function(String status) ticketsForColumn;
  final Set<String> updatingIds;
  final Future<void> Function(Ticket) onAdvanceStatus;

  const _KanbanBoard({
    required this.columns,
    required this.ticketsForColumn,
    required this.updatingIds,
    required this.onAdvanceStatus,
  });

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.all(12),
      itemCount: columns.length,
      separatorBuilder: (_, __) => const SizedBox(width: 12),
      itemBuilder: (context, index) {
        final status = columns[index];
        final tickets = ticketsForColumn(status);
        return _KanbanColumn(
          status: status,
          tickets: tickets,
          updatingIds: updatingIds,
          onAdvanceStatus: onAdvanceStatus,
        );
      },
    );
  }
}

// ── Kanban column ─────────────────────────────────────────────────────────────

/// A single vertical column in the Kanban board for one status.
class _KanbanColumn extends StatelessWidget {
  final String status;
  final List<Ticket> tickets;
  final Set<String> updatingIds;
  final Future<void> Function(Ticket) onAdvanceStatus;

  const _KanbanColumn({
    required this.status,
    required this.tickets,
    required this.updatingIds,
    required this.onAdvanceStatus,
  });

  Color get _headerColor {
    switch (status) {
      case 'In Progress':
        return Colors.orange.shade700;
      case 'Done':
        return Colors.green.shade700;
      case 'To Do':
      default:
        return const Color(0xFF1A73E8);
    }
  }

  @override
  Widget build(BuildContext context) {
    // Fixed column width so multiple columns are visible with horizontal scroll.
    return SizedBox(
      width: 300,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // ── Column header ─────────────────────────────────────────────
          _ColumnHeader(
            status: status,
            count: tickets.length,
            color: _headerColor,
          ),
          const SizedBox(height: 8),

          // ── Ticket cards (scrollable) ─────────────────────────────────
          Expanded(
            child: tickets.isEmpty
                ? _EmptyColumnHint(status: status)
                : ListView.separated(
                    padding: const EdgeInsets.only(bottom: 8),
                    itemCount: tickets.length,
                    separatorBuilder: (_, __) => const SizedBox(height: 8),
                    itemBuilder: (_, index) {
                      final ticket = tickets[index];
                      return _OfficialTicketCard(
                        ticket: ticket,
                        isUpdating: updatingIds.contains(ticket.id),
                        onAdvanceStatus: () => onAdvanceStatus(ticket),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}

// ── Column header ─────────────────────────────────────────────────────────────

class _ColumnHeader extends StatelessWidget {
  final String status;
  final int count;
  final Color color;

  const _ColumnHeader({
    required this.status,
    required this.count,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: color,
        borderRadius: BorderRadius.circular(10),
      ),
      child: Row(
        children: [
          Text(
            status,
            style: const TextStyle(
              fontSize: 14,
              fontWeight: FontWeight.w700,
              color: Colors.white,
            ),
          ),
          const Spacer(),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
            decoration: BoxDecoration(
              color: Colors.white.withValues(alpha: 0.25),
              borderRadius: BorderRadius.circular(10),
            ),
            child: Text(
              '$count',
              style: const TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w700,
                color: Colors.white,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

// ── Empty column hint ─────────────────────────────────────────────────────────

class _EmptyColumnHint extends StatelessWidget {
  final String status;

  const _EmptyColumnHint({required this.status});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.inbox_outlined,
                size: 40, color: Colors.grey.shade400),
            const SizedBox(height: 8),
            Text(
              'No tickets in "$status"',
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: 13,
                color: Colors.grey.shade500,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ── Official ticket card ──────────────────────────────────────────────────────

class _OfficialTicketCard extends StatefulWidget {
  final Ticket ticket;
  final bool isUpdating;
  final VoidCallback onAdvanceStatus;

  const _OfficialTicketCard({
    required this.ticket,
    required this.isUpdating,
    required this.onAdvanceStatus,
  });

  @override
  State<_OfficialTicketCard> createState() => _OfficialTicketCardState();
}

class _OfficialTicketCardState extends State<_OfficialTicketCard> {
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
    } catch (_) {
      // Geocoding failed — fall back to coordinates.
    }
  }

  String? get _nextStatus => _kNextStatus[widget.ticket.status];

  @override
  Widget build(BuildContext context) {
    final ticket = widget.ticket;
    return Card(
      elevation: 2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // ── Image thumbnail ──────────────────────────────────────────
            if (ticket.imageUrl.isNotEmpty)
              _ImageThumbnail(imageUrl: ticket.imageUrl),

            // ── Category chip + status badge ─────────────────────────────
            Row(
              children: [
                _CategoryChip(category: ticket.category),
                const Spacer(),
                _StatusBadge(status: ticket.status),
              ],
            ),
            const SizedBox(height: 8),

            // ── Title ────────────────────────────────────────────────────
            Text(
              ticket.title,
              style: const TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w700,
                color: Color(0xFF202124),
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),

            // ── Description ──────────────────────────────────────────────
            if (ticket.description.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(
                ticket.description,
                style: const TextStyle(
                  fontSize: 12,
                  color: Color(0xFF5F6368),
                  height: 1.4,
                ),
                maxLines: 3,
                overflow: TextOverflow.ellipsis,
              ),
            ],

            const SizedBox(height: 10),
            const Divider(height: 1),
            const SizedBox(height: 8),

            // ── Metadata rows ─────────────────────────────────────────────
            _MetaRow(
              icon: Icons.thumb_up_alt_outlined,
              label: '${ticket.upvotes} upvote${ticket.upvotes == 1 ? '' : 's'}',
            ),
            const SizedBox(height: 4),
            _MetaRow(
              icon: Icons.location_on_outlined,
              label: _areaName ??
                  '${ticket.latitude.toStringAsFixed(5)}, '
                  '${ticket.longitude.toStringAsFixed(5)}',
            ),
            const SizedBox(height: 4),
            _MetaRow(
              icon: Icons.schedule_outlined,
              label: _formatDateTime(ticket.createdAt),
            ),
            const SizedBox(height: 4),
            // Reporter (Req 8.3)
            _MetaRow(
              icon: Icons.person_outline,
              label: ticket.reportedByName.isNotEmpty
                  ? ticket.reportedByName
                  : ticket.reportedByEmail.isNotEmpty
                      ? ticket.reportedByEmail
                      : 'Unknown reporter',
            ),

            const SizedBox(height: 10),

            // ── Status advance button ─────────────────────────────────────
            if (_nextStatus != null)
              SizedBox(
                width: double.infinity,
                child: widget.isUpdating
                    ? const Center(
                        child: Padding(
                          padding: EdgeInsets.symmetric(vertical: 6),
                          child: SizedBox(
                            width: 22,
                            height: 22,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: Color(0xFF0D47A1),
                            ),
                          ),
                        ),
                      )
                    : ElevatedButton.icon(
                        onPressed: widget.onAdvanceStatus,
                        icon: const Icon(Icons.arrow_forward, size: 16),
                        label: Text(
                          'Move to "$_nextStatus"',
                          style: const TextStyle(fontSize: 12),
                        ),
                        style: ElevatedButton.styleFrom(
                          backgroundColor: const Color(0xFF0D47A1),
                          foregroundColor: Colors.white,
                          padding: const EdgeInsets.symmetric(
                              horizontal: 10, vertical: 8),
                          shape: RoundedRectangleBorder(
                            borderRadius: BorderRadius.circular(8),
                          ),
                        ),
                      ),
              )
            else
              // 'Done' — no further manual transition; show completed indicator.
              Container(
                width: double.infinity,
                padding: const EdgeInsets.symmetric(vertical: 6),
                decoration: BoxDecoration(
                  color: Colors.green.shade50,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Icon(Icons.check_circle_outline,
                        size: 16, color: Colors.green.shade700),
                    const SizedBox(width: 6),
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

// ── Image thumbnail ───────────────────────────────────────────────────────────

class _ImageThumbnail extends StatelessWidget {
  final String imageUrl;

  const _ImageThumbnail({required this.imageUrl});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(8),
        child: Image.network(
          imageUrl,
          height: 120,
          width: double.infinity,
          fit: BoxFit.cover,
          errorBuilder: (_, __, ___) => Container(
            height: 80,
            color: const Color(0xFFF1F3F4),
            child: const Center(
              child: Icon(
                Icons.broken_image_outlined,
                size: 32,
                color: Color(0xFF5F6368),
              ),
            ),
          ),
          loadingBuilder: (_, child, progress) {
            if (progress == null) return child;
            return Container(
              height: 80,
              color: const Color(0xFFF1F3F4),
              child: const Center(
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: Color(0xFF0D47A1),
                ),
              ),
            );
          },
        ),
      ),
    );
  }
}

// ── Shared small widgets ──────────────────────────────────────────────────────

class _MetaRow extends StatelessWidget {
  final IconData icon;
  final String label;

  const _MetaRow({required this.icon, required this.label});

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 14, color: const Color(0xFF5F6368)),
        const SizedBox(width: 6),
        Expanded(
          child: Text(
            label,
            style: const TextStyle(
              fontSize: 12,
              color: Color(0xFF5F6368),
            ),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ],
    );
  }
}

class _CategoryChip extends StatelessWidget {
  final String category;

  const _CategoryChip({required this.category});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: const Color(0xFFE8F0FE),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        category,
        style: const TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w600,
          color: Color(0xFF1A73E8),
        ),
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
        return Colors.orange.shade700;
      case 'Done':
        return Colors.green.shade700;
      case 'To Do':
      default:
        return const Color(0xFF1A73E8);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: _color.withValues(alpha: 0.1),
        border: Border.all(color: _color.withValues(alpha: 0.4)),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        status,
        style: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w600,
          color: _color,
        ),
      ),
    );
  }
}
