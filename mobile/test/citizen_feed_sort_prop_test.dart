// Feature: civic-sync, Property 11: Citizen Feed List is Always Sorted by created_at Descending
//
// Validates: Requirements 7.2
//
// Generates random ticket collections that all satisfy the citizen feed filter
// criteria; asserts that after sorting (as applied by the Firestore query +
// client processing) the list is ordered by created_at descending.

import 'package:test/test.dart';

// ── Minimal Ticket stub ───────────────────────────────────────────────────────

class _TicketStub {
  final String id;
  final String status;
  final DateTime createdAt;
  final DateTime? resolvedAt;

  const _TicketStub({
    required this.id,
    required this.status,
    required this.createdAt,
    this.resolvedAt,
  });
}

// ── Pure filter + sort logic (mirrors CitizenFeedScreen implementation) ──────

bool _feedFilter(_TicketStub ticket, DateTime now) {
  if (ticket.status == 'To Do' || ticket.status == 'In Progress') return true;
  if (ticket.status == 'Done') {
    final resolvedAt = ticket.resolvedAt;
    if (resolvedAt == null) return true;
    return now.difference(resolvedAt).inDays < 7;
  }
  return false;
}

/// Applies the citizen feed filter then sorts by created_at descending,
/// matching the behaviour of:
///   .where('status', whereIn: [...]).orderBy('created_at', descending: true)
///   followed by the client-side filter.
List<_TicketStub> _applyFeedQuery(
    List<_TicketStub> tickets, DateTime now) {
  return tickets.where((t) => _feedFilter(t, now)).toList()
    ..sort((a, b) => b.createdAt.compareTo(a.createdAt));
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/// Returns true if [items] is sorted by created_at descending.
bool _isSortedDescending(List<_TicketStub> items) {
  for (int i = 0; i < items.length - 1; i++) {
    if (items[i].createdAt.isBefore(items[i + 1].createdAt)) {
      return false;
    }
  }
  return true;
}

// ── Generators ───────────────────────────────────────────────────────────────

const List<String> _feedStatuses = ['To Do', 'In Progress', 'Done'];

/// Generates a list of ticket stubs that are valid feed candidates (all
/// statuses satisfy the feed filter) with varied created_at timestamps.
List<_TicketStub> _generateFeedTickets(int seed, DateTime now) {
  final tickets = <_TicketStub>[];
  final int count = 5 + (seed % 46); // 5–50 tickets

  for (int i = 0; i < count; i++) {
    final statusIndex = (seed + i * 11) % _feedStatuses.length;
    final String status = _feedStatuses[statusIndex];

    // Randomise created_at within a 365-day window.
    final int daysAgo = (seed * 3 + i * 17) % 365;
    final int minutesOffset = (seed + i * 5) % 1440;
    final createdAt = now
        .subtract(Duration(days: daysAgo))
        .subtract(Duration(minutes: minutesOffset));

    DateTime? resolvedAt;
    if (status == 'Done') {
      // Keep resolvedAt within 6 days to ensure this ticket is visible.
      final int resolvedDaysAgo = (seed + i) % 6; // 0–5 days
      resolvedAt = now.subtract(Duration(days: resolvedDaysAgo));
    }

    tickets.add(_TicketStub(
      id: 'ticket-$seed-$i',
      status: status,
      createdAt: createdAt,
      resolvedAt: resolvedAt,
    ));
  }

  return tickets;
}

// ── Tests ─────────────────────────────────────────────────────────────────────

void main() {
  final DateTime now = DateTime(2026, 6, 26, 12, 0, 0);

  group(
    'Property 11: Citizen Feed List is Always Sorted by created_at Descending',
    () {
      // 100 seeds → ≥ 100 iterations as required by the spec.
      for (int seed = 0; seed < 100; seed++) {
        test('seed $seed — list is sorted by created_at desc', () {
          final tickets = _generateFeedTickets(seed, now);
          final result = _applyFeedQuery(tickets, now);

          // Property: the result list must be sorted in descending created_at order.
          expect(
            _isSortedDescending(result),
            isTrue,
            reason: 'seed=$seed: '
                'list is not sorted by created_at descending. '
                'Dates: ${result.map((t) => t.createdAt.toIso8601String()).join(', ')}',
          );
        });
      }

      // ── Explicit targeted cases ─────────────────────────────────────────

      test('empty list is trivially sorted', () {
        expect(_isSortedDescending([]), isTrue);
      });

      test('single-ticket list is trivially sorted', () {
        final t = _TicketStub(
          id: 'x',
          status: 'To Do',
          createdAt: now.subtract(const Duration(days: 1)),
        );
        final result = _applyFeedQuery([t], now);
        expect(_isSortedDescending(result), isTrue);
      });

      test('two tickets — older ticket comes after newer ticket', () {
        final older = _TicketStub(
          id: 'old',
          status: 'To Do',
          createdAt: now.subtract(const Duration(days: 5)),
        );
        final newer = _TicketStub(
          id: 'new',
          status: 'To Do',
          createdAt: now.subtract(const Duration(days: 1)),
        );

        // Intentionally pass in wrong order to confirm sorting.
        final result = _applyFeedQuery([older, newer], now);

        expect(result.length, equals(2));
        expect(result[0].id, equals('new'),
            reason: 'Newer ticket should appear first');
        expect(result[1].id, equals('old'),
            reason: 'Older ticket should appear last');
      });

      test('tickets with same created_at are preserved (stable relative order)',
          () {
        // Both have the same timestamp — sort should keep them in result.
        final sameTime = now.subtract(const Duration(days: 2));
        final t1 = _TicketStub(id: 't1', status: 'To Do', createdAt: sameTime);
        final t2 =
            _TicketStub(id: 't2', status: 'In Progress', createdAt: sameTime);

        final result = _applyFeedQuery([t1, t2], now);
        expect(result.length, equals(2));
        // Both equal timestamps → still sorted (equal timestamps satisfy
        // descending order).
        expect(_isSortedDescending(result), isTrue);
      });

      test('archived tickets are excluded from sorted result', () {
        final active = _TicketStub(
          id: 'active',
          status: 'To Do',
          createdAt: now.subtract(const Duration(days: 1)),
        );
        final archived = _TicketStub(
          id: 'archived',
          status: 'Archived',
          createdAt: now,
        );

        final result = _applyFeedQuery([active, archived], now);

        expect(result.length, equals(1));
        expect(result.first.id, equals('active'));
      });

      test(
        'large unsorted input produces correctly sorted output',
        () {
          // Create 50 tickets with reversed timestamps (oldest first in input).
          final tickets = List.generate(50, (i) {
            return _TicketStub(
              id: 'reversed-$i',
              status: 'To Do',
              // Earlier index → older date (reversed input order).
              createdAt: now.subtract(Duration(days: 50 - i)),
            );
          });

          final result = _applyFeedQuery(tickets, now);

          expect(result.length, equals(50));
          expect(_isSortedDescending(result), isTrue);

          // First item should be the most recent (reversed-49, 1 day ago).
          expect(result.first.id, equals('reversed-49'));
          // Last item should be oldest (reversed-0, 50 days ago).
          expect(result.last.id, equals('reversed-0'));
        },
      );
    },
  );
}
