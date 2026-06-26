package models

import "time"

// User mirrors the Firestore /users/{uid} document schema.
type User struct {
	UID             string     `firestore:"uid"`
	Email           string     `firestore:"email"`
	Name            string     `firestore:"name"`
	Role            string     `firestore:"role"`            // "citizen" | "official"
	CreatedAt       time.Time  `firestore:"created_at"`
	PINFailures     int        `firestore:"pin_failures"`
	PINLockoutUntil *time.Time `firestore:"pin_lockout_until"`
}
