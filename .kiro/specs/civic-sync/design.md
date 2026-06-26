# Design Document: CivicSync (Community Hero)

## Overview

CivicSync is a hyperlocal civic issue reporting platform built on an entirely Google-native technology stack. Citizens report infrastructure problems (potholes, flooding, electrical hazards, etc.) via a Flutter Android application. An AI triage pipeline powered by Google Gemini 2.5 Flash auto-classifies each submission. A Go backend deployed on Google Cloud Run enforces authentication, geospatial duplicate detection, role-based access control, ticket lifecycle rules, and archival. Government officials manage and resolve issues through a real-time dashboard that syncs instantly to all connected citizen feeds via Firestore snapshot listeners.

### Key Design Goals

- **Mobile-first, camera-locked authenticity**: all issue evidence must originate from live hardware capture.
- **AI-first classification**: citizen effort is minimised; the AI generates category/title/description.
- **Stateless, cold-start-safe backend**: single Go binary compiled with all assets embedded; scale-to-zero friendly.
- **Real-time transparency**: Firestore snapshot listeners propagate official actions to citizen devices within seconds.
- **Minimal surface area**: no user-managed passwords, no custom session tokens — only Google ID tokens.


---

## Architecture

### Component Diagram

```
┌──────────────────────────────────────────────────────────────────────┐
│                          CITIZEN / OFFICIAL                           │
│                       Flutter Android App                             │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────────┐  │
│  │ Auth Screen  │  │ Report Flow  │  │   Feed / Dashboard         │  │
│  │ Google OAuth │  │ Camera+GPS   │  │   Map + List / Kanban      │  │
│  └──────┬───────┘  └──────┬───────┘  └────────────┬───────────────┘  │
└─────────┼────────────────┼───────────────────────┼───────────────────┘
          │ HTTPS           │ HTTPS                  │ Firestore SDK
          ▼                 ▼                        ▼
┌──────────────────────────────────────────┐  ┌──────────────────────┐
│          Go Backend  (Cloud Run)          │  │    Cloud Firestore   │
│  ┌──────────────────────────────────────┐│  │  /users  /tickets    │
│  │  Middleware: JWT Verify (Google PKI) ││  │  real-time listeners │
│  ├──────────────────────────────────────┤│  └──────────────────────┘
│  │  POST /auth/login                    ││
│  │  POST /tickets                       ││  ┌──────────────────────┐
│  │  POST /tickets/:id/upvote            ││  │  Cloud Storage (FB)  │
│  │  PUT  /tickets/:id/status            ││  │  ticket images       │
│  │  POST /auth/upgrade                  ││  │  android APK         │
│  │  GET  /                              ││  └──────────────────────┘
│  ├──────────────────────────────────────┤│
│  │  Geospatial Duplicate Detector       ││  ┌──────────────────────┐
│  │  AI Triage Agent                     ├┼──► Gemini 2.5 Flash API │
│  │  Archival Scheduler (background)     ││  └──────────────────────┘
│  │  Static File Server (landing page)   ││
│  └──────────────────────────────────────┘│  ┌──────────────────────┐
└──────────────────────────────────────────┘  │ Firebase Auth /      │
                                              │ Google OAuth PKI     │
                                              └──────────────────────┘
```

### Deployment Topology

| Component | Host | Scaling |
|---|---|---|
| Go backend binary | Google Cloud Run | Scale to zero; min 0, max N instances |
| Static landing page assets | Embedded in Go binary (`embed.FS`) | N/A — in-process |
| Flutter APK | Cloud Storage for Firebase | Static object |
| Ticket images | Cloud Storage for Firebase | Static objects |
| Database | Cloud Firestore (Native mode) | Managed, auto-scaling |
| Auth | Firebase Auth + Google OAuth | Managed |
| AI | Gemini 2.5 Flash via Google AI Studio API | Managed |


---

## Components and Interfaces

### 1. Flutter Android Application

**Responsibilities**:
- Google OAuth sign-in and token forwarding to the backend.
- Camera-only image capture (gallery disabled at the widget level).
- GPS coordinate acquisition and validation before submitting.
- Sending image + GPS to backend; displaying AI-generated fields for user confirmation.
- Subscribing to Firestore snapshot listeners for real-time feed updates.
- Role-based UI morphing: citizen feed vs. official Kanban dashboard.

**Key Flutter packages** (illustrative; actual project may vary):
| Package | Purpose |
|---|---|
| `google_sign_in` | Google OAuth flow |
| `image_picker` (camera-only) | Hardware camera capture |
| `geolocator` | GPS coordinate acquisition |
| `firebase_auth` | Auth state management |
| `cloud_firestore` | Snapshot listeners |
| `firebase_storage` | Image upload |
| `google_maps_flutter` | Map view with ticket markers |

**Role-based UI routing**:
```
Firestore /users/{uid}.role == "citizen"  →  CitizenFeedScreen
Firestore /users/{uid}.role == "official" →  OfficialDashboardScreen
```
The app listens to the user document in Firestore. When the backend upgrades a user's role, the snapshot fires and the Navigator replaces the active screen without requiring logout.

### 2. Go Backend (Cloud Run)

**Package structure**:
```
cmd/server/          — main entry point; wires dependencies; calls http.ListenAndServe
internal/
  auth/              — JWT verification, user profile upsert
  tickets/           — create, upvote, status update, duplicate check, archival
  triage/            — Gemini integration, prompt construction, response parsing
  geo/               — Haversine distance calculation
  middleware/         — auth middleware, rate-limit middleware
  models/            — shared Go structs mirroring Firestore schemas
  store/             — Firestore client wrapper
web/
  static/            — embedded landing page HTML/CSS/JS assets (go:embed)
```

**Startup sequence** (Requirement 11.3–11.5):
```
main() →
  1. Load env config (PROJECT_ID, GEMINI_API_KEY, MASTER_PIN, etc.)
  2. Init Firestore client          (fail → log + exit 1)
  3. Fetch + cache Google public keys (fail → log + exit 1)
  4. Init Gemini AI client          (fail → log + exit 1)
  5. Start archival background goroutine
  6. Register HTTP routes
  7. http.ListenAndServe(":8080")
```
If any step 2–4 fails the process exits immediately; Cloud Run marks the instance unhealthy and does not route traffic to it (satisfying Req 11.5).

