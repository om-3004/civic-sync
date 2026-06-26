// Package auth implements JWT verification, Google public-key caching,
// user profile upsert, and the /auth/login and /auth/upgrade HTTP handlers.
package auth

import (
	"encoding/json"
	"net/http"
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
