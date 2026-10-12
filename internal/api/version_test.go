package api

import (
	"net/http"
	"testing"
)

func TestGetVersion(t *testing.T) {
	oldVersion, oldCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = oldVersion, oldCommit })
	Version, Commit = "1.2.3", "0123456789abcdef0123456789abcdef01234567"

	s, _ := newServer(t, Config{})
	rec := do(t, s, http.MethodGet, "/api/v1/version", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[BuildInfo](t, rec)
	if got.Version != "1.2.3" || got.Commit != Commit || got.Dirty {
		t.Fatalf("unexpected build info: %+v", got)
	}
}

func TestGetVersionRequiresAuth(t *testing.T) {
	s, _ := newServer(t, Config{})
	rec := requestWithBearer(t, s, "/api/v1/version", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without a token, got %d", rec.Code)
	}
}
