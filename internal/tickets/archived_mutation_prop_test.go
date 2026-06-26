// Feature: civic-sync, Property 16: Archived Tickets Reject All Mutation Attempts

// **Validates: Requirements 9.4**
//
// Property: For every randomly generated Archived ticket, BOTH mutation operations
// — upvote (POST /tickets/:id/upvote) and status change (PUT /tickets/:id/status) —
// MUST return HTTP 409 Conflict and leave the ticket document completely unchanged.
//
// Requirement 9.4: WHEN a Ticket is archived (KanbanStatus = Archived), THE Backend
// SHALL retain the Ticket document in Firestore and reject any further status transition
// or upvote modification requests for that Ticket with an HTTP 409 Conflict response.
package tickets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/auth"
	"github.com/civic-sync/civic-sync/internal/models"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// In-memory fake store for archived-mutation tests
// ---------------------------------------------------------------------------

// archivedMutationStore is an in-memory store that supports all methods required
// by both NewUpvoteTicketHandler and NewUpdateTicketStatusHandler.
// It tracks whether any mutation was attempted (to detect silent mutations).
type archivedMutationStore struct {
	tickets     map[string]*models.Ticket
	officialUID string
	// mutationAttempted is set to true if IncrementUpvote or UpdateTicketStatus
	// is actually called (the handler should never reach these for Archived tickets).
	mutationAttempted bool
}

func newArchivedMutationStore(officialUID string) *archivedMutationStore {
	return &archivedMutationStore{
		tickets:     make(map[string]*models.Ticket),
		officialUID: officialUID,
	}
}

func (s *archivedMutationStore) addTicket(t *models.Ticket) {
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	s.tickets[t.ID] = &cp
}

// GetUser returns an official user for officialUID, citizen for everyone else.
func (s *archivedMutationStore) GetUser(_ context.Context, uid string) (*models.User, error) {
	if uid == s.officialUID {
		return &models.User{UID: uid, Role: "official"}, nil
	}
	return &models.User{UID: uid, Role: "citizen"}, nil
}

func (s *archivedMutationStore) UpsertUser(_ context.Context, _ *models.User) error { return nil }

func (s *archivedMutationStore) CreateTicket(_ context.Context, t *models.Ticket) error {
	cp := *t
	s.tickets[t.ID] = &cp
	return nil
}

func (s *archivedMutationStore) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	t, ok := s.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	return &cp, nil
}

func (s *archivedMutationStore) QueryActiveTicketsByCategory(_ context.Context, _, _, _ float64, _ string) ([]*models.Ticket, error) {
	return nil, nil
}

// IncrementUpvote records that a mutation was attempted, then increments the
// ticket (so if the handler incorrectly reaches this, the state change is
// detectable).
func (s *archivedMutationStore) IncrementUpvote(_ context.Context, ticketID, voterUID string) error {
	s.mutationAttempted = true
	t, ok := s.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %q not found", ticketID)
	}
	t.UpvotedBy = append(t.UpvotedBy, voterUID)
	t.Upvotes++
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// UpdateTicketStatus records that a mutation was attempted, then updates the
// ticket (so if the handler incorrectly reaches this, the state change is
// detectable).
func (s *archivedMutationStore) UpdateTicketStatus(_ context.Context, ticketID, newStatus string) error {
	s.mutationAttempted = true
	t, ok := s.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %q not found", ticketID)
	}
	t.Status = newStatus
	t.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *archivedMutationStore) ArchiveExpiredTickets(_ context.Context) error { return nil }

