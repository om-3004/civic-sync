package tickets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/auth"
	"github.com/civic-sync/civic-sync/internal/models"
	"github.com/civic-sync/civic-sync/internal/store"
)

// upvoteTestStore is a lightweight in-memory store for handler tests.

type upvoteTestStore struct {
	tickets map[string]*models.Ticket
}

func newUpvoteTestStore() *upvoteTestStore {
	return &upvoteTestStore{tickets: make(map[string]*models.Ticket)}
}

func (s *upvoteTestStore) addTicket(t *models.Ticket) {
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	s.tickets[t.ID] = &cp
}

func (s *upvoteTestStore) GetUser(_ context.Context, _ string) (*models.User, error) {
	return nil, nil
}
func (s *upvoteTestStore) UpsertUser(_ context.Context, _ *models.User) error { return nil }
func (s *upvoteTestStore) CreateTicket(_ context.Context, _ *models.Ticket) error {
	return nil
}
func (s *upvoteTestStore) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	t, ok := s.tickets[id]
	if !ok {
		return nil, nil
	}
	cp := *t
	cp.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	return &cp, nil
}
func (s *upvoteTestStore) QueryActiveTicketsByCategory(_ context.Context, _, _, _ float64, _ string) ([]*models.Ticket, error) {
	return nil, nil
}
func (s *upvoteTestStore) IncrementUpvote(_ context.Context, ticketID, voterUID string) error {
	t, ok := s.tickets[ticketID]
	if !ok {
		return nil
	}
	for _, uid := range t.UpvotedBy {
		if uid == voterUID {
			return store.ErrAlreadyUpvoted
		}
	}
	t.UpvotedBy = append(t.UpvotedBy, voterUID)
	t.Upvotes++
	t.UpdatedAt = time.Now().UTC()
	return nil
}
func (s *upvoteTestStore) UpdateTicketStatus(_ context.Context, _, _ string) error { return nil }
func (s *upvoteTestStore) ArchiveExpiredTickets(_ context.Context) error            { return nil }
func (s *upvoteTestStore) HasUserUpvoted(_ context.Context, ticketID, uid string) (bool, error) {
	t, ok := s.tickets[ticketID]
	if !ok {
		return false, nil
	}
	for _, u := range t.UpvotedBy {
		if u == uid {
			return true, nil
		}
	}
	return false, nil
}

// makeUpvoteRequest creates a POST request for /tickets/{id}/upvote optionally
// injecting an authenticated uid into the context.
func makeUpvoteRequest(ticketID, uid string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/tickets/"+ticketID+"/upvote", nil)
	if uid != "" {
		ctx := auth.WithClaims(r.Context(), uid, "test@example.com", "Test User")
		r = r.WithContext(ctx)
	}
	return r
}

// ---- Tests ------------------------------------------------------------------

// TestUpvoteHandler_Success verifies Req 5.1: first upvote increments count and returns 200.
func TestUpvoteHandler_Success(t *testing.T) {
	s := newUpvoteTestStore()
	now := time.Now().UTC()
	s.addTicket(&models.Ticket{
		ID:        "ticket-1",
		Status:    "To Do",
		Upvotes:   3,
		UpdatedAt: now,
		CreatedAt: now,
	})

	handler := NewUpvoteTicketHandler(s)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, makeUpvoteRequest("ticket-1", "user-abc"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp upvoteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Upvotes != 4 {
		t.Errorf("upvotes: got %d, want 4", resp.Upvotes)
	}
}

// TestUpvoteHandler_Unauthenticated verifies Req 5.5: no auth → 401.
func TestUpvoteHandler_Unauthenticated(t *testing.T) {
	s := newUpvoteTestStore()
	now := time.Now().UTC()
	s.addTicket(&models.Ticket{
		ID: "ticket-2", Status: "To Do", Upvotes: 0,
		UpdatedAt: now, CreatedAt: now,
	})

	handler := NewUpvoteTicketHandler(s)
	w := httptest.NewRecorder()
	// no uid injected → unauthenticated
	handler.ServeHTTP(w, makeUpvoteRequest("ticket-2", ""))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestUpvoteHandler_TicketNotFound verifies Req 5.6: unknown ticket → 404.
func TestUpvoteHandler_TicketNotFound(t *testing.T) {
	s := newUpvoteTestStore()

	handler := NewUpvoteTicketHandler(s)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, makeUpvoteRequest("nonexistent", "user-abc"))

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestUpvoteHandler_AlreadyUpvoted verifies Req 5.3/5.4: second upvote → 409.
func TestUpvoteHandler_AlreadyUpvoted(t *testing.T) {
	s := newUpvoteTestStore()
	now := time.Now().UTC()
	s.addTicket(&models.Ticket{
		ID:        "ticket-3",
		Status:    "To Do",
		Upvotes:   1,
		UpvotedBy: []string{"user-abc"},
		UpdatedAt: now,
		CreatedAt: now,
	})

	handler := NewUpvoteTicketHandler(s)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, makeUpvoteRequest("ticket-3", "user-abc"))

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}

	// Upvote count must be unchanged.
	tick, _ := s.GetTicket(context.Background(), "ticket-3")
	if tick.Upvotes != 1 {
		t.Errorf("upvotes changed after duplicate attempt: got %d, want 1", tick.Upvotes)
	}
}

// TestUpvoteHandler_ArchivedTicket verifies Req 9.4: upvoting archived ticket → 409.
func TestUpvoteHandler_ArchivedTicket(t *testing.T) {
	s := newUpvoteTestStore()
	now := time.Now().UTC()
	s.addTicket(&models.Ticket{
		ID:        "ticket-4",
		Status:    "Archived",
		Upvotes:   5,
		UpdatedAt: now,
		CreatedAt: now,
	})

	handler := NewUpvoteTicketHandler(s)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, makeUpvoteRequest("ticket-4", "user-abc"))

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}
