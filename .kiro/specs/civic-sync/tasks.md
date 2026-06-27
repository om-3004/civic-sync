# Implementation Plan: CivicSync (Community Hero)

## Overview

Implement the full CivicSync platform in Go (backend) and Dart/Flutter (Android app). The backend is a single Cloud Run binary; the app is a Flutter Android application. Tasks follow the architecture and data models in the design document, building up from foundational scaffolding through domain logic to integration and wiring.

## Tasks

- [x] 1. Set up Go backend project structure and core types
  - Create `cmd/server/` entry point and module layout (`internal/auth`, `internal/tickets`, `internal/triage`, `internal/geo`, `internal/middleware`, `internal/models`, `internal/store`, `web/static/`)
  - Define `internal/models/user.go` (`User` struct with all Firestore field tags)
  - Define `internal/models/ticket.go` (`Ticket`, `Location` structs with all Firestore and JSON tags)
  - Define `internal/store/store.go` — the `Store` interface with all nine methods matching the design
  - _Requirements: 1.6, 4.1, 5.1, 8.4, 9.1_

- [x] 2. Implement geospatial utilities
  - [x] 2.1 Implement `internal/geo/haversine.go` — `HaversineMeters` function and bounding-box helpers
    - Implement Haversine formula exactly as specified in the design
    - Implement `BoundingBoxDelta(lat, radiusMeters float64) (ΔlatDeg, ΔlngDeg float64)` helper
    - _Requirements: 4.1_

  - [x] 2.2 Write property test for Haversine symmetry and 50 m threshold (Property 6)
    - **Property 6: Haversine Distance is Symmetric and Satisfies the 50-Meter Threshold Correctly**
    - **Validates: Requirements 4.1**
    - Use `rapid.Float64Range` to generate random coordinate pairs; assert `H(a,b) == H(b,a)` within 1 mm tolerance

- [x] 3. Implement Firestore store layer
  - [x] 3.1 Implement `internal/store/firestore.go` — concrete `FirestoreStore` satisfying the `Store` interface
    - Implement all nine methods: `GetUser`, `UpsertUser`, `CreateTicket`, `GetTicket`, `QueryActiveTicketsByCategory`, `IncrementUpvote`, `UpdateTicketStatus`, `ArchiveExpiredTickets`, `HasUserUpvoted`
    - Wire Firestore composite indexes (document-level comments)
    - _Requirements: 1.6, 1.8, 4.1, 5.1, 8.4, 9.2_

  - [x] 3.2 Write unit tests for store layer with a mock store
    - Test `UpsertUser` idempotency using an in-memory fake
    - Test `IncrementUpvote` atomicity check (already-voted path)
    - _Requirements: 1.8, 5.3_

- [x] 4. Implement authentication middleware and login endpoint
  - [x] 4.1 Implement Google public-key cache (`internal/auth/keys.go`)
    - Fetch from `https://www.googleapis.com/oauth2/v3/certs` at startup
    - Background refresh goroutine every hour
    - _Requirements: 1.4, 11.3, 11.4_

  - [x] 4.2 Implement `JWTVerify` middleware (`internal/middleware/jwt.go`)
    - Strip Bearer prefix, parse header `kid`, verify RS256 signature, `exp`, `iss`, `aud`
    - Store `uid`/`email`/`name` in request context; return 401 on any failure
    - _Requirements: 1.4, 1.5_

  - [x] 4.3 Write property test for JWT verification (Property 1)
    - **Property 1: JWT Verification Accepts Valid Tokens and Rejects Invalid Ones**
    - **Validates: Requirements 1.4, 1.5**
    - Generate tokens with each defect type (expired, wrong sig, wrong iss, wrong aud); assert accept iff all four conditions satisfied

  - [x] 4.4 Implement `POST /auth/login` handler (`internal/auth/handler.go`)
    - Verify token, upsert user document, return `uid/email/name/role`
    - Return 500 on Firestore write failure
    - _Requirements: 1.3, 1.6, 1.7, 1.8_

  - [x] 4.5 Write property test for user profile idempotency (Property 2)
    - **Property 2: User Profile Creation is Idempotent**
    - **Validates: Requirements 1.6, 1.8**
    - Generate random user token payloads; call login N times (N=1..10); assert exactly one Firestore document per UID

