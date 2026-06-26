# Requirements Document

## Introduction

CivicSync (Community Hero) is a hyperlocal civic issue reporting platform that connects citizens and government officials under a real-time, transparent architecture. Citizens use a mobile-first Flutter application to photograph and report local infrastructure issues. An AI triage agent (Google Gemini 2.5 Flash) automatically categorizes and describes each submission. A Go backend enforces geospatial duplicate detection, role-based access control, and data integrity. Government officials access a real-time management dashboard to prioritize, inspect, and resolve reported issues. A static landing page deployed on Google Cloud Run serves as the public entry point for the platform.

---

## Glossary

- **App**: The CivicSync Flutter Android application installed on a citizen's or official's mobile device.
- **Backend**: The Go API service deployed on Google Cloud Run that executes all business logic and serves the landing page.
- **Citizen**: A registered user with the `citizen` role who can report, view, and upvote civic issues.
- **Official**: A registered user with the `official` role who can manage and update the status of civic issues.
- **Ticket**: A civic issue report stored in Firestore, containing category, title, description, image, location, status, and upvote count.
- **Triage_Agent**: The AI processing pipeline that sends image and location data to Gemini 2.5 Flash and returns structured classification results.
- **AI_Service**: Google Gemini 2.5 Flash, accessed via the Backend, that processes multimodal payloads and returns structured ticket metadata.
- **Auth_Service**: Firebase Auth combined with Google OAuth that handles user sign-in and JWT provisioning.
- **Firestore**: Cloud Firestore database that stores all user profiles and tickets with real-time sync.
- **Storage**: Cloud Storage for Firebase that holds uploaded ticket images and the downloadable APK file.
- **Landing_Page**: The static HTML/CSS page served from the root path (`/`) of the Cloud Run instance.
- **Duplicate**: A Ticket whose category and location fall within a 50-meter radius of an existing active Ticket with the same category.
- **PIN**: The secret alphanumeric code (e.g., `HERO-2026`) used to upgrade a Citizen account to an Official account.
- **Upvote**: A single endorsement a Citizen casts on an existing Ticket to signal urgency.
- **KanbanStatus**: The current resolution state of a Ticket — one of `To Do`, `In Progress`, or `Done`.
- **ArchivePeriod**: The 7-day window during which a Ticket with status `Done` remains publicly visible before being transitioned to `KanbanStatus = Archived`.
- **resolved_at**: The timestamp recorded on a Ticket document when its KanbanStatus is first set to `Done`, used as the archival countdown anchor.

---

## Requirements

### Requirement 1: Google OAuth Authentication

**User Story:** As a new user, I want to sign in with my Google account, so that I can access the platform without managing a separate password.

#### Acceptance Criteria

1. WHEN a user taps the sign-in button, THE App SHALL initiate a Google OAuth sign-in flow using Google Identity Services.
2. IF the Google OAuth flow fails or the user cancels sign-in, THEN THE App SHALL display an error message and return to the sign-in screen without granting access.
3. WHEN Google OAuth returns a valid ID Token, THE App SHALL transmit the token to the Backend for verification before granting access to the platform.
4. WHEN the Backend receives an authenticated request, THE Backend SHALL verify the token's authenticity and integrity against Google's public keys before authorizing the request.
5. IF the token verification fails, THEN THE Backend SHALL return an HTTP 401 Unauthorized response and reject the request.
6. WHEN a user authenticates for the first time, THE Backend SHALL create a user profile document in Firestore with the fields `uid`, `email`, `name`, `role` set to `"citizen"`, and `created_at`.
7. IF the Firestore profile document creation fails, THEN THE Backend SHALL return an error response and THE App SHALL display an error message informing the user that account setup failed.
8. WHEN a user authenticates and a profile document already exists, THE Backend SHALL return the existing profile without creating a duplicate document.

---

### Requirement 2: Authenticity-Locked Issue Reporting

**User Story:** As a citizen, I want to report a civic issue by taking a photo with my device camera, so that the report is anchored to a real, verifiable moment and location.

#### Acceptance Criteria

