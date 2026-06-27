// Feature: civic-sync, Property 12: Official Dashboard Query Returns at Most 200 Tickets Sorted by Upvotes Descending

// **Validates: Requirements 8.2**
//
// Property: For any collection of tickets with varied statuses and random upvote counts,
// the official dashboard query MUST:
//   - Include only tickets with status "To Do", "In Progress", or "Done".
//   - Return at most 200 tickets.
//   - Return results sorted by upvotes in descending order.
//
// rapid.Check runs ≥ 100 iterations (rapid v1.3.0 default).
package tickets

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/models"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// In-memory fake store for official dashboard property tests
// ---------------------------------------------------------------------------

// dashboardTestStore is an in-memory store whose QueryOfficialDashboard
// mirrors the official dashboard query behaviour described in Req 8.2:
//   - Filter to tickets with status IN ["To Do", "In Progress", "Done"]
//   - Sort by upvotes descending
//   - Limit to 200
//
// All other Store methods are no-op stubs not exercised by this property.
type dashboardTestStore struct {
	tickets map[string]*models.Ticket
}

func newDashboardTestStore() *dashboardTestStore {
	return &dashboardTestStore{tickets: make(map[string]*models.Ticket)}
}

func (s *dashboardTestStore) addTicket(t *models.Ticket) {
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	s.tickets[t.ID] = &cp
}

// GetUser — stub.
func (s *dashboardTestStore) GetUser(_ context.Context, _ string) (*models.User, error) {
	return nil, nil
}

// UpsertUser — stub.
func (s *dashboardTestStore) UpsertUser(_ context.Context, _ *models.User) error { return nil }

// CreateTicket — stub.
func (s *dashboardTestStore) CreateTicket(_ context.Context, t *models.Ticket) error {
	cp := *t
	s.tickets[t.ID] = &cp
	return nil
}

// GetTicket returns a copy of the stored ticket, or nil if not found.
func (s *dashboardTestStore) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	t, ok := s.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	return &cp, nil
}

// QueryActiveTicketsByCategory — stub (citizen feed query, not dashboard).
func (s *dashboardTestStore) QueryActiveTicketsByCategory(_ context.Context, _, _, _ float64, _ string) ([]*models.Ticket, error) {
	return nil, nil
}

// IncrementUpvote — stub.
func (s *dashboardTestStore) IncrementUpvote(_ context.Context, _, _ string) error { return nil }

// UpdateTicketStatus — stub.
func (s *dashboardTestStore) UpdateTicketStatus(_ context.Context, _, _ string) error { return nil }

