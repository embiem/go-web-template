package catalog

import (
	"context"
	"fmt"
	"log/slog"

	"errors"
	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/igdb"
	"github.com/embiem/indie-game-gems/steam"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
)

// SteamSync refreshes one game from Steam (appdetails + appreviews), applying
// the import merge rules: curated fields (tagline, gem note, editorial score,
// release date, store links) always win; Steam only fills description,
// review stats, empty website and a missing release date.
func SteamSync(ctx context.Context, q *data.Queries, sc *steam.Client, slug string) error {
	game, err := q.GetGameBySlug(ctx, slug)
	if err != nil {
		return fmt.Errorf("game %s: %w", slug, err)
	}
	if !game.SteamAppid.Valid {
		return fmt.Errorf("game %s has no steam_appid — add it via `gems games add --steam` or seed", slug)
	}

	details, err := sc.AppDetails(ctx, game.SteamAppid.Int64)
	if err != nil {
		return fmt.Errorf("steam appdetails %d: %w", game.SteamAppid.Int64, err)
	}
	reviews, err := sc.Reviews(ctx, game.SteamAppid.Int64)
	if err != nil {
		// Reviews are optional — metadata sync continues without them.
		slog.Warn("steam reviews unavailable", "slug", slug, "err", err)
	}

	if err := q.SetGameSteamSync(ctx, SteamImportParams(game, details, reviews)); err != nil {
		return err
	}
	if err := RescoreGame(ctx, q, game.ID); err != nil {
		return err
	}
	slog.Info("steam sync ok", "slug", slug)
	return nil
}

// SteamImportParams maps a Steam payload onto SetGameSteamSync parameters.
// Pure (no DB) so merge rules are unit-testable.
func SteamImportParams(game data.Game, details *steam.AppDetails, reviews *steam.ReviewSummary) data.SetGameSteamSyncParams {
	desc, err := steam.DescriptionToMarkdown(details.DetailedDescription)
	if err != nil || desc == "" {
		if alt, altErr := steam.DescriptionToMarkdown(details.AboutTheGame); altErr == nil && alt != "" {
			desc = alt
		} else if err != nil || desc == "" {
			// Conversion failed or produced nothing: keep the existing
			// description rather than storing an empty one.
			if err != nil {
				slog.Warn("steam description conversion failed; keeping existing", "slug", game.Slug, "err", err)
			}
			desc = game.DescriptionMd
		}
	}

	var pct pgtype.Int2
	var count int64
	if reviews != nil && reviews.TotalReviews > 0 {
		pct = pgtype.Int2{Int16: int16(reviews.ReviewPct() + 0.5), Valid: true}
		count = reviews.TotalReviews
	}

	releaseDate, hasDate := steam.ParseReleaseDate(details.ReleaseDate.Date, details.ReleaseDate.ComingSoon)
	var d pgtype.Date
	if hasDate {
		d = pgtype.Date{Time: releaseDate, Valid: true}
	}
	status := details.ReleaseStatus()

	return data.SetGameSteamSyncParams{
		ID:               game.ID,
		DescriptionMd:    desc,
		SteamReviewPct:   pct,
		SteamReviewCount: count,
		WebsiteUrl:       details.Website,
		ReleaseDate:      d,
		ReleaseStatus:    status,
	}
}

// RescoreGame recomputes gem_score for one game from its current inputs.
// Shared by seed, rescore and the importers.
func RescoreGame(ctx context.Context, q *data.Queries, gameID pgtype.UUID) error {
	g, err := q.GetGameByID(ctx, gameID)
	if err != nil {
		return err
	}
	return UpdateScore(ctx, q, g)
}

// UpdateScore computes and persists gem_score for a loaded game row.
func UpdateScore(ctx context.Context, q *data.Queries, g data.Game) error {
	var editorial, pct *float64
	if g.EditorialScore.Valid {
		v := float64(g.EditorialScore.Int16)
		editorial = &v
	}
	if g.SteamReviewPct.Valid {
		v := float64(g.SteamReviewPct.Int16)
		pct = &v
	}
	var score pgtype.Float4
	if s := GemScore(editorial, pct, g.SteamReviewCount); s != nil {
		score = pgtype.Float4{Float32: float32(*s), Valid: true}
	}
	return q.SetGameScores(ctx, data.SetGameScoresParams{
		ID:               g.ID,
		EditorialScore:   g.EditorialScore,
		SteamReviewPct:   g.SteamReviewPct,
		SteamReviewCount: g.SteamReviewCount,
		GemScore:         score,
	})
}

