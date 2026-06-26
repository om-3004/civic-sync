package geo

import "math"

const earthRadiusMeters = 6_371_000.0

// HaversineMeters returns the great-circle distance in metres between two
// geographic coordinates given as decimal degrees.
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	φ1, φ2 := lat1*math.Pi/180, lat2*math.Pi/180
	Δφ := (lat2 - lat1) * math.Pi / 180
	Δλ := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
	return earthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// BoundingBoxDelta returns the latitude and longitude deltas (in degrees) for
// a bounding box of the given radius around a point at the given latitude.
// Used to build a cheap rectangular pre-filter before running the exact
// Haversine check.
func BoundingBoxDelta(lat, radiusMeters float64) (deltaLatDeg, deltaLngDeg float64) {
	deltaLatDeg = radiusMeters / 111_320.0
	deltaLngDeg = radiusMeters / (111_320.0 * math.Cos(lat*math.Pi/180))
	return
}