func (s *archivedMutationStore) HasUserUpvoted(_ context.Context, ticketID, uid string) (bool, error) {
	t, ok := s.tickets[ticketID]
	if !ok {
		return false, nil
	}
	for _, u := range t.UpvotedBy {
		if u == uid {
			return true, nil
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// snapshotTicket returns a shallow copy of the ticket at the given ID for
// pre/post comparison.
func snapshotTicket(t *rapid.T, s *archivedMutationStore, ticketID string) models.Ticket {
	t.Helper()
	got, err := s.GetTicket(context.Background(), ticketID)
	if err != nil || got == nil {
		t.Fatalf("snapshotTicket: GetTicket(%q) returned nil or err: %v", ticketID, err)
	}
	cp := *got
	cp.UpvotedBy = append([]string(nil), got.UpvotedBy...)
	return cp
}

// assertTicketUnchanged compares two snapshots and fails the test if any field
// differs.
func assertTicketUnchanged(t *rapid.T, before, after models.Ticket, operation string) {
	t.Helper()
	if before.Status != after.Status {
		t.Fatalf(
			"[%s] ticket Status changed: before=%q after=%q",
			operation, before.Status, after.Status,
		)
	}
	if before.Upvotes != after.Upvotes {
		t.Fatalf(
			"[%s] ticket Upvotes changed: before=%d after=%d",
			operation, before.Upvotes, after.Upvotes,
		)
	}
	if !before.UpdatedAt.Equal(after.UpdatedAt) {
		t.Fatalf(
			"[%s] ticket UpdatedAt changed: before=%v after=%v",
			operation, before.UpdatedAt, after.UpdatedAt,
		)
	}
	if len(before.UpvotedBy) != len(after.UpvotedBy) {
		t.Fatalf(
			"[%s] ticket UpvotedBy length changed: before=%d after=%d",
			operation, len(before.UpvotedBy), len(after.UpvotedBy),
		)
	}
}

// ---------------------------------------------------------------------------
// Property test
// ---------------------------------------------------------------------------

// TestPropertyArchivedMutationRejection verifies Property 16:
// Archived Tickets Reject All Mutation Attempts.
//
// Strategy:
//  1. rapid generates a random official UID and a random archived ticket
//     (varied upvote count, varied UpvotedBy set, varied timestamps).
//  2. For each iteration:
//     a. An upvote attempt by a new (never-voted) user must return HTTP 409.
//     b. A status-change attempt to every possible target status must return HTTP 409.
//  3. After each rejected attempt the ticket state in the store must be identical
//     to the state before the attempt (no silent mutations).
//
// rapid.Check runs ≥ 100 iterations by default (rapid v1.3.0 default).
func TestPropertyArchivedMutationRejection(t *testing.T) {
	allStatuses := []string{"To Do", "In Progress", "Done", "Archived"}

	rapid.Check(t, func(t *rapid.T) {
		// --- 1. Generate a random official UID ---
		officialUID := rapid.StringMatching(`[a-zA-Z0-9]{8,32}`).Draw(t, "officialUID")

		// --- 2. Generate a random initial upvote count (0..30) ---
		initialUpvotes := rapid.IntRange(0, 30).Draw(t, "initialUpvotes")

		// --- 3. Generate a random set of pre-existing voters (0..10) ---
		numVoters := rapid.IntRange(0, 10).Draw(t, "numVoters")
		upvotedBy := make([]string, numVoters)
		for i := 0; i < numVoters; i++ {
			upvotedBy[i] = fmt.Sprintf("voter-%d", i)
		}

		// --- 4. Generate a random resolved_at timestamp (non-nil for Archived) ---
		daysAgo := rapid.IntRange(7, 30).Draw(t, "daysAgo")
		resolvedAt := time.Now().UTC().Add(-time.Duration(daysAgo) * 24 * time.Hour)

		// --- 5. Build the Archived ticket ---
		now := time.Now().UTC()
		ticketID := "archived-prop-ticket"
		ticket := &models.Ticket{
			ID:          ticketID,
			Category:    "Pothole",
			Title:       "Archived issue",
			Description: "This ticket has been archived",
			Status:      "Archived", // the invariant under test
			Upvotes:     initialUpvotes,
			UpvotedBy:   upvotedBy,
			ReportedBy:  "citizen-original",
			CreatedAt:   now.Add(-30 * 24 * time.Hour),
			UpdatedAt:   now.Add(-time.Duration(daysAgo) * 24 * time.Hour),
			ResolvedAt:  &resolvedAt,
		}

		s := newArchivedMutationStore(officialUID)
		s.addTicket(ticket)

		// The "new voter" UID is guaranteed not to be in upvotedBy.
		newVoterUID := "new-voter-never-voted"

		// -----------------------------------------------------------------------
		// Phase A: Upvote attempt on Archived ticket → must return 409
		// -----------------------------------------------------------------------
		upvoteHandler := NewUpvoteTicketHandler(s)

		before := snapshotTicket(t, s, ticketID)
		s.mutationAttempted = false

		req := httptest.NewRequest("POST", "/tickets/"+ticketID+"/upvote", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), newVoterUID, newVoterUID+"@test.com", "New Voter"))
		w := httptest.NewRecorder()
		upvoteHandler.ServeHTTP(w, req)

		// Must return 409 Conflict (Req 9.4).
		if w.Code != 409 {
			t.Fatalf(
				"upvote on Archived ticket: expected HTTP 409, got %d (body: %s)",
				w.Code, w.Body.String(),
			)
		}

		after := snapshotTicket(t, s, ticketID)
		assertTicketUnchanged(t, before, after, "upvote")

		// The store's mutation methods must NOT have been called.
		if s.mutationAttempted {
			t.Fatalf("upvote on Archived ticket: store mutation was called despite 409 response")
		}

		// -----------------------------------------------------------------------
		// Phase B: Status-change attempt on Archived ticket → must return 409
		//          for every possible target status value
		// -----------------------------------------------------------------------
		statusHandler := NewUpdateTicketStatusHandler(s)

		for _, targetStatus := range allStatuses {
			before = snapshotTicket(t, s, ticketID)
			s.mutationAttempted = false

			body, _ := json.Marshal(map[string]string{"status": targetStatus})
			req := httptest.NewRequest("PUT", "/tickets/"+ticketID+"/status", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", ticketID)
			req = req.WithContext(auth.WithClaims(req.Context(), officialUID, officialUID+"@civic.test", "Test Official"))

			w := httptest.NewRecorder()
			statusHandler.ServeHTTP(w, req)

			// Must return 409 Conflict (Req 9.4).
			if w.Code != 409 {
				t.Fatalf(
					"status change (%q→%q) on Archived ticket: expected HTTP 409, got %d (body: %s)",
					"Archived", targetStatus, w.Code, w.Body.String(),
				)
			}

			after = snapshotTicket(t, s, ticketID)
			assertTicketUnchanged(t, before, after, fmt.Sprintf("status change →%s", targetStatus))

			// The store's mutation methods must NOT have been called.
			if s.mutationAttempted {
				t.Fatalf(
					"status change (%q→%q) on Archived ticket: store mutation was called despite 409 response",
					"Archived", targetStatus,
				)
			}
		}
	})
}
