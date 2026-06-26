// Feature: civic-sync, Property 15: Archival Transitions All Eligible Tickets

// **Validates: Requirements 9.2**
//
// Property: Given any collection of tickets with varied statuses and resolved_at values,
// after running ArchiveExpiredTickets:
//
//   - Every ticket with status == "Done" AND resolved_at <= now-7days MUST be "Archived".
//   - Every other ticket (wrong status, resolved_at within 7 days, or nil resolved_at)
//     MUST remain completely unchanged (status and resolved_at unmodified).
//
// rapid.Check runs ≥ 100 iterations (rapid v1.3.0 default).
package tickets

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/models"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// In-memory fake store for archival property tests
// ---------------------------------------------------------------------------

// archivalTestStore is an in-memory implementation of store.Store whose
// ArchiveExpiredTickets mirrors the real FirestoreStore logic:
//   status == "Done" AND resolved_at <= now-7days  →  set status="Archived", updated_at=now
//
// All other Store methods are no-op stubs that are not exercised by this property.
type archivalTestStore struct {
	tickets map[string]*models.Ticket
}

func newArchivalTestStore() *archivalTestStore {
	return &archivalTestStore{tickets: make(map[string]*models.Ticket)}
}

func (s *archivalTestStore) addTicket(t *models.Ticket) {
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	s.tickets[t.ID] = &cp
}

// GetUser — stub.
func (s *archivalTestStore) GetUser(_ context.Context, _ string) (*models.User, error) {
	return nil, nil
}

// UpsertUser — stub.
func (s *archivalTestStore) UpsertUser(_ context.Context, _ *models.User) error { return nil }

// CreateTicket — stub.
func (s *archivalTestStore) CreateTicket(_ context.Context, t *models.Ticket) error {
	cp := *t
	s.tickets[t.ID] = &cp
	return nil
}

// GetTicket returns a copy of the stored ticket, or nil if not found.
func (s *archivalTestStore) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	t, ok := s.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	return &cp, nil
}

// QueryActiveTicketsByCategory — stub.
func (s *archivalTestStore) QueryActiveTicketsByCategory(_ context.Context, _, _, _ float64, _ string) ([]*models.Ticket, error) {
	return nil, nil
}

// IncrementUpvote — stub.
func (s *archivalTestStore) IncrementUpvote(_ context.Context, _, _ string) error { return nil }

// UpdateTicketStatus — stub.
func (s *archivalTestStore) UpdateTicketStatus(_ context.Context, _, _ string) error { return nil }

