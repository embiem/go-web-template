package mediacache

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 1x1 PNG and JPEG magic bytes (headers only suffice — we don't decode).
var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 100))
	jpegBytes = []byte("\xff\xd8\xff\xe0" + strings.Repeat("y", 100))
)

func testServer(t *testing.T, status int, ct string, body []byte, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadImage(t *testing.T) {
	var calls int
	srv := testServer(t, http.StatusOK, "image/jpeg", jpegBytes, &calls)
	dir := t.TempDir()
	c := NewClient(dir)

	key, err := c.Download(srv.URL, "games/hades/header.jpg", false)
	if err != nil {
		t.Fatal(err)
	}
	if key != "games/hades/header.jpg" {
		t.Errorf("key = %q", key)
	}
	if b, err := os.ReadFile(filepath.Join(dir, key)); err != nil || len(b) != len(jpegBytes) {
		t.Errorf("stored file: %d bytes, %v", len(b), err)
	}

	// Second call without force must not re-download (skipped as cached).
	if _, err := c.Download(srv.URL, "games/hades/header.jpg", false); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (skip existing unless force)", calls)
	}
	if _, err := c.Download(srv.URL, "games/hades/header.jpg", true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls with force = %d, want 2", calls)
	}
}

func TestDownloadExtensionFollowsContentType(t *testing.T) {
	var calls int
	srv := testServer(t, http.StatusOK, "image/png", pngBytes, &calls)
	c := NewClient(t.TempDir())

	// IGDB logos are jpeg-pathed but PNG content must land as .png.
	key, err := c.Download(srv.URL, "developers/supergiant-games/logo.jpg", false)
	if err != nil {
		t.Fatal(err)
	}
	if key != "developers/supergiant-games/logo.png" {
		t.Errorf("key = %q", key)
	}
}

func TestDownloadRejectsHTML(t *testing.T) {
	var calls int
	srv := testServer(t, http.StatusOK, "text/html; charset=utf-8", []byte("<html>404</html>"), &calls)
	c := NewClient(t.TempDir())
	if _, err := c.Download(srv.URL, "games/x/header.jpg", false); err == nil || !strings.Contains(err.Error(), "not an image") {
		t.Fatalf("expected not-an-image error, got %v", err)
	}
}

func TestDownload404IsErrNotFound(t *testing.T) {
	var calls int
	srv := testServer(t, http.StatusNotFound, "text/html", []byte("nope"), &calls)
	c := NewClient(t.TempDir())
	if _, err := c.Download(srv.URL, "games/x/cover.jpg", false); err != ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestDownloadSizeCap(t *testing.T) {
	big := make([]byte, MaxImageSize+10)
	var calls int
	srv := testServer(t, http.StatusOK, "image/jpeg", big, &calls)
	c := NewClient(t.TempDir())
	if _, err := c.Download(srv.URL, "games/x/header.jpg", false); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size cap error, got %v", err)
	}
	// No partial file may remain.
	entries, _ := os.ReadDir(c.Dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".media-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestKeys(t *testing.T) {
	if got := GameKey("hades", "header"); got != "games/hades/header" {
		t.Errorf("GameKey = %q", got)
	}
	if got := ScreenshotKey("hades", 3); got != "games/hades/screenshot-03" {
		t.Errorf("ScreenshotKey = %q", got)
	}
	if got := DeveloperKey("team-cherry"); got != "developers/team-cherry/logo" {
		t.Errorf("DeveloperKey = %q", got)
	}
	if got := TrailerKey("hades"); got != "games/hades/trailer" {
		t.Errorf("TrailerKey = %q", got)
	}
}