1. WHEN a Citizen initiates a new issue report, THE App SHALL request camera permission from the device OS and open the native device camera upon permission being granted.
2. IF the device OS denies camera permission, THEN THE App SHALL display an error message informing the Citizen that camera access is required and prevent the report submission flow from continuing.
3. WHILE a Citizen is in the issue reporting flow, THE App SHALL disable access to the device photo gallery so that only live camera capture is accepted.
4. WHEN a Citizen captures a photo, THE App SHALL automatically read the device GPS hardware to capture the current latitude and longitude coordinates with an accuracy of 50 meters or better.
5. IF GPS coordinates with the required accuracy cannot be obtained within 15 seconds, THEN THE App SHALL display an error message and prevent submission until valid coordinates within the required accuracy are available.
6. WHEN a photo is captured and GPS coordinates within the required accuracy are obtained, THE App SHALL upload the image to Cloud Storage for Firebase and then transmit the resulting Storage URL and coordinates to the Backend for AI processing.
7. IF the image upload to Cloud Storage for Firebase fails, THEN THE App SHALL display an error message and allow the Citizen to retry the upload without re-capturing the photo.

---

### Requirement 3: AI-Powered Ticket Triage

**User Story:** As a citizen, I want the app to automatically generate a category, title, and description for my reported issue, so that I can submit an accurate report without manually filling in every field.

#### Acceptance Criteria

1. WHEN a photo is captured and GPS coordinates are obtained, THE Backend SHALL send the image and location coordinates (latitude and longitude) as a multimodal payload to the AI_Service.
2. WHEN the AI_Service returns a classification result, THE Backend SHALL parse the result into a structured object containing `category`, `title`, and `description` fields.
3. WHEN the structured classification result is returned to the App, THE App SHALL display the `category`, `title`, and `description` on a confirmation screen before submission.
4. WHILE the confirmation screen is active, THE App SHALL allow the Citizen to modify the `category` via a dropdown and modify the `title` (up to 100 characters) and `description` (up to 500 characters) via manual text input.
5. IF the AI_Service returns an invalid or unparseable response, THEN THE Backend SHALL return an error response.
6. WHEN the App receives an error response from the AI triage step, THE App SHALL display a fallback message prompting the Citizen to fill in the `category`, `title`, and `description` fields manually.
7. THE AI_Service SHALL classify infrastructure issues into one of the following categories: `Pothole`, `Water Clogging`, `Drain Overflow`, `Electrical Hazard`, or `Other`. The `Other` category SHALL be used when the image does not clearly correspond to any of the defined infrastructure hazard types.

---

### Requirement 4: Proximity-Based Duplicate Filtering

**User Story:** As a citizen, I want to be redirected to upvote an existing report if one already exists nearby, so that duplicate issues do not clutter the platform.

#### Acceptance Criteria

1. WHEN a Citizen attempts to submit a new Ticket, THE Backend SHALL check for existing active Tickets of the same category within a 50-meter radius of the GPS-captured coordinates.
2. IF one or more existing active Tickets of the same category are found within the 50-meter radius, THEN THE Backend SHALL return the closest matching Ticket by distance instead of creating a new Ticket.
3. WHEN the App receives an existing Ticket in response to a submission request, THE App SHALL display the existing Ticket and prompt the Citizen to upvote it. IF the Citizen dismisses the prompt, THE App SHALL return the Citizen to the submission form without creating a new Ticket.
4. IF no existing active Ticket of the same category is found within the 50-meter radius, THEN THE Backend SHALL proceed to create a new Ticket in Firestore.
5. THE Backend SHALL treat only Tickets with KanbanStatus `To Do` or `In Progress` as active for the purpose of duplicate detection.
6. IF the submitted coordinates are absent or invalid at the time of the duplicate check, THEN THE Backend SHALL return an HTTP 400 Bad Request response and not create a new Ticket.

---

### Requirement 5: Ticket Upvoting

**User Story:** As a citizen, I want to upvote an existing civic issue report, so that high-priority problems receive greater visibility.

#### Acceptance Criteria

