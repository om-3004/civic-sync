// Package auth implements JWT verification, Google public-key caching,
// user profile upsert, and the /auth/login and /auth/upgrade HTTP handlers.
package auth

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/civic-sync/civic-sync/internal/models"
	"github.com/civic-sync/civic-sync/internal/store"
)

// loginResponse is the JSON body returned on a successful /auth/login.
type loginResponse struct {
	UID   string `json:"uid"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// NewLoginHandler returns an http.HandlerFunc that handles POST /auth/login.
//
// It expects the JWTVerify middleware to have already run, so uid/email/name
// are available in the request context. On a new user it creates a Firestore
// profile with role "citizen". On an existing user it returns the stored
// profile without modification (Req 1.8).
func NewLoginHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		uid := UIDFromContext(ctx)
		email := EmailFromContext(ctx)
		name := NameFromContext(ctx)

		// Check whether a profile already exists (Req 1.8).
		existing, err := s.GetUser(ctx, uid)
		if err != nil {
			// Unexpected store error — surface as 500.
			http.Error(w, `{"error":"failed to retrieve user profile"}`, http.StatusInternalServerError)
			return
		}

		var user *models.User

		if existing == nil {
			// First-time login — create a new citizen profile (Req 1.6).
			user = &models.User{
				UID:             uid,
				Email:           email,
				Name:            name,
				Role:            "citizen",
				CreatedAt:       time.Now().UTC(),
				PINFailures:     0,
				PINLockoutUntil: nil,
			}
			if err := s.UpsertUser(ctx, user); err != nil {
				// Firestore write failure → 500 (Req 1.7).
				http.Error(w, `{"error":"failed to create user profile"}`, http.StatusInternalServerError)
				return
			}
		} else {
			// Returning user — use the stored profile (Req 1.8).
			user = existing
		}

		resp := loginResponse{
			UID:   user.UID,
			Email: user.Email,
			Name:  user.Name,
			Role:  user.Role,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

const (
	pinLockoutDuration   = 15 * time.Minute
	pinMaxFailures       = 5
)

// upgradeRequest is the expected JSON body for POST /auth/upgrade.
type upgradeRequest struct {
	PIN string `json:"pin"`
}

// upgradeResponse is the JSON body returned on a successful /auth/upgrade.
type upgradeResponse struct {
	Role string `json:"role"`
}

// NewUpgradeHandler returns an http.HandlerFunc that handles POST /auth/upgrade.
//
// It validates the supplied PIN against the MASTER_PIN environment variable,
// enforces a 5-consecutive-failure lockout for 15 minutes (Req 6.6), and
// upgrades the caller's role from "citizen" to "official" on a match (Req 6.3).
//
// The JWTVerify middleware must run before this handler so that the uid is
// available in the request context.
//
// Response codes:
//   - 200: PIN matched → role upgraded to "official"
//   - 400: empty/blank PIN (Req 6.4)
//   - 403: wrong PIN (Req 6.5)
//   - 409: caller is already "official" (Req 6.3)
//   - 429: account locked out after 5 consecutive failures (Req 6.6)
func NewUpgradeHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// --- 1. Decode and validate request body ---
		var req upgradeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.PIN) == "" {
			http.Error(w, `{"error":"pin must not be blank"}`, http.StatusBadRequest)
			return
		}

		// --- 2. Retrieve caller's user profile ---
		uid := UIDFromContext(ctx)
		user, err := s.GetUser(ctx, uid)
		if err != nil {
			http.Error(w, `{"error":"failed to retrieve user profile"}`, http.StatusInternalServerError)
			return
		}
		if user == nil {
			// Should not happen if /auth/login was called first, but guard anyway.
			http.Error(w, `{"error":"user profile not found"}`, http.StatusNotFound)
			return
		}

		// --- 3. Already official → 409 (Req 6.3) ---
		if user.Role == "official" {
			http.Error(w, `{"error":"user is already an official"}`, http.StatusConflict)
			return
		}

		// --- 4. Lockout check → 429 (Req 6.6) ---
		now := time.Now().UTC()
		if user.PINLockoutUntil != nil && now.Before(*user.PINLockoutUntil) {
			http.Error(w, `{"error":"too many failed attempts, try again later"}`, http.StatusTooManyRequests)
			return
		}

		// --- 5. Compare PIN ---
		masterPIN := os.Getenv("MASTER_PIN")
		if req.PIN == masterPIN {
			// Correct PIN → upgrade role, reset failure counters.
			user.Role = "official"
			user.PINFailures = 0
			user.PINLockoutUntil = nil

			if err := s.UpsertUser(ctx, user); err != nil {
				http.Error(w, `{"error":"failed to update user profile"}`, http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(upgradeResponse{Role: "official"})
			return
		}

		// Wrong PIN → increment failure counter.
		user.PINFailures++
		if user.PINFailures >= pinMaxFailures {
			lockoutUntil := now.Add(pinLockoutDuration)
			user.PINLockoutUntil = &lockoutUntil

			if err := s.UpsertUser(ctx, user); err != nil {
				http.Error(w, `{"error":"failed to update user profile"}`, http.StatusInternalServerError)
				return
			}

			http.Error(w, `{"error":"too many failed attempts, try again later"}`, http.StatusTooManyRequests)
			return
		}

		if err := s.UpsertUser(ctx, user); err != nil {
			http.Error(w, `{"error":"failed to update user profile"}`, http.StatusInternalServerError)
			return
		}

		http.Error(w, `{"error":"invalid pin"}`, http.StatusForbidden)
	}
}
