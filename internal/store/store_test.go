package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/models"
)

// fakeStore is a thread-safe in-memory implementation of the Store interface,
// used for unit tests without a real Firestore backend.
type fakeStore struct {
	mu      sync.Mutex
	users   map[string]*models.User
	tickets map[string]*models.Ticket
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:   make(map[string]*models.User),
		tickets: make(map[string]*models.Ticket),
	}
}

// GetUser retrieves a user by UID.
func (f *fakeStore) GetUser(_ context.Context, uid string) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[uid]
	if !ok {
		return nil, nil
	}
	// Return a copy to avoid mutation of stored state.
	copy := *u
	return &copy, nil
}

// UpsertUser creates or fully overwrites a user document keyed by UID.
func (f *fakeStore) UpsertUser(_ context.Context, user *models.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := *user
	f.users[user.UID] = &copy
	return nil
}

// CreateTicket stores a new ticket by ID.
func (f *fakeStore) CreateTicket(_ context.Context, ticket *models.Ticket) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := *ticket
	copy.UpvotedBy = append([]string(nil), ticket.UpvotedBy...)
	f.tickets[ticket.ID] = &copy
	return nil
}

// GetTicket retrieves a ticket by ID.
func (f *fakeStore) GetTicket(_ context.Context, id string) (*models.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[id]
	if !ok {
		return nil, nil
	}
	copy := *t
	copy.UpvotedBy = append([]string(nil), t.UpvotedBy...)
	return &copy, nil
}

// QueryActiveTicketsByCategory is a no-op stub — not under test here.
func (f *fakeStore) QueryActiveTicketsByCategory(_ context.Context, _, _, _ float64, _ string) ([]*models.Ticket, error) {
	return nil, nil
}

