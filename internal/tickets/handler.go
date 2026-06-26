package tickets

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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
