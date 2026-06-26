// Feature: civic-sync, Property 2: User Profile Creation is Idempotent

// **Validates: Requirements 1.6, 1.8**
//
// Property: Calling POST /auth/login N times (N = 1..10) with the same UID
// produces exactly one Firestore user document per UID, with role "citizen"
// on first login and unchanged on subsequent calls.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/models"
	"github.com/civic-sync/civic-sync/internal/store"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// In-memory fake store (scoped to this test file)
// ---------------------------------------------------------------------------

// loginFakeStore is a thread-safe in-memory Store implementation used
// exclusively by the login idempotency property tests.
type loginFakeStore struct {
	mu    sync.Mutex
	users map[string]*models.User
}

func newLoginFakeStore() *loginFakeStore {
	return &loginFakeStore{users: make(map[string]*models.User)}
}

func (s *loginFakeStore) GetUser(_ context.Context, uid string) (*models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[uid]
	if !ok {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (s *loginFakeStore) UpsertUser(_ context.Context, user *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *user
	s.users[user.UID] = &cp
	return nil
}

// countUsers returns the total number of distinct user documents in the store.
func (s *loginFakeStore) countUsers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.users)
}

// --- Unused Store interface methods (stubs) ---

func (s *loginFakeStore) CreateTicket(_ context.Context, _ *models.Ticket) error {
	return nil
}
func (s *loginFakeStore) GetTicket(_ context.Context, _ string) (*models.Ticket, error) {
	return nil, nil
}
func (s *loginFakeStore) QueryActiveTicketsByCategory(_ context.Context, _, _, _ float64, _ string) ([]*models.Ticket, error) {
	return nil, nil
}
func (s *loginFakeStore) IncrementUpvote(_ context.Context, _, _ string) error { return nil }
func (s *loginFakeStore) UpdateTicketStatus(_ context.Context, _, _ string) error { return nil }
func (s *loginFakeStore) ArchiveExpiredTickets(_ context.Context) error          { return nil }
func (s *loginFakeStore) HasUserUpvoted(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

// Ensure loginFakeStore satisfies store.Store at compile time.
var _ store.Store = (*loginFakeStore)(nil)

// ---------------------------------------------------------------------------
// Helper: build a request with pre-populated auth context claims
// ---------------------------------------------------------------------------

// newLoginRequest constructs a POST /auth/login request whose context already
// holds the uid/email/name claims (as the JWTVerify middleware would inject).
func newLoginRequest(uid, email, name string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	ctx := WithClaims(req.Context(), uid, email, name)
	return req.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// Property test
// ---------------------------------------------------------------------------

// TestLoginIdempotencyProperty verifies Property 2:
//
//   - Calling /auth/login N times (N = 1..10) with the same UID always
//     results in exactly ONE user document for that UID in the store.
//   - The first successful login creates the document with role "citizen".
//   - Subsequent logins return the existing profile without creating duplicates
//     or overwriting the role.
//   - When multiple distinct UIDs each call login N times, there is exactly
//     one document per UID (no cross-UID collisions).
func TestLoginIdempotencyProperty(t *testing.T) {
	// rapid runs 100 iterations by default (satisfies the ≥100 requirement).
	rapid.Check(t, func(t *rapid.T) {
		// --- Draw random test parameters ---

		// N: number of repeated login calls (1..10).
		n := rapid.IntRange(1, 10).Draw(t, "n")

		// uid: non-empty identifier for the user being tested.
		uid := rapid.StringMatching(`[a-z][a-z0-9_-]{0,19}`).Draw(t, "uid")

		// email / name are arbitrary non-empty strings.
		email := rapid.StringMatching(`[a-z]{1,8}@example\.com`).Draw(t, "email")
		name := rapid.StringMatching(`[A-Za-z ]{1,20}`).Draw(t, "name")

		// --- Set up a fresh in-memory store for this iteration ---
		s := newLoginFakeStore()
		handler := NewLoginHandler(s)

		// --- Call /auth/login N times with identical claims ---
		for i := 0; i < n; i++ {
			req := newLoginRequest(uid, email, name)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf(
					"iteration %d/%d: expected 200 OK, got %d — body: %s",
					i+1, n, rec.Code, rec.Body.String(),
				)
			}
		}

		// --- Assert: exactly one document exists for the UID ---
		count := s.countUsers()
		if count != 1 {
			t.Fatalf(
				"after %d login call(s) for uid=%q: expected exactly 1 user document, got %d",
				n, uid, count,
			)
		}

		// --- Assert: the stored document has role "citizen" (Req 1.6) ---
		ctx := context.Background()
		stored, err := s.GetUser(ctx, uid)
		if err != nil {
			t.Fatalf("GetUser(%q): %v", uid, err)
		}
		if stored == nil {
			t.Fatalf("GetUser(%q): expected non-nil user, got nil", uid)
		}
		if stored.Role != "citizen" {
			t.Fatalf("uid=%q: expected role %q, got %q", uid, "citizen", stored.Role)
		}
		if stored.UID != uid {
			t.Fatalf("stored UID mismatch: got %q, want %q", stored.UID, uid)
		}
		if stored.Email != email {
			t.Fatalf("stored email mismatch: got %q, want %q", stored.Email, email)
		}
		if stored.CreatedAt.IsZero() {
			t.Fatalf("uid=%q: created_at must be set on first login", uid)
		}
	})
}

// TestLoginIdempotencyProperty_MultipleUIDs verifies that N logins for each of
// K distinct UIDs produces exactly K documents in the store — one per UID —
// with no cross-UID interference.
func TestLoginIdempotencyProperty_MultipleUIDs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Number of distinct UIDs (2..5) and calls per UID (1..5).
		numUIDs := rapid.IntRange(2, 5).Draw(t, "numUIDs")
		callsPerUID := rapid.IntRange(1, 5).Draw(t, "callsPerUID")

		s := newLoginFakeStore()
		handler := NewLoginHandler(s)

		// Build numUIDs distinct identifiers.
		uids := make([]string, numUIDs)
		for i := 0; i < numUIDs; i++ {
			uids[i] = rapid.StringMatching(`[a-z][a-z0-9]{3,8}`).Draw(t, fmt.Sprintf("uid%d", i))
		}

		// Ensure all generated UIDs are distinct; skip iteration if not.
		seen := make(map[string]bool, numUIDs)
		for _, uid := range uids {
			if seen[uid] {
				t.Skip("duplicate UID generated — skipping iteration")
			}
			seen[uid] = true
		}

		// Call login callsPerUID times for each UID.
		for _, uid := range uids {
			for call := 0; call < callsPerUID; call++ {
				req := newLoginRequest(uid, uid+"@example.com", "User "+uid)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					t.Fatalf(
						"uid=%q call=%d: expected 200, got %d — body: %s",
						uid, call+1, rec.Code, rec.Body.String(),
					)
				}
			}
		}

		// Assert: exactly numUIDs documents in the store.
		total := s.countUsers()
		if total != numUIDs {
			t.Fatalf(
				"expected exactly %d user documents for %d distinct UIDs (%d calls each), got %d",
				numUIDs, numUIDs, callsPerUID, total,
			)
		}

		// Assert: each UID has exactly one document with role "citizen".
		ctx := context.Background()
		for _, uid := range uids {
			u, err := s.GetUser(ctx, uid)
			if err != nil {
				t.Fatalf("GetUser(%q): %v", uid, err)
			}
			if u == nil {
				t.Fatalf("GetUser(%q): expected document, got nil", uid)
			}
			if u.Role != "citizen" {
				t.Fatalf("uid=%q: expected role %q, got %q", uid, "citizen", u.Role)
			}
		}
	})
}