- [x] 5. Implement role upgrade (PIN) endpoint
  - [x] 5.1 Implement `POST /auth/upgrade` handler (`internal/auth/handler.go`)
    - Validate non-empty PIN (400 on blank), compare to `MASTER_PIN` env var
    - Track `pin_failures` and `pin_lockout_until` in Firestore; return 429 after 5 consecutive failures for 15 minutes
    - Return 403 on wrong PIN, 409 if already official, 200 on success
    - _Requirements: 6.3, 6.4, 6.5, 6.6_

  - [x] 5.2 Write property test for PIN exact-match validation (Property 8)
    - **Property 8: PIN Validation Accepts Only the Exact Master PIN**
    - **Validates: Requirements 6.3, 6.4**
    - Generate random non-empty strings; assert success iff string equals master PIN exactly

  - [x] 5.3 Write property test for PIN rate limiting (Property 9)
    - **Property 9: PIN Rate Limiting Blocks After 5 Consecutive Failures**
    - **Validates: Requirements 6.6**
    - Generate sequences of ≥ 5 wrong PINs; assert 5th and subsequent attempts return 429 within lockout window

- [x] 6. Implement AI triage pipeline
  - [x] 6.1 Implement `internal/triage/agent.go` — Gemini API client and prompt construction
    - Build multimodal prompt (system instruction + user turn with image URL + coordinates)
    - Call Gemini 2.5 Flash `GenerateContent`; set 10 s timeout
    - _Requirements: 3.1_

  - [x] 6.2 Implement `internal/triage/parser.go` — `parseGeminiResponse` function
    - Strip markdown fences, `json.Unmarshal` into `TriageResult`
    - Validate category against `allowedCategories` enum; trim title to 100 chars, description to 500 chars
    - Return error for unknown category, empty fields, or unparseable JSON
    - _Requirements: 3.2, 3.5, 3.7_

  - [x] 6.3 Write property test for triage response parsing (Property 3)
    - **Property 3: AI Triage Response Parsing Always Produces a Valid Category**
    - **Validates: Requirements 3.2, 3.7**
    - Generate random Gemini response strings (valid JSON, malformed JSON, valid JSON with random category); assert parsed category ∈ allowed set OR error returned

  - [x] 6.4 Implement `POST /triage` handler (`internal/triage/handler.go`)
    - Validate `image_url` and `location` fields (400 on missing), invoke triage agent, return `TriageResult`
    - Return 422 on Gemini parse failure, 504 on timeout
    - _Requirements: 3.1, 3.2, 3.5_

- [x] 7. Implement duplicate detection and ticket creation
  - [x] 7.1 Implement bounding-box + Haversine duplicate detection (`internal/tickets/duplicate.go`)
    - Execute bounding-box pre-filter query against Firestore via `QueryActiveTicketsByCategory`
    - Run Haversine exact-distance check in-process; return closest match or nil
    - _Requirements: 4.1, 4.2, 4.5_

  - [x] 7.2 Write property test for duplicate detection correctness (Property 4)
    - **Property 4: Duplicate Detection Correctly Partitions Submissions by Distance and Category**
    - **Validates: Requirements 4.1, 4.4, 4.5**
    - Generate random (lat, lng) origin + ticket sets with random coordinates, statuses, categories; assert duplicate found iff active ticket within 50 m with matching category

  - [x] 7.3 Write property test for closest-duplicate selection (Property 5)
    - **Property 5: Duplicate Detection Returns the Closest Match**
    - **Validates: Requirements 4.2**
    - Generate origin + N tickets within 50 m with matching category; assert returned ticket has minimum Haversine distance

  - [x] 7.4 Implement `POST /tickets` handler (`internal/tickets/handler.go`)
    - Validate coordinates (400 on invalid/absent), run duplicate detection, create new ticket or return existing duplicate
    - Return 201 for new ticket (`"duplicate": false`), 200 for duplicate (`"duplicate": true`)
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.6_

