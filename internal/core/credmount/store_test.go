package credmount

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const nestedFixture = `{
  "claudeAiOauth": {
    "accessToken": "at-nested",
    "refreshToken": "rt-nested",
    "expiresAt": 1725734400000,
    "refreshTokenExpiresAt": 1728326400000,
    "scopes": ["read", "write"],
    "subscriptionType": "pro",
    "rateLimitTier": "tier1"
  }
}`

const flatFixture = `{
  "access_token": "at-flat",
  "refresh_token": "rt-flat",
  "expires_at": "2026-09-07T19:52:36.635049000Z",
  "token_type": "Bearer"
}`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadNested(t *testing.T) {
	c, err := Load(writeFixture(t, nestedFixture))
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken != "at-nested" {
		t.Fatalf("AccessToken: got %q", c.AccessToken)
	}
	if c.RefreshToken != "rt-nested" {
		t.Fatalf("RefreshToken: got %q", c.RefreshToken)
	}
	if c.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt is zero")
	}
	if len(c.Scopes) != 2 {
		t.Fatalf("Scopes: got %v", c.Scopes)
	}
	if c.SubscriptionType != "pro" {
		t.Fatalf("SubscriptionType: got %q", c.SubscriptionType)
	}
	if c.RateLimitTier != "tier1" {
		t.Fatalf("RateLimitTier: got %q", c.RateLimitTier)
	}
	if c.Format != FormatNested {
		t.Fatalf("Format: got %v", c.Format)
	}
}

func TestLoadFlat(t *testing.T) {
	c, err := Load(writeFixture(t, flatFixture))
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken != "at-flat" {
		t.Fatalf("AccessToken: got %q", c.AccessToken)
	}
	if c.Format != FormatFlat {
		t.Fatalf("Format: got %v", c.Format)
	}
}

func TestRegressionNestedNotEmpty(t *testing.T) {
	c, err := Load(writeFixture(t, nestedFixture))
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken == "" {
		t.Fatal("AccessToken must not be empty for nested shape (regression: old flat struct returned empty with nil error)")
	}
}

func TestLoadUnrecognisedShape(t *testing.T) {
	_, err := Load(writeFixture(t, `{"something":1}`))
	if err == nil {
		t.Fatal("expected error for unrecognised shape")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	_, err := Load(writeFixture(t, "not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadNestedEmptyAccessToken(t *testing.T) {
	fixture := `{"claudeAiOauth":{"accessToken":"","refreshToken":"r","expiresAt":1725734400000}}`
	_, err := Load(writeFixture(t, fixture))
	if err == nil {
		t.Fatal("expected error for empty accessToken")
	}
}

func TestRoundTripPreservesExtraKey(t *testing.T) {
	fixture := `{
  "claudeAiOauth": {
    "accessToken": "orig",
    "refreshToken": "r",
    "expiresAt": 1725734400000,
    "unknownField": "keepme"
  }
}`
	path := writeFixture(t, fixture)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.AccessToken = "mutated"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(data, &outer); err != nil {
		t.Fatal(err)
	}
	inner, ok := outer["claudeAiOauth"].(map[string]interface{})
	if !ok {
		t.Fatal("claudeAiOauth missing or wrong type")
	}
	if inner["accessToken"] != "mutated" {
		t.Fatalf("accessToken: got %v", inner["accessToken"])
	}
	if inner["unknownField"] != "keepme" {
		t.Fatalf("unknownField not preserved: got %v", inner["unknownField"])
	}
}

func TestSaveNewInode(t *testing.T) {
	path := writeFixture(t, nestedFixture)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	c.AccessToken = "changed"
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(fi1, fi2) {
		t.Fatal("Save must produce a new inode")
	}
}

func TestSaveInPlacePreservesInode(t *testing.T) {
	path := writeFixture(t, nestedFixture)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	c.AccessToken = "changed"
	if err := SaveInPlace(path, c); err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(fi1, fi2) {
		t.Fatal("SaveInPlace must preserve inode")
	}
}

func TestExpiredAndExpiresIn(t *testing.T) {
	now := time.Now()

	past := Credentials{ExpiresAt: now.Add(-time.Hour)}
	future := Credentials{ExpiresAt: now.Add(time.Hour)}
	zero := Credentials{}

	if !past.Expired(now) {
		t.Error("past token should be expired")
	}
	if future.Expired(now) {
		t.Error("future token should not be expired")
	}
	if !zero.Expired(now) {
		t.Error("zero ExpiresAt should be expired")
	}
	if past.ExpiresIn(now) != 0 {
		t.Error("expired ExpiresIn should be 0")
	}
	if future.ExpiresIn(now) <= 0 {
		t.Error("future ExpiresIn should be positive")
	}
	if zero.ExpiresIn(now) != 0 {
		t.Error("zero ExpiresAt ExpiresIn should be 0")
	}
}
