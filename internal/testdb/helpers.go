package testdb

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"
)

// withDatabase returns rawURL pointing at database name.
func withDatabase(t testing.TB, rawURL, name string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		t.Fatalf("testdb: %s must be a postgres:// URL: %v", EnvVar, err)
	}
	u.Path = "/" + name
	return u.String()
}

func randomSuffix(t testing.TB) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("testdb: random name: %v", err)
	}
	return hex.EncodeToString(b)
}
