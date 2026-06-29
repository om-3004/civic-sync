package models

import "time"

// Location holds the GPS coordinates of a reported civic issue.
type Location struct {
	Latitude  float64 `firestore:"latitude"  json:"latitude"`
	Longitude float64 `firestore:"longitude" json:"longitude"`
}

// Ticket mirrors the Firestore /tickets/{ticketId} document schema.
type Ticket struct {
	ID               string     `firestore:"id"                 json:"id"`
	Category         string     `firestore:"category"           json:"category"`
	Title            string     `firestore:"title"              json:"title"`
	Description      string     `firestore:"description"        json:"description"`
	ImageURL         string     `firestore:"image_url"          json:"image_url"`
	Location         Location   `firestore:"location"           json:"location"`
	Status           string     `firestore:"status"             json:"status"`
	Upvotes          int        `firestore:"upvotes"            json:"upvotes"`
	UpvotedBy        []string   `firestore:"upvoted_by"         json:"-"`
	ReportedBy       string     `firestore:"reported_by"        json:"reported_by"`
	ReportedByName   string     `firestore:"reported_by_name"   json:"reported_by_name"`
	ReportedByEmail  string     `firestore:"reported_by_email"  json:"reported_by_email"`
	CreatedAt        time.Time  `firestore:"created_at"         json:"created_at"`
	UpdatedAt        time.Time  `firestore:"updated_at"         json:"updated_at"`
	ResolvedAt       *time.Time `firestore:"resolved_at"        json:"resolved_at,omitempty"`
}
