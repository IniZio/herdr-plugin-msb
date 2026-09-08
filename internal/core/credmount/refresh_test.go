package credmount

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func baseCredentials() Credentials {
	return Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
		TokenType:    "Bearer",
		Format:       FormatNested,
	}
}

func longTokenBody() map[string]interface{} {
	return map[string]interface{}{
		"access_token":        strings.Repeat("a", 400),
		"refresh_token":       strings.Repeat("r", 400),
		"expires_in":          3600,
		"token_type":          "Bearer",
		"scope":               "read write",
		"subscription_type":   "pro",
		"rate_limit_tier":     "standard",
		"extra_field_ignored": "some-value-that-makes-body-exceed-512-bytes",
		"another_extra":       "padding-padding-padding-padding-padding-padding",
	}
}

func TestRefreshHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(longTokenBody())
	}))
	defer srv.Close()

	before := time.Now()
	c := baseCredentials()
	c.Format = FormatFlat
	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL}
	updated, err := r.Refresh(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.AccessToken) < 400 || !strings.HasPrefix(updated.AccessToken, "a") {
		t.Errorf("access_token wrong length or prefix: len=%d", len(updated.AccessToken))
	}
	if len(updated.RefreshToken) < 400 || !strings.HasPrefix(updated.RefreshToken, "r") {
		t.Errorf("refresh_token wrong: len=%d", len(updated.RefreshToken))
	}
	if !updated.ExpiresAt.After(before) {
		t.Errorf("ExpiresAt not in the future: %v", updated.ExpiresAt)
	}
	if updated.Format != FormatFlat {
		t.Errorf("Format not preserved: got %v", updated.Format)
	}
}

func TestRefreshRequestFields(t *testing.T) {
	var gotForm map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotForm = map[string][]string{}
		for k, v := range r.Form {
			gotForm[k] = v
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "x",
			"expires_in":   60,
		})
	}))
	defer srv.Close()

	c := baseCredentials()
	c.RefreshToken = "my-refresh-token"
	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL, ClientID: "test-client-id"}
	r.Refresh(context.Background(), c)

	if gotForm["grant_type"][0] != "refresh_token" {
		t.Errorf("grant_type: %v", gotForm["grant_type"])
	}
	if gotForm["refresh_token"][0] != "my-refresh-token" {
		t.Errorf("refresh_token: %v", gotForm["refresh_token"])
	}
	if gotForm["client_id"][0] != "test-client-id" {
		t.Errorf("client_id: %v", gotForm["client_id"])
	}
	if _, ok := gotForm["client_secret"]; ok {
		t.Error("client_secret must not be sent")
	}
}

func TestRefresh512CapRegression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(longTokenBody())
		if len(b) <= 512 {
			t.Errorf("test body must exceed 512 bytes, got %d", len(b))
		}
		w.Write(b)
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL}
	updated, err := r.Refresh(context.Background(), baseCredentials())
	if err != nil {
		t.Fatalf("body >512 bytes must parse cleanly: %v", err)
	}
	if len(updated.AccessToken) < 400 {
		t.Errorf("access_token truncated: len=%d", len(updated.AccessToken))
	}
}

func TestRefreshMalformed2xx(t *testing.T) {
	rawBody := `not-json-at-all ` + strings.Repeat("X", 600)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(rawBody))
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL}
	_, err := r.Refresh(context.Background(), baseCredentials())
	if err == nil {
		t.Fatal("expected error on malformed body")
	}
	if !strings.Contains(err.Error(), rawBody) {
		t.Errorf("error must contain full raw body for recovery; got: %s", err.Error())
	}
}

func TestRefreshRawDirCapture(t *testing.T) {
	rawBody := `{"access_token":"tok","expires_in":60}` + strings.Repeat(" ", 600)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(rawBody))
	}))
	defer srv.Close()

	dir := t.TempDir()
	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL, RawDir: dir}
	_, err := r.Refresh(context.Background(), baseCredentials())
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 raw file, got %d", len(entries))
	}
	name := filepath.Join(dir, entries[0].Name())
	info, _ := os.Stat(name)
	if info.Mode().Perm() != 0600 {
		t.Errorf("mode: got %v, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(name)
	if string(data) != rawBody {
		t.Error("raw file contents do not match sent body")
	}
}

func TestRefreshRawDirMalformed(t *testing.T) {
	rawBody := `{bad json ` + strings.Repeat("Z", 600)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(rawBody))
	}))
	defer srv.Close()

	dir := t.TempDir()
	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL, RawDir: dir}
	_, err := r.Refresh(context.Background(), baseCredentials())
	if err == nil {
		t.Fatal("expected parse error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected raw file even on parse failure, got %d entries", len(entries))
	}
	if !strings.Contains(err.Error(), rawBody) {
		t.Errorf("error must contain full raw body; got: %s", err.Error())
	}
}

func TestRefresh429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate_limit_error"}`))
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL}
	_, err := r.Refresh(context.Background(), baseCredentials())
	if err == nil {
		t.Fatal("expected error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "rate limit") && !strings.Contains(lower, "429") {
		t.Errorf("error must identify rate limiting; got: %s", err.Error())
	}
}

func TestRefresh403With1010(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`error code: 1010 blocked by Cloudflare`))
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL}
	_, err := r.Refresh(context.Background(), baseCredentials())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "1010") {
		t.Errorf("error must mention 1010; got: %s", msg)
	}
	if !strings.Contains(msg, "403") {
		t.Errorf("error must include HTTP status; got: %s", msg)
	}
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "not necessarily dead") {
		t.Errorf("error must say token is NOT necessarily dead; got: %s", msg)
	}
}

func TestRefreshCarriesOldRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "new-access",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL}
	updated, err := r.Refresh(context.Background(), baseCredentials())
	if err != nil {
		t.Fatal(err)
	}
	if updated.RefreshToken != "old-refresh" {
		t.Errorf("expected old refresh token carried forward, got %q", updated.RefreshToken)
	}
}

func TestRefreshNon2xxSurfacesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`internal server error`))
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL}
	_, err := r.Refresh(context.Background(), baseCredentials())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "500") {
		t.Errorf("error must include status 500, got: %s", msg)
	}
	if !strings.Contains(msg, "internal server error") {
		t.Errorf("error must include body, got: %s", msg)
	}
}

func TestRefreshFileSendsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "x",
			"expires_in":   60,
		})
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL, UserAgent: "test-agent/9.9"}
	r.Refresh(context.Background(), baseCredentials())
	if gotUA != "test-agent/9.9" {
		t.Errorf("expected custom User-Agent, got %q", gotUA)
	}
}

func TestRefreshFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "refreshed-access",
			"refresh_token": "refreshed-refresh",
			"expires_in":    7200,
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := Save(path, baseCredentials()); err != nil {
		t.Fatal(err)
	}

	r := Refresher{HTTPClient: srv.Client(), TokenEndpoint: srv.URL}
	updated, err := r.RefreshFile(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AccessToken != "refreshed-access" {
		t.Errorf("got %q", updated.AccessToken)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != "refreshed-access" {
		t.Errorf("persisted %q", loaded.AccessToken)
	}
}
