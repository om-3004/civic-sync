/*
Package store provides the Firestore-backed implementation of the Store interface.

Required Firestore composite indexes for /tickets collection:
  1. status ASC, created_at DESC   — citizen feed (active tickets sorted by newest)
  2. status ASC, upvotes DESC      — official dashboard (active tickets sorted by popularity)
  3. status ASC, resolved_at ASC   — archival query (Done tickets sorted by resolution time)
  4. category ASC, status ASC      — duplicate detection pre-filter

These indexes must be created in the Firebase console or via firestore.indexes.json
before deploying queries that depend on them.
*/
package store

import (
	"context"
	"errors"
	"math"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/civic-sync/civic-sync/internal/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	collectionUsers   = "users"
	collectionTickets = "tickets"

	// archiveThreshold is the number of days after which a Done ticket is archived.
	archiveThreshold = 7 * 24 * time.Hour

	// batchSize is the maximum number of documents Firestore allows in a single batch write.
	batchSize = 500

	// metersPerDegreeLat is the approximate number of meters per degree of latitude.
	metersPerDegreeLat = 111320.0
)

// Sentinel errors returned by store methods.
var (
	ErrAlreadyUpvoted = errors.New("already upvoted")
	ErrTicketArchived = errors.New("ticket is archived")
)

// FirestoreStore implements Store using Cloud Firestore as the backend.
type FirestoreStore struct {
	client *firestore.Client
}

// NewFirestoreStore returns a new FirestoreStore backed by the provided Firestore client.
func NewFirestoreStore(client *firestore.Client) *FirestoreStore {
	return &FirestoreStore{client: client}
}

// GetUser retrieves a user profile by Firebase Auth UID.
// Returns nil, nil if the document does not exist.
func (s *FirestoreStore) GetUser(ctx context.Context, uid string) (*models.User, error) {
	doc, err := s.client.Collection(collectionUsers).Doc(uid).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}

	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

// UpsertUser creates or fully overwrites a user profile document.
// The caller is expected to supply a fully-populated User struct.
func (s *FirestoreStore) UpsertUser(ctx context.Context, user *models.User) error {
	_, err := s.client.Collection(collectionUsers).Doc(user.UID).Set(ctx, user)
	return err
}

// CreateTicket writes a new ticket document at /tickets/{ticket.ID}.
func (s *FirestoreStore) CreateTicket(ctx context.Context, ticket *models.Ticket) error {
	_, err := s.client.Collection(collectionTickets).Doc(ticket.ID).Set(ctx, ticket)
	return err
}

// GetTicket retrieves a single ticket by its ID.
// Returns nil, nil if the document does not exist.
func (s *FirestoreStore) GetTicket(ctx context.Context, id string) (*models.Ticket, error) {
	doc, err := s.client.Collection(collectionTickets).Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}

	var ticket models.Ticket
	if err := doc.DataTo(&ticket); err != nil {
		return nil, err
	}
	return &ticket, nil
}

// QueryActiveTicketsByCategory returns tickets with status "To Do" or "In Progress"
// within the bounding box defined by lat/lng ± radiusMeters that match the given category.
//
// Strategy: Firestore does not allow range filters on more than one field in a single query.
// We therefore:
//  1. Filter status with whereIn ["To Do", "In Progress"].
//  2. Filter category with ==.
//  3. Filter latitude range with >= / <= (single range field).
//  4. Apply the longitude range filter in-process after fetching.
func (s *FirestoreStore) QueryActiveTicketsByCategory(
	ctx context.Context,
	lat, lng, radiusMeters float64,
	category string,
) ([]*models.Ticket, error) {
	deltaLat := radiusMeters / metersPerDegreeLat

	// Guard against division by zero near the poles (cos(90°) = 0).
	cosLat := math.Cos(lat * math.Pi / 180)
	var deltaLng float64
	if math.Abs(cosLat) < 1e-10 {
		deltaLng = 180.0 // effectively no longitude filter near poles
	} else {
		deltaLng = radiusMeters / (metersPerDegreeLat * math.Abs(cosLat))
	}

	minLat := lat - deltaLat
	maxLat := lat + deltaLat
	minLng := lng - deltaLng
	maxLng := lng + deltaLng

	iter := s.client.Collection(collectionTickets).
		Where("status", "in", []string{"To Do", "In Progress"}).
		Where("category", "==", category).
		Where("location.latitude", ">=", minLat).
		Where("location.latitude", "<=", maxLat).
		Documents(ctx)

	docs, err := iter.GetAll()
	if err != nil {
		return nil, err
	}

	var tickets []*models.Ticket
	for _, doc := range docs {
		var t models.Ticket
		if err := doc.DataTo(&t); err != nil {
			return nil, err
		}
		// Apply longitude filter in-process (Firestore range constraint).
		if t.Location.Longitude >= minLng && t.Location.Longitude <= maxLng {
			tickets = append(tickets, &t)
		}
	}
	return tickets, nil
}

