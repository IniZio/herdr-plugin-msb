package credmount

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Refresher struct {
	HTTPClient *http.Client
	UserAgent  string
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
		"client_id":     {c.ClientID},
	}
	if c.ClientSecret != "" {
		vals.Set("client_secret", c.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenEndpoint,
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

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		if strings.Contains(preview, "1010") {
			return Credentials{}, fmt.Errorf("HTTP %d: Cloudflare error 1010 — request blocked due to User-Agent; token is NOT necessarily dead: %s",
				resp.StatusCode, preview)
		}
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			return Credentials{}, fmt.Errorf("HTTP %d (Retry-After: %s): %s", resp.StatusCode, ra, preview)
		}
		return Credentials{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, preview)
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return Credentials{}, fmt.Errorf("parse token response: %w", err)
	}

	updated := c
	updated.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		updated.RefreshToken = tok.RefreshToken
	}
	if tok.TokenType != "" {
		updated.TokenType = tok.TokenType
	}
	expiry := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	updated.ExpiresAt = expiry.UTC().Format(time.RFC3339Nano)
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
		return Credentials{}, err
	}
	return updated, nil
}
