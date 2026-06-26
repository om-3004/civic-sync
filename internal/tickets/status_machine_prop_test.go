// Feature: civic-sync, Property 13: Ticket Status Transitions Follow the Permitted State Machine

// **Validates: Requirements 8.5, 8.6, 9.4**
//
// Property: For every (currentStatus, targetStatus) pair drawn from
//
//	{"To Do", "In Progress", "Done", "Archived"}:
//
//	- ("To Do",       "In Progress") → HTTP 200 (valid forward transition)
//	- ("In Progress", "Done")        → HTTP 200 (valid forward transition)
//	- (currentStatus == "Archived",  any target) → HTTP 409 (archived tickets locked)
//	- all other pairs                → HTTP 400 (invalid/disallowed transition)
//
// The test also uses rapid to generate random UID strings for the calling
// official, exercising the role-check path with varied identities.
package tickets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/auth"
	"github.com/civic-sync/civic-sync/internal/models"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// In-memory fake store for status-machine tests
// ---------------------------------------------------------------------------

// statusTestStore is an in-memory store that supports all methods required
// by NewUpdateTicketStatusHandler:
//   - GetUser    — returns a user with role "official" for any UID (role-gate satisfied)
//   - GetTicket  — single-ticket lookup
//   - UpdateTicketStatus — mutates the in-memory ticket
//
// All other methods are no-op stubs.
type statusTestStore struct {
	tickets map[string]*models.Ticket
	// officialUID is the UID that should be treated as "official".
	// Any other UID is treated as "citizen" (→ 403).
	officialUID string
}

func newStatusTestStore(officialUID string) *statusTestStore {
	return &statusTestStore{
		tickets:     make(map[string]*models.Ticket),
		officialUID: officialUID,
	}
}

func (s *statusTestStore) addTicket(t *models.Ticket) {
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	s.tickets[t.ID] = &cp
}

// GetUser returns an official user for officialUID, nil for anyone else.
func (s *statusTestStore) GetUser(_ context.Context, uid string) (*models.User, error) {
	if uid == s.officialUID {
		return &models.User{UID: uid, Role: "official"}, nil
	}
	// Return a citizen so the handler correctly returns 403.
	return &models.User{UID: uid, Role: "citizen"}, nil
}

func (s *statusTestStore) UpsertUser(_ context.Context, _ *models.User) error { return nil }

func (s *statusTestStore) CreateTicket(_ context.Context, t *models.Ticket) error {
	cp := *t
	s.tickets[t.ID] = &cp
	return nil
}

func (s *statusTestStore) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	t, ok := s.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	return &cp, nil
}

func (s *statusTestStore) QueryActiveTicketsByCategory(_ context.Context, _, _, _ float64, _ string) ([]*models.Ticket, error) {
	return nil, nil
}

func (s *statusTestStore) IncrementUpvote(_ context.Context, _, _ string) error { return nil }

// UpdateTicketStatus mutates the in-memory ticket to reflect the new status,
// mirroring the real FirestoreStore behaviour (including resolved_at for Done).
func (s *statusTestStore) UpdateTicketStatus(_ context.Context, ticketID, newStatus string) error {
	t, ok := s.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %q not found", ticketID)
	}
	t.Status = newStatus
	t.UpdatedAt = time.Now().UTC()
	if newStatus == "Done" {
		now := time.Now().UTC()
		t.ResolvedAt = &now
	}
	return nil
}

func (s *statusTestStore) ArchiveExpiredTickets(_ context.Context) error { return nil }

