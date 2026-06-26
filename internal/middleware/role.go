package middleware

import (
	"net/http"

	"github.com/civic-sync/civic-sync/internal/auth"
	"github.com/civic-sync/civic-sync/internal/store"
)

// RequireRole returns middleware that enforces a specific user role. It reads
// the authenticated user's UID from the request context (set by JWTVerify),
// looks up the user's profile via the store, and compares user.Role against
// the required role. Returns HTTP 403 if the role does not match.
//
// This middleware must be placed after JWTVerify in the chain.
func RequireRole(s store.Store, role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := auth.UIDFromContext(r.Context())
			if uid == "" {
				// No UID in context — JWTVerify was not run or failed.
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			user, err := s.GetUser(r.Context(), uid)
			if err != nil || user == nil {
				http.Error(w, `{"error":"user not found"}`, http.StatusForbidden)
				return
			}

			if user.Role != role {
				http.Error(w, `{"error":"forbidden: insufficient role"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
