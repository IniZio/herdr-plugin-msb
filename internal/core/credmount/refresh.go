package credmount

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultClientID      = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	DefaultTokenEndpoint = "https://platform.claude.com/v1/oauth/token"
)

type Refresher struct {
	HTTPClient    *http.Client
	UserAgent     string
	ClientID      string
	TokenEndpoint string
	RawDir        string
}

func (r Refresher) client() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (r Refresher) userAgent() string {
	if r.UserAgent != "" {
		return r.UserAgent
	}
	return "herdr-plugin-msb/1.0"
}

func (r Refresher) clientID() string {
	if r.ClientID != "" {
		return r.ClientID
	}
	return DefaultClientID
}

func (r Refresher) tokenEndpoint() string {
	if r.TokenEndpoint != "" {
		return r.TokenEndpoint
	}
	return DefaultTokenEndpoint
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func (r Refresher) Refresh(ctx context.Context, c Credentials) (Credentials, error) {
	vals := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.RefreshToken},
		"client_id":     {r.clientID()},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.tokenEndpoint(),
		strings.NewReader(vals.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", r.userAgent())

	resp, err := r.client().Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyStr := string(body)
		preview := bodyStr
		if len(preview) > 200 {
			preview = preview[:200]
		}
		if strings.Contains(bodyStr, "1010") {
			return Credentials{}, fmt.Errorf("HTTP %d: Cloudflare error 1010 — request blocked due to User-Agent; token is NOT necessarily dead: %s",
				resp.StatusCode, preview)
		}
		if resp.StatusCode == 429 {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				return Credentials{}, fmt.Errorf("HTTP 429 rate limit exceeded (Retry-After: %s): %s", ra, preview)
			}
			return Credentials{}, fmt.Errorf("HTTP 429 rate limit exceeded: %s", preview)
		}
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			return Credentials{}, fmt.Errorf("HTTP %d (Retry-After: %s): %s", resp.StatusCode, ra, preview)
		}
		return Credentials{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, preview)
	}

	if r.RawDir != "" {
		stamp := time.Now().UTC().Format(time.RFC3339Nano)
		name := filepath.Join(r.RawDir, "token-response-"+stamp+".json")
		f, werr := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if werr != nil {
			return Credentials{}, fmt.Errorf("save raw token response: %w; rotated body: %s", werr, body)
		}
		_, werr = f.Write(body)
		if werr == nil {
			werr = f.Sync()
		}
		f.Close()
		if werr != nil {
			return Credentials{}, fmt.Errorf("save raw token response: %w; rotated body: %s", werr, body)
		}
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return Credentials{}, fmt.Errorf("parse token response: %w; raw body: %s", err, body)
	}

	updated := c
	updated.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		updated.RefreshToken = tok.RefreshToken
	}
	if tok.TokenType != "" {
		updated.TokenType = tok.TokenType
	}
	updated.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return updated, nil
}

func (r Refresher) RefreshFile(ctx context.Context, path string, inPlace bool) (Credentials, error) {
	c, err := Load(path)
	if err != nil {
		return Credentials{}, err
	}
	updated, err := r.Refresh(ctx, c)
	if err != nil {
		return Credentials{}, err
	}
	if inPlace {
		err = SaveInPlace(path, updated)
	} else {
		err = Save(path, updated)
	}
	if err != nil {
		rawHint := ""
		if r.RawDir != "" {
			rawHint = fmt.Sprintf(" (rotated tokens captured in %s)", r.RawDir)
		}
		return Credentials{}, fmt.Errorf("persist refreshed credentials%s: %w; access_token=%s refresh_token=%s",
			rawHint, err, updated.AccessToken, updated.RefreshToken)
	}
	return updated, nil
}
