package view

import "os"

// MediaURL resolves a media key (e.g. "games/hades/header.jpg") to the public
// URL all views should use for <img>/<video> sources. The DB stores keys
// relative to the media cache root (MEDIA_DIR), never remote URLs.
//
// The public prefix comes from MEDIA_BASE_URL (default "/media"), so moving
// the media library to a CDN later is a config change only.
func MediaURL(key string) string {
	base := os.Getenv("MEDIA_BASE_URL")
	if base == "" {
		base = "/media"
	}
	return base + "/" + key
}
