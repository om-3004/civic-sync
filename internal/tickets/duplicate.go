package tickets

import (
	"context"

	"github.com/civic-sync/civic-sync/internal/geo"
	"github.com/civic-sync/civic-sync/internal/models"
	"github.com/civic-sync/civic-sync/internal/store"
)

const duplicateRadiusMeters = 50.0

// FindDuplicate queries active tickets by category within a bounding box,
// then applies Haversine exact-distance filtering to find duplicates within
// 50 meters. Returns the closest matching ticket, or nil if no duplicate found.
func FindDuplicate(ctx context.Context, s store.Store, category string, lat, lng float64) (*models.Ticket, error) {
	// Step 1: bounding-box pre-filter via Firestore
	candidates, err := s.QueryActiveTicketsByCategory(ctx, lat, lng, duplicateRadiusMeters, category)
	if err != nil {
		return nil, err
	}

	// Step 2: exact Haversine distance check in-process
	var closest *models.Ticket
	var closestDist float64

	for _, t := range candidates {
		d := geo.HaversineMeters(lat, lng, t.Location.Latitude, t.Location.Longitude)
		if d > duplicateRadiusMeters {
			continue
		}
		if closest == nil || d < closestDist {
			closest = t
			closestDist = d
		}
	}

	// Step 3: return closest match or nil
	return closest, nil
}
