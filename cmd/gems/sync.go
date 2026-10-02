package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/embiem/indie-game-gems/catalog"
	"github.com/embiem/indie-game-gems/db"
	"github.com/embiem/indie-game-gems/igdb"
	"github.com/embiem/indie-game-gems/mediacache"
	"github.com/embiem/indie-game-gems/steam"
	"github.com/spf13/cobra"
)

// syncTargets resolves `[<slug>... | --all]` into game slugs.
func syncTargets(ctx context.Context, args []string, all bool, needsSteamID, needsIGDB bool) ([]string, error) {
	if all {
		rows, err := db.Queries.ListGamesForSync(ctx)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, r := range rows {
			if needsSteamID && !r.SteamAppid.Valid {
				continue
			}
			if needsIGDB && !r.IgdbID.Valid && !r.SteamAppid.Valid {
				continue
			}
			out = append(out, r.Slug)
		}
		return out, nil
	}
	if len(args) > 0 {
		return args, nil
	}
	return nil, fmt.Errorf("pass game slugs or --all")
}

// steamSyncCmd implements `gems steam sync [<slug>...|--all]`.
func steamSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [<slug>... | --all]",
		Short: "Sync game metadata + review stats from Steam (≤1 req/s)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, _ := cmd.Flags().GetBool("all")
			targets, err := syncTargets(cmd.Context(), args, all, true, false)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				return fmt.Errorf("no games with a steam_appid found")
			}
			return runPerGame(cmd.Context(), targets, func(ctx context.Context, slug string) error {
				return catalog.SteamSync(ctx, db.Queries, steam.NewClient(""), slug)
			})
		},
	}
	cmd.Flags().Bool("all", false, "sync every game that has a steam_appid")
	return cmd
}

// igdbSyncCmd implements `gems igdb sync [<slug>...|--all]`.
func igdbSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [<slug>... | --all]",
		Short: "Sync IGDB data (igdb_id, trailer, store links, companies)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ic, err := catalog.IGDBClientFromEnv()
			if err != nil {
				return err
			}
			all, _ := cmd.Flags().GetBool("all")
			targets, err := syncTargets(cmd.Context(), args, all, false, true)
			if err != nil {
				return err
			}
			return runPerGame(cmd.Context(), targets, func(ctx context.Context, slug string) error {
				return catalog.IGDBSync(ctx, db.Queries, ic, slug)
			})
		},
	}
	cmd.Flags().Bool("all", false, "sync every game (needs igdb_id or steam_appid)")
	return cmd
}

// igdbCatalogueCmd implements `gems igdb catalogue <developer-slug>`.
func igdbCatalogueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "catalogue <developer-slug>",
		Short: "Add the developer's other IGDB games as uncurated games",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ic, err := catalog.IGDBClientFromEnv()
			if err != nil {
				return err
			}
			added, skipped, err := catalog.CatalogueAdd(cmd.Context(), db.Queries, ic, args[0])
			slog.Info("catalogue done", "developer", args[0], "added", added, "skipped", skipped)
			return err
		},
	}
}

// mediaFetchCmd implements `gems media fetch [<slug>...|--all] [--force]`.
func mediaFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch [<slug>... | --all]",
		Short: "Cache game media into MEDIA_DIR and upsert media rows",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")
			dir := os.Getenv("MEDIA_DIR")
			if dir == "" {
				dir = "media"
			}
			mc := mediacache.NewClient(dir)

			var targets []string
			if all, _ := cmd.Flags().GetBool("all"); all {
				rows, err := db.Queries.ListGamesForSync(cmd.Context())
				if err != nil {
					return err
				}
				for _, r := range rows {
					targets = append(targets, r.Slug)
				}
			} else if len(args) > 0 {
				targets = args
			} else {
				rows, err := db.Queries.ListGamesMissingMedia(cmd.Context())
				if err != nil {
					return err
				}
				if len(rows) == 0 {
					slog.Info("all games have media")
					return nil
				}
				for _, r := range rows {
					targets = append(targets, r.Slug)
				}
			}
			return runPerGame(cmd.Context(), targets, func(ctx context.Context, slug string) error {
				n, err := catalog.MediaSync(ctx, db.Queries, mc, slug, force)
				slog.Info("media fetched", "slug", slug, "files", n)
				return err
			})
		},
	}
	cmd.Flags().Bool("force", false, "re-download even if files exist")
	cmd.Flags().Bool("all", false, "fetch media for every game")
	return cmd
}

// refreshCmd is the cron-friendly combination: Steam review stats for all
// games + rescore; IGDB if credentials are present; media for games that
// miss it. Per-game errors are logged; the command fails at the end if any.
func refreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Cron job: steam reviews + rescore, igdb when configured, missing media",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var failed int

			rows, err := db.Queries.ListGamesForSync(ctx)
			if err != nil {
				return err
			}

			// 1. Steam review stats + rescore (only games with an appid).
			sc := steam.NewClient("")
			for _, r := range rows {
				if !r.SteamAppid.Valid {
					continue
				}
				if err := catalog.SteamSync(ctx, db.Queries, sc, r.Slug); err != nil {
					slog.Error("refresh: steam sync failed", "slug", r.Slug, "err", err)
					failed++
				}
			}

			// 2. IGDB (only when credentials are configured).
			if ic, err := catalog.IGDBClientFromEnv(); err == nil {
				for _, r := range rows {
					if !r.IgdbID.Valid && !r.SteamAppid.Valid {
						continue
					}
					if err := catalog.IGDBSync(ctx, db.Queries, ic, r.Slug); err != nil {
						slog.Error("refresh: igdb sync failed", "slug", r.Slug, "err", err)
						failed++
					}
				}
			} else if err != nil && err != igdb.ErrNoCredentials {
				slog.Warn("refresh: igdb client setup failed", "err", err)
			} else {
				slog.Info("refresh: IGDB credentials not set, skipping IGDB")
			}

			// 3. Media for games missing it.
			missing, err := db.Queries.ListGamesMissingMedia(ctx)
			if err != nil {
				return err
			}
			dir := os.Getenv("MEDIA_DIR")
			if dir == "" {
				dir = "media"
			}
			mc := mediacache.NewClient(dir)
			for _, r := range missing {
				n, err := catalog.MediaSync(ctx, db.Queries, mc, r.Slug, false)
				slog.Info("refresh: media", "slug", r.Slug, "files", n)
				if err != nil {
					slog.Error("refresh: media fetch failed", "slug", r.Slug, "err", err)
					failed++
				}
			}

			if failed > 0 {
				return fmt.Errorf("%d operations failed (see log above)", failed)
			}
			slog.Info("refresh complete", "games", len(rows))
			return nil
		},
	}
}

// runPerGame runs fn per slug, logging failures and continuing, returning a
// summarizing error at the end if anything failed.
func runPerGame(ctx context.Context, slugs []string, fn func(context.Context, string) error) error {
	failed := 0
	for _, slug := range slugs {
		if err := fn(ctx, slug); err != nil {
			slog.Error("failed", "slug", slug, "err", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d games failed", failed, len(slugs))
	}
	return nil
}
