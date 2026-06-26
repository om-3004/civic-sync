package middleware

import (
	"fmt"
	"log"
	"net/http"
)

// RecoverPanic returns middleware that catches any panic in a downstream handler,
// logs the error with the request ID (if present), and returns HTTP 500.
func RecoverPanic() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					rid := RequestIDFromContext(r.Context())
					log.Printf("[PANIC] request_id=%s error=%v", rid, rec)
					http.Error(w, fmt.Sprintf(`{"error":"internal server error"}`), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