### 3. Firestore Store Layer

All Firestore operations are encapsulated behind a `store.Store` interface, enabling unit-testable mocking:

```go
type Store interface {
    GetUser(ctx, uid) (*User, error)
    UpsertUser(ctx, *User) error
    CreateTicket(ctx, *Ticket) error
    GetTicket(ctx, id) (*Ticket, error)
    QueryActiveTicketsByCategory(ctx, lat, lng, radiusMeters float64, category string) ([]*Ticket, error)
    IncrementUpvote(ctx, ticketID, voterUID string) error
    UpdateTicketStatus(ctx, ticketID, newStatus string) error
    ArchiveExpiredTickets(ctx) error
    HasUserUpvoted(ctx, ticketID, uid string) (bool, error)
}
```

### 4. AI Triage Component (`internal/triage`)

Wraps the Gemini 2.5 Flash multimodal API. Accepts an image bytes payload and location, returns a structured `TriageResult`.

### 5. Geospatial Component (`internal/geo`)

Pure functions: Haversine distance, bounding-box pre-filter coordinates. No external dependencies.

### 6. Archival Scheduler

A goroutine launched at startup that ticks every 15 minutes, calling `store.ArchiveExpiredTickets`. This queries Firestore for tickets where `status == "Done"` and `resolved_at < now - 7 days`, then batch-updates them to `status = "Archived"`.


---

## Data Models

### Firestore Collection: `/users/{uid}`

```json
{
  "uid":        "google_oauth_sub_string",   // PK — matches Firebase Auth UID
  "email":      "user@example.com",
  "name":       "Alex Carter",
  "role":       "citizen",                   // "citizen" | "official"
  "created_at": "2026-06-26T14:30:00Z",      // ISO-8601 UTC, set once on first login
  "pin_failures":      0,                    // consecutive wrong PIN count (Req 6.6)
  "pin_lockout_until": null                  // null | ISO-8601 UTC timestamp
}
```

**Firestore indexes**: none required for single-document lookups by UID (default document path).

### Firestore Collection: `/tickets/{ticketId}`

```json
{
  "id":          "uuid-v4-string",
  "category":    "Pothole",                  // enum — see Req 3.7
  "title":       "Deep Pothole near Main Crossroad",
  "description": "Severe road deterioration disrupting traffic flow.",
  "image_url":   "https://storage.googleapis.com/...",
  "location": {
    "latitude":  23.0225,
    "longitude": 72.5714
  },
  "status":      "To Do",                    // "To Do" | "In Progress" | "Done" | "Archived"
  "upvotes":     14,
  "upvoted_by":  ["uid1", "uid2"],           // array of UIDs; used for idempotency check
  "reported_by": "google_oauth_sub_string",
  "created_at":  "2026-06-26T14:35:00Z",
  "updated_at":  "2026-06-26T14:35:00Z",
  "resolved_at": null                        // null | ISO-8601 UTC — set when status → "Done"
}
```

**Firestore composite indexes**:
| Fields | Order | Purpose |
|---|---|---|
| `status` ASC, `created_at` DESC | — | Citizen feed query |
| `status` ASC, `upvotes` DESC | — | Official dashboard sort |
| `status` ASC, `resolved_at` ASC | — | Archival query |
| `category` ASC, `status` ASC | — | Duplicate detection pre-filter |

### Go Struct Mirrors

```go
// internal/models/user.go
type User struct {
    UID            string     `firestore:"uid"`
    Email          string     `firestore:"email"`
    Name           string     `firestore:"name"`
    Role           string     `firestore:"role"`           // "citizen" | "official"
    CreatedAt      time.Time  `firestore:"created_at"`
    PINFailures    int        `firestore:"pin_failures"`
    PINLockoutUntil *time.Time `firestore:"pin_lockout_until"`
}

// internal/models/ticket.go
type Location struct {
    Latitude  float64 `firestore:"latitude"  json:"latitude"`
    Longitude float64 `firestore:"longitude" json:"longitude"`
}

type Ticket struct {
    ID          string    `firestore:"id"          json:"id"`
    Category    string    `firestore:"category"    json:"category"`
    Title       string    `firestore:"title"       json:"title"`
    Description string    `firestore:"description" json:"description"`
    ImageURL    string    `firestore:"image_url"   json:"image_url"`
    Location    Location  `firestore:"location"    json:"location"`
    Status      string    `firestore:"status"      json:"status"`
    Upvotes     int       `firestore:"upvotes"     json:"upvotes"`
    UpvotedBy   []string  `firestore:"upvoted_by"  json:"-"`
    ReportedBy  string    `firestore:"reported_by" json:"reported_by"`
    CreatedAt   time.Time `firestore:"created_at"  json:"created_at"`
    UpdatedAt   time.Time `firestore:"updated_at"  json:"updated_at"`
    ResolvedAt  *time.Time `firestore:"resolved_at" json:"resolved_at,omitempty"`
}
```


---

## API Design

All endpoints except `GET /` require `Authorization: Bearer <Google-ID-Token>` and pass through the JWT verification middleware. Error responses always return `{"error": "<message>"}`.

### Base URL
`https://<cloud-run-service-url>`

---

### `POST /auth/login`

Verifies the Google ID token and upserts a user profile. Called by the Flutter app after a successful Google OAuth flow.

**Request headers**: `Authorization: Bearer <id_token>`
**Request body**: none

**Response 200**:
```json
{
  "uid":   "google_oauth_sub_string",
  "email": "user@example.com",
  "name":  "Alex Carter",
  "role":  "citizen"
}
```
**Response 401**: token invalid / expired.
**Response 500**: Firestore write failure.

---

### `POST /auth/upgrade`

Validates the secret PIN and upgrades the caller's role to `"official"`.

**Request body**:
```json
{ "pin": "HERO-2026" }
```
**Response 200**:
```json
{ "role": "official" }
```
**Response 400**: empty / blank PIN.
**Response 403**: wrong PIN.
**Response 409**: caller is already `"official"`.
**Response 429**: PIN attempts locked out (5 consecutive failures within the lockout window).

---

### `POST /triage`

