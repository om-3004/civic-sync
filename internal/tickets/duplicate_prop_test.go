// Feature: civic-sync, Property 4: Duplicate Detection Correctly Partitions Submissions by Distance and Category

// **Validates: Requirements 4.1, 4.4, 4.5**
//
// Property: FindDuplicate returns a non-nil ticket if and only if at least one
// stored ticket satisfies ALL of the following simultaneously:
//   - status is "To Do" or "In Progress" (active)
//   - category matches the submitted category
//   - Haversine distance from the origin is <= 50.0 metres
package tickets

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/civic-sync/civic-sync/internal/geo"
	"github.com/civic-sync/civic-sync/internal/models"
	"github.com/civic-sync/civic-sync/internal/store"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// In-memory fake store for duplicate-detection tests
// ---------------------------------------------------------------------------

// dupFakeStore is a thread-safe in-memory Store that implements
// QueryActiveTicketsByCategory with genuine bounding-box + status + category
// filtering, mirroring what the production Firestore implementation does.
type dupFakeStore struct {
	mu      sync.Mutex
	tickets map[string]*models.Ticket
}

func newDupFakeStore() *dupFakeStore {
	return &dupFakeStore{tickets: make(map[string]*models.Ticket)}
}

// QueryActiveTicketsByCategory returns tickets whose status is "To Do" or
// "In Progress", whose category matches, and whose location falls within the
// bounding box defined by lat/lng ± radiusMeters. This faithfully reproduces
// the Firestore bounding-box pre-filter so that FindDuplicate can apply its
// exact Haversine post-filter on top.
func (f *dupFakeStore) QueryActiveTicketsByCategory(
	_ context.Context,
	lat, lng, radiusMeters float64,
	category string,
) ([]*models.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	deltaLat, deltaLng := geo.BoundingBoxDelta(lat, radiusMeters)
	minLat := lat - deltaLat
	maxLat := lat + deltaLat
	minLng := lng - deltaLng
	maxLng := lng + deltaLng

	var result []*models.Ticket
	for _, t := range f.tickets {
		// Active-status filter
		if t.Status != "To Do" && t.Status != "In Progress" {
			continue
		}
		// Category filter
		if t.Category != category {
			continue
		}
		// Bounding-box filter
		if t.Location.Latitude < minLat || t.Location.Latitude > maxLat {
			continue
		}
		if t.Location.Longitude < minLng || t.Location.Longitude > maxLng {
			continue
		}
		cp := *t
		result = append(result, &cp)
	}
	return result, nil
}

// CreateTicket stores a new ticket.
func (f *dupFakeStore) CreateTicket(_ context.Context, t *models.Ticket) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *t
	f.tickets[t.ID] = &cp
	return nil
}

// --- Unused Store interface stubs ---

