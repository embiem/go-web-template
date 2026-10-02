/*
Package steam is a minimal client for Steam's unauthenticated store APIs:
appdetails (game metadata), appreviews (aggregate review stats), the store
search listing (games by release date) and the keyless Web API store
services IStoreBrowseService/GetItems (batched metadata + review summary)
and IStoreService/GetTagList (tag names).

Rate limits: the store API has no official limit; community consensus is
~200 requests / 5 min per IP. The client therefore paces itself to at most
one request per second (Web API calls included) and retries 429/5xx
responses with backoff.
*/
package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// BaseURL is the Steam store API root. Overridable in tests.
	defaultBaseURL = "https://store.steampowered.com"

	// defaultAPIBaseURL is the Steam Web API root (GetItems, GetTagList).
	defaultAPIBaseURL = "https://api.steampowered.com"

	// UserAgent identifies us; Steam is friendlier to identified clients.
	defaultUserAgent = "indie-game-gems/0.1 (curator CLI; +https://github.com/embiem/indie-game-gems)"

	// requestInterval paces the client to <= 1 req/s (see package doc).
	requestInterval = time.Second

	// Timeout bounds a single HTTP request.
	Timeout = 15 * time.Second

	// maxRetries is the number of retries after the first attempt on
	// 429/5xx (total attempts = maxRetries + 1).
	maxRetries = 3
)

// Client talks to the Steam store API with pacing and retries.
type Client struct {
	http       *http.Client
	baseURL    string
	apiBaseURL string
	limiter    <-chan time.Time
	UserAgent  string

	// backoff is the base delay between retries (2s, 4s, 8s by default);
	// tests shrink it.
	backoff time.Duration
}

// NewClient returns a paced client. baseURL empty means the real store and
// Web API hosts; otherwise both are served from baseURL (tests).
func NewClient(baseURL string) *Client {
	apiBaseURL := baseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
		apiBaseURL = defaultAPIBaseURL
	}
	return &Client{
		http:       &http.Client{Timeout: Timeout},
		baseURL:    baseURL,
		apiBaseURL: apiBaseURL,
		limiter:    time.Tick(requestInterval),
		UserAgent:  defaultUserAgent,
		backoff:    2 * time.Second,
	}
}

// do performs a paced GET and retries 429/5xx with exponential backoff.
func (c *Client) do(ctx context.Context, rawURL string, out any) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		select {
		case <-c.limiter: // pace every request, including retries
		case <-ctx.Done():
			return ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("steam request %s: %w", rawURL, err)
		} else if shouldRetry(resp.StatusCode) {
			lastErr = fmt.Errorf("steam %s: HTTP %d (attempt %d/%d)", rawURL, resp.StatusCode, attempt+1, maxRetries+1)
			resp.Body.Close()
		} else {
			return decode(resp, out)
		}

		if attempt < maxRetries {
			// Exponential backoff: 2s, 4s, 8s (plus the pace tick).
			select {
			case <-time.After(time.Duration(1<<uint(attempt)) * c.backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}

func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func decode(resp *http.Response, out any) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

// AppDetails fetches the store metadata for one appid.
func (c *Client) AppDetails(ctx context.Context, appid int64) (*AppDetails, error) {
	u := fmt.Sprintf("%s/api/appdetails?appids=%d&cc=us&l=en", c.baseURL, appid)
	var resp map[string]struct {
		Success bool        `json:"success"`
		Data    *AppDetails `json:"data"`
	}
	if err := c.do(ctx, u, &resp); err != nil {
		return nil, err
	}
	entry, ok := resp[url.PathEscape(fmt.Sprint(appid))]
	if !ok || !entry.Success || entry.Data == nil {
		return nil, fmt.Errorf("steam appdetails: no data for appid %d", appid)
	}
	return entry.Data, nil
}

// Reviews fetches the aggregate review summary for one appid.
// num_per_page=0 returns only query_summary, no review bodies.
func (c *Client) Reviews(ctx context.Context, appid int64) (*ReviewSummary, error) {
	u := fmt.Sprintf("%s/appreviews/%d?json=1&language=all&purchase_type=all&num_per_page=0", c.baseURL, appid)
	var resp struct {
		QuerySummary *ReviewSummary `json:"query_summary"`
	}
	if err := c.do(ctx, u, &resp); err != nil {
		return nil, err
	}
	if resp.QuerySummary == nil {
		return nil, fmt.Errorf("steam appreviews: no query_summary for appid %d", appid)
	}
	return resp.QuerySummary, nil
}
