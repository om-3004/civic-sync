// Citizen feed screen — real-time map and list view of civic issue tickets.
//
// Subscribes to a Firestore snapshot listener that filters:
//   status IN ['To Do', 'In Progress', 'Done'], ordered by created_at desc.
//
// Client-side filter: Done tickets where resolved_at < now - 7 days are
// excluded. Remaining Done tickets display a "Resolved" badge.
//
// Both a map tab (GoogleMap with markers) and a list tab (scrollable list)
// are provided. Tapping a marker or list item navigates to TicketDetailScreen.
//
// Requirements: 7.1, 7.2, 7.4, 7.5, 7.6, 9.3, 9.5

import 'dart:async';

import 'package:cloud_firestore/cloud_firestore.dart';
import 'package:firebase_auth/firebase_auth.dart';
import 'package:flutter/material.dart';
import 'package:geolocator/geolocator.dart';
import 'package:google_maps_flutter/google_maps_flutter.dart';

import 'my_issues_screen.dart';
import 'ticket_detail_screen.dart';

// ── Ticket model (Dart-side, mirrors Firestore schema) ────────────────────────

/// Lightweight Dart model for a civic issue ticket as stored in Firestore.
class Ticket {
  final String id;
  final String category;
  final String title;
  final String description;
  final String imageUrl;
  final double latitude;
  final double longitude;
  final String status;
  final int upvotes;
  final String reportedBy;
  final String reportedByName;
  final String reportedByEmail;
  final DateTime createdAt;
  final DateTime updatedAt;
  final DateTime? resolvedAt;

  const Ticket({
    required this.id,
    required this.category,
    required this.title,
    required this.description,
    required this.imageUrl,
    required this.latitude,
    required this.longitude,
    required this.status,
    required this.upvotes,
    required this.reportedBy,
    this.reportedByName = '',
    this.reportedByEmail = '',
    required this.createdAt,
    required this.updatedAt,
    this.resolvedAt,
  });

  /// Constructs a [Ticket] from a Firestore document snapshot.
  factory Ticket.fromFirestore(DocumentSnapshot<Map<String, dynamic>> doc) {
    final data = doc.data()!;
    final location =
        (data['location'] as Map<String, dynamic>?) ?? {};
    return Ticket(
      id: (data['id'] as String?) ?? doc.id,
      category: (data['category'] as String?) ?? '',
      title: (data['title'] as String?) ?? '',
      description: (data['description'] as String?) ?? '',
      imageUrl: (data['image_url'] as String?) ?? '',
      latitude:
          (location['latitude'] as num?)?.toDouble() ?? 0.0,
      longitude:
          (location['longitude'] as num?)?.toDouble() ?? 0.0,
      status: (data['status'] as String?) ?? 'To Do',
      upvotes: (data['upvotes'] as int?) ?? 0,
      reportedBy: (data['reported_by'] as String?) ?? '',
      reportedByName: (data['reported_by_name'] as String?) ?? '',
      reportedByEmail: (data['reported_by_email'] as String?) ?? '',
      createdAt: _toDateTime(data['created_at']),
      updatedAt: _toDateTime(data['updated_at']),
      resolvedAt: data['resolved_at'] != null
          ? _toDateTime(data['resolved_at'])
          : null,
    );
  }

  static DateTime _toDateTime(dynamic value) {
    if (value is Timestamp) return value.toDate();
    if (value is String) return DateTime.tryParse(value) ?? DateTime(2000);
    return DateTime(2000);
  }
}

// ── Feed filter logic (pure, extracted for testability) ──────────────────────

/// Returns true if [ticket] should be visible in the citizen feed.
///
/// Visible when:
///   - status == 'To Do'
///   - status == 'In Progress'
///   - status == 'Done' AND resolvedAt > now − 7 days
///
/// Req 7.5, 9.5
bool citizenFeedFilter(Ticket ticket, DateTime now) {
  if (ticket.status == 'To Do' || ticket.status == 'In Progress') {
    return true;
  }
  if (ticket.status == 'Done') {
    final resolvedAt = ticket.resolvedAt;
    if (resolvedAt == null) return true; // edge case: show if no timestamp yet
    return now.difference(resolvedAt).inDays < 7;
  }
  return false;
}

// ── CitizenFeedScreen ─────────────────────────────────────────────────────────

/// Main citizen-facing screen showing civic issue tickets on a map and in a
/// scrollable list (Req 7.1, 7.2).
class CitizenFeedScreen extends StatefulWidget {
  const CitizenFeedScreen({super.key});

