# Product Requirements Document (PRD)
## Project: Community Hero — Hyperlocal Problem Solver

---

## 1. Executive Summary & Objectives
The **Community Hero** platform is an intelligent, real-time civic ecosystem designed to empower citizens to identify, report, and track hyperlocal infrastructure challenges while providing municipal authorities with a prioritized data stream to resolve them. By unifying citizens and government officials under a transparent, real-time architecture, this platform eliminates fragmented reporting and enforces structural accountability.

### Core Objectives
* Provide a frictionless, mobile-first reporting interface for citizens with automatic geolocation tracking.
* Enforce data authenticity via hardware-locked camera verification to prevent fraudulent reports.
* Maximize automation using Google's Gemini 2.5 Flash to categorize incoming imagery and generate automated details.
* Prevent ticket duplication through real-time proximity scanning.
* Close the resolution loop by providing a dedicated, real-time Government Management Dashboard.
* Enable seamless judging validation via a secure, self-serve role-upgrade system and a highly optimized static landing page deployment.

---

## 2. System Architecture & Technology Stack
To secure maximum points in the **"Usage of Google Technologies (15%)"** bracket, the system relies on a unified, high-performance ecosystem.

| Layer | Technology | Purpose |
| :--- | :--- | :--- |
| **Frontend** | Flutter (Dart) | Cross-platform mobile UI compiled natively for Android (`.apk`). |
| **Backend API & Web Server** | Go (Golang) | High-concurrency API deployed to execute business logic and serve the static landing page UI to judges. |
| **Hosting Engine** | Google Cloud Run | Serverless container environment hosting the compiled Go backend binary and static assets. |
| **Authentication** | Google OAuth & Firebase Auth | Handles secure, single-tap user sign-in and token provisioning. |
| **Database** | Cloud Firestore | Manages real-time data sync, cross-role updates, and asset state management. |
| **Object Storage** | Cloud Storage for Firebase | Holds raw, hardware-verified images uploaded by citizens and the downloadable `.apk` file. |
| **AI Processing** | Google AI Studio (Gemini 2.5 Flash) | Processes multimodal payloads for automated classification and text synthesis. |
| **CI/CD Pipeline** | GitHub Actions | Automatically compiles and packages a fresh Android release `.apk` file upon code updates. |

---

## 3. Core Functional Requirements

### 3.1 Authenticity-Locked Issue Reporting
* **Camera Enforcement:** Users are restricted from picking files from their local device gallery. The frontend application locks the intake source strictly to the native device camera hardware.
* **Automatic Geolocation Capture:** The system implicitly polls the device's GPS hardware during photo capture to anchor precise latitude and longitude coordinates to the ticket metadata.

### 3.2 AI-Powered Triage Agent
* **Multimodal Asset Processing:** Raw images and location headers are channeled to the Go API, which streams them directly to Gemini 2.5 Flash.
* **Structured Token Classification:** The AI confirms the validity of the infrastructure hazard and converts the asset into a strict category schema (e.g., `Pothole`, `Water Clogging`, `Drain Overflow`, `Electrical Hazard`).
* **User Validation Loop:** The generated title, category, and description populate a confirmation viewport. Users can instantly submit the ticket or adjust properties using a fallback dropdown or manual input field.

### 3.3 Proximity-Based Duplicate Filtering
* **Radius Scanning:** Before writing a new ticket to Firestore, the Go backend runs a geospatial distance calculation covering a **50-meter radius** from the target coordinates.
* **Upvote Redirection:** If an active issue matching that category is discovered, the creation pipeline pauses and prompts the user to upvote the existing ticket instead, preventing database pollution.

### 3.4 Google OAuth Authentication
* **Single-Tap Sign-In:** The application implements Google Identity Services natively on the frontend, allowing users to log in instantly without tracking passwords.
* **Token Verification Pattern:** The Flutter app transmits the retrieved Google ID Token (JWT) inside the `Authorization: Bearer <JWT>` header to the Go API. The Go backend cryptographically validates the token signature using Google's public keys before authorizing any state changes.

