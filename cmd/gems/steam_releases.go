package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/embiem/indie-game-gems/steam"
	"github.com/embiem/indie-game-gems/util"
	"github.com/spf13/cobra"
)

// releasesFile is the JSON document written by `gems steam releases`.
type releasesFile struct {
	From        string          `json:"from"`
	To          string          `json:"to"`
	IndieOnly   bool            `json:"indie_only"`
	GeneratedAt time.Time       `json:"generated_at"`
	Count       int             `json:"count"`
	Games       []releaseRecord `json:"games"`
}

type releaseRecord struct {
	SteamAppID  int64           `json:"steam_appid"`
	Name        string          `json:"name"`
	ReleaseDate string          `json:"release_date"` // YYYY-MM-DD
	ComingSoon  bool            `json:"coming_soon"`
	StoreURL    string          `json:"store_url"`
	Details     *releaseDetails `json:"details,omitempty"`
	Reviews     *releaseReviews `json:"reviews,omitempty"`
	// Error records a failed details lookup; the row is kept.
	Error string `json:"error,omitempty"`
}

type releaseDetails struct {
	ShortDescription string   `json:"short_description"`
	Developers       []string `json:"developers"`
	Publishers       []string `json:"publishers"`
	Tags             []string `json:"tags"` // top user tags, most-voted first
	IsFree           bool     `json:"is_free"`
	HeaderImage      string   `json:"header_image,omitempty"`
}

// releaseReviews is the store page's review score (Steam purchasers only).
type releaseReviews struct {
	Total   int64  `json:"total"`
	Percent int    `json:"percent"`
	Summary string `json:"summary"` // Steam's label, e.g. "Very Positive"
}

// steamReleasesCmd implements `gems steam releases --from D [--to D]`: every
// Steam game released (or scheduled) in a date range, as a JSON file. It
// reads only the public Steam store and needs no database.
func steamReleasesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "releases --from YYYY-MM-DD [--to YYYY-MM-DD] [--out file.json]",
		Short: "Write every Steam game released in a date range to a JSON file",
		Long: `Lists games whose Steam release date falls within --from..--to (inclusive),
covering both released games and games scheduled for a future date, then
adds store details, top tags and the review score.

Games with only a month/quarter date ("October 2026", "Coming soon") are
skipped. Details come from Steam's batched GetItems API, 100 games per
request at 1 req/s (a week of indie releases takes about 10 s);
--details=false writes only appid, name and date.
Feed interesting appids to 'gems games add --steam <appid>'.`,
		Args: cobra.NoArgs,
		// Overrides the root hook: this command needs no database.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			fromArg, _ := cmd.Flags().GetString("from")
			toArg, _ := cmd.Flags().GetString("to")
			out, _ := cmd.Flags().GetString("out")
			indie, _ := cmd.Flags().GetBool("indie")
			details, _ := cmd.Flags().GetBool("details")

			today := util.TodayUTC(time.Now())
			from, err := parseDateArg(fromArg)
			if err != nil {
				return err
			}
			to := today
			if toArg != "" {
				d, err := parseDateArg(toArg)
				if err != nil {
					return err
				}
				to = d.Time
			}
			if out == "" {
				out = filepath.Join("tmp", fmt.Sprintf("steam-releases-%s_%s.json", from.Time.Format(time.DateOnly), to.Format(time.DateOnly)))
			}

			ctx := cmd.Context()
			log := cmd.ErrOrStderr()
			client := steam.NewClient("")
			rows, err := client.ReleasesBetween(ctx, steam.ReleaseQuery{
				From: from.Time, To: to, Today: today, IndieOnly: indie,
			})
			if err != nil {
				return err
			}
			sort.SliceStable(rows, func(i, j int) bool {
				if !rows[i].ReleaseDate.Equal(rows[j].ReleaseDate) {
					return rows[i].ReleaseDate.Before(rows[j].ReleaseDate)
				}
				return rows[i].AppID < rows[j].AppID
			})
			fmt.Fprintf(log, "found %d games released %s..%s\n", len(rows), from.Time.Format(time.DateOnly), to.Format(time.DateOnly))

			records := make([]releaseRecord, len(rows))
			for i, r := range rows {
				records[i] = releaseRecord{
					SteamAppID:  r.AppID,
					Name:        r.Name,
					ReleaseDate: r.ReleaseDate.Format(time.DateOnly),
					ComingSoon:  r.ComingSoon,
					StoreURL:    fmt.Sprintf("https://store.steampowered.com/app/%d/", r.AppID),
				}
			}
			if details && len(records) > 0 {
				if err := fillReleaseDetails(ctx, client, records, log); err != nil {
					return err
				}
			}

			doc := releasesFile{
				From:        from.Time.Format(time.DateOnly),
				To:          to.Format(time.DateOnly),
				IndieOnly:   indie,
				GeneratedAt: time.Now().UTC().Truncate(time.Second),
				Count:       len(records),
				Games:       records,
			}
			b, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				return err
			}
			b = append(b, '\n')
			if out == "-" {
				_, err = cmd.OutOrStdout().Write(b)
				return err
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(out, b, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(log, "wrote %d games to %s\n", len(records), out)
			return nil
		},
	}
	cmd.Flags().String("from", "", "first release date, YYYY-MM-DD (required)")
	cmd.Flags().String("to", "", "last release date, YYYY-MM-DD (default: today, UTC)")
	cmd.Flags().String("out", "", `output file, "-" for stdout (default: tmp/steam-releases-<from>_<to>.json)`)
	cmd.Flags().Bool("indie", true, "only games with Steam's Indie tag")
	cmd.Flags().Bool("details", true, "fetch store details, tags and review score per game")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

