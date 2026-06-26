package middleware

import (
	"log"
	"net/http"
	"time"
)

// responseWriter is a minimal wrapper around http.ResponseWriter that captures
// the status code written by a downstream handler.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Logger returns middleware that logs the HTTP method, path, response status,
// latency, and request ID for every request.
func Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			rid := RequestIDFromContext(r.Context())
			log.Printf(
				"method=%s path=%s status=%d latency=%s request_id=%s",
				r.Method,
				r.URL.Path,
				wrapped.status,
				time.Since(start).String(),
				rid,
			)
		})
	}
}
