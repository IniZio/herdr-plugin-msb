package credmount

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func baseCredentials(endpoint string) Credentials {
	return Credentials{
		AccessToken:   "old-access",
		RefreshToken:  "old-refresh",
		ExpiresAt:     "2026-01-01T00:00:00Z",
		TokenType:     "Bearer",
		ClientID:      "client-id",
		ClientSecret:  "",
		TokenEndpoint: endpoint,
	}
}

func TestRefreshSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client()}
	c := baseCredentials(srv.URL)
	updated, err := r.Refresh(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AccessToken != "new-access" {
		t.Errorf("access_token: got %q", updated.AccessToken)
	}
	if updated.RefreshToken != "new-refresh" {
		t.Errorf("refresh_token: got %q", updated.RefreshToken)
	}
	if updated.ExpiresAt == c.ExpiresAt {
		t.Error("expires_at should be updated")
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

	r := Refresher{HTTPClient: srv.Client()}
	c := baseCredentials(srv.URL)
	updated, err := r.Refresh(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if updated.RefreshToken != "old-refresh" {
		t.Errorf("expected old refresh token carried forward, got %q", updated.RefreshToken)
	}
}

func TestRefresh403With1010(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`error code: 1010 — blocked by Cloudflare`))
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client()}
	_, err := r.Refresh(context.Background(), baseCredentials(srv.URL))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "1010") {
		t.Errorf("error must mention 1010, got: %s", msg)
	}
	if !strings.Contains(msg, "403") {
		t.Errorf("error must include HTTP status, got: %s", msg)
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "token is dead") || strings.Contains(lower, "invalid token") || strings.Contains(lower, "expired token") {
		t.Errorf("error must not claim token is dead, got: %s", msg)
	}
}

func TestRefreshNon2xxSurfacesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`internal server error`))
	}))
	defer srv.Close()

	r := Refresher{HTTPClient: srv.Client()}
	_, err := r.Refresh(context.Background(), baseCredentials(srv.URL))
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
	if err := Save(path, baseCredentials(srv.URL)); err != nil {
		t.Fatal(err)
	}

	r := Refresher{HTTPClient: srv.Client()}
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

	r := Refresher{HTTPClient: srv.Client(), UserAgent: "test-agent/9.9"}
	r.Refresh(context.Background(), baseCredentials(srv.URL))
	if gotUA != "test-agent/9.9" {
		t.Errorf("expected custom User-Agent, got %q", gotUA)
	}
}
