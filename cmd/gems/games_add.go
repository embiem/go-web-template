package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/embiem/indie-game-gems/catalog"
	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/db"
	"github.com/embiem/indie-game-gems/steam"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/spf13/cobra"
)

// gamesAddCmd creates a game from Steam data — the normal curation flow for
// new games. Steam fields are imported data; editorial fields start empty.
func gamesAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add --steam <appid> [--slug s] [--editorial N]",
		Short: "Create a game from Steam store data",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			appid, err := cmd.Flags().GetInt64("steam")
			if err != nil || appid <= 0 {
				return fmt.Errorf("--steam must be a positive appid")
			}
			slug, _ := cmd.Flags().GetString("slug")
			editorial, _ := cmd.Flags().GetInt("editorial")

			sc := steam.NewClient("")
			details, err := sc.AppDetails(ctx, appid)
			if err != nil {
				return fmt.Errorf("steam appdetails %d: %w", appid, err)
			}
			if slug == "" {
				slug = catalog.Slugify(details.Name)
			}
			if slug == "" {
				return fmt.Errorf("cannot derive slug from Steam name %q; pass --slug", details.Name)
			}
			if editorial != -1 && (editorial < 0 || editorial > 100) {
				return fmt.Errorf("--editorial must be 0-100 or omitted")
			}

			var releaseDate pgtype.Date
			if t, ok := steam.ParseReleaseDate(details.ReleaseDate.Date, details.ReleaseDate.ComingSoon); ok {
				releaseDate = pgtype.Date{Time: t, Valid: true}
			}
			desc, mdErr := steam.DescriptionToMarkdown(details.DetailedDescription)
			if mdErr != nil || desc == "" {
				desc, _ = steam.DescriptionToMarkdown(details.AboutTheGame)
			}

			var ed pgtype.Int2
			if editorial != -1 {
				ed = pgtype.Int2{Int16: int16(editorial), Valid: true}
			}

			row, err := db.Queries.UpsertGame(ctx, data.UpsertGameParams{
				Slug:           slug,
				Title:          details.Name,
				Tagline:        details.ShortDescription, // fresh game: nothing curated yet
				DescriptionMd:  desc,
				ReleaseDate:    releaseDate,
				ReleaseStatus:  details.ReleaseStatus(),
				SteamAppid:     pgtype.Int8{Int64: appid, Valid: true},
				WebsiteUrl:     details.Website,
				EditorialScore: ed,
			})
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
					if existing, lookErr := db.Queries.GetGameBySteamAppID(ctx, pgtype.Int8{Int64: appid, Valid: true}); lookErr == nil {
						return fmt.Errorf("steam appid %d is already in the catalog as %q", appid, existing.Slug)
					}
					return fmt.Errorf("game %q already exists (unique constraint)", slug)
				}
				return err
			}

			for _, name := range details.Developers {
				if err := linkCompany(ctx, name, row.ID, "developer"); err != nil {
					return err
				}
			}
			for _, name := range details.Publishers {
				if err := linkCompany(ctx, name, row.ID, "publisher"); err != nil {
					return err
				}
			}

			if reviews, err := sc.Reviews(ctx, appid); err == nil {
				if err := db.Queries.SetGameSteamSync(ctx, catalog.SteamImportParams(row, details, reviews)); err != nil {
					return err
				}
			} else {
				slog.Warn("steam reviews unavailable", "slug", slug, "err", err)
			}
			if err := catalog.RescoreGame(ctx, db.Queries, row.ID); err != nil {
				return err
			}
			slog.Info("game added", "slug", slug, "appid", appid, "title", details.Name)
			return nil
		},
	}
	cmd.Flags().Int64("steam", 0, "Steam appid (required)")
	cmd.Flags().String("slug", "", "catalog slug (default: slugified Steam name)")
	cmd.Flags().Int("editorial", -1, "editorial score 0-100 (omit for none)")
	return cmd
}

// linkCompany finds or creates a developer by name and links the role.
func linkCompany(ctx context.Context, name string, gameID pgtype.UUID, role string) error {
	dev, err := db.Queries.GetDeveloperByName(ctx, name)
	if err != nil {
		slug := catalog.Slugify(name)
		if slug == "" {
			return fmt.Errorf("cannot slugify company %q", name)
		}
		dev, err = db.Queries.ImportDeveloper(ctx, data.ImportDeveloperParams{
			Slug: slug, Name: name,
		})
		if err != nil {
			// Slug collision with an existing developer: reuse that row.
			if dev, err = db.Queries.GetDeveloperBySlug(ctx, slug); err != nil {
				return err
			}
		}
	}
	return db.Queries.UpsertGameDeveloperRole(ctx, data.UpsertGameDeveloperRoleParams{
		GameID: gameID, DeveloperID: dev.ID, Role: role,
	})
}
