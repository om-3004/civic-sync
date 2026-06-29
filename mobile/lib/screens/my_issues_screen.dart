// My Issues screen — lists all tickets reported by the current signed-in user.
//
// Subscribes to a Firestore real-time snapshot filtered to
//   reported_by == FirebaseAuth.instance.currentUser!.uid
// ordered by created_at descending.
//
// Each card shows the same fields as the citizen feed list view plus a Delete
// button that calls DELETE /tickets/:id. Deletion is owner-only — the backend
// enforces the same ownership check and returns 403 for anyone else.
//
// Tapping a card navigates to TicketDetailScreen (same as the main feed).

import 'dart:async';

import 'package:cloud_firestore/cloud_firestore.dart';
import 'package:firebase_auth/firebase_auth.dart';
import 'package:flutter/material.dart';

import '../services/ticket_service.dart';
import 'citizen_feed_screen.dart' show Ticket;
import 'ticket_detail_screen.dart';

// ── MyIssuesScreen ────────────────────────────────────────────────────────────

/// Standalone screen that shows only the issues reported by the current user.
///
/// Route: `/my-issues`
class MyIssuesScreen extends StatefulWidget {
  const MyIssuesScreen({super.key});

  @override
  State<MyIssuesScreen> createState() => _MyIssuesScreenState();
}

class _MyIssuesScreenState extends State<MyIssuesScreen> {
  // ── State ─────────────────────────────────────────────────────────────────

  List<Ticket> _tickets = [];
  bool _isLoading = true;
  String? _errorMessage;

  /// IDs of tickets currently being deleted (prevents double-tap).
  final Set<String> _deletingIds = {};

  // ── Firestore subscription ────────────────────────────────────────────────

  StreamSubscription<QuerySnapshot<Map<String, dynamic>>>? _subscription;

  // ── Service ───────────────────────────────────────────────────────────────

  final TicketService _ticketService = TicketService();

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  @override
  void initState() {
    super.initState();
    _subscribe();
  }

  @override
  void dispose() {
    _subscription?.cancel();
    super.dispose();
  }

  // ── Firestore ─────────────────────────────────────────────────────────────

  void _subscribe() {
    final uid = FirebaseAuth.instance.currentUser?.uid;
    if (uid == null) {
      setState(() {
        _isLoading = false;
        _errorMessage = 'Not signed in.';
      });
      return;
    }

    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    _subscription?.cancel();
    _subscription = FirebaseFirestore.instance
        .collection('tickets')
        .where('reported_by', isEqualTo: uid)
        .orderBy('created_at', descending: true)
        .snapshots()
        .listen(
          (snapshot) {
            if (!mounted) return;
            setState(() {
              _tickets = snapshot.docs.map((d) => Ticket.fromFirestore(d)).toList();
              _isLoading = false;
              _errorMessage = null;
            });
          },
          onError: (error) {
            if (!mounted) return;
            setState(() {
              _isLoading = false;
              _errorMessage = 'Unable to load your issues: $error';
            });
          },
        );
  }

  // ── Delete ────────────────────────────────────────────────────────────────

  Future<void> _confirmAndDelete(Ticket ticket) async {
    final bool? confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete Issue'),
        content: Text(
          'Are you sure you want to delete "${ticket.title}"? This cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          ElevatedButton(
            onPressed: () => Navigator.pop(ctx, true),
            style: ElevatedButton.styleFrom(
              backgroundColor: Colors.red.shade700,
              foregroundColor: Colors.white,
            ),
            child: const Text('Delete'),
          ),
        ],
      ),
    );

    if (confirmed != true || !mounted) return;

    setState(() => _deletingIds.add(ticket.id));

    try {
      await _ticketService.deleteTicket(ticket.id);
      // Firestore snapshot listener will remove the card automatically.
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Issue deleted.')),
        );
      }
    } on DeleteTicketException catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(e.message)),
        );
      }
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Failed to delete. Please try again.')),
        );
      }
    } finally {
      if (mounted) setState(() => _deletingIds.remove(ticket.id));
    }
  }

  // ── Build ─────────────────────────────────────────────────────────────────

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFFF8F9FA),
      appBar: AppBar(
        title: const Text('My Issues'),
        backgroundColor: const Color(0xFF1A73E8),
        foregroundColor: Colors.white,
        elevation: 0,
        actions: [
          IconButton(
            icon: const Icon(Icons.add_circle_outline),
            tooltip: 'Report an issue',
            onPressed: () => Navigator.pushNamed(context, '/report'),
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    if (_errorMessage != null) {
      return _ErrorRetryPanel(message: _errorMessage!, onRetry: _subscribe);
    }

    if (_isLoading) {
      return const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(color: Color(0xFF1A73E8)),
            SizedBox(height: 16),
            Text('Loading your issues…', style: TextStyle(color: Color(0xFF5F6368))),
          ],
        ),
      );
    }

    if (_tickets.isEmpty) {
      return const Center(
        child: Padding(
          padding: EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.inbox_outlined, size: 64, color: Color(0xFF5F6368)),
              SizedBox(height: 16),
              Text(
                "You haven't reported any issues yet.\nTap + to report one!",
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 15, color: Color(0xFF5F6368)),
              ),
            ],
          ),
        ),
      );
    }

    return RefreshIndicator(
      color: const Color(0xFF1A73E8),
      onRefresh: () async => await Future<void>.delayed(const Duration(milliseconds: 300)),
      child: ListView.separated(
        padding: EdgeInsets.fromLTRB(
          0,
          8,
          0,
          8 + MediaQuery.of(context).padding.bottom,
        ),
        itemCount: _tickets.length,
        separatorBuilder: (_, __) => const SizedBox(height: 12),
        itemBuilder: (_, index) {
          final ticket = _tickets[index];
          return _MyIssueCard(
            ticket: ticket,
            isDeleting: _deletingIds.contains(ticket.id),
            onTap: () => Navigator.push(
              context,
              MaterialPageRoute(builder: (_) => TicketDetailScreen(ticket: ticket)),
            ),
            onDelete: () => _confirmAndDelete(ticket),
          );
        },
      ),
    );
  }
}