// IncrementUpvote atomically increments the upvote count, returning
// ErrAlreadyUpvoted if the voter has already upvoted the ticket.
func (f *fakeStore) IncrementUpvote(_ context.Context, ticketID, voterUID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	t, ok := f.tickets[ticketID]
	if !ok {
		return errors.New("ticket not found")
	}

	for _, uid := range t.UpvotedBy {
		if uid == voterUID {
			return ErrAlreadyUpvoted
		}
	}

	t.UpvotedBy = append(t.UpvotedBy, voterUID)
	t.Upvotes++
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// UpdateTicketStatus is a no-op stub — not under test here.
func (f *fakeStore) UpdateTicketStatus(_ context.Context, _, _ string) error {
	return nil
}

// ArchiveExpiredTickets is a no-op stub — not under test here.
func (f *fakeStore) ArchiveExpiredTickets(_ context.Context) error {
	return nil
}

// HasUserUpvoted reports whether a user has already upvoted a ticket.
func (f *fakeStore) HasUserUpvoted(_ context.Context, ticketID, uid string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[ticketID]
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

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestUpsertUser_Idempotency verifies Requirement 1.8:
// calling UpsertUser with the same UID multiple times must result in exactly
// one user document whose fields reflect the most-recently upserted values.
func TestUpsertUser_Idempotency(t *testing.T) {
	ctx := context.Background()
	s := newFakeStore()

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	uid := "user-abc"

	// First upsert — initial state.
	if err := s.UpsertUser(ctx, &models.User{
		UID:       uid,
		Email:     "first@example.com",
		Name:      "First Name",
		Role:      "citizen",
		CreatedAt: base,
	}); err != nil {
		t.Fatalf("first UpsertUser: %v", err)
	}

	// Second upsert — same UID, updated fields.
	if err := s.UpsertUser(ctx, &models.User{
		UID:       uid,
		Email:     "second@example.com",
		Name:      "Second Name",
		Role:      "official",
		CreatedAt: base,
	}); err != nil {
		t.Fatalf("second UpsertUser: %v", err)
	}

	// Third upsert — final desired state.
	want := &models.User{
		UID:       uid,
		Email:     "final@example.com",
		Name:      "Final Name",
		Role:      "citizen",
		CreatedAt: base,
	}
	if err := s.UpsertUser(ctx, want); err != nil {
		t.Fatalf("third UpsertUser: %v", err)
	}

	// There must be exactly one document for this UID.
	s.mu.Lock()
	count := 0
	for k := range s.users {
		if k == uid {
			count++
		}
	}
	s.mu.Unlock()

	if count != 1 {
		t.Errorf("expected exactly 1 user document for UID %q, got %d", uid, count)
	}

	// The stored document must match the last-upserted values.
	got, err := s.GetUser(ctx, uid)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got == nil {
		t.Fatal("expected user to exist, got nil")
	}
	if got.Email != want.Email {
		t.Errorf("Email: got %q, want %q", got.Email, want.Email)
	}
	if got.Name != want.Name {
		t.Errorf("Name: got %q, want %q", got.Name, want.Name)
	}
	if got.Role != want.Role {
		t.Errorf("Role: got %q, want %q", got.Role, want.Role)
	}
}

// TestUpsertUser_Idempotency_SeparateUIDs verifies that upserting distinct UIDs
// creates separate documents and does not collide.
func TestUpsertUser_Idempotency_SeparateUIDs(t *testing.T) {
	ctx := context.Background()
	s := newFakeStore()

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	uids := []string{"uid-1", "uid-2", "uid-3"}

	for _, uid := range uids {
		if err := s.UpsertUser(ctx, &models.User{
			UID:       uid,
			Email:     uid + "@example.com",
			Name:      uid,
			Role:      "citizen",
			CreatedAt: base,
		}); err != nil {
			t.Fatalf("UpsertUser(%s): %v", uid, err)
		}
	}

	s.mu.Lock()
	total := len(s.users)
	s.mu.Unlock()

	if total != len(uids) {
		t.Errorf("expected %d user documents, got %d", len(uids), total)
	}
}

// TestIncrementUpvote_AlreadyVoted verifies Requirement 5.3:
// a second IncrementUpvote call by the same voter must return ErrAlreadyUpvoted,
// leave the upvote count at 1, and not advance updated_at.
func TestIncrementUpvote_AlreadyVoted(t *testing.T) {
	ctx := context.Background()
	s := newFakeStore()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	ticketID := "ticket-xyz"
	voterUID := "voter-001"

	ticket := &models.Ticket{
		ID:        ticketID,
		Title:     "Broken streetlight",
		Category:  "Infrastructure",
		Status:    "To Do",
		Upvotes:   0,
		UpdatedAt: now,
		CreatedAt: now,
	}
	if err := s.CreateTicket(ctx, ticket); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// First upvote — must succeed.
	if err := s.IncrementUpvote(ctx, ticketID, voterUID); err != nil {
		t.Fatalf("first IncrementUpvote: %v", err)
	}

	// Capture updated_at after the legitimate upvote.
	after1, err := s.GetTicket(ctx, ticketID)
	if err != nil || after1 == nil {
		t.Fatalf("GetTicket after first upvote: %v", err)
	}
	updatedAtAfterFirst := after1.UpdatedAt

	if after1.Upvotes != 1 {
		t.Errorf("upvote count after first vote: got %d, want 1", after1.Upvotes)
	}

	// Second upvote by the same voter — must return ErrAlreadyUpvoted.
	err = s.IncrementUpvote(ctx, ticketID, voterUID)
	if !errors.Is(err, ErrAlreadyUpvoted) {
		t.Errorf("second IncrementUpvote: got err %v, want ErrAlreadyUpvoted", err)
	}

	// Upvote count must still be 1.
	after2, err := s.GetTicket(ctx, ticketID)
	if err != nil || after2 == nil {
		t.Fatalf("GetTicket after second upvote attempt: %v", err)
	}
	if after2.Upvotes != 1 {
		t.Errorf("upvote count after duplicate attempt: got %d, want 1", after2.Upvotes)
	}

	// updated_at must be unchanged after the rejected duplicate.
	if !after2.UpdatedAt.Equal(updatedAtAfterFirst) {
		t.Errorf("updated_at changed after duplicate upvote: got %v, want %v",
			after2.UpdatedAt, updatedAtAfterFirst)
	}
}

// TestIncrementUpvote_DifferentVoters verifies that two distinct voters can
// each upvote once, bringing the count to 2.
func TestIncrementUpvote_DifferentVoters(t *testing.T) {
	ctx := context.Background()
	s := newFakeStore()

	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	ticketID := "ticket-multi"

	if err := s.CreateTicket(ctx, &models.Ticket{
		ID:        ticketID,
		Title:     "Pothole",
		Category:  "Roads",
		Status:    "To Do",
		Upvotes:   0,
		UpdatedAt: now,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	for _, uid := range []string{"voter-A", "voter-B"} {
		if err := s.IncrementUpvote(ctx, ticketID, uid); err != nil {
			t.Fatalf("IncrementUpvote(%s): %v", uid, err)
		}
	}

	got, err := s.GetTicket(ctx, ticketID)
	if err != nil || got == nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Upvotes != 2 {
		t.Errorf("upvote count: got %d, want 2", got.Upvotes)
	}
}
