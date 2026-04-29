package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
