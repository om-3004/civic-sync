//go:build !prod

package auth

import "crypto/rsa"

// NewKeyCacheFromKeys constructs a *KeyCache pre-populated with the supplied
// key map. This is a test-only constructor (excluded from production builds via
// the !prod build tag) that lets tests inject arbitrary in-process keys without
// hitting Google's JWKS endpoint.
func NewKeyCacheFromKeys(keys map[string]*rsa.PublicKey) *KeyCache {
	return &KeyCache{keys: keys}
}