// ── _MyIssueCard ──────────────────────────────────────────────────────────────

/// Ticket list card with an additional delete button for the owner.
class _MyIssueCard extends StatelessWidget {
  final Ticket ticket;
  final bool isDeleting;
  final VoidCallback onTap;
  final VoidCallback onDelete;

  const _MyIssueCard({
    required this.ticket,
    required this.isDeleting,
    required this.onTap,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    final bool isResolved = ticket.status == 'Done';

    return Card(
      elevation: 2,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: isDeleting ? null : onTap,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [

            // ── Image ────────────────────────────────────────────────────
            if (ticket.imageUrl.isNotEmpty)
              Stack(
                children: [
                  Image.network(
                    ticket.imageUrl,
                    height: 200,
                    width: double.infinity,
                    fit: BoxFit.cover,
                    errorBuilder: (_, __, ___) => Container(
                      height: 200,
                      color: const Color(0xFFF1F3F4),
                      child: const Center(
                        child: Icon(Icons.image_not_supported_outlined,
                            size: 48, color: Color(0xFFDADCE0)),
                      ),
                    ),
                    loadingBuilder: (_, child, progress) {
                      if (progress == null) return child;
                      return Container(
                        height: 200,
                        color: const Color(0xFFF1F3F4),
                        child: const Center(
                          child: CircularProgressIndicator(color: Color(0xFF1A73E8)),
                        ),
                      );
                    },
                  ),
                  Positioned(
                    top: 10,
                    right: 10,
                    child: isResolved
                        ? const _ResolvedBadge()
                        : _StatusBadge(status: ticket.status),
                  ),
                ],
              )
            else
              Container(
                height: 160,
                color: const Color(0xFFF1F3F4),
                child: Center(
                  child: isResolved
                      ? const _ResolvedBadge()
                      : _StatusBadge(status: ticket.status),
                ),
              ),

            // ── Details ──────────────────────────────────────────────────
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 12, 14, 14),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      _CategoryChip(category: ticket.category),
                      const Spacer(),
                      if (isDeleting)
                        const SizedBox(
                          width: 20,
                          height: 20,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                            color: Color(0xFFD93025),
                          ),
                        )
                      else if (ticket.status == 'To Do')
                        InkWell(
                          onTap: onDelete,
                          borderRadius: BorderRadius.circular(20),
                          child: Padding(
                            padding: const EdgeInsets.all(4),
                            child: Icon(Icons.delete_outline,
                                size: 20, color: Colors.red.shade600),
                          ),
                        ),
                    ],
                  ),
                  const SizedBox(height: 8),

                  Text(
                    ticket.title,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                      color: Color(0xFF202124),
                    ),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),

                  if (ticket.description.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(
                      ticket.description,
                      style: const TextStyle(fontSize: 13, color: Color(0xFF5F6368)),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],

                  const SizedBox(height: 10),

                  Row(
                    children: [
                      const Icon(Icons.thumb_up_alt_outlined,
                          size: 15, color: Color(0xFF5F6368)),
                      const SizedBox(width: 4),
                      Text('${ticket.upvotes}',
                          style: const TextStyle(
                              fontSize: 13, color: Color(0xFF5F6368))),
                      const Spacer(),
                      Text(
                        _formatDate(ticket.createdAt),
                        style: const TextStyle(
                            fontSize: 12, color: Color(0xFF9AA0A6)),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  String _formatDate(DateTime dt) {
    final now = DateTime.now();
    final diff = now.difference(dt);
    if (diff.inDays == 0) {
      if (diff.inHours == 0) return '${diff.inMinutes}m ago';
      return '${diff.inHours}h ago';
    }
    if (diff.inDays < 7) return '${diff.inDays}d ago';
    return '${dt.day}/${dt.month}/${dt.year}';
  }
}

// ── Error retry panel ─────────────────────────────────────────────────────────

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
              style: const TextStyle(fontSize: 15, color: Color(0xFF5F6368)),
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              onPressed: onRetry,
              icon: const Icon(Icons.refresh),
              label: const Text('Retry'),
              style: ElevatedButton.styleFrom(
                backgroundColor: const Color(0xFF1A73E8),
                foregroundColor: Colors.white,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ── Small reusable badge / chip widgets ───────────────────────────────────────

class _CategoryChip extends StatelessWidget {
  final String category;
  const _CategoryChip({required this.category});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: const Color(0xFFE8F0FE),
        borderRadius: BorderRadius.circular(12),
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

class _ResolvedBadge extends StatelessWidget {
  const _ResolvedBadge();

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: Colors.green.shade50,
        border: Border.all(color: Colors.green.shade300),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.check_circle, size: 12, color: Colors.green.shade700),
          const SizedBox(width: 4),
          Text(
            'Resolved',
            style: TextStyle(
              fontSize: 11,
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
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: _color.withValues(alpha: 0.1),
        border: Border.all(color: _color.withValues(alpha: 0.4)),
        borderRadius: BorderRadius.circular(12),
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
