package tickets

import (
	"context"
	"strings"

	"github.com/civic-sync/civic-sync/internal/geo"
	"github.com/civic-sync/civic-sync/internal/models"
	"github.com/civic-sync/civic-sync/internal/store"
)

const (
	duplicateRadiusMeters = 50.0

	// textSimilarityThreshold is the minimum Jaccard similarity score
	// (0.0–1.0) required between two tickets' combined title+description
	// text for them to be considered duplicates.
	// 0.25 means at least 25% word overlap — loose enough to catch the
	// same issue reported in slightly different words.
	textSimilarityThreshold = 0.25
)

// FindDuplicate queries active tickets by category within a bounding box,
// applies Haversine exact-distance filtering, and then uses text similarity
// on title+description to confirm the duplicate. Returns the closest
// matching ticket, or nil if no duplicate found.
func FindDuplicate(ctx context.Context, s store.Store, category string, lat, lng float64, title, description string) (*models.Ticket, error) {
	// Step 1: bounding-box pre-filter via Firestore
	candidates, err := s.QueryActiveTicketsByCategory(ctx, lat, lng, duplicateRadiusMeters, category)
	if err != nil {
		return nil, err
	}

	// Step 2: exact Haversine distance check in-process
	var locationMatches []*models.Ticket
	for _, t := range candidates {
		d := geo.HaversineMeters(lat, lng, t.Location.Latitude, t.Location.Longitude)
		if d <= duplicateRadiusMeters {
			locationMatches = append(locationMatches, t)
		}
	}

	if len(locationMatches) == 0 {
		return nil, nil
	}

	// Step 3: text similarity check on title + description.
	// Only tickets that pass the similarity threshold are considered duplicates.
	incomingText := title + " " + description
	var closest *models.Ticket
	var bestScore float64

	for _, t := range locationMatches {
		existingText := t.Title + " " + t.Description
		score := jaccardSimilarity(incomingText, existingText)
		if score >= textSimilarityThreshold && score > bestScore {
			closest = t
			bestScore = score
		}
	}

	return closest, nil
}

// jaccardSimilarity computes the Jaccard index between the word sets of
// two strings: |intersection| / |union|. Returns a value in [0.0, 1.0].
func jaccardSimilarity(a, b string) float64 {
	setA := wordSet(a)
	setB := wordSet(b)

	if len(setA) == 0 && len(setB) == 0 {
		return 1.0
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// wordSet converts a string to a set of lowercase words, stripping
// punctuation and ignoring common stop words.
func wordSet(s string) map[string]bool {
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "in": true,
		"on": true, "at": true, "of": true, "to": true, "and": true,
		"or": true, "with": true, "this": true, "that": true, "it": true,
		"are": true, "was": true, "has": true, "have": true, "for": true,
		"near": true, "there": true, "been": true, "by": true, "from": true,
	}

	set := make(map[string]bool)
	words := strings.Fields(strings.ToLower(s))
	for _, w := range words {
		// Strip leading/trailing punctuation
		w = strings.Trim(w, ".,!?;:\"'()-")
		if len(w) >= 3 && !stopWords[w] {
			set[w] = true
		}
	}
	return set
}
