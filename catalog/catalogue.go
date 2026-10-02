package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/igdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CatalogueAdd imports a developer's other IGDB games as uncurated entries:
// editorial_score NULL, linked to the developer, store links from IGDB where
// present. Games already in the catalog (by slug) are skipped. Steam-syncing
// those with a Steam appid is the caller's next step.
func CatalogueAdd(ctx context.Context, q *data.Queries, ic *igdb.Client, developerSlug string) (added, skipped int, err error) {
	dev, err := q.GetDeveloperBySlug(ctx, developerSlug)
	if err != nil {
		return 0, 0, fmt.Errorf("developer %s: %w", developerSlug, err)
	}
	if !dev.IgdbID.Valid {
		// Try to resolve via an existing game's IGDB data before failing.
		return 0, 0, fmt.Errorf(
			"developer %s has no igdb_id — run `gems igdb sync --all` first so IGDB company data fills in",
			developerSlug)
	}

	games, err := ic.CompanyCatalogue(ctx, dev.IgdbID.Int64)
	if err != nil {
		return 0, 0, err
	}
	for _, g := range games {
		slug := g.Slug
		if slug == "" {
			slug = Slugify(g.Name)
		}
		if slug == "" {
			continue
		}
		if _, err := q.GetGameBySlug(ctx, slug); err == nil {
			skipped++ // already in the catalog
			continue
		} else if !isNoRowsErr(err) {
			return added, skipped, err
		}

		status := "released"
		if g.GameStatus != nil {
			status = igdbStatus(*g.GameStatus)
		}
		var releaseDate pgtype.Date
		if t := g.ReleaseDate(); t != nil {
			releaseDate = pgtype.Date{Time: *t, Valid: true}
		}
		row, err := q.UpsertGame(ctx, data.UpsertGameParams{
			Slug:          slug,
			Title:         g.Name,
			Tagline:       "", // uncurated: filled later by a curator
			DescriptionMd: deref(g.Summary),
			ReleaseDate:   releaseDate,
			ReleaseStatus: status,
			IgdbID:        pgtype.Int8{Int64: g.ID, Valid: true},
		})
		if err != nil {
			return added, skipped, fmt.Errorf("game %s: %w", slug, err)
		}
		if err := q.UpsertGameDeveloperRole(ctx, data.UpsertGameDeveloperRoleParams{
			GameID: row.ID, DeveloperID: dev.ID, Role: "developer",
		}); err != nil {
			return added, skipped, err
		}
		for store, url := range g.StoreLinks() {
			if !ValidStores[store] {
				continue
			}
			if err := q.ImportStoreLink(ctx, data.ImportStoreLinkParams{
				GameID: row.ID, Store: store, Url: url, SourceUrl: url,
			}); err != nil {
				return added, skipped, err
			}
		}
		added++
		slog.Info("catalogue game added", "developer", developerSlug, "slug", slug)
	}
	return added, skipped, nil
}

func isNoRowsErr(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func igdbStatus(s int) string {
	switch s {
	case igdb.GameStatusReleased:
		return "released"
	case igdb.GameStatusEarlyAccess:
		return "early_access"
	default:
		return "upcoming"
	}
}