Submits an image Storage URL and GPS coordinates to the AI triage pipeline. Returns structured classification for the citizen confirmation screen.

**Request body**:
```json
{
  "image_url": "https://storage.googleapis.com/bucket/path/img.jpg",
  "location":  { "latitude": 23.0225, "longitude": 72.5714 }
}
```
**Response 200**:
```json
{
  "category":    "Pothole",
  "title":       "Deep Pothole near Main Crossroad",
  "description": "Severe road deterioration disrupting traffic flow."
}
```
**Response 400**: missing or invalid fields.
**Response 422**: AI returned an unparseable response.
**Response 401**: unauthenticated.

---

### `POST /tickets`

Creates a new ticket after duplicate detection. Returns either a newly created ticket or an existing duplicate.

**Request body**:
```json
{
  "category":    "Pothole",
  "title":       "Deep Pothole near Main Crossroad",
  "description": "Severe road deterioration disrupting traffic flow.",
  "image_url":   "https://storage.googleapis.com/...",
  "location":    { "latitude": 23.0225, "longitude": 72.5714 }
}
```
**Response 201** — new ticket created:
```json
{ "ticket": { ...TicketObject... }, "duplicate": false }
```
**Response 200** — duplicate found:
```json
{ "ticket": { ...TicketObject... }, "duplicate": true }
```
**Response 400**: missing / invalid coordinates.
**Response 401**: unauthenticated.

---

### `POST /tickets/:id/upvote`

Increments the upvote count for a ticket. Idempotency-safe: a second call from the same user returns 409.

**Response 200**:
```json
{ "upvotes": 15 }
```
**Response 401**: unauthenticated.
**Response 404**: ticket not found.
**Response 409**: already upvoted, or ticket is `Archived`.

---

### `PUT /tickets/:id/status`

Updates a ticket's KanbanStatus. Restricted to users with `role == "official"`.

**Request body**:
```json
{ "status": "In Progress" }
```
**Response 200**:
```json
{ "id": "...", "status": "In Progress", "updated_at": "..." }
```
**Response 400**: invalid status transition.
**Response 401**: unauthenticated.
**Response 403**: caller is not `"official"`.
**Response 404**: ticket not found.
**Response 409**: ticket is `Archived`.

---

### `GET /`

Serves the static landing page HTML. No authentication required.

**Response 200**: `Content-Type: text/html`; landing page body.

---

### Middleware Chain

```
[Request]
    → RecoverPanic
    → RequestID
    → Logger
    → CORSHeaders
    → JWTVerify  (skipped for GET /)
    → RoleCheck  (applied per-route)
    → Handler
```


---

## AI Triage Pipeline Design

The triage pipeline is invoked synchronously during the `POST /triage` request. It is intentionally isolated behind its own route so the Flutter app can display a loading state while the AI processes the image, then show the confirmation screen before the citizen chooses to submit.

### Sequence

```
Flutter App
  1. Camera captures image → upload to Cloud Storage → get image_url
  2. POST /triage  { image_url, location }
      │
      ▼
  Go backend (internal/triage.Agent)
  3. Fetch image bytes from Cloud Storage (or pass URL directly if Gemini accepts URLs)
  4. Construct multimodal prompt (see below)
  5. Call Gemini 2.5 Flash API  (GenerateContent, multimodal)
  6. Parse JSON from Gemini response
  7. Validate: category must be in allowed enum
  8. Return TriageResult  {category, title, description}
      │
      ▼
  Flutter App
  9. Pre-fill confirmation screen fields
  10. Citizen edits if needed, then submits → POST /tickets
```

### Gemini Prompt Design

```
System instruction:
  You are an infrastructure hazard classifier for a civic reporting platform.
  Analyze the provided image and location, then respond ONLY with valid JSON
  matching this schema (no markdown, no explanation):
  {
    "category":    "<one of: Pothole | Water Clogging | Drain Overflow | Electrical Hazard | Other>",
    "title":       "<concise title, max 100 chars>",
    "description": "<detailed description, max 500 chars>"
  }

User turn:
  Image: [inline image bytes or URL]
  Location: latitude=<lat>, longitude=<lng>
  Classify the infrastructure hazard shown in this image.
```

### Response Parsing

```go
type TriageResult struct {
    Category    string `json:"category"`
    Title       string `json:"title"`
    Description string `json:"description"`
}

var allowedCategories = map[string]bool{
    "Pothole": true, "Water Clogging": true,
    "Drain Overflow": true, "Electrical Hazard": true, "Other": true,
}

func parseGeminiResponse(raw string) (*TriageResult, error) {
    // 1. Strip any accidental markdown code fences
    // 2. json.Unmarshal into TriageResult
    // 3. Validate category in allowedCategories
    // 4. Trim title to 100 chars, description to 500 chars
    // 5. Return error if category unknown or fields empty
}
```

### Error Handling

| Gemini failure | Backend behaviour | App behaviour |
|---|---|---|
| Non-2xx HTTP from Gemini API | Return HTTP 422 | Show manual-entry fallback |
| Valid HTTP but unparseable JSON | Return HTTP 422 | Show manual-entry fallback |
| Category not in allowed enum | Return HTTP 422 | Show manual-entry fallback |
| Network timeout (>10 s) | Return HTTP 504 | Show manual-entry fallback |


---

## Geospatial Duplicate Detection Algorithm

Duplicate detection runs inside `POST /tickets`, before any Firestore write. It is a pure in-process computation using the Haversine formula; no geospatial database extensions are required.

### Algorithm

```
Input: (category, lat, lng)

Step 1 — Bounding-box pre-filter (cheap Firestore query)
  Δlat  = 50m / 111,320 m/deg  ≈ 0.000449°
  Δlng  = 50m / (111,320 × cos(lat))°
  Query Firestore /tickets where:
    status IN ["To Do", "In Progress"]
    AND category == submitted_category
    AND location.latitude  BETWEEN (lat - Δlat)  AND (lat + Δlat)
    AND location.longitude BETWEEN (lng - Δlng)  AND (lng + Δlng)

Step 2 — Haversine exact distance check (in-process)
  For each candidate ticket C:
    d = haversine(lat, lng, C.location.latitude, C.location.longitude)
    IF d <= 50.0 → add to matches list

Step 3 — Return closest match (if any)
  IF matches is non-empty:
    return closest ticket by distance (min d)
  ELSE:
    proceed to create new ticket
```