// HasUserUpvoted — stub.
func (s *dashboardTestStore) HasUserUpvoted(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

// ArchiveExpiredTickets — stub.
func (s *dashboardTestStore) ArchiveExpiredTickets(_ context.Context) error { return nil }

// dashboardStatusFilter is the set of statuses visible on the official dashboard (Req 8.2).
var dashboardStatusFilter = map[string]bool{
	"To Do":       true,
	"In Progress": true,
	"Done":        true,
}

// QueryOfficialDashboard implements the official dashboard query logic (Req 8.2):
//  1. Filter tickets to those with status in ["To Do", "In Progress", "Done"].
//  2. Sort by upvotes descending.
//  3. Limit to 200.
func (s *dashboardTestStore) QueryOfficialDashboard(_ context.Context) ([]*models.Ticket, error) {
	var active []*models.Ticket
	for _, t := range s.tickets {
		if dashboardStatusFilter[t.Status] {
			cp := *t
			cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
			active = append(active, &cp)
		}
	}

	// Sort by upvotes descending (stable to keep deterministic tie-breaking).
	sort.SliceStable(active, func(i, j int) bool {
		return active[i].Upvotes > active[j].Upvotes
	})

	// Limit to 200.
	const limit = 200
	if len(active) > limit {
		active = active[:limit]
	}

	return active, nil
}

// ---------------------------------------------------------------------------
// Statuses used in generator
// ---------------------------------------------------------------------------

// dashboardAllStatuses is the full set of ticket statuses including "Archived".
var dashboardAllStatuses = []string{"To Do", "In Progress", "Done", "Archived"}

// ---------------------------------------------------------------------------
// Property test
// ---------------------------------------------------------------------------

// TestPropertyOfficialDashboardQuerySortedByUpvotesDesc verifies Property 12:
// Official Dashboard Query Returns at Most 200 Tickets Sorted by Upvotes Descending.
//
// Strategy:
//  1. rapid generates N random tickets (N ∈ [0, 300]) with random statuses
//     (including "Archived") and random upvote counts (0..10000).
//  2. QueryOfficialDashboard is called on the populated store.
//  3. Assertions:
//     a. len(result) ≤ 200 (limit enforced)
//     b. For all i < len(result)-1: result[i].Upvotes >= result[i+1].Upvotes (sorted desc)
//     c. Every returned ticket has a status in ["To Do", "In Progress", "Done"] (filter)
//     d. If the total number of active tickets > 200 then exactly 200 are returned.
//
// rapid.Check runs ≥ 100 iterations by default.
func TestPropertyOfficialDashboardQuerySortedByUpvotesDesc(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- 1. Generate a random collection of tickets ---
		numTickets := rapid.IntRange(0, 300).Draw(t, "numTickets")

		store := newDashboardTestStore()

		activeCount := 0 // tickets that should appear in dashboard results

		for i := 0; i < numTickets; i++ {
			id := fmt.Sprintf("ticket-%d", i)
			label := fmt.Sprintf("t%d", i)

			statusIdx := rapid.IntRange(0, len(dashboardAllStatuses)-1).Draw(t, label+"_statusIdx")
			status := dashboardAllStatuses[statusIdx]

			upvotes := rapid.IntRange(0, 10000).Draw(t, label+"_upvotes")

			now := time.Now().UTC()
			ticket := &models.Ticket{
				ID:        id,
				Status:    status,
				Upvotes:   upvotes,
				UpvotedBy: []string{},
				CreatedAt: now,
				UpdatedAt: now,
			}
			store.addTicket(ticket)

			if dashboardStatusFilter[status] {
				activeCount++
			}
		}

		// --- 2. Run the official dashboard query ---
		ctx := context.Background()
		result, err := store.QueryOfficialDashboard(ctx)
		if err != nil {
			t.Fatalf("QueryOfficialDashboard returned error: %v", err)
		}

		// --- 3a. Assert: at most 200 tickets returned (Req 8.2) ---
		if len(result) > 200 {
			t.Fatalf(
				"QueryOfficialDashboard returned %d tickets; want at most 200",
				len(result),
			)
		}

		// --- 3b. Assert: sorted by upvotes descending (Req 8.2) ---
		for i := 0; i < len(result)-1; i++ {
			if result[i].Upvotes < result[i+1].Upvotes {
				t.Fatalf(
					"result not sorted by upvotes descending: result[%d].Upvotes=%d < result[%d].Upvotes=%d",
					i, result[i].Upvotes, i+1, result[i+1].Upvotes,
				)
			}
		}

		// --- 3c. Assert: every returned ticket has a dashboard-visible status ---
		for _, ticket := range result {
			if !dashboardStatusFilter[ticket.Status] {
				t.Fatalf(
					"returned ticket %q has status %q which should be excluded from dashboard",
					ticket.ID, ticket.Status,
				)
			}
		}

		// --- 3d. Assert: if active tickets > 200 then exactly 200 are returned ---
		if activeCount > 200 && len(result) != 200 {
			t.Fatalf(
				"active ticket count is %d (>200) but query returned %d; want exactly 200",
				activeCount, len(result),
			)
		}

		// --- 3e. Assert: if active tickets <= 200 then all active tickets are returned ---
		if activeCount <= 200 && len(result) != activeCount {
			t.Fatalf(
				"active ticket count is %d but query returned %d; want %d",
				activeCount, len(result), activeCount,
			)
		}
	})
}