// IncrementUpvote atomically increments the upvote count for a ticket and records
// the voter UID. Returns ErrAlreadyUpvoted if the voter has already upvoted,
// or ErrTicketArchived if the ticket has been archived.
func (s *FirestoreStore) IncrementUpvote(ctx context.Context, ticketID, voterUID string) error {
	ref := s.client.Collection(collectionTickets).Doc(ticketID)

	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}

		var ticket models.Ticket
		if err := doc.DataTo(&ticket); err != nil {
			return err
		}

		if ticket.Status == "Archived" {
			return ErrTicketArchived
		}

		for _, uid := range ticket.UpvotedBy {
			if uid == voterUID {
				return ErrAlreadyUpvoted
			}
		}

		return tx.Update(ref, []firestore.Update{
			{Path: "upvoted_by", Value: firestore.ArrayUnion(voterUID)},
			{Path: "upvotes", Value: ticket.Upvotes + 1},
			{Path: "updated_at", Value: time.Now().UTC()},
		})
	})
}

// UpdateTicketStatus updates the status field of a ticket and sets updated_at.
// If newStatus is "Done", resolved_at is also set to the current time.
func (s *FirestoreStore) UpdateTicketStatus(ctx context.Context, ticketID, newStatus string) error {
	now := time.Now().UTC()
	updates := []firestore.Update{
		{Path: "status", Value: newStatus},
		{Path: "updated_at", Value: now},
	}
	if newStatus == "Done" {
		updates = append(updates, firestore.Update{Path: "resolved_at", Value: now})
	}

	_, err := s.client.Collection(collectionTickets).Doc(ticketID).Update(ctx, updates)
	return err
}

// ArchiveExpiredTickets transitions all "Done" tickets whose resolved_at is older than
// 7 days to "Archived" status. It processes documents in batches of up to 500 to stay
// within Firestore's batch-write limit, looping until no more documents match.
func (s *FirestoreStore) ArchiveExpiredTickets(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-archiveThreshold)
	now := time.Now().UTC()

	for {
		iter := s.client.Collection(collectionTickets).
			Where("status", "==", "Done").
			Where("resolved_at", "<=", cutoff).
			Limit(batchSize).
			Documents(ctx)

		docs, err := iter.GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			break
		}

		batch := s.client.Batch()
		for _, doc := range docs {
			batch.Update(doc.Ref, []firestore.Update{
				{Path: "status", Value: "Archived"},
				{Path: "updated_at", Value: now},
			})
		}
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}

		// If fewer docs were returned than the batch limit, we have processed all.
		if len(docs) < batchSize {
			break
		}
	}
	return nil
}

// HasUserUpvoted reports whether the given user has already upvoted the ticket.
func (s *FirestoreStore) HasUserUpvoted(ctx context.Context, ticketID, uid string) (bool, error) {
	ticket, err := s.GetTicket(ctx, ticketID)
	if err != nil {
		return false, err
	}
	if ticket == nil {
		return false, nil
	}
	for _, u := range ticket.UpvotedBy {
		if u == uid {
			return true, nil
		}
	}
	return false, nil
}

// DeleteTicket permanently removes a ticket document from Firestore.
func (s *FirestoreStore) DeleteTicket(ctx context.Context, id string) error {
	_, err := s.client.Collection(collectionTickets).Doc(id).Delete(ctx)
	return err
}