1. WHEN an authenticated Citizen requests to upvote a Ticket they have not previously upvoted, THE Backend SHALL increment the `upvotes` field of that Ticket by exactly 1 in Firestore and update the `updated_at` timestamp.
2. WHEN the Backend increments the upvote count, THE Backend SHALL update the `updated_at` timestamp of the Ticket.
3. WHEN an authenticated Citizen requests to upvote a Ticket they have already upvoted, THE Backend SHALL return an HTTP 409 Conflict response and leave both the `upvotes` count and `updated_at` unchanged.
4. IF a Citizen attempts to upvote a Ticket they have already upvoted, THEN THE Backend SHALL return an HTTP 409 Conflict response and leave the `upvotes` count and `updated_at` unchanged.
5. WHEN an unauthenticated request to upvote a Ticket is received, THE Backend SHALL return an HTTP 401 Unauthorized response and leave the `upvotes` count unchanged.
6. IF a request is made to upvote a Ticket with a non-existent Ticket ID, THEN THE Backend SHALL return an HTTP 404 Not Found response and make no changes to Firestore.

---

### Requirement 6: Dual-Role Onboarding via Secret PIN

**User Story:** As a user, I want to upgrade my account to an official role by entering a secret PIN, so that I can access the government management dashboard without a separate registration process.

#### Acceptance Criteria

1. THE App SHALL display a "City Official Access" button within the profile settings screen for all authenticated users.
2. WHEN an authenticated user taps the "City Official Access" button, THE App SHALL display a PIN entry prompt.
3. IF an authenticated user submits an empty or blank PIN, THEN THE App SHALL display a validation error and not transmit the request to the Backend.
4. WHEN an authenticated user submits a non-empty PIN, THE Backend SHALL validate the submitted PIN against the designated master PIN.
5. IF the submitted PIN matches the master PIN, THEN THE Backend SHALL update the user's Firestore profile document to set `role` to `"official"`.
6. IF the submitted PIN does not match the master PIN, THEN THE Backend SHALL return an HTTP 403 Forbidden response, THE App SHALL display an error message, and the user's `role` SHALL remain unchanged. IF a user submits an incorrect PIN 5 consecutive times, THEN THE Backend SHALL block further PIN attempts from that user for 15 minutes and return an HTTP 429 Too Many Requests response.
7. WHEN the Backend updates the user's role to `"official"`, THE App SHALL transition the active UI to the Official Management Dashboard within 2 seconds without requiring a manual logout and re-login.
8. IF a user whose `role` is already `"official"` taps the "City Official Access" button, THEN THE App SHALL display a message indicating that the account is already upgraded and SHALL NOT submit a PIN request to the Backend.

---

### Requirement 7: Citizen Issue Feed

**User Story:** As a citizen, I want to view all reported civic issues on a map and list, so that I can stay informed about problems in my area.

#### Acceptance Criteria

1. WHEN a Citizen opens the issue feed, THE App SHALL display all Tickets with KanbanStatus `To Do`, `In Progress`, or `Done` (within the 7-day ArchivePeriod) on a map view with location markers.
2. WHEN a Citizen opens the issue feed, THE App SHALL display all Tickets with KanbanStatus `To Do`, `In Progress`, or `Done` (within the 7-day ArchivePeriod) in a scrollable list sorted by `created_at` in descending order.
3. WHEN a Citizen taps a Ticket marker on the map or a list item, THE App SHALL display the Ticket's full details including `category`, `title`, `description`, `image_url`, `location`, `status`, `upvotes`, `created_at`, and `updated_at`.
4. WHEN a Citizen is viewing the issue feed and a Ticket is updated in Firestore by an Official, THE App SHALL reflect the updated Ticket data within 3 seconds via Firestore snapshot listeners.
5. IF a Ticket's KanbanStatus is `Done` and 7 days have elapsed since `updated_at` was set to `Done`, THEN THE App SHALL exclude that Ticket from the citizen map and list feed.
6. IF the Firestore retrieval of Tickets fails, THEN THE App SHALL display an error message and provide a retry option without crashing.

---

### Requirement 8: Government Official Management Dashboard

**User Story:** As a government official, I want a dedicated dashboard to view, prioritize, and update the status of civic issue reports, so that I can efficiently manage and resolve community problems.

#### Acceptance Criteria