- [x] 8. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. Implement ticket upvoting
  - [x] 9.1 Implement `POST /tickets/:id/upvote` handler (`internal/tickets/handler.go`)
    - Check ticket exists (404), check archived status (409), check prior upvote (409)
    - Atomically increment `upvotes` and update `updated_at`; return new `upvotes` count
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6_

  - [x] 9.2 Write property test for upvote idempotency (Property 7)
    - **Property 7: Upvote Increments Count Exactly Once Per Unique User**
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4**
    - Generate random upvote counts + user lists; assert count += 1 on first vote; 409 + unchanged count on repeat

- [x] 10. Implement ticket status management and archival
  - [x] 10.1 Implement `PUT /tickets/:id/status` handler (`internal/tickets/handler.go`)
    - Enforce `RequireRole("official")` middleware; validate transition against state machine
    - Atomically write `status`, `updated_at`; write `resolved_at = now()` when transitioning to `Done`
    - Return 400 on invalid transition, 409 on Archived ticket, 403 on non-official
    - _Requirements: 8.4, 8.5, 8.6, 9.1_

  - [x] 10.2 Write property test for status machine transitions (Property 13)
    - **Property 13: Ticket Status Transitions Follow the Permitted State Machine**
    - **Validates: Requirements 8.5, 8.6, 9.4**
    - Generate all `(currentStatus, targetStatus)` pairs; assert exactly `(To Do→In Progress)` and `(In Progress→Done)` succeed; all others fail with 400; Archived tickets return 409

  - [x] 10.3 Write property test for resolved_at on Done transition (Property 14)
    - **Property 14: resolved_at is Set Exactly When Status Transitions to Done**
    - **Validates: Requirements 9.1**
    - Generate random tickets; apply Done transition; assert `resolved_at` is non-null and within 5 s of transition time; assert `resolved_at` unchanged for other transitions

  - [x] 10.4 Implement archival background goroutine (`internal/tickets/archival.go`)
    - Launch goroutine at startup ticking every 15 minutes; call `store.ArchiveExpiredTickets`
    - Query tickets with `status == "Done"` and `resolved_at <= now - 7 days`; batch-update to `Archived` in batches of 500
    - _Requirements: 9.2_

  - [x] 10.5 Write property test for archival correctness (Property 15)
    - **Property 15: Archival Transitions All Eligible Tickets**
    - **Validates: Requirements 9.2**
    - Generate random ticket collections with various `resolved_at` ages; after archival run assert all eligible archived; no others modified

  - [x] 10.6 Write property test for archived mutation rejection (Property 16)
    - **Property 16: Archived Tickets Reject All Mutation Attempts**
    - **Validates: Requirements 9.4**
    - Generate random Archived tickets; assert any upvote or status change returns 409 and ticket unchanged

- [x] 11. Implement middleware chain, role enforcement, and rate limiting
  - [x] 11.1 Implement `RequireRole` middleware and `RecoverPanic`, `RequestID`, `Logger`, `CORSHeaders` middleware (`internal/middleware/`)
    - Wire middleware chain: `RecoverPanic → RequestID → Logger → CORSHeaders → JWTVerify (skip GET /) → RoleCheck`
    - _Requirements: 1.4, 6.5, 8.1_

  - [x] 11.2 Enforce role-based access control on all routes per the permissions matrix in the design
    - `PUT /tickets/:id/status` requires `official`; `POST /auth/upgrade` returns 409 for already-official users
    - _Requirements: 6.8, 8.1_

- [x] 12. Implement static landing page and startup wiring
  - [x] 12.1 Create `web/static/` assets — `index.html` and `styles.css`
    - Include problem statement, CTA with APK download link, embedded `<video>` demo (≤60 s), tech stack grid naming all 6 Google technologies
    - APK URL injected via build-time env var; HEAD-check at startup caches availability; render "unavailable" message if HEAD fails
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6_

  - [x] 12.2 Register `GET /` landing page handler using `go:embed` in `cmd/server/main.go`
    - Embed `web/static/*` into binary; serve with `http.FileServer`; set Cache-Control headers
    - _Requirements: 10.1, 10.5_

  - [x] 12.3 Wire `cmd/server/main.go` — full startup sequence
    - Load env config; init Firestore, Google PKI, Gemini in parallel via `sync.WaitGroup`
    - On any init failure: log and exit (Cloud Run marks unhealthy → 503 routed by platform)
    - Start archival goroutine; register all HTTP routes; call `http.ListenAndServe(":8080")`
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5_

  - [x] 12.4 Write property test for startup 503 behavior (Property 17)
    - **Property 17: Backend Returns HTTP 503 When Any Required Dependency Fails to Initialize**
    - **Validates: Requirements 11.5**
    - Generate all subsets of `{Firestore, GoogleKeys, Gemini}` where at least one fails; assert server returns 503 for all incoming requests when any dependency failed