func (s *statusTestStore) HasUserUpvoted(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// smAllStatuses enumerates the four valid ticket status values for the
// state-machine property test (avoids a package-level name collision with
// the declaration in duplicate_prop_test.go).
var smAllStatuses = []string{"To Do", "In Progress", "Done", "Archived"}

// expectedHTTPStatus returns the HTTP status code the handler must produce
// for a given (currentStatus, targetStatus) pair.
func expectedHTTPStatus(current, target string) int {
	if current == "Archived" {
		return 409
	}
	if current == "To Do" && target == "In Progress" {
		return 200
	}
	if current == "In Progress" && target == "Done" {
		return 200
	}
	return 400
}

// ---------------------------------------------------------------------------
// Property test
// ---------------------------------------------------------------------------

// TestPropertyStatusMachineTransitions verifies Property 13:
// Ticket Status Transitions Follow the Permitted State Machine.
//
// Strategy:
//  1. rapid generates a random official UID string (exercises role path with varied identities).
//  2. All 16 (currentStatus, targetStatus) combinations are exercised deterministically
//     inside each rapid iteration so the property covers the full state-machine matrix
//     on every run.
//  3. For each combination a fresh ticket is created and the status handler is invoked
//     via httptest; the response code is compared against the expected value.
//
// rapid.Check runs ≥ 100 iterations (rapid v1.3.0 default).
func TestPropertyStatusMachineTransitions(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- 1. Generate a random official UID ---
		// Using StringMatching to produce plausible UID strings (alphanumeric, 8-32 chars).
		officialUID := rapid.StringMatching(`[a-zA-Z0-9]{8,32}`).Draw(t, "officialUID")

		// --- 2. Iterate over all 16 (currentStatus, targetStatus) combinations ---
		for _, currentStatus := range smAllStatuses {
			for _, targetStatus := range smAllStatuses {
				// Each combination gets its own fresh store + handler so there
				// is no bleed-over between iterations.
				store := newStatusTestStore(officialUID)

				ticketID := fmt.Sprintf("ticket-%s-%s",
					strings.ReplaceAll(currentStatus, " ", "_"),
					strings.ReplaceAll(targetStatus, " ", "_"),
				)

				now := time.Now().UTC()
				ticket := &models.Ticket{
					ID:        ticketID,
					Category:  "Pothole",
					Title:     "Test ticket",
					Status:    currentStatus,
					Upvotes:   0,
					UpvotedBy: []string{},
					CreatedAt: now,
					UpdatedAt: now,
				}
				store.addTicket(ticket)

				handler := NewUpdateTicketStatusHandler(store)

				// --- 3. Build and execute the HTTP request ---
				body, _ := json.Marshal(map[string]string{"status": targetStatus})
				req := httptest.NewRequest("PUT", "/tickets/"+ticketID+"/status", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.SetPathValue("id", ticketID)
				ctx := auth.WithClaims(req.Context(), officialUID, officialUID+"@civic.test", "Test Official")
				req = req.WithContext(ctx)

				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)

				// --- 4. Assert the expected HTTP status code ---
				want := expectedHTTPStatus(currentStatus, targetStatus)
				got := w.Code

				if got != want {
					t.Fatalf(
						"official=%q, currentStatus=%q, targetStatus=%q: "+
							"expected HTTP %d, got %d (body: %s)",
						officialUID, currentStatus, targetStatus, want, got, w.Body.String(),
					)
				}

				// --- 5. For successful transitions, verify the ticket was updated ---
				if want == 200 {
					ctx := context.Background()
					updated, err := store.GetTicket(ctx, ticketID)
					if err != nil || updated == nil {
						t.Fatalf(
							"official=%q, currentStatus=%q→%q: GetTicket after update: %v",
							officialUID, currentStatus, targetStatus, err,
						)
					}
					if updated.Status != targetStatus {
						t.Fatalf(
							"official=%q, currentStatus=%q→%q: "+
								"store status=%q after successful transition, want %q",
							officialUID, currentStatus, targetStatus, updated.Status, targetStatus,
						)
					}
				}

				// --- 6. For non-200 responses, verify the ticket status is unchanged ---
				if want != 200 {
					ctx := context.Background()
					unchanged, err := store.GetTicket(ctx, ticketID)
					if err != nil || unchanged == nil {
						t.Fatalf(
							"official=%q, currentStatus=%q→%q: GetTicket after rejection: %v",
							officialUID, currentStatus, targetStatus, err,
						)
					}
					if unchanged.Status != currentStatus {
						t.Fatalf(
							"official=%q, currentStatus=%q→%q: "+
								"ticket status mutated to %q after rejected transition (expected %q)",
							officialUID, currentStatus, targetStatus, unchanged.Status, currentStatus,
						)
					}
				}
			}
		}
	})
}