### 3.5 Unified Dual-Role Onboarding (The "Secret Code" System)
To streamline evaluation, both citizens and officials register via the exact same interface, eliminating the need to configure multiple routing configurations.

* **Default Account Provisioning:** Every new profile initialized in the database automatically defaults to a `"citizen"` role with standard access boundaries.
* **Secret Upgrade Portal:** A subtle button labeled *"City Official Access"* is accessible within the profile settings layout.
* **Master PIN Validation:** Clicking this button initiates a validation prompt. Entering the designated hackathon evaluation PIN (e.g., `HERO-2026`) dispatches an authenticated update request to the Go backend, which rewires the Firestore profile document to `"role": "official"`.
* **Dynamic Frontend Morphing:** Upon database mutation, the Flutter application intercepts the role state update in real time, immediately altering the active UI layers.

### 3.6 Government Official Management Dashboard
When a user switches to an `"official"` role state, the application interface transforms into an administrative control board.

* **Upvote Priority Queue:** Displays incoming civic issues automatically sorted by upvote density, highlighting the most urgent local demands.
* **Detailed Ticket Inspection:** Provides comprehensive views for individual tickets, rendering localized map points, submission timestamps, image links, and verified reporter emails.
* **Real-Time Kanban State Control:** Officials can transition ticket statuses directly via a drop-down menu or click actions across standard pillars: `To Do` -> `In Progress` -> `Done`.
* **Instant Citizen Sync:** Updates written to Firestore by an official instantly propagate to active citizen feeds in real time via live snapshot listeners.

---

## 4. Technical Specifications & Data Models

### 4.1 Database Architecture (Cloud Firestore)

#### Collection: `/users`
```json
{
  "uid": "google_oauth_sub_string",
  "email": "citizen_or_judge@example.com",
  "name": "Alex Carter",
  "role": "citizen", // Options: "citizen" | "official"
  "created_at": "2026-06-26T14:30:00Z"
}
Collection: /tickets
```json
{
  "id": "uuid_v4_string",
  "category": "Pothole",
  "title": "Deep Pothole near Main Crossroad",
  "description": "Severe road deterioration disrupting traffic flow.",
  "image_url": "[https://storage.googleapis.com/community-hero/img_5541.jpg](https://storage.googleapis.com/community-hero/img_5541.jpg)",
  "location": {
    "latitude": 23.0225,
    "longitude": 72.5714
  },
  "status": "To Do", // Options: "To Do" | "In Progress" | "Done"
  "upvotes": 14,
  "reported_by": "google_oauth_sub_string",
  "created_at": "2026-06-26T14:35:00Z",
  "updated_at": "2026-06-26T14:35:00Z"
}
```
## 5. Non-Functional & Hackathon Constraints

### 5.1 Presentation Layer & Deployment Link (The Landing Page)


To satisfy the mandatory Google Cloud deployment requirement while ensuring a frictionless judging experience, the root domain (/) of the Cloud Run instance serves a static HTML/CSS presentation page.

- **The Pitch & Download:** The page acts as a storefront detailing the problem statement, with a prominent call-to-action button linking directly to the compiled Android .apk file (hosted via Firebase Storage or GitHub Actions).

- **The Agentic Demo:** The landing page embeds a 60-second recorded demonstration of the mobile app in action, explicitly highlighting the Gemini AI categorization and dual-role backend sync to guarantee the judges witness the 20% Agentic Depth evaluation criteria.

- **Architecture Display:** A summarized, visual breakdown of the Google-exclusive tech stack to satisfy the 15% Google Technologies requirement.

### 5.2 Serverless Operational Lifecycles
- **Cold-Start Optimization:** The Go backend microservice deployed to Cloud Run is built to scale completely to zero instances to safeguard quotas within Google Cloud’s free tiers.

- **Archival Thresholds:** Resolved issues ("status": "Done") remain visible on public citizen maps for 7 days to preserve transparent accountability before transitioning into passive, read-only architectural logs.