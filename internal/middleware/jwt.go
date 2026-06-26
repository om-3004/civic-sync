package middleware

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/civic-sync/civic-sync/internal/auth"
)

// jwtHeader holds the fields we need from a JWT's JOSE header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// jwtClaims holds the standard + Google-specific claims we validate.
type jwtClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Iss   string `json:"iss"`
	Aud   string `json:"aud"`
	Exp   int64  `json:"exp"`
}

const (
	expectedIssuer = "accounts.google.com"
)

// JWTVerify returns an HTTP middleware that validates a Google ID token supplied
// as a Bearer token in the Authorization header.
//
// On success the sub, email, and name claims are stored in the request context
// and the next handler is called. On any failure an HTTP 401 is returned.
//
// The keyCache parameter must be the application-level *auth.KeyCache that was
// initialised at startup (Req 1.4).
func JWTVerify(keyCache *auth.KeyCache) func(http.Handler) http.Handler {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				http.Error(w, `{"error":"missing or malformed Authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				http.Error(w, `{"error":"malformed JWT"}`, http.StatusUnauthorized)
				return
			}

			// --- 1. Decode and parse the JOSE header ---
			headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
			if err != nil {
				http.Error(w, `{"error":"invalid JWT header encoding"}`, http.StatusUnauthorized)
				return
			}
			var header jwtHeader
			if err := json.Unmarshal(headerJSON, &header); err != nil {
				http.Error(w, `{"error":"invalid JWT header"}`, http.StatusUnauthorized)
				return
			}
			if header.Alg != "RS256" {
				http.Error(w, `{"error":"unsupported JWT algorithm"}`, http.StatusUnauthorized)
				return
			}
			if header.Kid == "" {
				http.Error(w, `{"error":"missing kid in JWT header"}`, http.StatusUnauthorized)
				return
			}

			// --- 2. Look up the public key by kid ---
			pubKey, found := keyCache.GetKey(header.Kid)
			if !found {
				http.Error(w, `{"error":"unknown JWT key ID"}`, http.StatusUnauthorized)
				return
			}

			// --- 3. Verify the RS256 signature ---
			// The signed input is the raw bytes of "header.payload" (base64url).
			signingInput := parts[0] + "." + parts[1]
			sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				http.Error(w, `{"error":"invalid JWT signature encoding"}`, http.StatusUnauthorized)
				return
			}
			digest := sha256.Sum256([]byte(signingInput))
			if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, digest[:], sigBytes); err != nil {
				http.Error(w, `{"error":"invalid JWT signature"}`, http.StatusUnauthorized)
				return
			}

			// --- 4. Decode and validate the payload claims ---
			payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				http.Error(w, `{"error":"invalid JWT payload encoding"}`, http.StatusUnauthorized)
				return
			}
			var claims jwtClaims
			if err := json.Unmarshal(payloadJSON, &claims); err != nil {
				http.Error(w, `{"error":"invalid JWT payload"}`, http.StatusUnauthorized)
				return
			}

			// exp: must not be in the past
			if time.Now().Unix() > claims.Exp {
				http.Error(w, `{"error":"JWT has expired"}`, http.StatusUnauthorized)
				return
			}

			// iss: must be Google's issuer
			if claims.Iss != expectedIssuer {
				http.Error(w, `{"error":"invalid JWT issuer"}`, http.StatusUnauthorized)
				return
			}

			// aud: must match our client ID
			if clientID != "" && claims.Aud != clientID {
				http.Error(w, `{"error":"invalid JWT audience"}`, http.StatusUnauthorized)
				return
			}

			// sub: must be non-empty
			if claims.Sub == "" {
				http.Error(w, `{"error":"missing sub claim"}`, http.StatusUnauthorized)
				return
			}

			// --- 5. Store claims in request context and continue ---
			ctx := r.Context()
			ctx = auth.WithClaims(ctx, claims.Sub, claims.Email, claims.Name)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// bearerToken extracts the raw JWT string from "Authorization: Bearer <token>".
// Returns the token and true on success, empty string and false otherwise.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(h, prefix)
	if token == "" {
		return "", false
	}
	return token, true
}
