# CivicSync

> **Hyperlocal civic issue reporting — built on an entirely Google-native stack.**

Citizens photograph infrastructure problems from their phone. Gemini AI classifies each report. Government officials manage and resolve issues through a real-time Kanban dashboard. Everything syncs live via Firestore snapshot listeners.

---

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Tech Stack](#tech-stack)
- [Features](#features)
- [Project Structure](#project-structure)
- [Data Models](#data-models)
- [API Reference](#api-reference)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Backend (Go)](#backend-go)
  - [Flutter App (Android)](#flutter-app-android)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [Testing](#testing)
- [Ticket Lifecycle](#ticket-lifecycle)
- [Contributing](#contributing)
- [License](#license)

---

## Overview

CivicSync connects citizens and government officials under a transparent, real-time platform. A citizen spots a pothole, taps **Report**, takes a photo — Gemini AI fills in the category, title, and description automatically. If an identical issue has already been reported nearby, the citizen is redirected to upvote it instead. Officials see a live dashboard sorted by community urgency (upvotes) and move tickets through a simple `To Do → In Progress → Done` workflow. Resolved issues stay visible for seven days with a "Resolved" badge, then archive automatically.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                          CITIZEN / OFFICIAL                          │
│                       Flutter Android App                            │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────────┐ │
│  │ Auth Screen  │  │ Report Flow  │  │   Feed / Dashboard         │ │
│  │ Google OAuth │  │ Camera + GPS │  │   Map + List / Kanban      │ │
│  └──────┬───────┘  └──────┬───────┘  └────────────┬───────────────┘ │
└─────────┼────────────────┼───────────────────────┼──────────────────┘
          │ HTTPS           │ HTTPS                  │ Firestore SDK
          ▼                 ▼                        ▼
┌──────────────────────────────────────────┐  ┌──────────────────────┐
│          Go Backend  (Cloud Run)          │  │    Cloud Firestore   │
│  ┌──────────────────────────────────────┐│  │  /users  /tickets    │
│  │  Middleware: JWT Verify (Google PKI) ││  │  real-time listeners │
│  ├──────────────────────────────────────┤│  └──────────────────────┘
│  │  POST /auth/login                    ││
│  │  POST /triage                        ││  ┌──────────────────────┐
│  │  POST /tickets                       ││  │  Cloud Storage (FB)  │
│  │  POST /tickets/:id/upvote            ││  │  ticket images / APK │
│  │  PUT  /tickets/:id/status            ││  └──────────────────────┘
│  │  POST /auth/upgrade                  ││
│  │  GET  /  (landing page)              ││  ┌──────────────────────┐
│  ├──────────────────────────────────────┤│  │  Gemini 2.5 Flash    │
│  │  Geospatial Duplicate Detector       ├┼──►  AI triage pipeline  │
│  │  AI Triage Agent                     ││  └──────────────────────┘
│  │  Archival Scheduler (goroutine)      ││
│  │  Static File Server (landing page)   ││  ┌──────────────────────┐
│  └──────────────────────────────────────┘│  │ Firebase Auth /      │
└──────────────────────────────────────────┘  │ Google OAuth PKI     │
                                              └──────────────────────┘
```

### Deployment Topology

| Component | Host | Scaling |
|---|---|---|
| Go backend | Google Cloud Run | Scale-to-zero, min 0 instances |
| Landing page assets | Embedded in Go binary (`embed.FS`) | In-process |
| Flutter APK | Cloud Storage for Firebase | Static object |
| Ticket images | Cloud Storage for Firebase | Static objects |
| Database | Cloud Firestore (Native mode) | Managed, auto-scaling |
| Auth | Firebase Auth + Google OAuth | Managed |
| AI triage | Gemini 2.5 Flash | Managed |

---

## Tech Stack

| Layer | Technology |
|---|---|
| Mobile app | Flutter (Android) |
| Backend | Go 1.22+ |
| Hosting | Google Cloud Run |
| Database | Cloud Firestore |
| Storage | Cloud Storage for Firebase |
| Authentication | Firebase Auth + Google OAuth |
| AI classification | Google Gemini 2.5 Flash |

---

## Features

- **Camera-locked authenticity** — only live hardware capture is accepted; gallery access is disabled.
- **AI-powered triage** — Gemini 2.5 Flash auto-generates category, title, and description from the photo and GPS coordinates.
- **Geospatial duplicate detection** — Haversine-based 50 m radius check prevents duplicate reports; existing tickets are surfaced for upvoting instead.
- **Role-based access** — citizens report and upvote; officials manage status via a Kanban dashboard. Role upgrade via a secret PIN with rate-limiting (5 failures → 15-minute lockout).
- **Real-time sync** — Firestore snapshot listeners propagate official status changes to all citizen feeds within seconds.
- **Automatic archival** — tickets resolved for more than 7 days transition to `Archived` via a background goroutine.
- **Static landing page** — served directly from the Go binary (`go:embed`); includes APK download, demo video, and tech stack summary.

---

## Project Structure

```
.
├── cmd/
│   └── server/              # main entry point; dependency wiring; HTTP server
├── internal/
│   ├── auth/                # JWT verification, login handler, PIN upgrade handler
│   ├── tickets/             # ticket creation, upvoting, status updates, archival
│   ├── triage/              # Gemini API client, prompt construction, response parsing
│   ├── geo/                 # Haversine distance, bounding-box helpers
│   ├── middleware/          # JWT middleware, role enforcement, panic recovery, logging
│   ├── models/              # shared Go structs (User, Ticket, Location)
│   └── store/               # Store interface + FirestoreStore implementation
├── web/
│   └── static/              # index.html, styles.css (embedded into binary)
├── flutter_app/             # Flutter Android application
│   └── lib/
│       ├── screens/         # auth_screen, report_flow, confirm_screen, feed, dashboard
│       └── services/        # triage_service, backend API client
├── Dockerfile
├── cloudbuild.yaml
├── firestore.indexes.json
└── README.md
```

---

## Data Models

### User (`/users/{uid}`)

```json
{
  "uid":              "google_oauth_sub_string",
  "email":            "user@example.com",
  "name":             "Alex Carter",
  "role":             "citizen",
  "created_at":       "2026-06-26T14:30:00Z",
  "pin_failures":     0,
  "pin_lockout_until": null
}
```

### Ticket (`/tickets/{ticketId}`)

```json
{
  "id":          "uuid-v4-string",
  "category":    "Pothole",
  "title":       "Deep Pothole near Main Crossroad",
  "description": "Severe road deterioration disrupting traffic flow.",
  "image_url":   "https://storage.googleapis.com/...",
  "location":    { "latitude": 23.0225, "longitude": 72.5714 },
  "status":      "To Do",
  "upvotes":     14,
  "upvoted_by":  ["uid1", "uid2"],
  "reported_by": "google_oauth_sub_string",
  "created_at":  "2026-06-26T14:35:00Z",
  "updated_at":  "2026-06-26T14:35:00Z",
  "resolved_at": null
}
```

**Issue categories**: `Pothole` · `Water Clogging` · `Drain Overflow` · `Electrical Hazard` · `Other`

---

## API Reference

All endpoints except `GET /` require `Authorization: Bearer <Google-ID-Token>`.  
Error responses always use the shape `{"error": "<message>"}`.

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/` | None | Static landing page |
| `POST` | `/auth/login` | Bearer token | Verify ID token, upsert user profile |
| `POST` | `/auth/upgrade` | Bearer token | Upgrade role to `official` via PIN |
| `POST` | `/triage` | Bearer token | AI classification of image + GPS |
| `POST` | `/tickets` | Bearer token | Create ticket (duplicate-aware) |
| `POST` | `/tickets/:id/upvote` | Bearer token | Upvote a ticket (idempotent) |
| `PUT` | `/tickets/:id/status` | Bearer token (`official` only) | Advance ticket status |

### Key Response Codes

| Code | Meaning |
|---|---|
| 200 | OK / duplicate ticket found |
| 201 | New ticket created |
| 400 | Validation error (missing/invalid fields) |
| 401 | Unauthenticated |
| 403 | Forbidden (wrong role or wrong PIN) |
| 404 | Ticket not found |
| 409 | Conflict (already upvoted, already official, or ticket archived) |
| 422 | AI response unparseable |
| 429 | PIN rate limit exceeded |
| 503 | Dependency failed at startup |
| 504 | AI request timed out |

---

## Getting Started

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [Flutter 3.x](https://flutter.dev/docs/get-started/install) with Android SDK
- [Google Cloud SDK](https://cloud.google.com/sdk/docs/install)
- A Firebase project with Firestore, Storage, and Authentication enabled
- A Google AI Studio API key for Gemini 2.5 Flash

### Backend (Go)

```bash
# Clone the repository
git clone https://github.com/your-org/civicsync.git
cd civicsync

# Install dependencies
go mod download

# Set required environment variables (see Configuration section)
export PROJECT_ID="your-gcp-project-id"
export GEMINI_API_KEY="your-gemini-api-key"
export MASTER_PIN="HERO-2026"
export GOOGLE_CLIENT_ID="your-oauth-client-id"

# Run locally
go run ./cmd/server

# Run tests
go test ./...
```

The server starts on `:8080` by default.

### Flutter App (Android)

```bash
cd flutter_app

# Install Flutter dependencies
flutter pub get

# Add your google-services.json to android/app/
# (download from Firebase Console → Project Settings → Android app)

# Run on a connected device or emulator
flutter run

# Build a release APK
flutter build apk --release
```

---

## Configuration

The backend is configured entirely through environment variables. **Never commit secrets to source control** — provision them via [Google Secret Manager](https://cloud.google.com/secret-manager).

| Variable | Required | Description |
|---|:---:|---|
| `PROJECT_ID` | ✅ | Google Cloud project ID (used for Firestore) |
| `GEMINI_API_KEY` | ✅ | Google AI Studio API key for Gemini 2.5 Flash |
| `MASTER_PIN` | ✅ | Secret PIN for role upgrade (e.g. `HERO-2026`) |
| `GOOGLE_CLIENT_ID` | ✅ | OAuth 2.0 client ID used for JWT `aud` verification |
| `APK_DOWNLOAD_URL` | ✅ | Public Cloud Storage URL for the Android APK |
| `PORT` | ❌ | HTTP listen port (default: `8080`) |

---

## Deployment

### Docker

```bash
# Build the static binary and container image
docker build -t civicsync-backend .

# Run locally
docker run -p 8080:8080 \
  -e PROJECT_ID=... \
  -e GEMINI_API_KEY=... \
  -e MASTER_PIN=... \
  -e GOOGLE_CLIENT_ID=... \
  civicsync-backend
```

### Cloud Run (via Cloud Build)

```bash
# Submit build and deploy in one step
gcloud builds submit --config cloudbuild.yaml \
  --substitutions _PROJECT_ID=your-project,_REGION=us-central1
```

The `cloudbuild.yaml` pipeline:
1. Builds the Go binary with `CGO_ENABLED=0` for a `scratch`-based image
2. Pushes the image to Artifact Registry
3. Deploys to Cloud Run with `--min-instances=0` (scale-to-zero)

### Firestore Indexes

Provision the required composite indexes before running against a live Firestore instance:

```bash
firebase deploy --only firestore:indexes
```

Index definitions are in [`firestore.indexes.json`](./firestore.indexes.json).

---

## Testing

The backend uses [`github.com/flyingmutant/rapid`](https://github.com/flyingmutant/rapid) for property-based testing (minimum 100 iterations per property). All tests use an in-memory `Store` fake — no live Firestore connection required.

```bash
# Run all tests
go test ./...

# Run with verbose output and race detector
go test -v -race ./...

# Run a specific property test
go test -v ./internal/geo/... -run TestHaversineSymmetry
```

### Property Tests

| Property | Package | What it verifies |
|---|---|---|
| 1 — JWT Verification | `internal/middleware` | Accepts valid tokens; rejects expired / wrong sig / wrong iss / wrong aud |
| 2 — Login Idempotency | `internal/auth` | N logins produce exactly one Firestore user document |
| 3 — Triage Parsing | `internal/triage` | Parsed category ∈ allowed set, or error returned |
| 4 — Duplicate Detection | `internal/tickets` | Duplicate found iff active ticket within 50 m with matching category |
| 5 — Closest Duplicate | `internal/tickets` | Returned ticket has minimum Haversine distance |
| 6 — Haversine Symmetry | `internal/geo` | `H(a,b) == H(b,a)` within 1 mm; 50 m threshold correct |
| 7 — Upvote Idempotency | `internal/tickets` | Count += 1 on first vote; 409 + unchanged count on repeat |
| 8 — PIN Exact Match | `internal/auth` | Success iff submitted string equals master PIN exactly |
| 9 — PIN Rate Limiting | `internal/auth` | 5th+ consecutive failure returns 429 within lockout window |
| 13 — Status Machine | `internal/tickets` | Only `To Do→In Progress` and `In Progress→Done` succeed |
| 14 — `resolved_at` | `internal/tickets` | Set non-null on Done transition; unchanged for all other transitions |
| 15 — Archival | `internal/tickets` | All eligible tickets archived; no others modified |
| 16 — Archived Rejection | `internal/tickets` | Any mutation on Archived ticket returns 409 |
| 17 — Startup 503 | `cmd/server` | Any failed dependency causes 503 for all incoming requests |

---

## Ticket Lifecycle

```
                   ┌──────────────────────────────────┐
                   │                                  │
              [submit]                         [7 days elapsed]
                   │                                  │
                   ▼                                  │
             ┌─────────┐  [official]  ┌─────────────┐  [official]  ┌──────┐
  ─(create)─►│  To Do  ├─────────────►│  In Progress ├─────────────►│ Done ├──────────►[Archived]
             └─────────┘             └─────────────┘              └──────┘
```

| Status | Citizen feed | Official dashboard | Notes |
|---|:---:|:---:|---|
| `To Do` | ✅ | ✅ | |
| `In Progress` | ✅ | ✅ | |
| `Done` (< 7 days) | ✅ Resolved badge | ✅ | |
| `Done` (≥ 7 days, pending archive) | ❌ client filter | ✅ | |
| `Archived` | ❌ | ❌ | No further mutations allowed (409) |

---

## Contributing

1. Fork the repository and create a feature branch: `git checkout -b feat/your-feature`
2. Make your changes and add tests where applicable
3. Ensure all tests pass: `go test -race ./...`
4. Open a pull request with a clear description of the change

Please keep PRs focused. Bug fixes and features should be separate PRs.

---

## License

This project is licensed under the [MIT License](./LICENSE).