### Haversine Implementation

```go
// internal/geo/haversine.go
const earthRadiusMeters = 6_371_000.0

func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
    φ1, φ2 := lat1*math.Pi/180, lat2*math.Pi/180
    Δφ := (lat2-lat1) * math.Pi / 180
    Δλ := (lng2-lng1) * math.Pi / 180
    a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
        math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
    return earthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
```

### Design Notes

- The bounding-box step reduces Firestore reads significantly in dense areas: most reads are excluded before the Haversine pass.
- The bounding-box longitude correction uses `cos(lat)` to remain accurate near the poles (irrelevant for India-centric deployments but correct in principle).
- Only tickets with `status IN ["To Do", "In Progress"]` are considered active (Req 4.5). `Done` and `Archived` tickets do not block new submissions.
- The Firestore composite index on `(category, status, location.latitude, location.longitude)` is approximated; Firestore does not support true geospatial range queries, so the bounding-box uses two separate range fields. In practice this means the Firestore query filters on `category` and `status` and returns all matching documents, and the Go layer performs the lat/lng range check plus the Haversine step in memory. For a hyperlocal deployment with modest data volumes this is performant enough.


---

## Real-Time Feed Architecture (Firestore Snapshot Listeners)

The Flutter app uses Firestore's native snapshot listener SDK to push changes to the UI without polling.

### Citizen Feed Listener

```dart
// Subscribes to all non-archived, non-expired tickets
FirebaseFirestore.instance
    .collection('tickets')
    .where('status', whereIn: ['To Do', 'In Progress', 'Done'])
    .orderBy('created_at', descending: true)
    .snapshots()
    .listen((snapshot) {
        final now = DateTime.now();
        final tickets = snapshot.docs
            .map((d) => Ticket.fromFirestore(d))
            .where((t) {
                // Client-side filter: exclude Done tickets past 7-day window
                if (t.status == 'Done' && t.resolvedAt != null) {
                    return now.difference(t.resolvedAt!).inDays < 7;
                }
                return true;
            })
            .toList();
        _updateFeedState(tickets);
    });
```

The 7-day expiry filter is applied client-side in real time. The backend's archival scheduler also periodically flips `status` to `Archived` in Firestore, which will cause those documents to fall outside the `whereIn` filter and disappear from the listener stream automatically.

### Official Dashboard Listener

```dart
FirebaseFirestore.instance
    .collection('tickets')
    .where('status', whereIn: ['To Do', 'In Progress', 'Done'])
    .orderBy('upvotes', descending: true)
    .limit(200)
    .snapshots()
    .listen((snapshot) { ... });
```

### Update Propagation Guarantee (Req 7.4, Req 8.7)

Firestore's typical mobile SDK latency is under 1 second for snapshot delivery on active connections. The requirements specify ≤3 s (citizen feed) and ≤5 s (official → citizen propagation). These are satisfied by the Firestore SDK under normal network conditions. No additional push mechanism is needed.

### User Role Listener

The app also subscribes to its own user document to detect role upgrades in real time:

```dart
FirebaseFirestore.instance
    .collection('users')
    .doc(currentUser.uid)
    .snapshots()
    .listen((snap) {
        final role = snap.data()?['role'] as String? ?? 'citizen';
        if (role == 'official') _navigateToDashboard();
    });
```

This drives the ≤2-second UI transition required by Req 6.7.


---

## Role-Based Access Control Design

### Roles and Permissions Matrix

| Action | Unauthenticated | `citizen` | `official` |
|---|:---:|:---:|:---:|
| `GET /` (landing page) | ✅ | ✅ | ✅ |
| `POST /auth/login` | ✅ | ✅ | ✅ |
| `POST /triage` | ❌ | ✅ | ✅ |
| `POST /tickets` | ❌ | ✅ | ✅ |
| `POST /tickets/:id/upvote` | ❌ | ✅ | ✅ |
| `POST /auth/upgrade` | ❌ | ✅ | ❌ (409) |
| `PUT /tickets/:id/status` | ❌ | ❌ (403) | ✅ |

### JWT Verification Middleware

```go
func JWTVerify(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        // 1. Strip "Bearer " prefix
        // 2. Parse JWT header → extract kid
        // 3. Fetch matching public key from in-memory cache (refreshed every hour)
        // 4. Verify signature, exp, iss == "accounts.google.com", aud == CLIENT_ID
        // 5. Extract sub (UID), email, name → store in request context
        // 6. On any failure → 401
        next.ServeHTTP(w, r)
    })
}
```

### Google Public Key Caching

Google's public keys are fetched from `https://www.googleapis.com/oauth2/v3/certs` at startup and refreshed every hour in a background goroutine. This prevents per-request network latency and ensures cold starts complete within the 3-second target (Req 11.4).

### Role Enforcement (per-route)

```go
func RequireRole(role string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            uid := uidFromContext(r.Context())
            user, err := store.GetUser(r.Context(), uid)
            if err != nil || user.Role != role {
                http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### PIN Rate Limiting (Req 6.6)

Tracked in the user's Firestore document (`pin_failures`, `pin_lockout_until`), not in process memory, so it survives container restarts:

```
On each POST /auth/upgrade:
  1. Check pin_lockout_until: if set and future → return 429
  2. Validate PIN:
     - Match → reset pin_failures to 0, update role to "official"
     - No match:
         pin_failures += 1
         if pin_failures >= 5:
             pin_lockout_until = now + 15 min
             return 429
         return 403
```


---

## Ticket Lifecycle / State Machine

### States

```
                   ┌──────────────────────────────────┐
                   │                                  │
              [submit]                          [7 days elapse]
                   │                                  │
                   ▼                                  │
             ┌─────────┐   [official]   ┌─────────────┐   [official]   ┌──────┐
  ─(create)─►│  To Do  ├──────────────►│  In Progress ├──────────────►│ Done ├──────────►[Archived]
             └─────────┘               └─────────────┘               └──────┘
