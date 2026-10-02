/*
Command gems is the curation CLI for Indie Game Gems.

It shares the app's database setup (db.Init runs pending migrations), so run
it from the repository root with Postgres up:

	gems seed seed/games.yaml
	gems pick set 2026-10-01 hades --note "Why we love it"
	gems pick auto --days 7
	gems pick list
	gems link set hades gog https://www.gog.com/game/hades
	gems games list
	gems rescore
*/
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/embiem/indie-game-gems/catalog"
	"github.com/embiem/indie-game-gems/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:          "gems",
		Short:        "Curation CLI for Indie Game Gems",
		SilenceUsage: true, // usage is for --help, not runtime errors
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			_ = godotenv.Load()
			if os.Getenv("DATABASE_URL") == "" {
				return fmt.Errorf("DATABASE_URL is not set (copy .env.example to .env)")
			}
			return db.Init()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	steamCmd := &cobra.Command{Use: "steam", Short: "Steam importer"}
	steamCmd.AddCommand(steamSyncCmd(), steamReleasesCmd())

	igdbCmd := &cobra.Command{Use: "igdb", Short: "IGDB importer"}
	igdbCmd.AddCommand(igdbSyncCmd(), igdbCatalogueCmd())

	mediaCmd := &cobra.Command{Use: "media", Short: "Local media cache"}
	mediaCmd.AddCommand(mediaFetchCmd())

	root.AddCommand(seedCmd(), pickCmd(), linkCmd(), gamesCmd(), rescoreCmd(),
		steamCmd, igdbCmd, mediaCmd, refreshCmd(), newsletterCmd())

	if err := root.Execute(); err != nil {
		db.Teardown()
		os.Exit(1)
	}
	db.Teardown()
}

// parseDateArg validates a strict YYYY-MM-DD CLI date argument.
func parseDateArg(s string) (pgtype.Date, error) {
	if _, _, _, err := catalog.ParseDate(s); err != nil {
		return pgtype.Date{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}
