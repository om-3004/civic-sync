// Feature: civic-sync, Property 14: resolved_at is Set Exactly When Status Transitions to Done

// **Validates: Requirements 9.1**
//
// Property: For every ticket with a valid non-Archived status:
//
//   - When status transitions to "Done" (In Progress → Done):
//     resolved_at must be non-nil and within 5 seconds of the transition time.
//
//   - For all other valid transitions (To Do → In Progress):
//     resolved_at must remain nil / unchanged.
//
// rapid.Check runs ≥ 100 iterations (rapid v1.3.0 default).
package tickets

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/auth"
	"github.com/civic-sync/civic-sync/internal/models"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// In-memory fake store for resolved_at tests
// ---------------------------------------------------------------------------

// resolvedAtTestStore is a minimal in-memory store for Property 14.
// It reuses the same shape as statusTestStore but lives in this file to keep
// the test self-contained.
type resolvedAtTestStore struct {
	tickets     map[string]*models.Ticket
	officialUID string
}

func newResolvedAtTestStore(officialUID string) *resolvedAtTestStore {
	return &resolvedAtTestStore{
		tickets:     make(map[string]*models.Ticket),
		officialUID: officialUID,
	}
}

func (s *resolvedAtTestStore) addTicket(t *models.Ticket) {
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	s.tickets[t.ID] = &cp
}

func (s *resolvedAtTestStore) GetUser(_ context.Context, uid string) (*models.User, error) {
	if uid == s.officialUID {
		return &models.User{UID: uid, Role: "official"}, nil
	}
	return &models.User{UID: uid, Role: "citizen"}, nil
}

func (s *resolvedAtTestStore) UpsertUser(_ context.Context, _ *models.User) error { return nil }

func (s *resolvedAtTestStore) CreateTicket(_ context.Context, t *models.Ticket) error {
	cp := *t
	s.tickets[t.ID] = &cp
	return nil
}

func (s *resolvedAtTestStore) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	t, ok := s.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	return &cp, nil
}

func (s *resolvedAtTestStore) QueryActiveTicketsByCategory(_ context.Context, _, _, _ float64, _ string) ([]*models.Ticket, error) {
	return nil, nil
}

func (s *resolvedAtTestStore) IncrementUpvote(_ context.Context, _, _ string) error { return nil }

// UpdateTicketStatus mirrors the real FirestoreStore behaviour:
// sets status and updated_at, and atomically sets resolved_at when transitioning to Done.
func (s *resolvedAtTestStore) UpdateTicketStatus(_ context.Context, ticketID, newStatus string) error {
	t, ok := s.tickets[ticketID]
	if !ok {
		return nil
	}
	t.Status = newStatus
	t.UpdatedAt = time.Now().UTC()
	if newStatus == "Done" {
		now := time.Now().UTC()
		t.ResolvedAt = &now
	}
	return nil
}

func (s *resolvedAtTestStore) ArchiveExpiredTickets(_ context.Context) error { return nil }

