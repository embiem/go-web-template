/*
Package igdb is a minimal client for the IGDB v4 API (Twitch client-credentials
auth, Apicalypse query bodies, 4 req/s rate limit per the official docs).

Credentials come from the environment: IGDB_CLIENT_ID and IGDB_CLIENT_SECRET.
All lookups are unit-testable against httptest servers; the live API has been
verified only against the official docs (see tmp/research/igdb.md).
*/
package igdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL   = "https://api.igdb.com/v4"
	defaultAuthURL   = "https://id.twitch.tv/oauth2/token"
	defaultUserAgent = "indie-game-gems/0.1 (curator CLI)"

	// Official IGDB limit: 4 requests/second.
	requestInterval = 250 * time.Millisecond
	Timeout         = 15 * time.Second
)

// ErrNoCredentials is returned when IGDB_CLIENT_ID/IGDB_CLIENT_SECRET are unset.
var ErrNoCredentials = errors.New("IGDB credentials missing: set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET (Twitch developer console, client type Confidential)")

// Credentials holds the Twitch app credentials.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// FromEnv reads credentials from the environment.
func FromEnv(getenv func(string) string) (Credentials, error) {
	c := Credentials{ClientID: getenv("IGDB_CLIENT_ID"), ClientSecret: getenv("IGDB_CLIENT_SECRET")}
	if c.ClientID == "" || c.ClientSecret == "" {
		return Credentials{}, ErrNoCredentials
	}
	return c, nil
}

// token is a cached client-credentials access token.
type token struct {
	AccessToken string
	Expiry      time.Time
}

func (t token) valid() bool {
	return t.AccessToken != "" && time.Now().Before(t.Expiry.Add(-time.Minute))
}

// Client is an IGDB API client with token caching and rate limiting.
type Client struct {
	creds Credentials
	http  *http.Client
	auth  string // token endpoint override for tests
	base  string // API base override for tests
	lim   <-chan time.Time

	mu        sync.Mutex
	tok       token
	UserAgent string
}

// NewClient returns a client for the real API. Test callers override the
// auth/base URLs via fields.
func NewClient(creds Credentials) *Client {
	return &Client{
		creds:     creds,
		http:      &http.Client{Timeout: Timeout},
		auth:      defaultAuthURL,
		lim:       time.Tick(requestInterval),
		UserAgent: defaultUserAgent,
	}
}

// fetchToken requests a fresh client-credentials token from Twitch.
func (c *Client) fetchToken(ctx context.Context) (token, error) {
	u := c.auth + "?" + url.Values{
		"client_id":     {c.creds.ClientID},
		"client_secret": {c.creds.ClientSecret},
		"grant_type":    {"client_credentials"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return token{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return token{}, fmt.Errorf("twitch token request: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Message     string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return token{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return token{}, fmt.Errorf("twitch token request: HTTP %d: %s", resp.StatusCode, body.Message)
	}
	return token{
		AccessToken: body.AccessToken,
		Expiry:      time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

// accessToken returns the cached token or fetches a new one (thread-safe).
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tok.valid() {
		return c.tok.AccessToken, nil
	}
	t, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	c.tok = t
	return t.AccessToken, nil
}

// Query runs one Apicalypse query against an endpoint (e.g. "games") and
// decodes the JSON array response into out. The rate limiter paces to 4 req/s.
func (c *Client) Query(ctx context.Context, endpoint, apicalypse string, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}

	select {
	case <-c.lim:
	case <-ctx.Done():
		return ctx.Err()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL()+"/"+endpoint, strings.NewReader(apicalypse))
	if err != nil {
		return err
	}
	req.Header.Set("Client-ID", c.creds.ClientID)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("igdb %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body := make([]byte, 300)
		n, _ := resp.Body.Read(body)
		return fmt.Errorf("igdb %s: HTTP %d: %s", endpoint, resp.StatusCode, body[:n])
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) baseURL() string {
	if c.base != "" {
		return c.base
	}
	return defaultBaseURL
}
