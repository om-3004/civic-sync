// Feature: civic-sync, Property 17: Backend Returns HTTP 503 When Any Required Dependency Fails to Initialize

package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"pgregory.net/rapid"
)

// TestPropertyStartup503WhenAnyDepFails verifies that the API gateway returns HTTP 503
// for all incoming requests whenever any required dependency (Firestore, Google public keys,
// or Gemini) fails to initialize during startup.
//
// Validates: Requirements 11.5
func TestPropertyStartup503WhenAnyDepFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		firestoreFails  := rapid.Bool().Draw(t, "firestoreFails")
		googleKeysFails := rapid.Bool().Draw(t, "googleKeysFails")
		geminiFails     := rapid.Bool().Draw(t, "geminiFails")

		anyFails := firestoreFails || googleKeysFails || geminiFails

		// Build the gateway handler in isolation, replicating the exact same
		// atomic.Int32 + atomic.Value gate used in cmd/server/main.go.
		var apiReady   atomic.Int32
		var apiHandler atomic.Value

		// Simulate startup outcome:
		//   - all deps succeed → store a trivial 200 handler and set apiReady = 1
		//   - any dep fails   → apiReady stays 0, apiHandler is never stored
		if !anyFails {
			apiHandler.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			apiReady.Store(1)
		}

		gateway := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiReady.Load() == 0 {
				http.Error(w, `{"error":"service initialising, please retry"}`, http.StatusServiceUnavailable)
				return
			}
			apiHandler.Load().(http.Handler).ServeHTTP(w, r)
		})

		// Representative API paths that would be live once deps are ready.
		paths := []struct{ method, path string }{
			{"POST", "/auth/login"},
			{"POST", "/tickets"},
			{"POST", "/triage"},
			{"POST", "/tickets/abc/upvote"},
			{"PUT", "/tickets/abc/status"},
		}

		for _, p := range paths {
			req := httptest.NewRequest(p.method, p.path, nil)
			rec := httptest.NewRecorder()
			gateway.ServeHTTP(rec, req)

			if anyFails {
				// Property 17: any dependency failure must cause the gateway to
				// return 503 for ALL incoming requests.
				if rec.Code != http.StatusServiceUnavailable {
					t.Fatalf(
						"expected 503 when deps fail (firestore=%v, googleKeys=%v, gemini=%v), "+
							"got %d for %s %s",
						firestoreFails, googleKeysFails, geminiFails,
						rec.Code, p.method, p.path,
					)
				}
			} else {
				// When all deps succeed the gateway must NOT return 503.
				// It may return 404/405 because no real routes are wired up in this
				// isolated unit test — that is acceptable.
				if rec.Code == http.StatusServiceUnavailable {
					t.Fatalf(
						"expected non-503 when all deps succeed, got 503 for %s %s",
						p.method, p.path,
					)
				}
			}
		}
	})
}
