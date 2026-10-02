/*
Package media downloads remote images into the local cache (MEDIA_DIR) under
deterministic keys ("games/<slug>/header.jpg", "developers/<slug>/logo.jpg"),
validating content type and size, writing atomically, and skipping files that
already exist unless forced. DB rows for cached media live in the media table;
the caller upserts them with the returned key.
*/
package mediacache

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxImageSize caps downloads (largest expected: 1920x1080 screenshots).
const MaxImageSize = 20 << 20 // 20 MiB

// Client downloads media files. No rate limiter: image CDNs tolerate the
// sequential fetches the CLI performs.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Dir is the media cache root (MEDIA_DIR).
	Dir string
}

// NewClient builds a client for dir (e.g. the MEDIA_DIR env var).
func NewClient(dir string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: "indie-game-gems/0.1 (curator CLI)",
		Dir:       dir,
	}
}

// ErrNotFound reports a 404 — callers skip optional assets (e.g. Steam
// library_600x900 covers on older apps) without failing the whole game.
var ErrNotFound = errors.New("media not found")

// extForContent maps image content types to file extensions.
func extForContent(ct string) (string, bool) {
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		switch mt {
		case "image/jpeg":
			return ".jpg", true
		case "image/png":
			return ".png", true
		case "image/webp":
			return ".webp", true
		case "image/gif":
			return ".gif", true
		}
	}
	return "", false
}

// Download fetches url into key (extension fixed up to the actual
// content type) and returns the stored key. Existing files are kept unless
// force is set. The URL is recorded by the caller as source_url.
func (c *Client) Download(url, key string, force bool) (string, error) {
	if c.Dir == "" {
		return "", fmt.Errorf("media dir is empty (set MEDIA_DIR)")
	}
	base := strings.TrimSuffix(key, filepath.Ext(key))

	dst := filepath.Join(c.Dir, key)
	if !force {
		if _, err := os.Stat(dst); err == nil {
			return key, nil // cached
		}
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("media fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("media fetch %s: HTTP %d", url, resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	ext, ok := extForContent(ct)
	if !ok {
		// Refuse HTML/error pages masquerading as images.
		return "", fmt.Errorf("media fetch %s: not an image (Content-Type %q)", url, ct)
	}
	key = base + ext
	dst = filepath.Join(c.Dir, key)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}

	// Atomic write: temp file in the same directory, bounded read, rename.
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".media-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	if n, err := io.Copy(tmp, io.LimitReader(resp.Body, MaxImageSize+1)); err != nil {
		return "", err
	} else if n > MaxImageSize {
		return "", fmt.Errorf("media fetch %s: exceeds %d bytes", url, MaxImageSize)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", err
	}
	return key, nil
}

// Game keys — deterministic layout under MEDIA_DIR.
func GameKey(slug, kind string) string {
	switch kind {
	case "screenshot":
		return filepath.Join("games", slug, kind) // caller appends -NN
	default:
		return filepath.Join("games", slug, kind)
	}
}

// ScreenshotKey returns "games/<slug>/screenshot-NN".
func ScreenshotKey(slug string, n int) string {
	return filepath.Join("games", slug, fmt.Sprintf("screenshot-%02d", n))
}

// DeveloperKey returns "developers/<slug>/logo".
func DeveloperKey(slug string) string {
	return filepath.Join("developers", slug, "logo")
}

// TrailerKey returns "games/<slug>/trailer" (poster cached from ytimg).
func TrailerKey(slug string) string {
	return filepath.Join("games", slug, "trailer")
}