```

### Transition Rules

| Current Status | Allowed Next Status | Triggered by |
|---|---|---|
| `To Do` | `In Progress` | Official via `PUT /tickets/:id/status` |
| `In Progress` | `Done` | Official via `PUT /tickets/:id/status` |
| `Done` | `Archived` | Backend archival scheduler (automatic, after 7 days) |
| `Archived` | — | No further transitions allowed |

**Invalid transitions** (Req 8.5, 8.6): any `status` value not matching the table above returns HTTP 400. Backward transitions (`Done` → `In Progress`) and skip transitions (`To Do` → `Done`) are rejected.

### `resolved_at` Timestamp (Req 9.1)

When `PUT /tickets/:id/status` transitions a ticket to `Done`, the backend atomically writes both `status = "Done"` and `resolved_at = now()` in the same Firestore update. This is the authoritative start of the 7-day ArchivePeriod.

### Archival Trigger (Req 9.2)

```go
// internal/tickets/archival.go
func ArchiveExpiredTickets(ctx context.Context, store Store) error {
    cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
    tickets, err := store.QueryDoneTicketsBefore(ctx, cutoff)
    // Batch update: status = "Archived"
    // Reject any upvote or status change on Archived tickets with 409
}
```

The scheduler runs every 15 minutes. In the worst case a ticket is archived up to 15 minutes late, which is acceptable for a civic platform.

### Archived Ticket Constraints (Req 9.4)

- `POST /tickets/:id/upvote` on an archived ticket → HTTP 409
- `PUT /tickets/:id/status` on an archived ticket → HTTP 409
- The Firestore snapshot listener's `whereIn` filter excludes `Archived`, so archived tickets disappear from citizen feeds automatically.


---

## Archival Mechanism Design

### Design Choice: Scheduled Background Goroutine vs. Firestore TTL

Cloud Firestore does not natively support document TTLs with custom status transitions. The alternatives considered were:

| Approach | Pros | Cons |
|---|---|---|
| Background goroutine in Go (chosen) | No external scheduler; self-contained in binary | Stops if instance scales to zero |
| Cloud Scheduler + Cloud Run job | Reliable even at zero scale | Adds infrastructure complexity |
| Client-side expiry only | Zero backend cost | Archived documents linger in DB |

**Decision**: The background goroutine is sufficient for a hackathon-scale deployment. Because Cloud Run scales to zero, the goroutine may not run during extended periods of inactivity. This is acceptable: the first incoming request after a cold start re-initialises the goroutine, and the archival catches up immediately. For a production deployment, a Cloud Scheduler job calling a dedicated `/internal/archive` endpoint would be preferred.

### Archival Query

```
Firestore query:
  collection: /tickets
  where: status == "Done"
  where: resolved_at <= (now - 7 days)
  limit: 500 (per batch)
```

Firestore batch writes are used to update up to 500 documents atomically per call. For deployments with more than 500 expiring tickets in a single batch window, the scheduler loops until no more results are returned.

### Visibility Rules Summary

| Status | Citizen map/list | Official dashboard | Notes |
|---|:---:|:---:|---|
| `To Do` | ✅ | ✅ | |
| `In Progress` | ✅ | ✅ | |
| `Done` (< 7 days) | ✅ (Resolved badge) | ✅ | |
| `Done` (≥ 7 days, not yet archived) | ❌ (client filter) | ✅ | |
| `Archived` | ❌ | ❌ | Excluded by `whereIn` filter |


---

## Landing Page Serving Architecture

### Embedding Strategy

The landing page assets (HTML, CSS, any inline JS, the demo video thumbnail) are embedded directly into the Go binary using Go's `embed` package:

```go
//go:embed web/static/*
var staticFiles embed.FS

func RegisterLandingPage(mux *http.ServeMux) {
    sub, _ := fs.Sub(staticFiles, "web/static")
    mux.Handle("GET /", http.FileServer(http.FS(sub)))
}
```

This means:
- Zero filesystem I/O on the container at request time.
- The binary is fully self-contained; no volume mounts or sidecar containers.
- The landing page is served from memory — latency is sub-millisecond.

### Content Requirements (Req 10.2–10.4)

| Section | Implementation |
|---|---|
| Problem statement + CTA | Static HTML section with prominent APK download button |
| APK download link | `<a href="<firebase-storage-signed-url>" download>` — URL is injected at build time as an environment variable |
| Video demo | `<video src="...">` or `<iframe>` to a hosted 60-second clip; autoplay-disabled, controls enabled |
| Tech stack visual | HTML/CSS card grid listing all 6 Google technologies with icons |

### APK URL Availability (Req 10.6)

The backend performs a lightweight HEAD request to the APK URL at startup and caches the availability result. The landing page template renders either the download link or an "APK temporarily unavailable" message based on this cached flag, refreshed every 5 minutes. This avoids broken links without requiring real-time storage checks on each page load.

### MIME Types and Caching

```go
// Served with appropriate headers
"text/html; charset=utf-8"       // index.html
"text/css; charset=utf-8"        // styles.css
"image/png", "image/svg+xml"     // logos / icons
Cache-Control: public, max-age=3600  // all static assets
```


---

## Cold-Start Optimization Strategy

The requirement is that the first request after a cold start is handled within 3 seconds (Req 11.4). The following strategies are employed:

### 1. Single Static Binary

The Go binary is compiled with `CGO_ENABLED=0 GOARCH=amd64 GOOS=linux` and produces a single statically linked binary. No dynamic libraries to load, no interpreter start-up.

### 2. Minimal Base Image

```dockerfile
FROM scratch
COPY civicsync-backend /civicsync-backend
COPY web/static /web/static
ENTRYPOINT ["/civicsync-backend"]
```

Using `scratch` (or `gcr.io/distroless/static`) eliminates OS-level startup overhead. Container cold starts on Cloud Run for a Go binary in a scratch container are typically under 500ms.

### 3. Parallel Dependency Initialisation

The three external clients (Firestore, Google PKI, Gemini) are initialised concurrently using a `sync.WaitGroup`:

```go
var wg sync.WaitGroup
var firestoreErr, keysErr, geminiErr error

wg.Add(3)
go func() { defer wg.Done(); firestoreClient, firestoreErr = initFirestore(ctx) }()
go func() { defer wg.Done(); googleKeys, keysErr = fetchGooglePublicKeys(ctx) }()
go func() { defer wg.Done(); geminiClient, geminiErr = initGemini(ctx) }()
wg.Wait()

