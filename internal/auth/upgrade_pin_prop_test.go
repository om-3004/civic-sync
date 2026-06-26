// Feature: civic-sync, Property 8: PIN Validation Accepts Only the Exact Master PIN

// **Validates: Requirements 6.3, 6.4**
//
// Property: For any non-empty PIN candidate submitted to POST /auth/upgrade:
//   - If candidate == masterPIN → the handler must return HTTP 200 and role "official"
//   - If candidate != masterPIN (and non-empty) → the handler must return HTTP 403
//   - If candidate is empty/blank → the handler must return HTTP 400
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/civic-sync/civic-sync/internal/models"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Helper: seed a fresh citizen user in the store for a given UID
// ---------------------------------------------------------------------------

func seedCitizenUser(s *loginFakeStore, uid string) {
	_ = s.UpsertUser(context.Background(), &models.User{
		UID:             uid,
		Email:           uid + "@example.com",
		Name:            "Test User",
		Role:            "citizen",
		PINFailures:     0,
		PINLockoutUntil: nil,
	})
}

// newUpgradeRequest builds a POST /auth/upgrade request with the given PIN
// and the auth context claims injected (as JWTVerify middleware would).
func newUpgradeRequest(uid, pin string) *http.Request {
	payload, _ := json.Marshal(upgradeRequest{PIN: pin})
	body := bytes.NewReader(payload)
	req := httptest.NewRequest(http.MethodPost, "/auth/upgrade", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := WithClaims(req.Context(), uid, uid+"@example.com", "Test User")
	return req.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// Property test
// ---------------------------------------------------------------------------

// TestPINExactMatchProperty verifies Property 8:
//
//   - Exact match: submitting the master PIN returns 200 with role "official".
//   - Non-match:   submitting any other non-empty PIN returns 403.
//   - Blank PIN:   submitting an empty or whitespace-only PIN returns 400.
func TestPINExactMatchProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Draw a non-empty master PIN (printable ASCII, no control chars).
		masterPIN := rapid.StringMatching(`[!-~]{1,32}`).Draw(rt, "masterPIN")

		// Set MASTER_PIN for this test iteration; auto-restored after test.
		// Use the outer *testing.T for Setenv (rapid.T doesn't support it).
		t.Setenv("MASTER_PIN", masterPIN)

		// Stable UID for all sub-scenarios in this iteration.
		uid := rapid.StringMatching(`[a-z][a-z0-9]{3,12}`).Draw(rt, "uid")

		// ----------------------------------------------------------------
		// Sub-scenario 1: Exact match → 200 + role "official"
		// ----------------------------------------------------------------
		{
			s := newLoginFakeStore()
			seedCitizenUser(s, uid)
			handler := NewUpgradeHandler(s)

			req := newUpgradeRequest(uid, masterPIN)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				rt.Fatalf(
					"exact match: expected 200, got %d (masterPIN=%q) — body: %s",
					rec.Code, masterPIN, rec.Body.String(),
				)
			}

			var resp upgradeResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				rt.Fatalf("exact match: failed to decode response body: %v", err)
			}
			if resp.Role != "official" {
				rt.Fatalf("exact match: expected role %q, got %q", "official", resp.Role)
			}
		}

		// ----------------------------------------------------------------
		// Sub-scenario 2: Non-matching non-empty PIN → 403
		// ----------------------------------------------------------------
		{
			// Draw a candidate PIN from the same character set.
			candidate := rapid.StringMatching(`[!-~]{1,32}`).Draw(rt, "candidate")

			// Skip the rare case where the random candidate equals the master PIN.
			if candidate == masterPIN {
				rt.Skip("candidate accidentally equals masterPIN — skipping iteration")
			}

			s := newLoginFakeStore()
			seedCitizenUser(s, uid)
			handler := NewUpgradeHandler(s)

			req := newUpgradeRequest(uid, candidate)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				rt.Fatalf(
					"non-match: expected 403, got %d (masterPIN=%q, candidate=%q) — body: %s",
					rec.Code, masterPIN, candidate, rec.Body.String(),
				)
			}
		}

		// ----------------------------------------------------------------
		// Sub-scenario 3: Blank PIN → 400
		// ----------------------------------------------------------------
		{
			s := newLoginFakeStore()
			seedCitizenUser(s, uid)
			handler := NewUpgradeHandler(s)

			// Test with a completely empty PIN.
			req := newUpgradeRequest(uid, "")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				rt.Fatalf(
					"blank PIN: expected 400, got %d — body: %s",
					rec.Code, rec.Body.String(),
				)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 9: PIN Rate Limiting Blocks After 5 Consecutive Failures
// Feature: civic-sync, Property 9: PIN Rate Limiting Blocks After 5 Consecutive Failures
//
// **Validates: Requirements 6.6**
//
// Property: For any sequence of ≥5 wrong PINs submitted consecutively to
// POST /auth/upgrade for the same user (within the 15-minute lockout window):
//   - Attempts 1–4 must return HTTP 403 (wrong PIN, not yet locked)
//   - Attempts 5 and all subsequent attempts must return HTTP 429 (locked out)
// ---------------------------------------------------------------------------

// TestPINRateLimitingProperty verifies Property 9:
//
//   - 1st through 4th consecutive wrong PIN submissions return HTTP 403.
//   - 5th and all subsequent wrong PIN submissions (within lockout window) return HTTP 429.
func TestPINRateLimitingProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Draw a master PIN that will never be submitted (we only send wrong PINs).
		masterPIN := rapid.StringMatching(`[!-~]{4,16}`).Draw(rt, "masterPIN")
		t.Setenv("MASTER_PIN", masterPIN)

		// Draw the user UID for this iteration.
		uid := rapid.StringMatching(`[a-z][a-z0-9]{3,12}`).Draw(rt, "uid")

		// Draw a sequence length of ≥5 wrong attempts (up to 12 for variety).
		numAttempts := rapid.IntRange(5, 12).Draw(rt, "numAttempts")

		// Use a single shared store for the whole sequence (persists failure
		// counters and lockout across requests, as production would).
		s := newLoginFakeStore()
		seedCitizenUser(s, uid)
		handler := NewUpgradeHandler(s)

		for i := 1; i <= numAttempts; i++ {
			// Generate a wrong PIN: any printable ASCII string that differs
			// from the master PIN.
			wrongPIN := rapid.StringMatching(`[!-~]{1,16}`).Draw(rt, "wrongPIN")
			// If it accidentally equals the master PIN, append a suffix to
			// ensure it is wrong.
			if wrongPIN == masterPIN {
				wrongPIN = wrongPIN + "X"
			}

			req := newUpgradeRequest(uid, wrongPIN)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if i < 5 {
				// Attempts 1–4: must be 403 Forbidden.
				if rec.Code != http.StatusForbidden {
					rt.Fatalf(
						"attempt %d/%d: expected 403 (wrong PIN, not locked), got %d — masterPIN=%q wrongPIN=%q body: %s",
						i, numAttempts, rec.Code, masterPIN, wrongPIN, rec.Body.String(),
					)
				}
			} else {
				// Attempt 5 and beyond (within lockout window): must be 429.
				if rec.Code != http.StatusTooManyRequests {
					rt.Fatalf(
						"attempt %d/%d: expected 429 (locked out after 5 failures), got %d — masterPIN=%q wrongPIN=%q body: %s",
						i, numAttempts, rec.Code, masterPIN, wrongPIN, rec.Body.String(),
					)
				}
			}
		}
	})
}