// TestLoginIdempotency_CreatedAtSetOnce verifies that created_at is stamped
// on the first login and is not advanced by subsequent logins (Req 1.6).
func TestLoginIdempotency_CreatedAtSetOnce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 10).Draw(t, "n") // at least 2 so "subsequent" makes sense
		uid := rapid.StringMatching(`[a-z][a-z0-9_-]{0,15}`).Draw(t, "uid")

		s := newLoginFakeStore()
		handler := NewLoginHandler(s)

		var firstCreatedAt time.Time

		for i := 0; i < n; i++ {
			req := newLoginRequest(uid, uid+"@example.com", "User")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("iteration %d: expected 200, got %d", i+1, rec.Code)
			}

			ctx := context.Background()
			u, _ := s.GetUser(ctx, uid)
			if u == nil {
				t.Fatalf("iteration %d: user not found after login", i+1)
			}

			if i == 0 {
				// Record the created_at set by the very first login.
				firstCreatedAt = u.CreatedAt
				if firstCreatedAt.IsZero() {
					t.Fatalf("first login: created_at must not be zero")
				}
			} else {
				// Subsequent logins must not change created_at.
				if !u.CreatedAt.Equal(firstCreatedAt) {
					t.Fatalf(
						"iteration %d: created_at changed from %v to %v after repeated login",
						i+1, firstCreatedAt, u.CreatedAt,
					)
				}
			}
		}
	})
}
