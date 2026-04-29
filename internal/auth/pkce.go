package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"
)

// GenerateState returns a cryptographically random base64url-encoded string
// used to prevent CSRF in the OAuth flow.
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// S256Challenge derives the PKCE code_challenge from a code_verifier
// using the S256 method: BASE64URL(SHA256(ASCII(code_verifier))).
func S256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// GenerateOpaqueToken returns a 32-byte cryptographically random
// base64url-encoded string suitable for use as a refresh token.
func GenerateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hex digest of a token string.
// This is stored in the DB so the raw token is never persisted.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// --- PKCE challenge store ---

type pkceEntry struct {
	challenge string
	expiresAt time.Time
}

var (
	challengeStore   = map[string]pkceEntry{}
	challengeStoreMu sync.Mutex
)

// StoreChallenge saves the code_challenge keyed by state with a 10-minute TTL.
// It also prunes any expired entries opportunistically on each call.
func StoreChallenge(state, challenge string) {
	challengeStoreMu.Lock()
	defer challengeStoreMu.Unlock()

	now := time.Now()
	// Prune expired entries
	for k, v := range challengeStore {
		if now.After(v.expiresAt) {
			delete(challengeStore, k)
		}
	}

	challengeStore[state] = pkceEntry{
		challenge: challenge,
		expiresAt: now.Add(10 * time.Minute),
	}
}

// ConsumeChallenge retrieves and deletes the stored challenge for the given state.
// Returns ("", false) if state is empty, not found, or the entry has expired.
func ConsumeChallenge(state string) (string, bool) {
	if state == "" {
		return "", false
	}

	challengeStoreMu.Lock()
	defer challengeStoreMu.Unlock()

	entry, found := challengeStore[state]
	if !found {
		return "", false
	}
	delete(challengeStore, state)

	if time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.challenge, true
}

// VerifyPKCE returns true if S256Challenge(verifier) equals the stored challenge.
// The comparison is done with subtle.ConstantTimeCompare to prevent timing attacks.
func VerifyPKCE(verifier, challenge string) bool {
	derived := S256Challenge(verifier)
	return subtle.ConstantTimeCompare([]byte(derived), []byte(challenge)) == 1
}