if firestoreErr != nil || keysErr != nil || geminiErr != nil {
    // Req 11.5: refuse requests, return 503
    log.Fatal("dependency init failed", ...)
}
```

All three are network-bound operations. Running them in parallel reduces startup time from ~3× the slowest call to ~1× the slowest call.

### 4. Pre-warmed Landing Page

Because landing page assets are embedded, `GET /` can be served immediately even if the background initialisations have not yet completed. An `isReady` atomic flag gates all authenticated routes; the landing page is always served.

### 5. Minimum Instances Configuration

For hackathon evaluation (where a judge visit will trigger a cold start), Cloud Run can be configured with `--min-instances=1` to keep at least one instance warm. This is optional and costs money, so the default remains zero with the optimizations above providing an acceptable cold-start time.

### Expected Cold-Start Timeline

| Step | Duration (estimate) |
|---|---|
| Container image pull (cached in Cloud Run) | ~0ms |
| Binary load + OS start | ~100ms |
| Parallel dependency init (slowest: Firestore ~800ms) | ~900ms |
| HTTP server ready | ~50ms |
| **Total** | **~1,050ms** |

This is well within the 3-second requirement.


---

## Security Considerations

### Authentication and Token Security

- **No custom sessions**: the system is stateless. All authorization flows from Google-signed JWTs. No session cookies, no custom tokens.
- **Token verification**: the backend verifies `iss`, `aud`, `exp`, and the RS256 signature against Google's cached public keys on every authenticated request. A token cannot be reused after expiry.
- **ID token, not access token**: the Flutter app sends the ID token (not the access token), which is the correct token for backend identity verification.

### Role Escalation Security

- The master PIN (`HERO-2026` or configured) is stored only as an environment variable on Cloud Run, never in source code, Firestore, or build artifacts.
- PIN brute-force is mitigated by the 5-attempt / 15-minute lockout stored in Firestore (survives container restarts).
- An already-`official` user cannot re-submit a PIN upgrade request (HTTP 409 + no Firestore write).

### Upvote Idempotency

- The `upvoted_by` array in each ticket document stores UIDs. The backend checks this array before incrementing, preventing multiple upvotes from a single account. Firestore transactions ensure the read-check-write is atomic.

### Image Upload Security

- Images are uploaded directly from the Flutter app to Cloud Storage using a Firebase Storage security rule that allows authenticated writes to `images/{uid}/{filename}`.
- The backend only receives the resulting Storage URL, not the raw bytes on the `/tickets` creation path. The triage pipeline fetches bytes from Storage using service-account credentials.
- Cloud Storage security rules prevent authenticated users from reading other users' paths if needed.

### RBAC Enforcement

- `PUT /tickets/:id/status` rejects any non-`official` caller with HTTP 403 before touching Firestore.
- The backend re-reads the user's role from Firestore on each sensitive request, not just from the JWT claims, so a downgraded role takes effect immediately.

### Input Validation

- Coordinates are validated: latitude in `[-90, 90]`, longitude in `[-180, 180]`.
- Title max 100 chars, description max 500 chars (enforced by backend, not just app).
- Category must be in the allowed enum; any other value is rejected with HTTP 400.
- PIN input is trimmed of whitespace before comparison; an all-whitespace PIN is rejected client-side and backend-side.

### Secrets Management

| Secret | Storage |
|---|---|
| `GEMINI_API_KEY` | Cloud Run env var (via Secret Manager) |
| `MASTER_PIN` | Cloud Run env var (via Secret Manager) |
| Firebase service account | Application Default Credentials (Cloud Run identity) |
| Google OAuth client ID | Flutter app config (public, non-secret) |


---

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

**Property reflection notes**: After reviewing all prework entries, the following consolidations were made:
- 1.4 + 1.5 (JWT verification + 401 on failure) are combined into one property.
- 5.1 + 5.2 (upvote increment + timestamp update) are combined into one property.
- 5.3 + 5.4 (duplicate upvote idempotency) are the same criterion restated; merged into one property.
- 4.1 + 4.4 (duplicate detection positive + negative path) are combined into one property.
- 8.5 + 8.6 (valid/invalid status transitions) are combined into one property.
- 9.2 (archival by scheduler) and 9.4 (archived tickets reject mutations) are kept separate since they test different behaviors.

---

### Property 1: JWT Verification Accepts Valid Tokens and Rejects Invalid Ones

*For any* Google ID token presented to the backend, the JWT verification middleware shall accept the token if and only if it carries a valid signature from Google's public keys, a non-expired `exp` claim, the correct `iss` (`accounts.google.com`), and the correct `aud` (the configured client ID). Any token that fails any of these checks shall cause the backend to return HTTP 401.

**Validates: Requirements 1.4, 1.5**

---

### Property 2: User Profile Creation is Idempotent

*For any* valid Google ID token representing a user, calling `POST /auth/login` any number of times (N ≥ 1) shall result in exactly one user profile document existing in Firestore with the correct `uid`, `email`, `name`, `role = "citizen"`, and `created_at` set on the first call. No additional documents shall be created on subsequent calls.

**Validates: Requirements 1.6, 1.8**

---

### Property 3: AI Triage Response Parsing Always Produces a Valid Category

*For any* response string returned by the Gemini API, the backend triage parser shall produce either: (a) a `TriageResult` whose `category` field is one of `{Pothole, Water Clogging, Drain Overflow, Electrical Hazard, Other}`, or (b) an error. A `TriageResult` with a category outside the allowed set shall never be returned to the caller.

**Validates: Requirements 3.2, 3.7**

---

### Property 4: Duplicate Detection Correctly Partitions Submissions by Distance and Category

*For any* submitted GPS coordinate pair `(lat, lng)` and category `C`, the duplicate detection algorithm shall return an existing active ticket if and only if there exists at least one active ticket (status `To Do` or `In Progress`) with category `C` whose Haversine distance from `(lat, lng)` is ≤ 50 meters. Tickets with status `Done` or `Archived`, or with a different category, shall never trigger duplicate detection regardless of distance.

**Validates: Requirements 4.1, 4.4, 4.5**

---

### Property 5: Duplicate Detection Returns the Closest Match

*For any* set of candidate duplicate tickets that all fall within the 50-meter radius with matching category, the backend shall return the one ticket with the minimum Haversine distance to the submitted coordinates.

**Validates: Requirements 4.2**

---

### Property 6: Haversine Distance is Symmetric and Satisfies the 50-Meter Threshold Correctly

*For any* two geographic coordinate pairs `(lat1, lng1)` and `(lat2, lng2)`, the `HaversineMeters` function shall return the same value regardless of argument order (symmetry), and shall return a value ≤ 50.0 if and only if the two points are genuinely within 50 meters of each other on the Earth's surface.

**Validates: Requirements 4.1**

---

### Property 7: Upvote Increments Count Exactly Once Per Unique User

*For any* ticket `T` with current upvote count `N` and any authenticated user `U` who has not previously upvoted `T`, after a successful upvote the ticket's `upvotes` count shall be exactly `N + 1` and the `updated_at` timestamp shall be updated. Any subsequent upvote attempt by the same user `U` on the same ticket `T` shall return HTTP 409 and leave both `upvotes` and `updated_at` unchanged.

**Validates: Requirements 5.1, 5.2, 5.3, 5.4**

---

### Property 8: PIN Validation Accepts Only the Exact Master PIN

*For any* non-empty string `S`, the PIN validation logic shall return success if and only if `S` is exactly equal to the configured master PIN (after trimming). For any string that does not equal the master PIN, the backend shall return HTTP 403. An empty or all-whitespace PIN shall be rejected by the backend with HTTP 400 without performing a comparison.

**Validates: Requirements 6.3, 6.4**

---

### Property 9: PIN Rate Limiting Blocks After 5 Consecutive Failures

*For any* authenticated user who submits 5 or more consecutive incorrect PINs, all PIN upgrade attempts from that user within the subsequent 15-minute window shall return HTTP 429, regardless of whether the correct PIN is later submitted within that window.

**Validates: Requirements 6.6**

---

### Property 10: Citizen Feed Query Returns Exactly the Correct Ticket Set

*For any* collection of tickets in Firestore with various statuses and `resolved_at` timestamps, the citizen feed query shall return exactly those tickets satisfying: `status ∈ {To Do, In Progress}` OR `(status = Done AND resolved_at > now − 7 days)`. Tickets with `status = Archived` or `Done` tickets older than 7 days shall never appear in the result set.

**Validates: Requirements 7.1, 7.5, 9.5**

---

### Property 11: Citizen Feed List is Always Sorted by created_at Descending

*For any* non-empty collection of tickets satisfying the citizen feed criteria, the returned list shall be ordered such that for any two adjacent tickets `T_i` and `T_{i+1}`, `T_i.created_at ≥ T_{i+1}.created_at`.

**Validates: Requirements 7.2**

---

### Property 12: Official Dashboard Query Returns at Most 200 Tickets Sorted by Upvotes Descending

*For any* collection of tickets, the official dashboard query shall return at most 200 tickets, and the returned list shall be ordered such that for any two adjacent tickets `T_i` and `T_{i+1}`, `T_i.upvotes ≥ T_{i+1}.upvotes`.

**Validates: Requirements 8.2**

---

### Property 13: Ticket Status Transitions Follow the Permitted State Machine

*For any* ticket in status `S` and any requested target status `S'`, the backend shall permit the transition if and only if `(S, S')` is one of `(To Do, In Progress)` or `(In Progress, Done)`. All other `(S, S')` combinations, including backward transitions and skip transitions, shall be rejected with HTTP 400. Transitions on `Archived` tickets shall return HTTP 409.

**Validates: Requirements 8.5, 8.6, 9.4**

---

### Property 14: resolved_at is Set Exactly When Status Transitions to Done

*For any* ticket `T`, if a status transition to `Done` is applied, the resulting ticket document shall have a non-null `resolved_at` field set to a timestamp within a 5-second window of the transition time. If `T` is in any status other than being transitioned to `Done`, `resolved_at` shall remain unchanged.

**Validates: Requirements 9.1**

---

### Property 15: Archival Transitions All Eligible Tickets

*For any* collection of tickets, after running the archival function, every ticket with `status = Done` and `resolved_at ≤ now − 7 days` shall have its status changed to `Archived`. No ticket with `status ≠ Done` or `resolved_at > now − 7 days` shall be modified by the archival function.

**Validates: Requirements 9.2**

---

### Property 16: Archived Tickets Reject All Mutation Attempts

*For any* ticket with `status = Archived`, any attempt to upvote it or change its status shall be rejected with HTTP 409, and the ticket's `upvotes` count, `status`, and `updated_at` shall remain unchanged after the rejection.

**Validates: Requirements 9.4**

---

### Property 17: Backend Returns HTTP 503 When Any Required Dependency Fails to Initialize

*For any* combination of dependency initialization outcomes where at least one of {Firestore client, Google public keys, Gemini client} fails to initialize, the backend shall not begin accepting incoming requests and shall return HTTP 503 for any request that arrives before all dependencies are successfully initialized.

**Validates: Requirements 11.5**


---

## Error Handling

### Error Response Format

All backend error responses use a consistent JSON envelope:
```json
{ "error": "human-readable message" }
```

### HTTP Status Code Conventions

| Condition | Code |
|---|---|
| Success with resource created | 201 |
| Success, existing resource returned | 200 |
| Bad request / invalid input | 400 |
| Missing or invalid auth token | 401 |
| Caller lacks permission (wrong role) | 403 |
| Resource not found | 404 |
| Conflict (duplicate upvote, bad state transition, archived) | 409 |
| Too many requests (PIN lockout) | 429 |
| AI parsing failure | 422 |
| Dependency not ready at startup | 503 |
| Upstream timeout (Gemini) | 504 |

### Retry and Resilience Patterns

**Gemini API calls**: one automatic retry with exponential back-off (200ms, 400ms) before returning 504. The Flutter app has its own manual retry option for users.

**Firestore writes**: Firestore client uses gRPC with built-in retries for transient network errors. Permanent failures (permission denied, invalid document path) are not retried and are returned immediately as 500.

**Image upload (Flutter)**: the app stores the local image path until a successful upload URL is confirmed, allowing the user to retry the upload step without re-taking the photo (Req 2.7).

**GPS acquisition**: the app polls for coordinates with the required 50-meter accuracy for up to 15 seconds before surfacing the error (Req 2.5).

### Graceful Degradation

- If Gemini is unavailable: the app falls back to manual field entry (Req 3.6).
- If Firestore snapshot listener drops: the Flutter app displays a stale-data warning and a manual refresh button (Req 7.6).
- If the APK URL is unavailable: the landing page shows an "unavailable" message instead of a broken link (Req 10.6).


---

## Testing Strategy

### Overview

CivicSync's backend contains substantial pure logic (JWT verification, Haversine geometry, triage parsing, state machine transitions, archival logic) that is highly amenable to property-based testing. UI interactions and infrastructure wiring are tested with example-based unit tests and integration tests respectively.

**Property-based testing library**: [`github.com/flyingmutant/rapid`](https://github.com/flyingmutant/rapid) for Go.

Each property test is configured to run a minimum of 100 iterations. Tests are tagged with a comment referencing the design property:

```go
// Feature: civic-sync, Property 6: Haversine distance is symmetric
```

---

### Unit Tests (Example-Based)

| Area | What is tested |
|---|---|
| Auth middleware | Valid token accepted; expired token rejected; wrong `aud` rejected; missing Bearer prefix rejected |
| Triage parser | Valid Gemini JSON → TriageResult; missing field → error; unknown category → error; markdown fences stripped |
| Status transition handler | `To Do` → `In Progress` allowed; `In Progress` → `Done` allowed; `Done` → `In Progress` rejected; any transition on `Archived` → 409 |
| PIN validation | Correct PIN → role updated; wrong PIN → 403; empty PIN → 400; already official → 409 |
| Landing page | `GET /` returns 200 with `text/html`; HTML contains all 6 technology names |
| Startup failure | Mocked Firestore init failure → server returns 503 |
| Archival scheduler | Mock tickets with expired `resolved_at` → status set to `Archived`; unexpired tickets untouched |

---

### Property-Based Tests

Each test references the corresponding design document property.

| Design Prop | Generator | Invariant asserted |
|---|---|---|
| Prop 1: JWT verification | Generate valid tokens and tokens with each type of defect (expired, wrong sig, wrong iss, wrong aud) | Acceptance iff all four conditions satisfied |
| Prop 2: Profile idempotency | Generate random user token payloads; call login N times (N=1..10) | Firestore document count for UID is always 1 |
| Prop 3: Triage category | Generate random Gemini response strings (valid JSON, malformed JSON, valid JSON with random category strings) | Parsed category ∈ allowed set, or error |
| Prop 4: Duplicate detection correctness | Generate random (lat, lng) origin + set of tickets with random coordinates, statuses, categories | Duplicate found iff ∃ active ticket with same category within 50m |
| Prop 5: Closest duplicate returned | Generate origin + N tickets within 50m with same category | Returned ticket has minimum Haversine distance |
| Prop 6: Haversine symmetry | Generate random coordinate pairs | `H(a,b) == H(b,a)` and correct threshold classification |
| Prop 7: Upvote idempotency | Generate random ticket upvote counts + user lists | Count += 1 for first vote; 409 + unchanged count for repeat vote |
| Prop 8: PIN exact match | Generate random non-empty strings | Accept iff string equals master PIN exactly |
| Prop 9: PIN rate limiting | Generate sequences of ≥ 5 wrong PINs | 5th+ attempts return 429 within lockout window |
| Prop 10: Citizen feed query | Generate random ticket collections with various statuses and `resolved_at` offsets | Returned set matches predicate exactly |
| Prop 11: Citizen feed sort | Generate random ticket collections | List is `created_at` descending |
| Prop 12: Dashboard query | Generate random ticket collections with random upvote counts | At most 200 tickets; sorted by upvotes descending |
| Prop 13: Status machine | Generate all (currentStatus, targetStatus) pairs for each ticket state | Exactly the two allowed transitions succeed; all others fail |
| Prop 14: resolved_at on Done | Generate random tickets; apply Done transition | `resolved_at` set to timestamp ≈ transition time |
| Prop 15: Archival correctness | Generate random ticket collections with various `resolved_at` ages | After archival run: all eligible archived; no others changed |
| Prop 16: Archived mutation rejection | Generate random Archived tickets | Any upvote or status change returns 409; ticket unchanged |
| Prop 17: Startup 503 | Generate all subsets of {Firestore, GoogleKeys, Gemini} where at least one fails | Server returns 503 for all requests when any dependency failed |

---

### Integration Tests

| Area | What is tested |
|---|---|
| Gemini API call | Real (or sandbox) Gemini API call with a test image returns valid TriageResult |
| Firestore snapshot listener | Updating a Firestore document causes the Flutter listener callback to fire |
| End-to-end ticket creation | POST /tickets creates a document in Firestore with correct fields |
| Role upgrade end-to-end | Correct PIN → user document role updated to "official" in Firestore |
| Cold start latency | Container start to first HTTP 200 on `GET /` is under 3 seconds |

---

### Test Configuration

```go
// Minimum iterations for all property tests
const MinIterations = 100

// Example property test
func TestHaversineSymmetry(t *testing.T) {
    // Feature: civic-sync, Property 6: Haversine distance is symmetric
    rapid.Check(t, func(t *rapid.T) {
        lat1 := rapid.Float64Range(-90, 90).Draw(t, "lat1")
        lng1 := rapid.Float64Range(-180, 180).Draw(t, "lng1")
        lat2 := rapid.Float64Range(-90, 90).Draw(t, "lat2")
        lng2 := rapid.Float64Range(-180, 180).Draw(t, "lng2")
        
        d1 := geo.HaversineMeters(lat1, lng1, lat2, lng2)
        d2 := geo.HaversineMeters(lat2, lng2, lat1, lng1)
        
        if math.Abs(d1-d2) > 0.001 { // 1mm tolerance for floating point
            t.Fatalf("Haversine is not symmetric: H(%v,%v,%v,%v)=%v, H(%v,%v,%v,%v)=%v",
                lat1, lng1, lat2, lng2, d1,
                lat2, lng2, lat1, lng1, d2)
        }
    })
}
```