// IGDBSync refreshes one game from IGDB: resolves igdb_id (via Steam appid
// when needed), the YouTube trailer, missing store links, and fills missing
// developer IGDB data. Curated fields always win.
func IGDBSync(ctx context.Context, q *data.Queries, ic *igdb.Client, slug string) error {
	game, err := q.GetGameBySlug(ctx, slug)
	if err != nil {
		return fmt.Errorf("game %s: %w", slug, err)
	}

	var g *igdb.Game
	switch {
	case game.IgdbID.Valid:
		g, err = ic.Game(ctx, game.IgdbID.Int64)
	case game.SteamAppid.Valid:
		g, err = ic.GameBySteamAppID(ctx, game.SteamAppid.Int64)
	default:
		return fmt.Errorf("game %s has neither igdb_id nor steam_appid to look it up", slug)
	}
	if err != nil {
		return fmt.Errorf("game %s: %w", slug, err)
	}

	if err := q.SetGameIGDBSync(ctx, IGDBGameParams(game, g)); err != nil {
		return err
	}

	// Missing store links (import variant never overwrites existing links).
	for store, url := range g.StoreLinks() {
		if !ValidStores[store] {
			continue
		}
		if err := q.ImportStoreLink(ctx, data.ImportStoreLinkParams{
			GameID: game.ID, Store: store, Url: url, SourceUrl: url,
		}); err != nil {
			return err
		}
	}

	// Website: official site goes to games.website_url only when empty.
	if w := g.OfficialWebsite(); w != "" && game.WebsiteUrl == "" {
		if err := q.SetGameWebsiteIfEmpty(ctx, data.SetGameWebsiteIfEmptyParams{
			ID: game.ID, WebsiteUrl: w,
		}); err != nil {
			return err
		}
	}

	// Developers: fill igdb_id/logo/description for involved companies.
	if err := syncGameCompanies(ctx, q, ic, game, g); err != nil {
		return err
	}
	slog.Info("igdb sync ok", "slug", slug)
	return nil
}

// IGDBGameParams maps an IGDB game payload onto SetGameIGDBSync parameters.
// The SQL layer COALESCEs, so we pass the discovered values only when the
// game row doesn't already have them (curated/imported values win).
func IGDBGameParams(game data.Game, g *igdb.Game) data.SetGameIGDBSyncParams {
	var igdbID pgtype.Int8
	if !game.IgdbID.Valid {
		igdbID = pgtype.Int8{Int64: g.ID, Valid: true}
	}
	var trailer pgtype.Text
	if game.TrailerYoutubeID.String == "" {
		trailer = pgtype.Text{String: g.YouTubeTrailer(), Valid: g.YouTubeTrailer() != ""}
	}
	return data.SetGameIGDBSyncParams{
		ID:               game.ID,
		IgdbID:           igdbID,
		TrailerYoutubeID: trailer,
	}
}

func syncGameCompanies(ctx context.Context, q *data.Queries, ic *igdb.Client, game data.Game, g *igdb.Game) error {
	for _, inc := range g.InvolvedCompanies {
		dev, err := matchDeveloper(ctx, q, inc.Company)
		if err != nil {
			return err
		}
		// Logo key for the media fetcher; downloads happen in `gems media fetch`.
		logoKey := ""
		if inc.Company.Logo != nil {
			logoKey = "developers/" + dev.Slug + "/logo"
		}
		var website string
		for _, w := range inc.Company.Websites {
			if w.Type == igdb.WebsiteOfficial {
				website = w.URL
				break
			}
		}
		if err := q.SetDeveloperIGDBSync(ctx, data.SetDeveloperIGDBSyncParams{
			ID:         dev.ID,
			IgdbID:     pgtype.Int8{Int64: inc.Company.ID, Valid: true},
			BioMd:      deref(inc.Company.Description),
			WebsiteUrl: website,
			LogoKey:    logoKey,
		}); err != nil {
			return err
		}
	}
	return nil
}

// matchDeveloper finds a developer by igdb_id, then case-insensitive name,
// creating one with a slugified name when there's no match.
func matchDeveloper(ctx context.Context, q *data.Queries, company igdb.Company) (data.Developer, error) {
	if company.ID == 0 {
		return data.Developer{}, nil
	}
	for _, d := range mustDevelopers(ctx, q) {
		if d.IgdbID.Valid && d.IgdbID.Int64 == company.ID {
			return d, nil
		}
	}
	if d, err := q.GetDeveloperByName(ctx, company.Name); err == nil {
		return d, nil
	}
	slug := company.Slug
	if slug == "" {
		slug = Slugify(company.Name)
	}
	if d, err := q.ImportDeveloper(ctx, data.ImportDeveloperParams{Slug: slug, Name: company.Name}); err == nil {
		return d, nil
	} else if !isNoRows(err) {
		return data.Developer{}, err
	}
	// Slug conflict with an unrelated developer: fall back to the existing row.
	return q.GetDeveloperBySlug(ctx, slug)
}

func mustDevelopers(ctx context.Context, q *data.Queries) []data.Developer {
	devs, err := q.ListDevelopers(ctx)
	if err != nil {
		return nil
	}
	return devs
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Slugify converts a company name to our slug convention (lowercase, hyphens).
func Slugify(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
