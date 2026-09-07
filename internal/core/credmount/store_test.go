package credmount

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	c := Credentials{
		AccessToken:   "tok1",
		RefreshToken:  "rtok1",
		ExpiresAt:     "2026-09-07T19:52:36.635049000Z",
		TokenType:     "Bearer",
		ClientID:      "client-id-1",
		ClientSecret:  "secret1",
		TokenEndpoint: "https://example.com/token",
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != c {
		t.Fatalf("Save/Load: got %+v want %+v", got, c)
	}
}

func TestSaveInPlaceRoundTrip(t *testing.T) {
	c := Credentials{
		AccessToken:   "tok2",
		RefreshToken:  "rtok2",
		ExpiresAt:     "2026-09-07T19:52:36.635049000Z",
		TokenType:     "Bearer",
		ClientID:      "client-id-2",
		ClientSecret:  "sec2",
		TokenEndpoint: "https://example.com/token",
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	if err := SaveInPlace(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != c {
		t.Fatalf("SaveInPlace/Load: got %+v want %+v", got, c)
	}
}

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		t.Fatal(err)
	}
	return st.Ino
}

func TestSaveChangesInode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	c := Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: "2026-01-01T00:00:00Z"}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	before := inodeOf(t, path)
	c.AccessToken = "b"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	after := inodeOf(t, path)
	if before == after {
		t.Fatalf("Save must change inode: before=%d after=%d (same)", before, after)
	}
}

func TestSaveInPlacePreservesInode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	c := Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: "2026-01-01T00:00:00Z"}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	before := inodeOf(t, path)
	c.AccessToken = "b"
	if err := SaveInPlace(path, c); err != nil {
		t.Fatal(err)
	}
	after := inodeOf(t, path)
	if before != after {
		t.Fatalf("SaveInPlace must preserve inode: before=%d after=%d (changed)", before, after)
	}
}

func TestExpiredAndExpiresIn(t *testing.T) {
	now := time.Now()
	cPast := Credentials{ExpiresAt: "2020-01-01T00:00:00Z"}
	cFuture := Credentials{ExpiresAt: "2099-01-01T00:00:00Z"}
	cBad := Credentials{ExpiresAt: "not-a-time"}

	if !cPast.Expired(now) {
		t.Error("past token should be expired")
	}
	if cFuture.Expired(now) {
		t.Error("future token should not be expired")
	}
	if !cBad.Expired(now) {
		t.Error("unparseable expires_at should be treated as expired")
	}
	if cPast.ExpiresIn(now) != 0 {
		t.Error("expired ExpiresIn should be 0")
	}
	if cFuture.ExpiresIn(now) <= 0 {
		t.Error("future ExpiresIn should be positive")
	}
	if cBad.ExpiresIn(now) != 0 {
		t.Error("unparseable ExpiresIn should be 0")
	}
}

func TestLoadNonexistent(t *testing.T) {
	_, err := Load("/nonexistent/path/creds.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
