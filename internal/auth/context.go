package auth

import "context"

// ctxKey is an unexported type for context keys owned by the auth package,
// preventing collisions with keys from other packages.
type ctxKey int

const (
	ctxKeyUID ctxKey = iota
	ctxKeyEmail
	ctxKeyName
)

// WithClaims returns a new context carrying the authenticated user's uid,
// email, and name. Called by the JWTVerify middleware after successful
// token validation.
func WithClaims(ctx context.Context, uid, email, name string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUID, uid)
	ctx = context.WithValue(ctx, ctxKeyEmail, email)
	ctx = context.WithValue(ctx, ctxKeyName, name)
	return ctx
}

// UIDFromContext retrieves the authenticated user's UID (sub claim) from ctx.
// Returns an empty string if not present.
func UIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUID).(string)
	return v
}

// EmailFromContext retrieves the authenticated user's email from ctx.
func EmailFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyEmail).(string)
	return v
}

// NameFromContext retrieves the authenticated user's name from ctx.
func NameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyName).(string)
	return v
}