  @override
  State<CitizenFeedScreen> createState() => _CitizenFeedScreenState();
}

class _CitizenFeedScreenState extends State<CitizenFeedScreen>
    with SingleTickerProviderStateMixin {
  // ── Tab controller (Feed | Map | My Issues) ───────────────────────────────

  late final TabController _tabController;

  // ── Feed state ────────────────────────────────────────────────────────────

  /// All tickets from Firestore (unfiltered).
  List<Ticket> _allTickets = [];

  /// Whether to show resolved (Done) tickets in addition to open ones.
  bool _showResolved = false;

  /// Tickets visible in the current view based on [_showResolved].
  List<Ticket> get _tickets =>
      _showResolved ? _allTickets : _allTickets.where((t) => t.status != 'Done').toList();

  /// Whether the initial load is still in progress.
  bool _isLoading = true;

  /// Non-null when the Firestore listener has reported an error (Req 7.6).
  String? _errorMessage;

  // ── Firestore subscription ────────────────────────────────────────────────

  StreamSubscription<QuerySnapshot<Map<String, dynamic>>>? _subscription;

  // ── Google Maps controller ────────────────────────────────────────────────

  GoogleMapController? _mapController;

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this);
    _subscribeToFeed();
  }

  @override
  void dispose() {
    _tabController.dispose();
    _subscription?.cancel();
    _mapController?.dispose();
    super.dispose();
  }

  // ── Firestore subscription ────────────────────────────────────────────────

  /// Subscribes to the Firestore snapshot listener (Req 7.1, 7.4).
  ///
  /// The Firestore query filters status IN ['To Do', 'In Progress', 'Done']
  /// and orders by created_at descending. An additional client-side filter
  /// (Req 7.5, 9.5) excludes Done tickets older than 7 days.
  void _subscribeToFeed() {
    setState(() {
      _isLoading = true;
      _errorMessage = null;
    });

    _subscription?.cancel();

    _subscription = FirebaseFirestore.instance
        .collection('tickets')
        .where('status', whereIn: ['To Do', 'In Progress', 'Done'])
        .orderBy('created_at', descending: true)
        .snapshots()
        .listen(
          _onSnapshot,
          onError: _onError,
        );
  }

  void _onSnapshot(QuerySnapshot<Map<String, dynamic>> snapshot) {
    final now = DateTime.now();
    final tickets = snapshot.docs
        .map((d) => Ticket.fromFirestore(d))
        .where((t) => citizenFeedFilter(t, now))
        .toList();

    if (mounted) {
      setState(() {
        _allTickets = tickets;
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
            'Unable to load issue feed. Please check your connection.';
      });
    }
  }

  // ── Navigation ────────────────────────────────────────────────────────────

  void _openTicketDetail(Ticket ticket) {
    Navigator.push(
      context,
      MaterialPageRoute(
        builder: (_) => TicketDetailScreen(ticket: ticket),
      ),
    );
  }

  // ── Build ─────────────────────────────────────────────────────────────────

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: const Color(0xFFF8F9FA),
      appBar: AppBar(
        title: const Text('CivicSync'),
        backgroundColor: const Color(0xFF1A73E8),
        foregroundColor: Colors.white,
        elevation: 0,
        titleTextStyle: const TextStyle(
          color: Colors.white,
          fontSize: 18,
          fontWeight: FontWeight.w600,
        ),
        actions: [
          IconButton(
            icon: const Icon(Icons.add_circle_outline),
            tooltip: 'Report an issue',
            onPressed: () => Navigator.pushNamed(context, '/report'),
          ),
          IconButton(
            icon: const Icon(Icons.person_outline),
            tooltip: 'Profile',
            onPressed: () => Navigator.pushNamed(context, '/profile'),
          ),
        ],
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(72),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(12, 0, 4, 0),
                child: Row(
                  children: [
                    const Text('Show Resolved',
                        style: TextStyle(color: Colors.white70, fontSize: 12)),
                    Transform.scale(
                      scale: 0.75,
                      child: Switch(
                        value: _showResolved,
                        onChanged: (v) => setState(() => _showResolved = v),
                        activeColor: Colors.white,
                        activeTrackColor: Colors.green,
                        inactiveThumbColor: Colors.white,
                        inactiveTrackColor: Colors.white24,
                      ),
                    ),
                    const Spacer(),
                    Text(
                      _showResolved ? '✓ All issues' : 'Open only',
                      style: TextStyle(
                        color: _showResolved ? Colors.greenAccent : Colors.white54,
                        fontSize: 11,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ],
                ),
              ),
              TabBar(
                controller: _tabController,
                indicatorColor: Colors.white,
                labelColor: Colors.white,
                unselectedLabelColor: Colors.white70,
                labelStyle: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                unselectedLabelStyle: const TextStyle(fontSize: 12),
                tabs: const [
                  Tab(text: 'Feed'),
                  Tab(text: 'Map'),
                  Tab(text: 'My Issues'),
                ],
              ),
            ],
          ),
        ),
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    // Error state (Req 7.6): show error + retry button.
    if (_errorMessage != null) {
      return _ErrorRetryPanel(
        message: _errorMessage!,
        onRetry: _subscribeToFeed,
      );
    }

    // Loading state.
    if (_isLoading) {
      return const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CircularProgressIndicator(color: Color(0xFF1A73E8)),
            SizedBox(height: 16),
            Text('Loading issues…', style: TextStyle(color: Color(0xFF5F6368))),
          ],
        ),
      );
    }

    // Normal state: tab view with map and list.
    return TabBarView(
      controller: _tabController,
      physics: const NeverScrollableScrollPhysics(),
      children: [
        _ListView(
          tickets: _tickets,
          onTicketTap: _openTicketDetail,
        ),
        _MapView(
          tickets: _tickets,
          onMarkerTap: _openTicketDetail,
          onMapCreated: (c) => _mapController = c,
        ),
        const MyIssuesScreen(),
      ],
    );
  }
}

