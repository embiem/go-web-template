package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/embiem/indie-game-gems/catalog"
	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/db"
	"github.com/embiem/indie-game-gems/util"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/spf13/cobra"
)

func pickCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pick",
		Short: "Manage the daily pick (Game of the Day)",
	}
	auto := &cobra.Command{
		Use:   "auto --days N",
		Short: "Fill the next N days without picks, avoiding recently picked games",
		Args:  cobra.NoArgs,
		RunE:  runPickAuto,
	}
	auto.Flags().Int("days", 0, "number of days to fill")
	list := &cobra.Command{
		Use:   "list [--from YYYY-MM-DD] [--to YYYY-MM-DD]",
		Short: "List picks in a date range (default: today → +30 days)",
		Args:  cobra.NoArgs,
		RunE:  runPickList,
	}
	list.Flags().String("from", "", "range start (YYYY-MM-DD)")
	list.Flags().String("to", "", "range end (YYYY-MM-DD)")
	cmd.AddCommand(
		&cobra.Command{
			Use:   "set <YYYY-MM-DD> <game-slug> [--note \"markdown\"]",
			Short: "Set the pick for a date",
			Args:  cobra.ExactArgs(2),
			RunE:  runPickSet,
		},
		auto,
		list,
	)
	cmd.PersistentFlags().String("note", "", "editorial note (markdown) shown with the pick")
	return cmd
}

func runPickSet(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	date, err := parseDateArg(args[0])
	if err != nil {
		return err
	}
	game, err := db.Queries.GetGameBySlug(ctx, args[1])
	if err != nil {
		return fmt.Errorf("unknown game %q: %w", args[1], err)
	}
	note, _ := cmd.Flags().GetString("note")
	if err := db.Queries.UpsertDailyPick(ctx, data.UpsertDailyPickParams{
		PickDate: date, GameID: game.ID, NoteMd: note,
	}); err != nil {
		return err
	}
	slog.Info("pick set", "date", date.Time.Format("2006-01-02"), "game", game.Slug)
	return nil
}

// recentPickLookback is how far back pick auto looks to avoid repeats.
const recentPickLookback = 14 * 24 * time.Hour

func runPickAuto(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	days, err := cmd.Flags().GetInt("days")
	if err != nil || days < 1 {
		return fmt.Errorf("--days must be a positive integer")
	}

	today := util.TodayUTC(time.Now())

	// Only fill days that have no pick yet.
	from, to := today.AddDate(0, 0, 1), today.AddDate(0, 0, days)
	// One window covers both "which days are filled" and "which games to
	// avoid": past picks within the lookback AND every already-scheduled
	// future pick up to `to`. Excluding today/future picks here is what used
	// to make repeated runs schedule the same game on back-to-back days.
	existing, err := db.Queries.ListPicksBetween(ctx, data.ListPicksBetweenParams{
		PickDate:   pgtype.Date{Time: today.Add(-recentPickLookback), Valid: true},
		PickDate_2: pgtype.Date{Time: to, Valid: true},
	})
	if err != nil {
		return err
	}
	hasPick := map[string]bool{}
	recent := map[string]bool{}
	for _, p := range existing {
		key := p.PickDate.Time.Format("2006-01-02")
		hasPick[key] = true
		recent[p.Slug] = true
	}

	// Rotation candidates: best games first; avoid the recently picked.
	candidates, err := db.Queries.ListGamesForPickRotation(ctx)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return fmt.Errorf("no games in the catalog — run `gems seed` first")
	}
	var order []string
	byID := map[string]data.ListGamesForPickRotationRow{}
	for _, g := range candidates {
		order = append(order, g.Slug)
		byID[g.Slug] = g
	}

	var empty []time.Time
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if !hasPick[d.Format("2006-01-02")] {
			empty = append(empty, d)
		}
	}

	set := 0
	for _, p := range planPickAuto(empty, order, recent) {
		if err := db.Queries.UpsertDailyPick(ctx, data.UpsertDailyPickParams{
			PickDate: pgtype.Date{Time: p.date, Valid: true},
			GameID:   byID[p.slug].ID,
		}); err != nil {
			return err
		}
		set++
		slog.Info("pick set", "date", p.date.Format("2006-01-02"), "game", p.slug)
	}
	slog.Info("pick auto complete", "requested", days, "set", set)
	return nil
}

// pickPlan is one scheduled pick produced by planPickAuto.
type pickPlan struct {
	date time.Time
	slug string
}

// planPickAuto assigns a candidate slug to every date without a pick,
// avoiding recently picked games. Pure — unit-tested without a DB.
func planPickAuto(dates []time.Time, order []string, recent map[string]bool) []pickPlan {
	chosen := catalog.PickRotation(order, recent, len(dates))
	plan := make([]pickPlan, 0, len(chosen))
	for i, slug := range chosen {
		plan = append(plan, pickPlan{date: dates[i], slug: slug})
	}
	return plan
}

func runPickList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}
	to, err := cmd.Flags().GetString("to")
	if err != nil {
		return err
	}
	if from == "" {
		from = util.TodayUTC(time.Now()).Format("2006-01-02")
	}
	if to == "" {
		to = util.TodayUTC(time.Now()).AddDate(0, 0, 30).Format("2006-01-02")
	}
	f, err := parseDateArg(from)
	if err != nil {
		return err
	}
	t, err := parseDateArg(to)
	if err != nil {
		return err
	}
	if t.Time.Before(f.Time) {
		return fmt.Errorf("--to (%s) is before --from (%s)", to, from)
	}
	picks, err := db.Queries.ListPicksBetween(ctx, data.ListPicksBetweenParams{PickDate: f, PickDate_2: t})
	if err != nil {
		return err
	}
	if len(picks) == 0 {
		fmt.Println("no picks in range")
		return nil
	}
	for _, p := range picks {
		note := p.NoteMd
		if len(note) > 40 {
			note = note[:40] + "…"
		}
		fmt.Printf("%s  %-30s %s  %s\n", p.PickDate.Time.Format("2006-01-02"), p.Title, p.Slug, note)
	}
	return nil
}
