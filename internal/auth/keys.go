package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const (
	googleCertsURL    = "https://www.googleapis.com/oauth2/v3/certs"
	keyRefreshInterval = time.Hour
)

// jwk represents a single JSON Web Key as returned by Google's JWKS endpoint.
type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Use string `json:"use"`
	N   string `json:"n"` // base64url-encoded modulus
	E   string `json:"e"` // base64url-encoded exponent
}

// jwks is the top-level structure returned by the JWKS endpoint.
type jwks struct {
	Keys []jwk `json:"keys"`
}

// KeyCache holds Google's RSA public keys, keyed by key ID (kid).
// Keys are fetched at startup and refreshed every hour in the background.
type KeyCache struct {
	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
}

// NewKeyCache creates a KeyCache, performs an initial key fetch, and starts
// a background refresh goroutine. If the initial fetch fails the error is
// returned so main() can log it and exit (Req 11.5).
func NewKeyCache(ctx context.Context) (*KeyCache, error) {
	kc := &KeyCache{
		keys: make(map[string]*rsa.PublicKey),
	}

	if err := kc.fetchKeys(); err != nil {
		return nil, fmt.Errorf("auth: initial Google public-key fetch failed: %w", err)
	}

	go kc.startRefresh(ctx)

	return kc, nil
}

// GetKey returns the RSA public key for the given key ID, and whether it was found.
// Safe for concurrent use.
func (kc *KeyCache) GetKey(kid string) (*rsa.PublicKey, bool) {
	kc.mu.RLock()
	defer kc.mu.RUnlock()
	key, ok := kc.keys[kid]
	return key, ok
}

// startRefresh runs in a background goroutine, refreshing keys every hour.
// It exits when ctx is cancelled (graceful shutdown).
func (kc *KeyCache) startRefresh(ctx context.Context) {
	ticker := time.NewTicker(keyRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := kc.fetchKeys(); err != nil {
				// Log but do not crash; the previous keys remain valid.
				// In production this would go through a structured logger.
				_ = err // retained in cache until next successful fetch
			}
		}
	}
}

// fetchKeys downloads Google's JWKS, parses it, and atomically replaces the
// in-memory key map. Called once at startup and then on every refresh tick.
func (kc *KeyCache) fetchKeys() error {
	resp, err := http.Get(googleCertsURL) //nolint:noctx // background refresh; no per-request context needed
	if err != nil {
		return fmt.Errorf("GET %s: %w", googleCertsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned status %d", googleCertsURL, resp.StatusCode)
	}

	var set jwks
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("decoding JWKS response: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Use != "sig" {
			continue
		}
		pub, err := jwkToRSA(k)
		if err != nil {
			return fmt.Errorf("parsing key kid=%q: %w", k.Kid, err)
		}
		newKeys[k.Kid] = pub
	}

	kc.mu.Lock()
	kc.keys = newKeys
	kc.mu.Unlock()

	return nil
}

// jwkToRSA converts a JWK (RSA) to an *rsa.PublicKey.
// The "n" field is a base64url-encoded big-endian unsigned integer (the modulus).
// The "e" field is a base64url-encoded big-endian unsigned integer (the public exponent).
func jwkToRSA(k jwk) (*rsa.PublicKey, error) {
	// Decode modulus n.
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decoding n: %w", err)
	}
	if len(nBytes) == 0 {
		return nil, fmt.Errorf("empty modulus n")
	}
	n := new(big.Int).SetBytes(nBytes)

	// Decode exponent e.
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decoding e: %w", err)
	}
	if len(eBytes) == 0 || len(eBytes) > 8 {
		return nil, fmt.Errorf("exponent e has unexpected length %d", len(eBytes))
	}
	// Convert big-endian bytes to int.
	var eVal uint64
	for _, b := range eBytes {
		eVal = (eVal << 8) | uint64(b)
	}
	if eVal == 0 {
		return nil, fmt.Errorf("exponent e is zero")
	}

	return &rsa.PublicKey{N: n, E: int(eVal)}, nil
}