- [x] 13. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 14. Implement Flutter Android application scaffold
  - [x] 14.1 Set up Flutter Android project with required packages
    - Add `google_sign_in`, `firebase_auth`, `cloud_firestore`, `firebase_storage`, `image_picker` (camera-only), `geolocator`, `google_maps_flutter`
    - Configure `google-services.json` and Firebase initialization in `main.dart`
    - _Requirements: 1.1, 2.1, 7.1_

  - [x] 14.2 Implement Google OAuth sign-in screen (`lib/screens/auth_screen.dart`)
    - Call `GoogleSignIn().signIn()`, obtain ID token, `POST /auth/login` to backend
    - Display error and return to sign-in screen on OAuth failure or backend 401
    - _Requirements: 1.1, 1.2, 1.3_

- [ ] 15. Implement citizen issue reporting flow
  - [x] 15.1 Implement camera-only capture with GPS (`lib/screens/report_flow.dart`)
    - Open native camera (gallery disabled at widget level); request camera permission (show error if denied)
    - Poll `geolocator` for coordinates with 50 m accuracy for up to 15 s; show error if unable to acquire
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [x] 15.2 Implement image upload and AI triage call (`lib/services/triage_service.dart`)
    - Upload captured image to Cloud Storage for Firebase; on failure show retry message without re-capturing
    - `POST /triage` with `image_url` and coordinates; display loading state
    - _Requirements: 2.6, 2.7, 3.1_

  - [ ] 15.3 Implement AI confirmation screen (`lib/screens/confirm_screen.dart`)
    - Display `category` dropdown, `title` text field (100-char limit), `description` text field (500-char limit) pre-filled from triage response
    - On AI error response show fallback manual-entry screen
    - On confirmation `POST /tickets`; handle duplicate response by prompting upvote
    - _Requirements: 3.3, 3.4, 3.6, 4.3_

- [ ] 16. Implement citizen issue feed
  - [ ] 16.1 Implement `CitizenFeedScreen` with map and list views (`lib/screens/citizen_feed_screen.dart`)
    - Subscribe to Firestore snapshot listener filtering `status IN [To Do, In Progress, Done]` ordered by `created_at` desc
    - Client-side filter: exclude Done tickets where `resolved_at < now - 7 days`; display "Resolved" badge on remaining Done tickets
    - Show error message and retry button on listener failure
    - _Requirements: 7.1, 7.2, 7.4, 7.5, 7.6, 9.3, 9.5_

  - [ ]* 16.2 Write property test for citizen feed query predicate (Property 10)
    - **Property 10: Citizen Feed Query Returns Exactly the Correct Ticket Set**
    - **Validates: Requirements 7.1, 7.5, 9.5**
    - Generate random ticket collections with various statuses and `resolved_at` offsets; assert returned set exactly matches `status ∈ {To Do, In Progress}` OR `(Done AND resolved_at > now − 7 days)`

  - [ ]* 16.3 Write property test for citizen feed sort order (Property 11)
    - **Property 11: Citizen Feed List is Always Sorted by created_at Descending**
    - **Validates: Requirements 7.2**
    - Generate random ticket collections satisfying feed criteria; assert list ordered by `created_at` descending

  - [ ] 16.4 Implement ticket detail view (`lib/screens/ticket_detail_screen.dart`)
    - Display all fields: `category`, `title`, `description`, `image_url`, `location`, `status`, `upvotes`, `created_at`, `updated_at`
    - Include upvote button that calls `POST /tickets/:id/upvote`; display 409 conflict message on duplicate upvote
    - _Requirements: 7.3, 5.1, 5.3_