func (s *resolvedAtTestStore) HasUserUpvoted(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

// ---------------------------------------------------------------------------
// Property test
// ---------------------------------------------------------------------------

// TestProperty14ResolvedAtOnDoneTransition verifies Property 14:
// resolved_at is Set Exactly When Status Transitions to Done.
//
// Strategy:
//  1. rapid generates a random official UID string.
//  2. For the "Done transition" branch: create a ticket with status "In Progress",
//     record the time just before the transition, fire PUT /tickets/:id/status → "Done",
//     and assert resolved_at is non-nil and within 5 s of the recorded time.
//  3. For the "non-Done transition" branch: create a ticket with status "To Do",
//     fire PUT /tickets/:id/status → "In Progress",
//     and assert resolved_at remains nil.
func TestProperty14ResolvedAtOnDoneTransition(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- 1. Generate a random official UID ---
		officialUID := rapid.StringMatching(`[a-zA-Z0-9]{8,32}`).Draw(t, "officialUID")

		// ---------------------------------------------------------------
		// Branch A: In Progress → Done  (resolved_at must be set)
		// ---------------------------------------------------------------
		{
			st := newResolvedAtTestStore(officialUID)
			now := time.Now().UTC()
			ticket := &models.Ticket{
				ID:         "ticket-inprogress-to-done",
				Category:   "Roads",
				Title:      "Pothole on Main St",
				Status:     "In Progress",
				Upvotes:    0,
				UpvotedBy:  []string{},
				CreatedAt:  now,
				UpdatedAt:  now,
				ResolvedAt: nil, // must be nil before the transition
			}
			st.addTicket(ticket)

			handler := NewUpdateTicketStatusHandler(st)

			// Record the wall-clock instant just before the HTTP call.
			before := time.Now().UTC()

			body, _ := json.Marshal(map[string]string{"status": "Done"})
			req := httptest.NewRequest("PUT", "/tickets/ticket-inprogress-to-done/status", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", "ticket-inprogress-to-done")
			ctx := auth.WithClaims(req.Context(), officialUID, officialUID+"@civic.test", "Test Official")
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			after := time.Now().UTC()

			if w.Code != 200 {
				t.Fatalf("In Progress→Done: expected HTTP 200, got %d (body: %s)",
					w.Code, w.Body.String())
			}

			// Retrieve the updated ticket from the store.
			updated, err := st.GetTicket(context.Background(), "ticket-inprogress-to-done")
			if err != nil || updated == nil {
				t.Fatalf("In Progress→Done: GetTicket after transition: %v", err)
			}

			// resolved_at must be non-nil (Req 9.1).
			if updated.ResolvedAt == nil {
				t.Fatal("In Progress→Done: resolved_at is nil after Done transition; expected a timestamp")
			}

			// resolved_at must be within 5 s of the transition wall-clock window.
			ra := *updated.ResolvedAt
			if ra.Before(before.Add(-5*time.Second)) || ra.After(after.Add(5*time.Second)) {
				t.Fatalf(
					"In Progress→Done: resolved_at %v is not within 5 s of transition window [%v, %v]",
					ra, before, after,
				)
			}
		}

		// ---------------------------------------------------------------
		// Branch B: To Do → In Progress  (resolved_at must remain nil)
		// ---------------------------------------------------------------
		{
			st := newResolvedAtTestStore(officialUID)
			now := time.Now().UTC()
			ticket := &models.Ticket{
				ID:         "ticket-todo-to-inprogress",
				Category:   "Lighting",
				Title:      "Broken streetlight",
				Status:     "To Do",
				Upvotes:    0,
				UpvotedBy:  []string{},
				CreatedAt:  now,
				UpdatedAt:  now,
				ResolvedAt: nil,
			}
			st.addTicket(ticket)

			handler := NewUpdateTicketStatusHandler(st)

			body, _ := json.Marshal(map[string]string{"status": "In Progress"})
			req := httptest.NewRequest("PUT", "/tickets/ticket-todo-to-inprogress/status", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", "ticket-todo-to-inprogress")
			ctx := auth.WithClaims(req.Context(), officialUID, officialUID+"@civic.test", "Test Official")
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != 200 {
				t.Fatalf("To Do→In Progress: expected HTTP 200, got %d (body: %s)",
					w.Code, w.Body.String())
			}

			// Retrieve the updated ticket from the store.
			updated, err := st.GetTicket(context.Background(), "ticket-todo-to-inprogress")
			if err != nil || updated == nil {
				t.Fatalf("To Do→In Progress: GetTicket after transition: %v", err)
			}

			// resolved_at must remain nil for non-Done transitions (Req 9.1).
			if updated.ResolvedAt != nil {
				t.Fatalf("To Do→In Progress: resolved_at must be nil after non-Done transition, got %v",
					*updated.ResolvedAt)
			}
		}
	})
}
