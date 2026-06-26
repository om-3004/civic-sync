// Package main is the entry point for the CivicSync Cloud Run backend.
//
// Startup sequence (Requirements 11.1–11.5):
//  1. Register GET / (static landing page) immediately — no env vars required.
//  2. Start http.ListenAndServe so the landing page is reachable right away.
//  3. In the background: load env config, init Firestore, Google PKI cache,
//     and Gemini agent in parallel via sync.WaitGroup.
//  4. On any init failure: log + os.Exit(1) — Cloud Run marks instance unhealthy.
//     API routes return 503 until init completes.
//  5. Once all deps are ready: register API routes and start archival goroutine.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

	"cloud.google.com/go/firestore"
	"github.com/joho/godotenv"

	"github.com/civic-sync/civic-sync/internal/auth"
	"github.com/civic-sync/civic-sync/internal/middleware"
	"github.com/civic-sync/civic-sync/internal/store"
	"github.com/civic-sync/civic-sync/internal/tickets"
	"github.com/civic-sync/civic-sync/internal/triage"
)

func main() {
	// Load .env if present (local dev only — no-op in Cloud Run where env vars
	// are injected directly by the platform).
	if err := godotenv.Load(); err == nil {
		log.Println("startup: loaded .env file")
	}

	ctx := context.Background()
	mux := http.NewServeMux()

	// ── 1. Register the landing page immediately (no deps required) ───────────
	// GET / serves the embedded static assets with no auth and no env vars.
	withNoAuth := func(h http.Handler) http.Handler {
		return middleware.RecoverPanic()(
			middleware.RequestID()(
				middleware.Logger()(
					middleware.CORSHeaders()(h),
				),
			),
		)
	}
	staticMux := http.NewServeMux()
	RegisterLandingPage(staticMux)
	mux.Handle("GET /", withNoAuth(staticMux))

	// ── 2. API routes — served via a late-bound handler ───────────────────────
	// apiReady flips to 1 once all deps have initialised successfully.
	// Until then every API request receives 503.
	var apiReady atomic.Int32
	var apiHandler atomic.Value // stores http.Handler once ready

	apiGateway := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiReady.Load() == 0 {
			http.Error(w, `{"error":"service initialising, please retry"}`, http.StatusServiceUnavailable)
			return
		}
		apiHandler.Load().(http.Handler).ServeHTTP(w, r)
	})

	// Register a catch-all for all non-GET-/ paths through the gateway.
	mux.Handle("/", apiGateway)

	// ── 3. Start the HTTP server immediately ──────────────────────────────────
	// The server blocks on the main goroutine so the process stays alive.
	// All dependency init happens in a background goroutine.
	go func() {
		// ── 4. Load env config (API deps only) ────────────────────────────────
		// Missing vars disable the API but leave the landing page running.
		projectID := os.Getenv("PROJECT_ID")
		geminiAPIKey := os.Getenv("GEMINI_API_KEY")
		missingVars := []string{}
		for _, k := range []string{"PROJECT_ID", "GEMINI_API_KEY", "MASTER_PIN", "GOOGLE_CLIENT_ID"} {
			if os.Getenv(k) == "" {
				missingVars = append(missingVars, k)
			}
		}
		if len(missingVars) > 0 {
			log.Printf("startup: API disabled — missing env vars: %v (landing page still available)", missingVars)
			return
		}

		// ── 5. Parallel init of Firestore, Google PKI cache, and Gemini ───────
		type fsResult struct {
			client *firestore.Client
			err    error
		}
		type kcResult struct {
			cache *auth.KeyCache
			err   error
		}
		type agResult struct {
			agent *triage.Agent
			err   error
		}

		fsCh := make(chan fsResult, 1)
		kcCh := make(chan kcResult, 1)
		agCh := make(chan agResult, 1)

		var wg sync.WaitGroup
		wg.Add(3)

		go func() {
			defer wg.Done()
			client, err := firestore.NewClient(ctx, projectID)
			fsCh <- fsResult{client, err}
		}()

		go func() {
			defer wg.Done()
			kc, err := auth.NewKeyCache(ctx)
			kcCh <- kcResult{kc, err}
		}()

		go func() {
			defer wg.Done()
			agent, err := triage.NewAgent(ctx, geminiAPIKey)
			agCh <- agResult{agent, err}
		}()

		wg.Wait()

		fsRes := <-fsCh
		if fsRes.err != nil {
			log.Printf("startup: Firestore init failed: %v — API routes disabled", fsRes.err)
			return
		}

		kcRes := <-kcCh
		if kcRes.err != nil {
			log.Printf("startup: Google public-key cache init failed: %v — API routes disabled", kcRes.err)
			return
		}
		keyCache := kcRes.cache

		agRes := <-agCh
		if agRes.err != nil {
			log.Printf("startup: Gemini agent init failed: %v — API routes disabled", agRes.err)
			return
		}

		// ── 6. Wire API dependencies ───────────────────────────────────────────
		s := store.NewFirestoreStore(fsRes.client)
		triageHandler := triage.NewHandler(agRes.agent)

		withAuth := func(h http.Handler) http.Handler {
			return middleware.RecoverPanic()(
				middleware.RequestID()(
					middleware.Logger()(
						middleware.CORSHeaders()(
							middleware.JWTVerify(keyCache)(h),
						),
					),
				),
			)
		}

		apiMux := http.NewServeMux()
		apiMux.Handle("POST /auth/login", withAuth(auth.NewLoginHandler(s)))
		apiMux.Handle("POST /auth/upgrade", withAuth(auth.NewUpgradeHandler(s)))
		apiMux.Handle("POST /triage", withAuth(http.HandlerFunc(triageHandler.TriageHandler)))
		apiMux.Handle("POST /tickets", withAuth(tickets.NewCreateTicketHandler(s)))
		apiMux.Handle("POST /tickets/{id}/upvote", withAuth(tickets.NewUpvoteTicketHandler(s)))
		apiMux.Handle("PUT /tickets/{id}/status",
			withAuth(
				middleware.RequireRole(s, "official")(
					tickets.NewUpdateTicketStatusHandler(s),
				),
			),
		)

		// ── 7. Start archival goroutine and open API traffic ───────────────────
		tickets.StartArchivalScheduler(ctx, s)
		apiHandler.Store(http.Handler(apiMux))
		apiReady.Store(1)
		log.Printf("startup: all dependencies ready — API routes now active")
	}()

	addr := ":8080"
	log.Printf("startup: CivicSync listening on %s (API initialising in background)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: ListenAndServe failed: %v", err)
	}
}

