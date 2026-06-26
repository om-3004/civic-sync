// Feature: civic-sync, Property 5: Duplicate Detection Returns the Closest Match

// **Validates: Requirements 4.2**
//
// Property: When multiple active tickets exist within 50 m of the origin and
// share the submitted category, FindDuplicate returns the ticket with the
// minimum Haversine distance from the origin.
package tickets

import (
	"context"
	"fmt"
	"testing"

	"github.com/civic-sync/civic-sync/internal/geo"
	"github.com/civic-sync/civic-sync/internal/models"
	"pgregory.net/rapid"
)

// TestPropertyClosestDuplicate verifies Property 5:
// FindDuplicate returns the closest (minimum Haversine distance) ticket when
// multiple active, category-matching tickets are within 50 m of the origin.
//
// rapid.Check runs >= 100 iterations by default.
func TestPropertyClosestDuplicate(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// --- Draw a random origin ---
		originLat := rapid.Float64Range(-89.0, 89.0).Draw(t, "originLat")
		originLng := rapid.Float64Range(-180.0, 180.0).Draw(t, "originLng")

		// --- Draw the submitted category ---
		submittedCategory := genCategory(t, "submittedCategory")

		// --- Draw N (2..8) tickets, all guaranteed within 50 m ---
		n := rapid.IntRange(2, 8).Draw(t, "numTickets")

		s := newDupFakeStore()
		ctx := context.Background()

		// Small offset range: ±0.0003° ≈ ±33 m in latitude, well within 50 m.
		// We verify each ticket is truly within 50 m via the oracle below.
		const maxOffset = 0.0003

		storedTickets := make([]*models.Ticket, 0, n)

		for i := 0; i < n; i++ {
			latOffset := rapid.Float64Range(-maxOffset, maxOffset).Draw(t, fmt.Sprintf("latOffset%d", i))
			lngOffset := rapid.Float64Range(-maxOffset, maxOffset).Draw(t, fmt.Sprintf("lngOffset%d", i))

			ticketLat := originLat + latOffset
			ticketLng := originLng + lngOffset

			// Clamp to valid coordinate ranges.
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

			// Pick an active status for all candidates.
			activeStatusList := []string{"To Do", "In Progress"}
			statusIdx := rapid.IntRange(0, len(activeStatusList)-1).Draw(t, fmt.Sprintf("statusIdx%d", i))

			ticket := &models.Ticket{
				ID:       fmt.Sprintf("closest-ticket-%d", i),
				Category: submittedCategory,
				Status:   activeStatusList[statusIdx],
				Location: models.Location{
					Latitude:  ticketLat,
					Longitude: ticketLng,
				},
			}

			if err := s.CreateTicket(ctx, ticket); err != nil {
				t.Fatalf("CreateTicket(%d): %v", i, err)
			}
			storedTickets = append(storedTickets, ticket)
		}

		// --- Verify all tickets are within 50 m (oracle pre-condition) ---
		// If any ticket landed outside 50 m after clamping, skip (skip is safe
		// in rapid — it shrinks the search space rather than failing).
		for _, ticket := range storedTickets {
			d := geo.HaversineMeters(originLat, originLng, ticket.Location.Latitude, ticket.Location.Longitude)
			if d > 50.0 {
				t.Skip("generated ticket outside 50 m after clamping — skipping iteration")
			}
		}

		// --- Call the function under test ---
		got, err := FindDuplicate(ctx, s, submittedCategory, originLat, originLng)
		if err != nil {
			t.Fatalf("FindDuplicate returned error: %v", err)
		}

		// --- Assert: result must be non-nil (all tickets are valid duplicates) ---
		if got == nil {
			t.Fatalf("FindDuplicate returned nil, expected a ticket (all %d tickets are active, matching category, within 50 m)", n)
		}

		// --- Assert: no other stored ticket has a strictly smaller distance ---
		returnedDist := geo.HaversineMeters(originLat, originLng, got.Location.Latitude, got.Location.Longitude)

		for _, ticket := range storedTickets {
			d := geo.HaversineMeters(originLat, originLng, ticket.Location.Latitude, ticket.Location.Longitude)
			if d < returnedDist {
				t.Fatalf(
					"FindDuplicate did not return the closest ticket:\n"+
						"  returned ticket %q at %.6f m\n"+
						"  but ticket %q is at %.6f m (closer)\n"+
						"  origin=(%.6f, %.6f)",
					got.ID, returnedDist,
					ticket.ID, d,
					originLat, originLng,
				)
			}
		}
	})
}
