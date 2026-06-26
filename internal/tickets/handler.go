package tickets

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/civic-sync/civic-sync/internal/auth"
	"github.com/civic-sync/civic-sync/internal/models"
	"github.com/civic-sync/civic-sync/internal/store"
)

// createTicketRequest is the expected JSON body for POST /tickets.
type createTicketRequest struct {
	Category    string          `json:"category"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	ImageURL    string          `json:"image_url"`
	Location    models.Location `json:"location"`
}

// createTicketResponse is the JSON body returned for POST /tickets.
type createTicketResponse struct {
	Ticket    *models.Ticket `json:"ticket"`
	Duplicate bool           `json:"duplicate"`
}

// newUUID generates a random UUID v4 using crypto/rand.
func newUUID() string {
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%12x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

// NewCreateTicketHandler returns an http.HandlerFunc that handles POST /tickets.
//
// It validates the request, runs proximity-based duplicate detection, and either
// returns an existing duplicate ticket (HTTP 200) or creates a new one (HTTP 201).
//
// Requirements: 4.1, 4.2, 4.3, 4.4, 4.6
func NewCreateTicketHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// --- 1. Decode request body (Req 4.6) ---
		var req createTicketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		// --- 2. Validate required string fields ---
		if req.Category == "" {
			http.Error(w, `{"error":"category is required"}`, http.StatusBadRequest)
			return
		}
		if req.Title == "" {
			http.Error(w, `{"error":"title is required"}`, http.StatusBadRequest)
			return
		}
		if req.Description == "" {
			http.Error(w, `{"error":"description is required"}`, http.StatusBadRequest)
			return
		}
		if req.ImageURL == "" {
			http.Error(w, `{"error":"image_url is required"}`, http.StatusBadRequest)
			return
		}

		// --- 3. Validate coordinates (Req 4.6) ---
		lat := req.Location.Latitude
		lng := req.Location.Longitude

		if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90.0 || lat > 90.0 {
			http.Error(w, `{"error":"latitude must be a finite value in [-90, 90]"}`, http.StatusBadRequest)
			return
		}
		if math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180.0 || lng > 180.0 {
			http.Error(w, `{"error":"longitude must be a finite value in [-180, 180]"}`, http.StatusBadRequest)
			return
		}

		// --- 4. Extract authenticated caller's UID ---
		uid := auth.UIDFromContext(ctx)

		// --- 5. Duplicate detection (Req 4.1, 4.2, 4.5) ---
		duplicate, err := FindDuplicate(ctx, s, req.Category, lat, lng)
		if err != nil {
			http.Error(w, `{"error":"failed to check for duplicate tickets"}`, http.StatusInternalServerError)
			return
		}

		// --- 6a. Duplicate found → return existing ticket (Req 4.2) ---
		if duplicate != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(createTicketResponse{
				Ticket:    duplicate,
				Duplicate: true,
			})
			return
		}

		// --- 6b. No duplicate → create a new ticket (Req 4.4) ---
		now := time.Now().UTC()
		ticket := &models.Ticket{
			ID:          newUUID(),
			Category:    req.Category,
			Title:       req.Title,
			Description: req.Description,
			ImageURL:    req.ImageURL,
			Location:    req.Location,
			Status:      "To Do",
			Upvotes:     0,
			UpvotedBy:   []string{},
			ReportedBy:  uid,
			CreatedAt:   now,
			UpdatedAt:   now,
			ResolvedAt:  nil,
		}

		if err := s.CreateTicket(ctx, ticket); err != nil {
			http.Error(w, `{"error":"failed to create ticket"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createTicketResponse{
			Ticket:    ticket,
			Duplicate: false,
		})
	}
}

// upvoteResponse is the JSON body returned for a successful POST /tickets/:id/upvote.
type upvoteResponse struct {
	Upvotes int `json:"upvotes"`
}

