package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type requestIDKey struct{}

const requestIDHeader = "X-Request-ID"

// newRequestID generates a random 16-byte hex string suitable for use as a
// request ID. It falls back to a zero-value string on the (extremely unlikely)
// error from crypto/rand.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// RequestID returns middleware that attaches a unique request ID to the request
// context and sets the X-Request-ID response header. If the incoming request
// already carries an X-Request-ID header its value is reused; otherwise a new
// ID is generated.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := r.Header.Get(requestIDHeader)
			if rid == "" {
				rid = newRequestID()
			}
			w.Header().Set(requestIDHeader, rid)
			ctx := context.WithValue(r.Context(), requestIDKey{}, rid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFromContext retrieves the request ID stored by the RequestID
// middleware. Returns an empty string if no ID is present.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}
