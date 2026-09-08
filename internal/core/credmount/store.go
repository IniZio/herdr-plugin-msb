package credmount

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Format int

const (
	FormatNested Format = iota
	FormatFlat
)

type Credentials struct {
	AccessToken           string
	RefreshToken          string
	ExpiresAt             time.Time
	RefreshTokenExpiresAt time.Time
	Scopes                []string
	SubscriptionType      string
	RateLimitTier         string
	TokenType             string
	Format                Format
	rawOuter              map[string]json.RawMessage
	rawInner              map[string]json.RawMessage
}

func DefaultStorePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "nexus3", "claude-dedicated", ".credentials.json")
}

func DefaultStoreDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "nexus3", "claude-dedicated")
}

type nestedDoc struct {
	AccessToken           string   `json:"accessToken"`
	RefreshToken          string   `json:"refreshToken"`
	ExpiresAt             *float64 `json:"expiresAt"`
	RefreshTokenExpiresAt *float64 `json:"refreshTokenExpiresAt"`
	Scopes                []string `json:"scopes"`
	SubscriptionType      string   `json:"subscriptionType"`
	RateLimitTier         string   `json:"rateLimitTier"`
}

type flatDoc struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
	TokenType    string `json:"token_type"`
}

func msToTime(ms float64) time.Time {
	return time.UnixMilli(int64(ms)).UTC()
}

func Load(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, err
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil {
		return Credentials{}, fmt.Errorf("%s: invalid JSON: %w", path, err)
	}
	if raw, ok := outer["claudeAiOauth"]; ok {
		return loadNested(path, raw, outer)
	}
	if _, ok := outer["access_token"]; ok {
		return loadFlat(path, data, outer)
	}
	return Credentials{}, fmt.Errorf("%s: unrecognised credential store shape", path)
}

func loadNested(path string, raw json.RawMessage, outer map[string]json.RawMessage) (Credentials, error) {
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil {
		return Credentials{}, fmt.Errorf("%s: claudeAiOauth: %w", path, err)
	}
	var doc nestedDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Credentials{}, fmt.Errorf("%s: claudeAiOauth: %w", path, err)
	}
	if doc.AccessToken == "" {
		return Credentials{}, fmt.Errorf("%s: accessToken is empty", path)
	}
	if doc.ExpiresAt == nil || *doc.ExpiresAt == 0 {
		return Credentials{}, fmt.Errorf("%s: expiresAt is missing or zero", path)
	}
	c := Credentials{
		AccessToken:      doc.AccessToken,
		RefreshToken:     doc.RefreshToken,
		ExpiresAt:        msToTime(*doc.ExpiresAt),
		Scopes:           doc.Scopes,
		SubscriptionType: doc.SubscriptionType,
		RateLimitTier:    doc.RateLimitTier,
		Format:           FormatNested,
		rawOuter:         outer,
		rawInner:         inner,
	}
	if doc.RefreshTokenExpiresAt != nil && *doc.RefreshTokenExpiresAt != 0 {
		c.RefreshTokenExpiresAt = msToTime(*doc.RefreshTokenExpiresAt)
	}
	return c, nil
}

func loadFlat(path string, data []byte, outer map[string]json.RawMessage) (Credentials, error) {
	var doc flatDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return Credentials{}, fmt.Errorf("%s: %w", path, err)
	}
	if doc.AccessToken == "" {
		return Credentials{}, fmt.Errorf("%s: access_token is empty", path)
	}
	t, err := time.Parse(time.RFC3339Nano, doc.ExpiresAt)
	if err != nil {
		return Credentials{}, fmt.Errorf("%s: expires_at: %w", path, err)
	}
	return Credentials{
		AccessToken:  doc.AccessToken,
		RefreshToken: doc.RefreshToken,
		ExpiresAt:    t,
		TokenType:    doc.TokenType,
		Format:       FormatFlat,
		rawOuter:     outer,
	}, nil
}

func marshalCredentials(c Credentials) ([]byte, error) {
	if c.Format == FormatFlat {
		return json.MarshalIndent(flatDoc{
			AccessToken:  c.AccessToken,
			RefreshToken: c.RefreshToken,
			ExpiresAt:    c.ExpiresAt.Format(time.RFC3339Nano),
			TokenType:    c.TokenType,
		}, "", "  ")
	}
	inner := make(map[string]json.RawMessage, len(c.rawInner)+8)
	for k, v := range c.rawInner {
		inner[k] = v
	}
	set := func(k string, v interface{}) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		inner[k] = b
		return nil
	}
	if err := set("accessToken", c.AccessToken); err != nil {
		return nil, err
	}
	if err := set("refreshToken", c.RefreshToken); err != nil {
		return nil, err
	}
	if err := set("expiresAt", c.ExpiresAt.UnixMilli()); err != nil {
		return nil, err
	}
	if !c.RefreshTokenExpiresAt.IsZero() {
		if err := set("refreshTokenExpiresAt", c.RefreshTokenExpiresAt.UnixMilli()); err != nil {
			return nil, err
		}
	}
	if len(c.Scopes) > 0 {
		if err := set("scopes", c.Scopes); err != nil {
			return nil, err
		}
	}
	if c.SubscriptionType != "" {
		if err := set("subscriptionType", c.SubscriptionType); err != nil {
			return nil, err
		}
	}
	if c.RateLimitTier != "" {
		if err := set("rateLimitTier", c.RateLimitTier); err != nil {
			return nil, err
		}
	}
	innerBytes, err := json.Marshal(inner)
	if err != nil {
		return nil, err
	}
	outer := make(map[string]json.RawMessage, len(c.rawOuter)+1)
	for k, v := range c.rawOuter {
		outer[k] = v
	}
	outer["claudeAiOauth"] = innerBytes
	return json.MarshalIndent(outer, "", "  ")
}

func Save(path string, c Credentials) error {
	data, err := marshalCredentials(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".creds-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := os.Chmod(tmpName, 0600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}

func SaveInPlace(path string, c Credentials) error {
	data, err := marshalCredentials(c)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func (c Credentials) Expired(now time.Time) bool {
	if c.ExpiresAt.IsZero() {
		return true
	}
	return !now.Before(c.ExpiresAt)
}

func (c Credentials) ExpiresIn(now time.Time) time.Duration {
	if c.ExpiresAt.IsZero() {
		return 0
	}
	d := c.ExpiresAt.Sub(now)
	if d < 0 {
		return 0
	}
	return d
}