// NewUpvoteTicketHandler returns an http.HandlerFunc that handles POST /tickets/:id/upvote.
//
// It performs the following checks in order:
//  1. Extracts the ticket ID from the URL path.
//  2. Extracts the authenticated user's UID from the request context (401 if absent).
//  3. Fetches the ticket — returns 404 if it does not exist.
//  4. Rejects the request with 409 if the ticket status is "Archived".
//  5. Rejects the request with 409 if the user has already upvoted.
//  6. Atomically increments `upvotes` and updates `updated_at` via the store.
//  7. Returns {"upvotes": <new count>} with HTTP 200.
//
// Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6
func NewUpvoteTicketHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// --- 1. Extract ticket ID from URL path ---
		// Supports both Go 1.22+ pattern "/tickets/{id}/upvote" and
		// manual path parsing for older ServeMux registrations.
		ticketID := r.PathValue("id")
		if ticketID == "" {
			// Fallback: parse manually from "/tickets/<id>/upvote"
			path := strings.TrimSuffix(r.URL.Path, "/upvote")
			idx := strings.LastIndex(path, "/")
			if idx >= 0 {
				ticketID = path[idx+1:]
			}
		}
		if ticketID == "" {
			http.Error(w, `{"error":"missing ticket id"}`, http.StatusBadRequest)
			return
		}

		// --- 2. Extract authenticated user UID (Req 5.5) ---
		uid := auth.UIDFromContext(ctx)
		if uid == "" {
			http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
			return
		}

		// --- 3. Fetch the ticket; 404 if not found (Req 5.6) ---
		ticket, err := s.GetTicket(ctx, ticketID)
		if err != nil {
			http.Error(w, `{"error":"failed to retrieve ticket"}`, http.StatusInternalServerError)
			return
		}
		if ticket == nil {
			http.Error(w, `{"error":"ticket not found"}`, http.StatusNotFound)
			return
		}

		// --- 4. Reject archived tickets with 409 (Req 9.4) ---
		if ticket.Status == "Archived" {
			http.Error(w, `{"error":"ticket is archived"}`, http.StatusConflict)
			return
		}

		// --- 5. Reject duplicate upvotes with 409 (Req 5.3, 5.4) ---
		alreadyVoted, err := s.HasUserUpvoted(ctx, ticketID, uid)
		if err != nil {
			http.Error(w, `{"error":"failed to check upvote status"}`, http.StatusInternalServerError)
			return
		}
		if alreadyVoted {
			http.Error(w, `{"error":"already upvoted"}`, http.StatusConflict)
			return
		}

		// --- 6. Atomically increment upvotes + update updated_at (Req 5.1, 5.2) ---
		if err := s.IncrementUpvote(ctx, ticketID, uid); err != nil {
			// The store may return sentinel errors if a race occurred between
			// the HasUserUpvoted check and the write.
			if errors.Is(err, store.ErrAlreadyUpvoted) {
				http.Error(w, `{"error":"already upvoted"}`, http.StatusConflict)
				return
			}
			if errors.Is(err, store.ErrTicketArchived) {
				http.Error(w, `{"error":"ticket is archived"}`, http.StatusConflict)
				return
			}
			http.Error(w, `{"error":"failed to upvote ticket"}`, http.StatusInternalServerError)
			return
		}

		// --- 7. Fetch updated ticket to read new upvote count ---
		updated, err := s.GetTicket(ctx, ticketID)
		if err != nil || updated == nil {
			http.Error(w, `{"error":"failed to retrieve updated ticket"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(upvoteResponse{Upvotes: updated.Upvotes})
	}
}


// updateStatusRequest is the expected JSON body for PUT /tickets/:id/status.
type updateStatusRequest struct {
	Status string `json:"status"`
}

// statusResponse is the JSON body returned for a successful PUT /tickets/:id/status.
type statusResponse struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// validTransitions defines the only allowed status transitions for the
// ticket state machine (Requirements 8.5, 8.6).
var validTransitions = map[string]string{
	"To Do":       "In Progress",
	"In Progress": "Done",
}

// NewUpdateTicketStatusHandler returns an http.HandlerFunc that handles
// PUT /tickets/:id/status.
//
// It enforces that the caller has role "official", validates the requested
// transition against the state machine, and atomically updates the ticket
// status (and resolved_at when transitioning to Done).
//
// Response codes:
//   - 200: transition applied successfully
//   - 400: invalid or missing status, or disallowed transition
//   - 401: unauthenticated (no UID in context)
//   - 403: caller is not "official"
//   - 404: ticket not found
//   - 409: ticket is Archived (no further transitions allowed)
//
// Requirements: 8.4, 8.5, 8.6, 9.1
func NewUpdateTicketStatusHandler(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// --- 1. Extract ticket ID from URL path ---
		// Supports both Go 1.22+ pattern "/tickets/{id}/status" and
		// manual path parsing for older ServeMux registrations.
		ticketID := r.PathValue("id")
		if ticketID == "" {
			// Fallback: parse manually from "/tickets/<id>/status"
			path := strings.TrimSuffix(r.URL.Path, "/status")
			idx := strings.LastIndex(path, "/")
			if idx >= 0 {
				ticketID = path[idx+1:]
			}
		}
		if ticketID == "" {
			http.Error(w, `{"error":"missing ticket id"}`, http.StatusBadRequest)
			return
		}

		// --- 2. Extract authenticated user UID ---
		uid := auth.UIDFromContext(ctx)
		if uid == "" {
			http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
			return
		}

		// --- 3. Enforce role == "official" (Req 8.4) ---
		user, err := s.GetUser(ctx, uid)
		if err != nil {
			http.Error(w, `{"error":"failed to retrieve user profile"}`, http.StatusInternalServerError)
			return
		}
		if user == nil || user.Role != "official" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		// --- 4. Decode and validate request body ---
		var req updateStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if req.Status == "" {
			http.Error(w, `{"error":"status is required"}`, http.StatusBadRequest)
			return
		}

		// --- 5. Fetch the ticket; 404 if not found ---
		ticket, err := s.GetTicket(ctx, ticketID)
		if err != nil {
			http.Error(w, `{"error":"failed to retrieve ticket"}`, http.StatusInternalServerError)
			return
		}
		if ticket == nil {
			http.Error(w, `{"error":"ticket not found"}`, http.StatusNotFound)
			return
		}

		// --- 6. Reject archived tickets with 409 (Req 9.4) ---
		if ticket.Status == "Archived" {
			http.Error(w, `{"error":"ticket is archived"}`, http.StatusConflict)
			return
		}

		// --- 7. Validate state machine transition (Req 8.5, 8.6) ---
		allowed, ok := validTransitions[ticket.Status]
		if !ok || allowed != req.Status {
			http.Error(w, `{"error":"invalid status transition"}`, http.StatusBadRequest)
			return
		}

		// --- 8. Apply the status update atomically (Req 9.1) ---
		// UpdateTicketStatus writes status, updated_at, and resolved_at (for Done).
		if err := s.UpdateTicketStatus(ctx, ticketID, req.Status); err != nil {
			if errors.Is(err, store.ErrTicketArchived) {
				http.Error(w, `{"error":"ticket is archived"}`, http.StatusConflict)
				return
			}
			http.Error(w, `{"error":"failed to update ticket status"}`, http.StatusInternalServerError)
			return
		}

		// --- 9. Fetch updated ticket to read accurate updated_at ---
		updated, err := s.GetTicket(ctx, ticketID)
		if err != nil || updated == nil {
			http.Error(w, `{"error":"failed to retrieve updated ticket"}`, http.StatusInternalServerError)
			return
		}

		// --- 10. Return 200 with id, status, updated_at ---
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(statusResponse{
			ID:        updated.ID,
			Status:    updated.Status,
			UpdatedAt: updated.UpdatedAt,
		})
	}
}
