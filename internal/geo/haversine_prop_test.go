// Feature: civic-sync, Property 6: Haversine Distance is Symmetric and Satisfies the 50-Meter Threshold Correctly

// **Validates: Requirements 4.1**
package geo

import (
	"math"
	"testing"

	"pgregory.net/rapid"
)

// TestHaversineSymmetry verifies that H(a,b) == H(b,a) within 1 mm tolerance
// for any valid coordinate pair.
func TestHaversineSymmetry(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lat1 := rapid.Float64Range(-90, 90).Draw(t, "lat1")
		lng1 := rapid.Float64Range(-180, 180).Draw(t, "lng1")
		lat2 := rapid.Float64Range(-90, 90).Draw(t, "lat2")
		lng2 := rapid.Float64Range(-180, 180).Draw(t, "lng2")

		dAB := HaversineMeters(lat1, lng1, lat2, lng2)
		dBA := HaversineMeters(lat2, lng2, lat1, lng1)

		if math.Abs(dAB-dBA) > 0.001 {
			t.Fatalf("symmetry violated: H(%v,%v -> %v,%v)=%.6f, H(%v,%v -> %v,%v)=%.6f, diff=%.9f",
				lat1, lng1, lat2, lng2, dAB,
				lat2, lng2, lat1, lng1, dBA,
				math.Abs(dAB-dBA))
		}
	})
}

// TestHaversineWithin50m verifies that a point offset by a known small amount
// (well under 50 m) is measured as <= 50 m from the origin point.
func TestHaversineWithin50m(t *testing.T) {
	// 1 degree of latitude ≈ 111 320 m, so 40 m ≈ 0.000359° latitude.
	// We fix a small offset of 0.0003° lat (≈ 33 m) and 0° lng so the
	// actual Haversine distance is deterministically < 50 m regardless of
	// the base longitude (longitude offset only affects east-west distance
	// which would compress toward the poles, making it even smaller).
	const smallOffsetDeg = 0.0003 // ≈ 33 m in latitude

	rapid.Check(t, func(t *rapid.T) {
		lat := rapid.Float64Range(-89, 89).Draw(t, "lat")
		lng := rapid.Float64Range(-180, 180).Draw(t, "lng")

		// B is slightly north of A — guaranteed < 50 m away.
		lat2 := lat + smallOffsetDeg
		d := HaversineMeters(lat, lng, lat2, lng)

		if d > 50.0 {
			t.Fatalf("expected distance <= 50 m for small offset, got %.4f m (lat=%.6f)", d, lat)
		}
	})
}

// TestHaversineBeyond50m verifies that a point offset by a large amount
// (clearly > 50 m) is measured as > 50 m.
func TestHaversineBeyond50m(t *testing.T) {
	// 0.01° latitude ≈ 1 113 m — always beyond 50 m.
	const largeOffsetDeg = 0.01

	rapid.Check(t, func(t *rapid.T) {
		lat := rapid.Float64Range(-89, 89).Draw(t, "lat")
		lng := rapid.Float64Range(-180, 180).Draw(t, "lng")

		lat2 := lat + largeOffsetDeg
		d := HaversineMeters(lat, lng, lat2, lng)

		if d <= 50.0 {
			t.Fatalf("expected distance > 50 m for large offset, got %.4f m (lat=%.6f)", d, lat)
		}
	})
}

// TestHaversineNonNegative verifies that the distance is always >= 0.
func TestHaversineNonNegative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lat1 := rapid.Float64Range(-90, 90).Draw(t, "lat1")
		lng1 := rapid.Float64Range(-180, 180).Draw(t, "lng1")
		lat2 := rapid.Float64Range(-90, 90).Draw(t, "lat2")
		lng2 := rapid.Float64Range(-180, 180).Draw(t, "lng2")

		d := HaversineMeters(lat1, lng1, lat2, lng2)
		if d < 0 {
			t.Fatalf("distance must be non-negative, got %.6f for (%v,%v)->(%v,%v)",
				d, lat1, lng1, lat2, lng2)
		}
	})
}
