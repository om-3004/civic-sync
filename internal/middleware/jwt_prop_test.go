// Feature: civic-sync, Property 1: JWT Verification Accepts Valid Tokens and Rejects Invalid Ones

// **Validates: Requirements 1.4, 1.5**
package middleware

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/civic-sync/civic-sync/internal/auth"
	"pgregory.net/rapid"
)

const testClientID = "test-client-id-12345"

// base64url encodes data as unpadded base64url.
func base64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// mustMarshal marshals v to JSON and panics on error.
func mustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// buildJWT constructs a signed RS256 JWT from its constituent parts.
// The kid in the header references the key ID in the cache; privKey is the
// signing key (which may differ from the cached key to exercise invalid-sig cases).
func buildJWT(kid, iss, aud string, exp int64, privKey *rsa.PrivateKey) string {
	header := base64url(mustMarshal(map[string]string{
		"alg": "RS256",
		"kid": kid,
	}))
	payload := base64url(mustMarshal(map[string]interface{}{
		"sub":   "user1",
		"email": "user1@example.com",
		"name":  "Test User",
		"iss":   iss,
		"aud":   aud,
		"exp":   exp,
	}))

	sigInput := header + "." + payload
	digest := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// TestJWTVerifyProperty verifies Property 1:
// JWTVerify accepts a token if and only if all four validity conditions hold:
// valid signature, non-expired exp, correct issuer, correct audience.
//
// For each combination of (validSig, validExp, validIss, validAud) the test
// asserts HTTP 200 iff all four are true, and HTTP 401 otherwise.
func TestJWTVerifyProperty(t *testing.T) {
	// Pin the client ID env var for the duration of the test.
	os.Setenv("GOOGLE_CLIENT_ID", testClientID)
	defer os.Unsetenv("GOOGLE_CLIENT_ID")

	// rapid v1.3.0 runs 100 iterations by default (satisfies the ≥100 requirement).
	// Use RAPID_CHECKS env var to increase if needed.
	rapid.Check(t, func(t *rapid.T) {
		// --- Generate RSA key pairs in-process ---
		// correctKey: the key whose public part is stored in the cache.
		correctKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("failed to generate correct RSA key: %v", err)
		}

		// wrongKey: a distinct key used when validSig=false.
		wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("failed to generate wrong RSA key: %v", err)
		}

		// --- Draw the four boolean flags ---
		validSig := rapid.Bool().Draw(t, "validSig")
		validExp := rapid.Bool().Draw(t, "validExp")
		validIss := rapid.Bool().Draw(t, "validIss")
		validAud := rapid.Bool().Draw(t, "validAud")

		// --- Build a KeyCache containing only the correct public key ---
		const kid = "key-1"
		keyMap := map[string]*rsa.PublicKey{
			kid: &correctKey.PublicKey,
		}
		keyCache := auth.NewKeyCacheFromKeys(keyMap)

		// --- Select signing key ---
		signingKey := correctKey
		if !validSig {
			signingKey = wrongKey
		}

		// --- Select exp claim ---
		var exp int64
		if validExp {
			exp = time.Now().Unix() + 3600 // 1 hour in the future
		} else {
			exp = time.Now().Unix() - 1 // 1 second in the past
		}

		// --- Select iss claim ---
		iss := "accounts.google.com"
		if !validIss {
			iss = "evil.example.com"
		}

		// --- Select aud claim ---
		aud := testClientID
		if !validAud {
			aud = "wrong-audience-99999"
		}

		// --- Build the JWT ---
		token := buildJWT(kid, iss, aud, exp, signingKey)

		// --- Set up the middleware + dummy next handler ---
		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		handler := JWTVerify(keyCache)(next)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// --- Assert ---
		allValid := validSig && validExp && validIss && validAud
		if allValid {
			if rec.Code != http.StatusOK {
				t.Fatalf(
					"expected 200 for all-valid token (sig=%v exp=%v iss=%v aud=%v), got %d — body: %s",
					validSig, validExp, validIss, validAud, rec.Code, rec.Body.String(),
				)
			}
			if !nextCalled {
				t.Fatalf("expected next handler to be called for all-valid token")
			}
		} else {
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf(
					"expected 401 for invalid token (sig=%v exp=%v iss=%v aud=%v), got %d — body: %s",
					validSig, validExp, validIss, validAud, rec.Code, rec.Body.String(),
				)
			}
			if nextCalled {
				t.Fatalf("next handler must NOT be called for invalid token (sig=%v exp=%v iss=%v aud=%v)",
					validSig, validExp, validIss, validAud)
			}
		}
	})
}