// fillReleaseDetails adds GetItems details and, for released games, the
// review score, MaxItemsPerRequest games per request. A failed batch marks
// its records with the error and the walk continues.
func fillReleaseDetails(ctx context.Context, client *steam.Client, records []releaseRecord, log io.Writer) error {
	tagNames, err := client.TagNames(ctx)
	if err != nil {
		return err
	}
	for start := 0; start < len(records); start += steam.MaxItemsPerRequest {
		batch := records[start:min(start+steam.MaxItemsPerRequest, len(records))]
		fmt.Fprintf(log, "details %d-%d/%d\n", start+1, start+len(batch), len(records))
		appids := make([]int64, len(batch))
		for i := range batch {
			appids[i] = batch[i].SteamAppID
		}
		items, err := client.Items(ctx, appids)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(log, "  warning: %v\n", err)
			for i := range batch {
				batch[i].Error = err.Error()
			}
			continue
		}
		for i := range batch {
			applyStoreItem(&batch[i], items[batch[i].SteamAppID], tagNames)
		}
	}
	return nil
}

// applyStoreItem fills rec from it; a nil item (unknown or hidden app)
// records an error instead.
func applyStoreItem(rec *releaseRecord, it *steam.StoreItem, tagNames map[int64]string) {
	if it == nil {
		rec.Error = fmt.Sprintf("steam GetItems: no data for appid %d", rec.SteamAppID)
		return
	}
	tags := make([]string, 0, len(it.Tags))
	for _, t := range it.Tags {
		if name, ok := tagNames[t.TagID]; ok {
			tags = append(tags, name)
		}
	}
	rec.Details = &releaseDetails{
		ShortDescription: it.BasicInfo.ShortDescription,
		Developers:       it.DeveloperNames(),
		Publishers:       it.PublisherNames(),
		Tags:             tags,
		IsFree:           it.IsFree,
		HeaderImage:      it.HeaderImageURL(),
	}
	if rec.ComingSoon || it.Reviews.Summary == nil {
		return // unreleased games have no reviews
	}
	s := it.Reviews.Summary
	rec.Reviews = &releaseReviews{Total: s.ReviewCount, Percent: s.PercentPositive, Summary: s.ReviewScoreDesc}
}