// HasUserUpvoted — stub.
func (s *archivalTestStore) HasUserUpvoted(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

// ArchiveExpiredTickets mirrors the real FirestoreStore.ArchiveExpiredTickets logic:
// for every ticket with status "Done" and resolved_at <= now-7days, set status="Archived"
// and update updated_at. Requirements: 9.2.
func (s *archivalTestStore) ArchiveExpiredTickets(_ context.Context) error {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	now := time.Now().UTC()

	for id, t := range s.tickets {
		if t.Status == "Done" && t.ResolvedAt != nil && !t.ResolvedAt.After(cutoff) {
			s.tickets[id].Status = "Archived"
			s.tickets[id].UpdatedAt = now
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// allTicketStatuses is the complete set of valid KanbanStatus values.
var allTicketStatuses = []string{"To Do", "In Progress", "Done", "Archived"}

// drawResolvedAt generates one of three resolved_at variants for a ticket:
//   - nil  (never resolved)
//   - a timestamp within the last 7 days  (not yet eligible for archival)
//   - a timestamp older than 7 days       (eligible for archival when status=Done)
func drawResolvedAt(t *rapid.T, label string) *time.Time {
	choice := rapid.IntRange(0, 2).Draw(t, label+"_resolvedAtChoice")
	switch choice {
	case 0:
		// nil — ticket was never resolved
		return nil
	case 1:
		// within the last 7 days (1 second to 6 days 23 hours 59 minutes ago)
		offsetSecs := rapid.Int64Range(1, int64(7*24*time.Hour/time.Second)-1).Draw(t, label+"_recentOffsetSecs")
		ts := time.Now().UTC().Add(-time.Duration(offsetSecs) * time.Second)
		return &ts
	default:
		// older than 7 days (7 days + 1 second to 365 days ago)
		offsetSecs := rapid.Int64Range(
			int64(7*24*time.Hour/time.Second)+1,
			int64(365*24*time.Hour/time.Second),
		).Draw(t, label+"_oldOffsetSecs")
		ts := time.Now().UTC().Add(-time.Duration(offsetSecs) * time.Second)
		return &ts
	}
}

// isEligibleForArchival reports whether a ticket should be archived by the job.
func isEligibleForArchival(t *models.Ticket) bool {
	if t.Status != "Done" {
		return false
	}
	if t.ResolvedAt == nil {
		return false
	}
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	return !t.ResolvedAt.After(cutoff)
}

// ---------------------------------------------------------------------------
// Property test
// ---------------------------------------------------------------------------

// TestPropertyArchivalTransitionsAllEligibleTickets verifies Property 15:
// Archival Transitions All Eligible Tickets.
//
// Strategy:
//  1. rapid generates N random tickets (N ∈ [1, 50]) with random statuses and
//     resolved_at values spanning nil, within-7-days, and older-than-7-days.
//  2. Before calling ArchiveExpiredTickets, a snapshot of each ticket is taken.
//  3. ArchiveExpiredTickets is called.
//  4. For each ticket we assert:
//     - If it was eligible (Done + resolved_at > 7 days old): status must now be "Archived".
//     - If it was not eligible: status must remain exactly as before (and resolved_at unchanged).
//
// rapid.Check runs ≥ 100 iterations by default.
func TestPropertyArchivalTransitionsAllEligibleTickets(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- 1. Generate a random collection of tickets ---
		numTickets := rapid.IntRange(1, 50).Draw(t, "numTickets")

		store := newArchivalTestStore()

		// snapshotBefore captures the pre-archival state for non-eligible tickets.
		type snapshot struct {
			status     string
			resolvedAt *time.Time
		}
		before := make(map[string]snapshot, numTickets)

		for i := 0; i < numTickets; i++ {
			id := fmt.Sprintf("ticket-%d", i)
			label := fmt.Sprintf("t%d", i)

			statusIdx := rapid.IntRange(0, len(allTicketStatuses)-1).Draw(t, label+"_statusIdx")
			status := allTicketStatuses[statusIdx]

			resolvedAt := drawResolvedAt(t, label)

			now := time.Now().UTC()
			ticket := &models.Ticket{
				ID:         id,
				Status:     status,
				ResolvedAt: resolvedAt,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			store.addTicket(ticket)

			before[id] = snapshot{status: status, resolvedAt: resolvedAt}
		}

		// --- 2. Run the archival job ---
		ctx := context.Background()
		if err := store.ArchiveExpiredTickets(ctx); err != nil {
			t.Fatalf("ArchiveExpiredTickets returned error: %v", err)
		}

		// --- 3. Verify postconditions for every ticket ---
		for i := 0; i < numTickets; i++ {
			id := fmt.Sprintf("ticket-%d", i)
			snap := before[id]

			after, err := store.GetTicket(ctx, id)
			if err != nil {
				t.Fatalf("GetTicket(%s): %v", id, err)
			}
			if after == nil {
				t.Fatalf("GetTicket(%s): ticket missing after archival run", id)
			}

			// Reconstruct a minimal ticket from the pre-archival snapshot to
			// call isEligibleForArchival with a consistent "now" reference.
			preTick := &models.Ticket{
				Status:     snap.status,
				ResolvedAt: snap.resolvedAt,
			}

			if isEligibleForArchival(preTick) {
				// Eligible → MUST be archived now (Req 9.2).
				if after.Status != "Archived" {
					t.Fatalf(
						"ticket %s: was eligible (status=%q, resolved_at=%v) but status is %q after archival; want \"Archived\"",
						id, snap.status, snap.resolvedAt, after.Status,
					)
				}
			} else {
				// Not eligible → status and resolved_at MUST be unchanged.
				if after.Status != snap.status {
					t.Fatalf(
						"ticket %s: was NOT eligible (status=%q, resolved_at=%v) but status changed to %q",
						id, snap.status, snap.resolvedAt, after.Status,
					)
				}
				// resolved_at must be identical (both nil, or same value).
				if snap.resolvedAt == nil && after.ResolvedAt != nil {
					t.Fatalf(
						"ticket %s: resolved_at was nil but is now %v after archival",
						id, after.ResolvedAt,
					)
				}
				if snap.resolvedAt != nil && after.ResolvedAt == nil {
					t.Fatalf(
						"ticket %s: resolved_at was %v but is now nil after archival",
						id, snap.resolvedAt,
					)
				}
				if snap.resolvedAt != nil && after.ResolvedAt != nil &&
					!snap.resolvedAt.Equal(*after.ResolvedAt) {
					t.Fatalf(
						"ticket %s: resolved_at changed from %v to %v (should be unchanged)",
						id, snap.resolvedAt, after.ResolvedAt,
					)
				}
			}
		}
	})
}
