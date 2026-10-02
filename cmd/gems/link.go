package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/embiem/indie-game-gems/catalog"
	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/db"
	"github.com/spf13/cobra"
)

func linkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Manage per-game store links (steam, gog, epic, itch, humble, direct)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "set <game-slug> <store> <url>",
			Short: "Set a store link for a game",
			Args:  cobra.ExactArgs(3),
			RunE:  runLinkSet,
		},
		&cobra.Command{
			Use:   "rm <game-slug> <store>",
			Short: "Remove a store link from a game",
			Args:  cobra.ExactArgs(2),
			RunE:  runLinkRm,
		},
	)
	return cmd
}

func runLinkSet(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	slug, store, url := args[0], args[1], args[2]
	if !catalog.ValidStores[store] {
		return fmt.Errorf("unknown store %q (allowed: steam, gog, epic, itch, humble, direct)", store)
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("invalid URL %q: must start with http:// or https://", url)
	}
	game, err := db.Queries.GetGameBySlug(ctx, slug)
	if err != nil {
		return fmt.Errorf("unknown game %q", slug)
	}
	if err := db.Queries.UpsertStoreLink(ctx, data.UpsertStoreLinkParams{
		GameID: game.ID, Store: store, Url: url, SourceUrl: url,
	}); err != nil {
		return err
	}
	slog.Info("store link set", "game", slug, "store", store, "url", url)
	return nil
}

func runLinkRm(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	slug, store := args[0], args[1]
	if !catalog.ValidStores[store] {
		return fmt.Errorf("unknown store %q (allowed: steam, gog, epic, itch, humble, direct)", store)
	}
	game, err := db.Queries.GetGameBySlug(ctx, slug)
	if err != nil {
		return fmt.Errorf("unknown game %q", slug)
	}
	if err := db.Queries.DeleteStoreLink(ctx, data.DeleteStoreLinkParams{
		GameID: game.ID, Store: store,
	}); err != nil {
		return err
	}
	slog.Info("store link removed", "game", slug, "store", store)
	return nil
}
