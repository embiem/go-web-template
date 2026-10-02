package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/igdb"
	"github.com/embiem/indie-game-gems/mediacache"
	"github.com/embiem/indie-game-gems/steam"
)

// mediaJob is one planned download (key base + source URL + media kind).
type mediaJob struct {
	key      string // key base without extension; mediacache.Client fixes the ext
	url      string
	kind     string
	position int16
}

const maxScreenshots = 8

// MediaJobs plans the downloads for one game. Steam assets win; IGDB images
// only fill gaps for games without a Steam appid. Pure, hence testable.
func MediaJobs(game data.ListGamesForSyncRow, details *steam.AppDetails, g *igdb.Game) []mediaJob {
	var jobs []mediaJob
	add := func(key, url, kind string, pos int16) {
		if url != "" {
			jobs = append(jobs, mediaJob{key: key, url: url, kind: kind, position: pos})
		}
	}

	if details != nil {
		base := "games/" + game.Slug
		add(base+"/header", details.HeaderImage, "header", 0)
		add(base+"/capsule", firstNonEmptyStr(details.CapsuleImage, details.CapsuleImageV5), "capsule", 0)
		add(fmt.Sprintf("games/%s/hero", game.Slug), steamLibraryURL(game.SteamAppid.Int64, "library_hero"), "hero", 0)
		add(fmt.Sprintf("games/%s/cover", game.Slug), steamLibraryURL(game.SteamAppid.Int64, "library_600x900"), "cover", 0)
		for i, shot := range details.Screenshots {
			if i >= maxScreenshots {
				break
			}
			add(mediacache.ScreenshotKey(game.Slug, i+1), shot.PathFull, "screenshot", int16(i+1))
		}
	}

	if details == nil && g != nil {
		// No Steam assets: IGDB cover + screenshots fill the card media.
		if g.Cover != nil {
			add(fmt.Sprintf("games/%s/cover", game.Slug), igdb.ImageURL(g.Cover.ImageID, igdb.SizeCoverBig), "cover", 0)
		}
		for i, shot := range g.Screenshots {
			if i >= maxScreenshots {
				break
			}
			add(mediacache.ScreenshotKey(game.Slug, i+1), igdb.ImageURL(shot.ImageID, igdb.SizeScreenshotBig), "screenshot", int16(i+1))
		}
	}

	// Trailer poster from YouTube (both paths when the id is known).
	if game.TrailerYoutubeID.Valid && game.TrailerYoutubeID.String != "" {
		add(mediacache.TrailerKey(game.Slug), ytPosterURL(game.TrailerYoutubeID.String), "trailer", 0)
	}
	return jobs
}

// steamLibraryBase is the verified convention path for library assets
// (research: shared.akamai.steamstatic.com; older/unknown assets may 404 —
// the fetcher skips those).
const steamLibraryBase = "https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/"

func steamLibraryURL(appid int64, asset string) string {
	return fmt.Sprintf("%s%d/%s.jpg", steamLibraryBase, appid, asset)
}

func ytPosterURL(youtubeID string) string {
	return "https://i.ytimg.com/vi/" + youtubeID + "/hqdefault.jpg"
}

// IGDBClientFromEnv builds an IGDB client from IGDB_CLIENT_ID /
// IGDB_CLIENT_SECRET, failing with a clear message when missing.
func IGDBClientFromEnv() (*igdb.Client, error) {
	creds, err := igdb.FromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	return igdb.NewClient(creds), nil
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// MediaSync downloads a game's media into MEDIA_DIR and upserts media rows
// by key. Skips existing files unless force. Optional assets (Steam library
// hero/cover, trailer poster) may 404 — that's not an error.
func MediaSync(ctx context.Context, q *data.Queries, mc *mediacache.Client, slug string, force bool) (int, error) {
	game, err := q.GetGameBySlug(ctx, slug)
	if err != nil {
		return 0, fmt.Errorf("game %s: %w", slug, err)
	}
	syncRow := data.ListGamesForSyncRow{
		ID: game.ID, Slug: game.Slug, Title: game.Title,
		SteamAppid: game.SteamAppid, IgdbID: game.IgdbID,
		TrailerYoutubeID: game.TrailerYoutubeID,
	}

	var details *steam.AppDetails
	if game.SteamAppid.Valid {
		sc := steam.NewClient("")
		if details, err = sc.AppDetails(ctx, game.SteamAppid.Int64); err != nil {
			return 0, fmt.Errorf("steam appdetails %d: %w", game.SteamAppid.Int64, err)
		}
	}
	var g *igdb.Game
	if game.IgdbID.Valid && details == nil {
		ic, err := IGDBClientFromEnv()
		if err != nil {
			return 0, err
		}
		if g, err = ic.Game(ctx, game.IgdbID.Int64); err != nil {
			return 0, err
		}
	}

	n := 0
	var firstErr error
	for _, job := range MediaJobs(syncRow, details, g) {
		key, err := mc.Download(job.url, job.key, force)
		switch {
		case errors.Is(err, mediacache.ErrNotFound):
			slog.Warn("media missing upstream, skipped", "slug", slug, "key", job.key)
			continue
		case err != nil:
			slog.Error("media fetch failed", "slug", slug, "key", job.key, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if _, err := q.UpsertMedia(ctx, data.UpsertMediaParams{
			GameID: game.ID, Kind: job.kind, Key: key,
			SourceUrl: job.url, Position: job.position,
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, firstErr
}
