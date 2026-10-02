package steam

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// indieTagID is Steam's user tag id for "Indie" (search `tags=` parameter).
const indieTagID = 492

// searchPageSize is the row count requested per search page. Steam may
// return fewer, so walks advance by the rows actually received.
const searchPageSize = 100

// SearchResult is one game row of the store search listing.
type SearchResult struct {
	AppID int64
	Name  string
	// ReleaseText is the listing's raw date: "Oct 1, 2026", "October 2026",
	// "Coming soon", ...
	ReleaseText string
	// ReleaseDate is set only when ReleaseText has day precision.
	ReleaseDate time.Time
	// ComingSoon is true for rows from the coming-soon listing.
	ComingSoon bool
}

// ReleaseQuery selects games by Steam release date.
type ReleaseQuery struct {
	From, To  time.Time // inclusive day range, UTC midnight
	Today     time.Time // UTC day separating released from upcoming games
	IndieOnly bool      // restrict to the "Indie" user tag
}

type searchListing int

const (
	listingReleased   searchListing = iota // released games, newest first
	listingComingSoon                      // unreleased games, soonest first
)

/*
ReleasesBetween lists games whose Steam release date falls within
[q.From, q.To]. Released games come from the newest-first listing, upcoming
games from the coming-soon listing; each walk stops at the first row past the
range, so the request count scales with the range, not the catalogue.

Only games with English support are listed (`supportedlang=english`). Rows
with an imprecise date ("October 2026", "Coming soon") cannot be placed in a
day range and are skipped. Results are deduplicated by appid because pages
shift while new games are published.
*/
func (c *Client) ReleasesBetween(ctx context.Context, q ReleaseQuery) ([]SearchResult, error) {
	if q.To.Before(q.From) {
		return nil, fmt.Errorf("release range: to %s is before from %s", q.To.Format(time.DateOnly), q.From.Format(time.DateOnly))
	}
	seen := map[int64]bool{}
	var out []SearchResult
	if !q.From.After(q.Today) {
		if err := c.walkListing(ctx, listingReleased, q, seen, &out); err != nil {
			return nil, err
		}
	}
	if !q.To.Before(q.Today) {
		if err := c.walkListing(ctx, listingComingSoon, q, seen, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *Client) walkListing(ctx context.Context, listing searchListing, q ReleaseQuery, seen map[int64]bool, out *[]SearchResult) error {
	for start := 0; ; {
		rows, total, err := c.searchPage(ctx, listing, q.IndieOnly, start)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, r := range rows {
			if r.ReleaseDate.IsZero() {
				continue
			}
			// Released rows descend by date, coming-soon rows ascend: rows
			// before the range are skipped, the first row past it ends the walk.
			if listing == listingReleased {
				if r.ReleaseDate.After(q.To) {
					continue
				}
				if r.ReleaseDate.Before(q.From) {
					return nil
				}
			} else {
				if r.ReleaseDate.Before(q.From) {
					continue
				}
				if r.ReleaseDate.After(q.To) {
					return nil
				}
			}
			if seen[r.AppID] {
				continue
			}
			seen[r.AppID] = true
			*out = append(*out, r)
		}
		start += len(rows)
		if start >= total {
			return nil
		}
	}
}

// searchPage fetches one page of the store search (`infinite=1` returns the
// rows as an HTML fragment plus the total count).
func (c *Client) searchPage(ctx context.Context, listing searchListing, indieOnly bool, start int) ([]SearchResult, int, error) {
	v := url.Values{}
	v.Set("query", "")
	v.Set("start", strconv.Itoa(start))
	v.Set("count", strconv.Itoa(searchPageSize))
	v.Set("category1", "998") // games only: no DLC, soundtracks, software
	v.Set("supportedlang", "english")
	v.Set("infinite", "1")
	v.Set("cc", "us")
	v.Set("l", "english") // fixes the date format to "Oct 1, 2026"
	if listing == listingReleased {
		v.Set("sort_by", "Released_DESC")
	} else {
		v.Set("filter", "comingsoon")
		v.Set("sort_by", "Released_ASC")
	}
	if indieOnly {
		v.Set("tags", strconv.Itoa(indieTagID))
	}

	var resp struct {
		ResultsHTML string `json:"results_html"`
		TotalCount  int    `json:"total_count"`
	}
	if err := c.do(ctx, c.baseURL+"/search/results/?"+v.Encode(), &resp); err != nil {
		return nil, 0, err
	}
	rows, err := parseSearchResults(resp.ResultsHTML, listing == listingComingSoon)
	if err != nil {
		return nil, 0, err
	}
	return rows, resp.TotalCount, nil
}

// parseSearchResults extracts the game rows from a results_html fragment.
// Bundle rows (comma-separated appids) are skipped.
func parseSearchResults(fragment string, comingSoon bool) ([]SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return nil, fmt.Errorf("steam search: parse results: %w", err)
	}
	var rows []SearchResult
	doc.Find("a.search_result_row").Each(func(_ int, s *goquery.Selection) {
		appid, err := strconv.ParseInt(s.AttrOr("data-ds-appid", ""), 10, 64)
		if err != nil {
			return
		}
		text := strings.TrimSpace(s.Find(".search_released").Text())
		rows = append(rows, SearchResult{
			AppID:       appid,
			Name:        strings.TrimSpace(s.Find(".title").Text()),
			ReleaseText: text,
			ReleaseDate: parseDayDate(text),
			ComingSoon:  comingSoon,
		})
	})
	return rows, nil
}

// parseDayDate parses a listing date with day precision; month, quarter and
// year dates yield the zero time.
func parseDayDate(s string) time.Time {
	for _, layout := range []string{"Jan 2, 2006", "2 Jan, 2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