func (f *dupFakeStore) GetUser(_ context.Context, _ string) (*models.User, error) {
	return nil, nil
}
func (f *dupFakeStore) UpsertUser(_ context.Context, _ *models.User) error { return nil }
func (f *dupFakeStore) GetTicket(_ context.Context, _ string) (*models.Ticket, error) {
	return nil, nil
}
func (f *dupFakeStore) IncrementUpvote(_ context.Context, _, _ string) error { return nil }
func (f *dupFakeStore) UpdateTicketStatus(_ context.Context, _, _ string) error { return nil }
func (f *dupFakeStore) ArchiveExpiredTickets(_ context.Context) error           { return nil }
func (f *dupFakeStore) HasUserUpvoted(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

// Ensure dupFakeStore satisfies store.Store at compile time.
var _ store.Store = (*dupFakeStore)(nil)

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

var allStatuses = []string{"To Do", "In Progress", "Done", "Archived"}
var allCategories = []string{"Pothole", "Water Clogging", "Drain Overflow", "Electrical Hazard", "Other"}

// activeStatuses is the set of statuses FindDuplicate considers "active".
var activeStatuses = map[string]bool{
	"To Do":       true,
	"In Progress": true,
}

// genStatus draws a random status from allStatuses.
func genStatus(t *rapid.T, label string) string {
	idx := rapid.IntRange(0, len(allStatuses)-1).Draw(t, label)
	return allStatuses[idx]
}

// genCategory draws a random category from allCategories.
func genCategory(t *rapid.T, label string) string {
	idx := rapid.IntRange(0, len(allCategories)-1).Draw(t, label)
	return allCategories[idx]
}

// ---------------------------------------------------------------------------
// Oracle: independently determines whether a duplicate should exist
// ---------------------------------------------------------------------------

// hasDuplicate returns true if any ticket in ts is active, matches category,
// and is within 50 m of (originLat, originLng). This is the reference
// implementation against which FindDuplicate is verified.
func hasDuplicate(ts []*models.Ticket, originLat, originLng float64, category string) bool {
	for _, t := range ts {
		if !activeStatuses[t.Status] {
			continue
		}
		if t.Category != category {
			continue
		}
		d := geo.HaversineMeters(originLat, originLng, t.Location.Latitude, t.Location.Longitude)
		if d <= 50.0 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Property test
// ---------------------------------------------------------------------------

// TestPropertyDuplicateDetectionPartitioning verifies Property 4:
// FindDuplicate correctly partitions submissions — it returns a non-nil ticket
// if and only if at least one active, category-matching ticket exists within
// 50 metres of the submitted location.
//
// rapid.Check runs ≥ 100 iterations by default.
func TestPropertyDuplicateDetectionPartitioning(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- Draw a random origin ---
		originLat := rapid.Float64Range(-89.0, 89.0).Draw(t, "originLat")
		originLng := rapid.Float64Range(-180.0, 180.0).Draw(t, "originLng")

		// --- Draw the submitted category ---
		submittedCategory := genCategory(t, "submittedCategory")

		// --- Draw a random set of tickets (0..10) ---
		numTickets := rapid.IntRange(0, 10).Draw(t, "numTickets")

		s := newDupFakeStore()
		ctx := context.Background()

		tickets := make([]*models.Ticket, 0, numTickets)

		for i := 0; i < numTickets; i++ {
			// Each ticket has a random location drawn from a wider area so we
			// get a natural mix of near (<= 50 m) and far (> 50 m) tickets.
			// ~±0.002° ≈ ±220 m in latitude, giving plenty of tickets outside
			// the 50 m radius alongside some inside it.
			ticketLat := originLat + rapid.Float64Range(-0.002, 0.002).Draw(t, fmt.Sprintf("ticketLat%d", i))
			ticketLng := originLng + rapid.Float64Range(-0.002, 0.002).Draw(t, fmt.Sprintf("ticketLng%d", i))

			// Clamp to valid coordinate ranges to avoid floating-point edge cases.
			if ticketLat < -90.0 {
				ticketLat = -90.0
			}
			if ticketLat > 90.0 {
				ticketLat = 90.0
			}
			if ticketLng < -180.0 {
				ticketLng = -180.0
			}
			if ticketLng > 180.0 {
				ticketLng = 180.0
			}

			ticket := &models.Ticket{
				ID:       fmt.Sprintf("ticket-%d", i),
				Category: genCategory(t, fmt.Sprintf("ticketCategory%d", i)),
				Status:   genStatus(t, fmt.Sprintf("ticketStatus%d", i)),
				Location: models.Location{
					Latitude:  ticketLat,
					Longitude: ticketLng,
				},
			}

			if err := s.CreateTicket(ctx, ticket); err != nil {
				t.Fatalf("CreateTicket(%d): %v", i, err)
			}
			tickets = append(tickets, ticket)
		}

		// --- Call the function under test ---
		got, err := FindDuplicate(ctx, s, submittedCategory, originLat, originLng)
		if err != nil {
			t.Fatalf("FindDuplicate returned error: %v", err)
		}

		// --- Oracle: compute expected result independently ---
		expectDuplicate := hasDuplicate(tickets, originLat, originLng, submittedCategory)

		// --- Assert: duplicate found iff oracle says so ---
		gotDuplicate := got != nil

		if gotDuplicate != expectDuplicate {
			t.Fatalf(
				"partition mismatch: FindDuplicate returned %v, oracle says duplicate=%v\n"+
					"  origin=(%.6f, %.6f) category=%q numTickets=%d",
				got, expectDuplicate,
				originLat, originLng, submittedCategory, numTickets,
			)
		}

		// --- Additional invariant: returned ticket must itself satisfy the criteria ---
		if got != nil {
			if !activeStatuses[got.Status] {
				t.Fatalf("returned ticket has inactive status %q", got.Status)
			}
			if got.Category != submittedCategory {
				t.Fatalf("returned ticket category %q != submitted category %q", got.Category, submittedCategory)
			}
			d := geo.HaversineMeters(originLat, originLng, got.Location.Latitude, got.Location.Longitude)
			if d > 50.0 {
				t.Fatalf("returned ticket is %.4f m away, exceeds 50 m threshold", d)
			}
		}
	})
}