// ── Error + retry panel ───────────────────────────────────────────────────────

/// Displayed when the Firestore listener fails (Req 7.6).
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

// ── Map view (Req 7.1) ────────────────────────────────────────────────────────

/// GoogleMap view with a marker for each ticket in the feed.
class _MapView extends StatefulWidget {
  final List<Ticket> tickets;
  final void Function(Ticket) onMarkerTap;
  final void Function(GoogleMapController) onMapCreated;

  const _MapView({
    required this.tickets,
    required this.onMarkerTap,
    required this.onMapCreated,
  });

  @override
  State<_MapView> createState() => _MapViewState();
}

class _MapViewState extends State<_MapView> {
  final Map<MarkerId, Ticket> _markerTicketMap = {};
  LatLng? _userLocation;
  GoogleMapController? _controller;

  @override
  void initState() {
    super.initState();
    _fetchUserLocation();
  }

  Future<void> _fetchUserLocation() async {
    try {
      LocationPermission permission = await Geolocator.checkPermission();
      if (permission == LocationPermission.denied) {
        permission = await Geolocator.requestPermission();
      }
      if (permission == LocationPermission.denied ||
          permission == LocationPermission.deniedForever) return;

      final position = await Geolocator.getCurrentPosition(
        locationSettings: const LocationSettings(accuracy: LocationAccuracy.high),
      );

      if (!mounted) return;
      final latLng = LatLng(position.latitude, position.longitude);
      setState(() => _userLocation = latLng);
      _controller?.animateCamera(CameraUpdate.newLatLngZoom(latLng, 14));
    } catch (_) {
      // Permission denied or location unavailable — stay on default view.
    }
  }

  // ── Build ─────────────────────────────────────────────────────────────────

  Set<Marker> _buildMarkers() {
    _markerTicketMap.clear();
    final markers = <Marker>{};

    for (final ticket in widget.tickets) {
      if (ticket.latitude == 0.0 && ticket.longitude == 0.0) continue;

      final markerId = MarkerId(ticket.id);
      _markerTicketMap[markerId] = ticket;

      // Use different hues for Done (Resolved) vs active tickets (Req 9.3).
      final double hue = ticket.status == 'Done'
          ? BitmapDescriptor.hueGreen
          : BitmapDescriptor.hueRed;

      markers.add(
        Marker(
          markerId: markerId,
          position: LatLng(ticket.latitude, ticket.longitude),
          infoWindow: InfoWindow(
            title: ticket.title,
            snippet: '${ticket.category} · ${_statusLabel(ticket)}',
          ),
          icon: BitmapDescriptor.defaultMarkerWithHue(hue),
          onTap: () => widget.onMarkerTap(ticket),
        ),
      );
    }

    return markers;
  }

  String _statusLabel(Ticket ticket) {
    if (ticket.status == 'Done') return 'Resolved';
    return ticket.status;
  }