1. WHEN a user with `role` set to `"official"` opens the App, THE App SHALL display the Official Management Dashboard instead of the citizen issue feed.
2. WHEN the Official Management Dashboard loads, THE App SHALL display up to 200 Tickets sorted by `upvotes` in descending order.
3. WHEN an Official selects a Ticket in the dashboard, THE App SHALL display the full Ticket details including `title`, `category`, `description`, `upvotes`, `status`, the map location point, submission timestamp, image, and reporter email.
4. WHEN an Official changes the KanbanStatus of a Ticket, THE Backend SHALL update the `status` field of that Ticket in Firestore and set the `updated_at` timestamp to the current time.
5. THE Backend SHALL only permit KanbanStatus transitions in the following order: `To Do` → `In Progress` → `Done`.
6. IF an Official attempts a KanbanStatus transition that does not follow the permitted order, THEN THE Backend SHALL return an HTTP 400 Bad Request response and leave the Ticket status unchanged.
7. WHEN an Official updates a Ticket's KanbanStatus in Firestore, THE App SHALL propagate the updated status to all active Citizen feeds within 5 seconds via Firestore snapshot listeners.

---

### Requirement 9: Ticket Archival

**User Story:** As a citizen, I want resolved issues to remain visible for a short period, so that I can confirm that reported problems have been addressed.

#### Acceptance Criteria

1. WHEN a Ticket's KanbanStatus is set to `Done`, THE Backend SHALL record the timestamp of that transition in a dedicated `resolved_at` field on the Ticket document.
2. WHEN 7 days have elapsed since a Ticket's `resolved_at` timestamp, THE Backend SHALL transition the Ticket to `KanbanStatus = Archived`, which excludes it from all public citizen feed queries.
3. WHILE a Ticket's KanbanStatus is `Done` and the ArchivePeriod has not elapsed, THE App SHALL display the Ticket on the citizen map and list feed with a distinct "Resolved" badge label.
4. WHEN a Ticket is archived (KanbanStatus = `Archived`), THE Backend SHALL retain the Ticket document in Firestore and reject any further status transition or upvote modification requests for that Ticket with an HTTP 409 Conflict response.
5. WHEN a Ticket is archived, THE App SHALL not display it in the active citizen feed.

---

### Requirement 10: Static Landing Page

**User Story:** As a visitor, I want to access a landing page that presents the platform and provides an APK download link, so that I can learn about CivicSync and install the mobile app.

#### Acceptance Criteria

1. WHEN a visitor navigates to the root path (`/`) of the Cloud Run instance, THE Backend SHALL serve a static HTML/CSS landing page.
2. THE Landing_Page SHALL display a problem statement, a call-to-action section, and a direct download link that initiates a file download of the compiled Android APK hosted on Storage.
3. THE Landing_Page SHALL embed a video demonstration of the mobile application that plays within the page without requiring navigation away, and the video SHALL be at most 60 seconds in duration.
4. THE Landing_Page SHALL display a visual summary of the platform's technology stack naming at minimum all six Google technologies in use: Flutter, Go on Cloud Run, Google OAuth, Cloud Firestore, Cloud Storage for Firebase, and Gemini 2.5 Flash.
5. WHEN a visitor navigates to the root path (`/`), THE Backend SHALL serve the Landing_Page with an HTTP 200 OK response status code.
6. IF the APK file resource is unavailable at the time of the download request, THEN THE Landing_Page SHALL display an error message indicating the file is temporarily unavailable instead of presenting a broken link.

---

### Requirement 11: Serverless Deployment and Cold-Start Optimization

**User Story:** As a platform operator, I want the backend to scale to zero and restart efficiently, so that the platform incurs minimal cost during periods of inactivity.

#### Acceptance Criteria

1. THE Backend SHALL be packaged as a single self-contained binary and deployed to Google Cloud Run with scale-to-zero configuration enabled.
2. WHEN Cloud Run scales the Backend to zero instances during inactivity, THE Backend SHALL resume handling requests upon the next incoming request without manual intervention.
3. WHEN the Backend process starts, THE Backend SHALL complete initialization of all static configuration (Google public keys, Firestore client, AI_Service client) before beginning to accept incoming requests.
4. WHEN a Cloud Run cold start occurs, THE Backend SHALL return a valid HTTP response to the first incoming request within 3 seconds of container process start.
5. IF any required dependency (Google public keys, Firestore client, or AI_Service client) fails to initialize during startup, THEN THE Backend SHALL refuse incoming requests and return an HTTP 503 Service Unavailable response indicating that the service is not ready.
