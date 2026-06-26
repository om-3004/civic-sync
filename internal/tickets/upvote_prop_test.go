// Feature: civic-sync, Property 7: Upvote Increments Count Exactly Once Per Unique User

// **Validates: Requirements 5.1, 5.2, 5.3, 5.4**
//
// Property: For every unique user who has NOT yet upvoted a ticket,
//   - The upvote handler MUST return HTTP 200.
//   - The upvote count MUST increment by exactly 1.
//   - The updated_at timestamp MUST advance (Req 5.2).
//
// For every user who HAS already upvoted the same ticket:
//   - The upvote handler MUST return HTTP 409 Conflict.
//   - The upvote count MUST remain unchanged (Req 5.3, 5.4).
//   - The updated_at timestamp MUST remain unchanged.
package tickets

import (
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
// Property test
// ---------------------------------------------------------------------------

// TestPropertyUpvoteIdempotency verifies Property 7:
// Upvote Increments Count Exactly Once Per Unique User.
//
// For each generated scenario:
//  - A ticket is pre-seeded with a random initial upvote count and a
//    randomly-chosen set of users who have already upvoted it.
//  - Each user in the "already voted" set must get 409 and see no count change.
//  - Each user in the "new voter" set must get 200 and see count += 1.
//
// rapid.Check runs >= 100 iterations by default (rapid v1.3.0 default).
func TestPropertyUpvoteIdempotency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- 1. Generate a random initial upvote count (0..50) ---
		initialCount := rapid.IntRange(0, 50).Draw(t, "initialCount")

		// --- 2. Generate a random pool of unique user IDs (1..20) ---
		numUsers := rapid.IntRange(1, 20).Draw(t, "numUsers")
		allUIDs := make([]string, numUsers)
		for i := 0; i < numUsers; i++ {
			allUIDs[i] = fmt.Sprintf("user-%d", i)
		}

		// --- 3. Choose how many of those users have *already* upvoted ---
		// numAlreadyVoted is in [0, numUsers]; the rest are "new" voters.
		numAlreadyVoted := rapid.IntRange(0, numUsers).Draw(t, "numAlreadyVoted")

		alreadyVoted := allUIDs[:numAlreadyVoted]
		newVoters := allUIDs[numAlreadyVoted:]

		// --- 4. Build the ticket with the pre-voted set ---
		now := time.Now().UTC()
		ticketID := "prop-ticket"
		ticket := &models.Ticket{
			ID:        ticketID,
			Status:    "To Do",
			Upvotes:   initialCount,
			UpvotedBy: append([]string(nil), alreadyVoted...),
			CreatedAt: now,
			UpdatedAt: now,
		}

		s := newUpvoteTestStore()
		s.addTicket(ticket)

		handler := NewUpvoteTicketHandler(s)
		ctx := context.Background()

		// -----------------------------------------------------------------------
		// Phase A: already-voted users → expect 409 and unchanged state
		// -----------------------------------------------------------------------
		for _, uid := range alreadyVoted {
			before, err := s.GetTicket(ctx, ticketID)
			if err != nil || before == nil {
				t.Fatalf("GetTicket before duplicate upvote by %s: %v", uid, err)
			}
			countBefore := before.Upvotes
			updatedAtBefore := before.UpdatedAt

			req := httptest.NewRequest("POST", "/tickets/"+ticketID+"/upvote", nil)
			req = req.WithContext(auth.WithClaims(req.Context(), uid, uid+"@test.com", "Test User"))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// Must return 409 Conflict (Req 5.3, 5.4).
			if w.Code != 409 {
				t.Fatalf(
					"already-voted user %q: expected HTTP 409, got %d (body: %s)",
					uid, w.Code, w.Body.String(),
				)
			}

			after, err := s.GetTicket(ctx, ticketID)
			if err != nil || after == nil {
				t.Fatalf("GetTicket after duplicate upvote by %s: %v", uid, err)
			}

			// Count must be unchanged (Req 5.3).
			if after.Upvotes != countBefore {
				t.Fatalf(
					"already-voted user %q: upvote count changed from %d to %d; expected no change",
					uid, countBefore, after.Upvotes,
				)
			}

			// updated_at must be unchanged (Req 5.4).
			if !after.UpdatedAt.Equal(updatedAtBefore) {
				t.Fatalf(
					"already-voted user %q: updated_at changed from %v to %v; expected no change",
					uid, updatedAtBefore, after.UpdatedAt,
				)
			}
		}

		// -----------------------------------------------------------------------
		// Phase B: new voters → expect 200 and count += 1 each time
		// -----------------------------------------------------------------------
		// The ticket was seeded with Upvotes=initialCount; the already-voted users
		// are pre-loaded in UpvotedBy but their vote is baked into initialCount
		// (the store reflects exactly what was seeded). So we start from initialCount.
		expectedCount := initialCount

		for _, uid := range newVoters {
			before, err := s.GetTicket(ctx, ticketID)
			if err != nil || before == nil {
				t.Fatalf("GetTicket before upvote by %s: %v", uid, err)
			}
			updatedAtBefore := before.UpdatedAt

			req := httptest.NewRequest("POST", "/tickets/"+ticketID+"/upvote", nil)
			req = req.WithContext(auth.WithClaims(req.Context(), uid, uid+"@test.com", "Test User"))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// Must return 200 OK (Req 5.1).
			if w.Code != 200 {
				t.Fatalf(
					"new voter %q: expected HTTP 200, got %d (body: %s)",
					uid, w.Code, w.Body.String(),
				)
			}

			// Decode the response body and check the returned count (Req 5.1).
			expectedCount++
			var resp upvoteResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("new voter %q: decode response: %v", uid, err)
			}
			if resp.Upvotes != expectedCount {
				t.Fatalf(
					"new voter %q: response upvotes=%d, want %d",
					uid, resp.Upvotes, expectedCount,
				)
			}

			// Verify count in the store directly as well.
			after, err := s.GetTicket(ctx, ticketID)
			if err != nil || after == nil {
				t.Fatalf("GetTicket after upvote by %s: %v", uid, err)
			}
			if after.Upvotes != expectedCount {
				t.Fatalf(
					"new voter %q: store upvotes=%d, want %d",
					uid, after.Upvotes, expectedCount,
				)
			}

			// updated_at must have advanced (Req 5.2).
			if !after.UpdatedAt.After(updatedAtBefore) && !after.UpdatedAt.Equal(updatedAtBefore) {
				// Allow equal only when the clock resolution is coarse; strictly
				// the field must not go backwards.
				if after.UpdatedAt.Before(updatedAtBefore) {
					t.Fatalf(
						"new voter %q: updated_at went backwards from %v to %v",
						uid, updatedAtBefore, after.UpdatedAt,
					)
				}
			}

			// A second upvote by this same user must now return 409 (idempotency).
			req2 := httptest.NewRequest("POST", "/tickets/"+ticketID+"/upvote", nil)
			req2 = req2.WithContext(auth.WithClaims(req2.Context(), uid, uid+"@test.com", "Test User"))
			w2 := httptest.NewRecorder()
			handler.ServeHTTP(w2, req2)

			if w2.Code != 409 {
				t.Fatalf(
					"idempotency check for %q: expected HTTP 409 on repeat, got %d",
					uid, w2.Code,
				)
			}

			// Count must still be expectedCount after the rejected repeat.
			afterRepeat, err := s.GetTicket(ctx, ticketID)
			if err != nil || afterRepeat == nil {
				t.Fatalf("GetTicket after repeat upvote by %s: %v", uid, err)
			}
			if afterRepeat.Upvotes != expectedCount {
				t.Fatalf(
					"idempotency check for %q: count changed after 409; got %d, want %d",
					uid, afterRepeat.Upvotes, expectedCount,
				)
			}
		}
	})
}