  @override
  Widget build(BuildContext context) {
    // Use user's location if available, otherwise centre of India.
    final CameraPosition initialPosition = _userLocation != null
        ? CameraPosition(target: _userLocation!, zoom: 14)
        : const CameraPosition(target: LatLng(20.5937, 78.9629), zoom: 5);

    return GoogleMap(
      initialCameraPosition: initialPosition,
      markers: _buildMarkers(),
      myLocationButtonEnabled: true,
      myLocationEnabled: true,
      mapToolbarEnabled: false,
      onMapCreated: (c) {
        _controller = c;
        widget.onMapCreated(c);
        // If location was already fetched before map was ready, move now.
        if (_userLocation != null) {
          c.animateCamera(CameraUpdate.newLatLngZoom(_userLocation!, 14));
        }
      },
    );
  }
}

// ── List view (Req 7.2) ───────────────────────────────────────────────────────

/// Scrollable list of tickets sorted by created_at descending (maintained by
/// the Firestore query order). Displays a "Resolved" badge on Done tickets
/// (Req 9.3).
class _ListView extends StatelessWidget {
  final List<Ticket> tickets;
  final void Function(Ticket) onTicketTap;

  const _ListView({required this.tickets, required this.onTicketTap});

  @override
  Widget build(BuildContext context) {
    if (tickets.isEmpty) {
      return const Center(
        child: Padding(
          padding: EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.inbox_outlined, size: 64, color: Color(0xFF5F6368)),
              SizedBox(height: 16),
              Text(
                'No issues reported yet.\nTap + to report one!',
                textAlign: TextAlign.center,
                style: TextStyle(
                  fontSize: 15,
                  color: Color(0xFF5F6368),
                ),
              ),
            ],
          ),
        ),
      );
    }

    return RefreshIndicator(
      color: const Color(0xFF1A73E8),
      onRefresh: () async {
        // The listener updates automatically — no manual refresh needed.
        // This provides the expected pull-to-refresh UX.
        await Future<void>.delayed(const Duration(milliseconds: 400));
      },
      child: ListView.separated(
        padding: EdgeInsets.fromLTRB(
          0,
          8,
          0,
          8 + MediaQuery.of(context).padding.bottom,
        ),
        itemCount: tickets.length,
        separatorBuilder: (context, index) => const SizedBox(height: 12),
        itemBuilder: (_, index) {
          final ticket = tickets[index];
          return _TicketListCard(
            ticket: ticket,
            onTap: () => onTicketTap(ticket),
          );
        },
      ),
    );
  }
}

// ── Ticket list card ──────────────────────────────────────────────────────────

/// Instagram-style card: full-width image on top, details below.
class _TicketListCard extends StatelessWidget {
  final Ticket ticket;
  final VoidCallback onTap;

  const _TicketListCard({required this.ticket, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final bool isResolved = ticket.status == 'Done';

    return Card(
      elevation: 2,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
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
                          child: CircularProgressIndicator(
                              color: Color(0xFF1A73E8)),
                        ),
                      );
                    },
                  ),
                  // Status badge overlaid on top-right of image
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
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(Icons.image_outlined,
                          size: 48, color: Color(0xFFDADCE0)),
                      if (isResolved)
                        const Padding(
                          padding: EdgeInsets.only(top: 8),
                          child: _ResolvedBadge(),
                        )
                      else
                        Padding(
                          padding: const EdgeInsets.only(top: 8),
                          child: _StatusBadge(status: ticket.status),
                        ),
                    ],
                  ),
                ),
              ),

            // ── Details ──────────────────────────────────────────────────
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 12, 14, 14),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // Category chip
                  _CategoryChip(category: ticket.category),
                  const SizedBox(height: 8),

                  // Title
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
                      style: const TextStyle(
                        fontSize: 13,
                        color: Color(0xFF5F6368),
                      ),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],

                  const SizedBox(height: 10),

                  // Footer: upvotes + date
                  Row(
                    children: [
                      const Icon(Icons.thumb_up_alt_outlined,
                          size: 15, color: Color(0xFF5F6368)),
                      const SizedBox(width: 4),
                      Text(
                        '${ticket.upvotes}',
                        style: const TextStyle(
                            fontSize: 13, color: Color(0xFF5F6368)),
                      ),
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

/// Green "Resolved" badge shown on Done tickets within 7-day ArchivePeriod
/// (Req 9.3).
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
          color: _color.shade700 ?? _color,
        ),
      ),
    );
  }
}

// Extension to safely access shade on Color
extension _ColorShade on Color {
  Color? get shade700 {
    // Only MaterialColor instances have .shade700.
    // Plain Color does not — return null to fall back.
    return null;
  }
}
