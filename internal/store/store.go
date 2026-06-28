package store

import (
	"context"

	"github.com/civic-sync/civic-sync/internal/models"
)

// Store encapsulates all Firestore operations, enabling unit-testable mocking.
type Store interface {
	// GetUser retrieves a user profile by Firebase Auth UID.
	GetUser(ctx context.Context, uid string) (*models.User, error)

	// UpsertUser creates or updates a user profile document.
	UpsertUser(ctx context.Context, user *models.User) error

	// CreateTicket writes a new ticket document to Firestore.
	CreateTicket(ctx context.Context, ticket *models.Ticket) error

	// GetTicket retrieves a single ticket by its ID.
	GetTicket(ctx context.Context, id string) (*models.Ticket, error)

	// QueryActiveTicketsByCategory returns active tickets (To Do or In Progress)
	// within the bounding box defined by lat/lng ± radiusMeters and matching category.
	QueryActiveTicketsByCategory(ctx context.Context, lat, lng, radiusMeters float64, category string) ([]*models.Ticket, error)

	// IncrementUpvote atomically increments the upvote count for a ticket and
	// records the voter's UID to prevent duplicate upvotes.
	IncrementUpvote(ctx context.Context, ticketID, voterUID string) error

	// UpdateTicketStatus updates the status field of a ticket and sets updated_at.
	UpdateTicketStatus(ctx context.Context, ticketID, newStatus string) error

	// ArchiveExpiredTickets transitions all Done tickets whose resolved_at is older
	// than 7 days to Archived status. Called by the background archival goroutine.
	ArchiveExpiredTickets(ctx context.Context) error

	// HasUserUpvoted reports whether the given user has already upvoted the ticket.
	HasUserUpvoted(ctx context.Context, ticketID, uid string) (bool, error)

	// DeleteTicket permanently removes a ticket document from Firestore.
	// The caller is responsible for verifying ownership before calling this.
	DeleteTicket(ctx context.Context, id string) error
}
