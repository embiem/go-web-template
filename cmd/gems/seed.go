package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/embiem/indie-game-gems/catalog"
	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/spf13/cobra"
)

// seedCmd imports a seed YAML file, idempotently upserting by slug:
// developers, games, roles, tags, store links — then recomputes gem_score.
func seedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed <file>",
		Short: "Import a seed YAML file (upserts by slug, safe to re-run)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := catalog.ParseSeedFile(args[0])
			if err != nil {
				return err
			}
			q := db.Queries

			for _, d := range f.Developers {
				socials, err := catalog.MarshalSocials(d.Socials)
				if err != nil {
					return fmt.Errorf("developer %s: %w", d.Slug, err)
				}
				if _, err := q.UpsertDeveloper(cmd.Context(), data.UpsertDeveloperParams{
					Slug:       d.Slug,
					Name:       d.Name,
					BioMd:      d.Bio,
					WebsiteUrl: d.WebsiteURL,
					Socials:    socials,
				}); err != nil {
					return fmt.Errorf("upsert developer %s: %w", d.Slug, err)
				}
				slog.Info("developer upserted", "slug", d.Slug)
			}

			for _, g := range f.Games {
				var releaseDate pgtype.Date
				if g.ReleaseDate != "" {
					releaseDate, err = parseDateArg(g.ReleaseDate)
					if err != nil {
						return fmt.Errorf("game %s: %w", g.Slug, err)
					}
				}
				// igdb_slug in the seed is a display slug, not the numeric
				// IGDB id; games.igdb_id stays NULL until the IGDB importer
				// (phase 2) resolves real ids.
				var steamAppid pgtype.Int8
				if g.SteamAppID != nil {
					steamAppid = pgtype.Int8{Int64: *g.SteamAppID, Valid: true}
				}
				var editorial pgtype.Int2
				if g.EditorialScore > 0 {
					editorial = pgtype.Int2{Int16: int16(g.EditorialScore), Valid: true}
				}
				row, err := q.UpsertGame(cmd.Context(), data.UpsertGameParams{
					Slug:           g.Slug,
					Title:          g.Title,
					Tagline:        g.Tagline,
					DescriptionMd:  g.Description,
					GemNoteMd:      g.GemNote,
					ReleaseDate:    releaseDate,
					ReleaseStatus:  g.ReleaseStatus,
					SteamAppid:     steamAppid,
					IgdbID:         pgtype.Int8{},
					WebsiteUrl:     g.WebsiteURL,
					EditorialScore: editorial,
				})
				if err != nil {
					return fmt.Errorf("upsert game %s: %w", g.Slug, err)
				}

				// Roles
				developerIDs := map[string]bool{}
				for _, slug := range g.Developers {
					dev, err := q.GetDeveloperBySlug(cmd.Context(), slug)
					if err != nil {
						return fmt.Errorf("game %s: developer %s: %w", g.Slug, slug, err)
					}
					developerIDs[dev.ID.String()] = true
					if err := q.UpsertGameDeveloperRole(cmd.Context(), data.UpsertGameDeveloperRoleParams{
						GameID: row.ID, DeveloperID: dev.ID, Role: "developer",
					}); err != nil {
						return err
					}
				}
				for _, slug := range g.Publishers {
					dev, err := q.GetDeveloperBySlug(cmd.Context(), slug)
					if err != nil {
						return fmt.Errorf("game %s: publisher %s: %w", g.Slug, slug, err)
					}
					if err := q.UpsertGameDeveloperRole(cmd.Context(), data.UpsertGameDeveloperRoleParams{
						GameID: row.ID, DeveloperID: dev.ID, Role: "publisher",
					}); err != nil {
						return err
					}
				}

				// Tags
				for _, tag := range g.Tags {
					slug := catalog.NormalizeTagSlug(tag)
					t, err := q.UpsertTag(cmd.Context(), data.UpsertTagParams{Slug: slug, Name: tagTitle(tag)})
					if err != nil {
						return fmt.Errorf("game %s: tag %s: %w", g.Slug, slug, err)
					}
					if err := q.UpsertGameTag(cmd.Context(), data.UpsertGameTagParams{GameID: row.ID, TagID: t.ID}); err != nil {
						return err
					}
				}

				// Store links
				for store, url := range g.StoreLinks {
					if !catalog.ValidStores[store] {
						return fmt.Errorf("game %s: unknown store %q", g.Slug, store)
					}
					if err := q.UpsertStoreLink(cmd.Context(), data.UpsertStoreLinkParams{
						GameID: row.ID, Store: store, Url: url, SourceUrl: "",
					}); err != nil {
						return fmt.Errorf("game %s: store link %s: %w", g.Slug, store, err)
					}
				}

				// Recompute gem_score from editorial + stored Steam stats.
				if err := rescoreGame(cmd, row.ID); err != nil {
					return fmt.Errorf("game %s: %w", g.Slug, err)
				}
				slog.Info("game upserted", "slug", g.Slug)
			}
			slog.Info("seed complete", "developers", len(f.Developers), "games", len(f.Games))
			return nil
		},
	}
}

func tagTitle(tag string) string {
	if tag == "" {
		return tag
	}
	return strings.ToUpper(tag[:1]) + tag[1:]
}

// rescoreGame recomputes and persists gem_score for one game; the shared
// implementation lives in catalog.RescoreGame.
func rescoreGame(cmd *cobra.Command, gameID pgtype.UUID) error {
	return catalog.RescoreGame(cmd.Context(), db.Queries, gameID)
}