- [ ] 17. Implement official management dashboard
  - [ ] 17.1 Implement `OfficialDashboardScreen` Kanban view (`lib/screens/official_dashboard_screen.dart`)
    - Subscribe to Firestore snapshot listener with `status IN [To Do, In Progress, Done]` ordered by `upvotes` desc, limit 200
    - Display full ticket details including reporter email; show status change controls
    - Call `PUT /tickets/:id/status` on status change; handle 400/409 errors
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.7_

  - [ ]* 17.2 Write property test for official dashboard query (Property 12)
    - **Property 12: Official Dashboard Query Returns at Most 200 Tickets Sorted by Upvotes Descending**
    - **Validates: Requirements 8.2**
    - Generate random ticket collections; assert at most 200 returned and sorted by `upvotes` descending

- [ ] 18. Implement role upgrade UI and real-time role listener
  - [ ] 18.1 Implement "City Official Access" PIN entry in profile settings (`lib/screens/profile_screen.dart`)
    - Show button for all authenticated users; display PIN prompt on tap
    - Validate non-empty PIN client-side (show validation error without transmitting)
    - Display error on 403; display lockout message on 429; hide button and show "already upgraded" message if already official
    - _Requirements: 6.1, 6.2, 6.3, 6.6, 6.8_

  - [ ] 18.2 Implement real-time user role listener and navigation routing
    - Subscribe to `/users/{uid}` Firestore document snapshot
    - When role changes to `"official"`, navigate to `OfficialDashboardScreen` within 2 s without logout
    - _Requirements: 6.7_

- [ ] 19. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 20. Write Dockerfile and Cloud Run deployment configuration
  - [ ] 20.1 Write `Dockerfile` using `scratch` base image
    - Build with `CGO_ENABLED=0 GOARCH=amd64 GOOS=linux`; copy single static binary and `web/static/` into `scratch` image
    - _Requirements: 11.1_

  - [ ] 20.2 Write `cloudbuild.yaml` / deployment script
    - Build image, push to Artifact Registry, deploy to Cloud Run with scale-to-zero (`--min-instances=0`) and required env vars (`PROJECT_ID`, `GEMINI_API_KEY`, `MASTER_PIN`)
    - _Requirements: 11.1, 11.2_

- [ ] 21. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP; they implement property-based and unit tests.
- The Go backend uses [`github.com/flyingmutant/rapid`](https://github.com/flyingmutant/rapid) for property-based testing; configure all property tests with a minimum of 100 iterations.
- Each property test file should begin with a comment: `// Feature: civic-sync, Property N: <title>`
- The `Store` interface enables in-memory fakes for all backend unit and property tests — use these instead of live Firestore in tests.
- Firestore composite indexes must be provisioned before integration tests run; document them in `firestore.indexes.json`.
- The archival goroutine stops when the Cloud Run instance scales to zero; this is acceptable for hackathon scale (see design trade-off analysis).
- The `MASTER_PIN` and `GEMINI_API_KEY` must be provisioned as Cloud Run environment variables via Secret Manager — never committed to source control.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1"] },
    { "id": 1, "tasks": ["2.1", "3.1"] },
    { "id": 2, "tasks": ["2.2", "3.2", "4.1"] },
    { "id": 3, "tasks": ["4.2", "6.1", "6.2", "7.1"] },
    { "id": 4, "tasks": ["4.3", "4.4", "6.3", "6.4", "7.2", "7.3"] },
    { "id": 5, "tasks": ["4.5", "7.4", "9.1", "10.1", "10.4"] },
    { "id": 6, "tasks": ["5.1", "9.2", "10.2", "10.3", "10.5", "10.6"] },
    { "id": 7, "tasks": ["5.2", "5.3", "11.1"] },
    { "id": 8, "tasks": ["11.2", "12.1", "12.2"] },
    { "id": 9, "tasks": ["12.3"] },
    { "id": 10, "tasks": ["12.4", "14.1"] },
    { "id": 11, "tasks": ["14.2"] },
    { "id": 12, "tasks": ["15.1"] },
    { "id": 13, "tasks": ["15.2"] },
    { "id": 14, "tasks": ["15.3", "16.1", "17.1"] },
    { "id": 15, "tasks": ["16.2", "16.3", "16.4", "17.2", "18.1"] },
    { "id": 16, "tasks": ["17.2", "18.2"] },
    { "id": 17, "tasks": ["20.1"] },
    { "id": 18, "tasks": ["20.2"] }
  ]
}
```
