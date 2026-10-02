package main

import (
	"fmt"
	"log/slog"

	"github.com/embiem/indie-game-gems/catalog"
	"github.com/embiem/indie-game-gems/db"
	"github.com/spf13/cobra"
)

func gamesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "games",
		Short: "Catalog utilities",
	}
	cmd.AddCommand(gamesListCmd(), gamesAddCmd())
	return cmd
}

func gamesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all games in the catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			games, err := db.Queries.ListGames(cmd.Context())
			if err != nil {
				return err
			}
			if len(games) == 0 {
				fmt.Println("catalog is empty — run `gems seed <file>`")
				return nil
			}
			for _, g := range games {
				score := "—"
				if g.GemScore.Valid {
					score = fmt.Sprintf("%.1f", g.GemScore.Float32)
				}
				date := "TBA"
				if g.ReleaseDate.Valid {
					date = g.ReleaseDate.Time.Format("2006-01-02")
				}
				fmt.Printf("%-30s %-30s %-12s %s  %s\n", g.Slug, g.Title, g.ReleaseStatus, date, score)
			}
			return nil
		},
	}
}

func rescoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rescore",
		Short: "Recompute gem_score for every game",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			games, err := db.Queries.ListGames(cmd.Context())
			if err != nil {
				return err
			}
			for _, g := range games {
				if err := catalog.UpdateScore(cmd.Context(), db.Queries, g); err != nil {
					return err
				}
			}
			slog.Info("rescored games", "count", len(games))
			return nil
		},
	}
}
