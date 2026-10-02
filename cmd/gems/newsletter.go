package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/embiem/indie-game-gems/db"
	"github.com/embiem/indie-game-gems/mail"
	"github.com/embiem/indie-game-gems/newsletter"
	viewemail "github.com/embiem/indie-game-gems/view/email"
	"github.com/spf13/cobra"
)

// Directories for issue authoring, relative to the repo root (the CLI, like
// the other gems commands, is run from the repository root).
const (
	issuesDir    = "newsletter/issues"
	scaffoldFile = "newsletter/templates/weekly.md"
	previewDir   = "tmp/newsletter"
)

// previewUnsubURL substitutes {{unsubscribe_url}} in preview/test output —
// clearly inert, and live only when the recipient is a real subscriber.
const previewUnsubURL = "https://example.com/newsletter/unsubscribe/preview-only"

func newsletterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "newsletter",
		Short: "Author, preview and send the email newsletter",
		Long: `Author, preview and send the Indie Game Gems email newsletter.

Issues are markdown files in newsletter/issues/<slug>.md with YAML front
matter and {{gotd}}/{{radar}}/{{top}}/{{game}} directives that pull live
data from the database. See newsletter/templates/weekly.md.`,
	}
	cmd.AddCommand(
		newsletterNewCmd(),
		newsletterPreviewCmd(),
		newsletterTestCmd(),
		newsletterSendCmd(),
		newsletterSubscribersCmd(),
		newsletterIssuesCmd(),
	)
	return cmd
}

// newsletterNewCmd implements `gems newsletter new --week 2026-W40`.
func newsletterNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new issue file from the weekly scaffold",
		RunE: func(cmd *cobra.Command, args []string) error {
			week, _ := cmd.Flags().GetString("week")
			start, _, err := newsletter.ParseISOWeek(week)
			if err != nil {
				return fmt.Errorf("--week: %w", err)
			}
			slug := strings.ToLower(week) // 2026-W40 → 2026-w40
			dst := filepath.Join(issuesDir, slug+".md")
			if _, err := os.Stat(dst); err == nil {
				return fmt.Errorf("%s already exists (edit it instead)", dst)
			}
			scaffold, err := os.ReadFile(scaffoldFile)
			if err != nil {
				return err
			}
			cfg, err := newsletter.LoadConfigEnv()
			if err != nil {
				return err
			}
			baseURL := cfg.BaseURL
			// "Top of the Month" covers the month the week starts in (a
			// W40 issue written in early October rounds up September —
			// the month readers just finished). GOTD defaults to the
			// week's Sunday, the last day covered (ParseISOWeek's end is
			// the next Monday, exclusive).
			monthStart := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
			sunday := start.AddDate(0, 0, 6)
			body := strings.NewReplacer(
				"@@WEEK@@", week,
				"@@GOTD_DATE@@", sunday.Format("2006-01-02"), // week's Sunday
				"@@MONTH@@", monthStart.Format("2006-01"),
				"@@MONTH_TITLE@@", monthStart.Format("January 2006"),
				"@@BASE_URL@@", baseURL,
			).Replace(string(scaffold))
			if err := os.MkdirAll(issuesDir, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
				return err
			}
			fmt.Printf("created %s\nnext: edit it, then gems newsletter preview %s\n", dst, dst)
			return nil
		},
	}
	cmd.Flags().String("week", "", "ISO week to author, e.g. 2026-W40")
	_ = cmd.MarkFlagRequired("week")
	return cmd
}

func newsletterPreviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preview <file>",
		Short: "Render an issue to HTML (+ text) without sending",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, _ := cmd.Flags().GetString("out")
			withText, _ := cmd.Flags().GetBool("text")
			html, text, slug, err := renderIssueFile(cmd, args[0], previewUnsubURL)
			if err != nil {
				return err
			}
			if out == "" {
				out = filepath.Join(previewDir, slug+".html")
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(out, []byte(html), 0o644); err != nil {
				return err
			}
			fmt.Printf("html: %s\n", out)
			if withText {
				txt := strings.TrimSuffix(out, ".html") + ".txt"
				if err := os.WriteFile(txt, []byte(text), 0o644); err != nil {
					return err
				}
				fmt.Printf("text: %s\n", txt)
			}
			return nil
		},
	}
	cmd.Flags().String("out", "", "output HTML path (default tmp/newsletter/<slug>.html)")
	cmd.Flags().Bool("text", false, "also write the plain-text alternative")
	return cmd
}

func newsletterTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test <file>",
		Short: "Send one copy of an issue to a test address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			to, _ := cmd.Flags().GetString("to")
			to, err := newsletter.NormalizeAndValidate(to)
			if err != nil {
				return fmt.Errorf("--to: %w", err)
			}
			cfg, err := newsletter.LoadConfigEnv()
			if err != nil {
				return err
			}
			if err := cfg.ValidateForSending(); err != nil {
				return err
			}
			// The recipient's real unsubscribe link when they are a
			// subscriber; otherwise an inert placeholder.
			unsub := previewUnsubURL
			if sub, err := db.Queries.GetSubscriberByEmail(cmd.Context(), to); err == nil {
				unsub = cfg.UnsubURL(sub.ID.String())
			}
			html, text, _, err := renderIssueFile(cmd, args[0], unsub)
			if err != nil {
				return err
			}
			iss, err := parseIssueFile(args[0])
			if err != nil {
				return err
			}
			mailer, err := mail.New(cfg.MailConfig())
			if err != nil {
				return err
			}
			if err := mailer.Newsletter(cmd.Context(), mail.NewsletterMessage{
				To: to, Subject: "[TEST] " + iss.Subject, HTML: html, Text: text, UnsubURL: unsub,
			}); err != nil {
				return err
			}
			fmt.Printf("sent test mail to %s (unsubscribe link %s)\n", to, liveOrInert(unsub))
			return nil
		},
	}
	cmd.Flags().String("to", "", "test recipient address")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newsletterSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send <file>",
		Short: "Snapshot an issue and send it to all confirmed subscribers",
		Long: `Snapshots the rendered issue into the database, creates deliveries
for every confirmed subscriber and sends the pending ones. Resumable:
re-running a completed send delivers nothing new; an interrupted run
continues where it stopped.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			yes, _ := cmd.Flags().GetBool("yes")

			cfg, err := newsletter.LoadConfigEnv()
			if err != nil {
				return err
			}
			if err := cfg.ValidateForSending(); err != nil {
				return err
			}
			renderer := &newsletter.Renderer{Q: db.Queries, Cfg: cfg}
			iss, err := parseIssueFile(args[0])
			if err != nil {
				return err
			}
			doc, err := renderer.Resolve(cmd.Context(), iss)
			if err != nil {
				return err
			}
			n, err := db.Queries.CountConfirmedSubscribers(cmd.Context())
			if err != nil {
				return err
			}
			summary := fmt.Sprintf("issue %q — %d block(s), %d confirmed recipient(s)",
				iss.Subject, len(doc.Blocks), n)
			if dryRun {
				html, err := viewemail.RenderHTML(doc)
				if err != nil {
					return err
				}
				out := filepath.Join(previewDir, iss.Slug+".dry-run.html")
				if err := os.MkdirAll(previewDir, 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(out, []byte(html), 0o644); err != nil {
					return err
				}
				fmt.Printf("dry run — %s\nhtml: %s\ndatabase untouched, nothing sent\n", summary, out)
				return nil
			}
			if !yes && !confirmPrompt(fmt.Sprintf("%s\nsend now? [y/N] ", summary)) {
				return errors.New("aborted")
			}

			mailer, err := mail.New(cfg.MailConfig())
			if err != nil {
				return err
			}
			sender := &newsletter.Sender{Q: db.Queries, Cfg: cfg, Renderer: renderer, Mailer: mailer}
			issue, err := sender.SnapshotIssue(cmd.Context(), iss)
			if err != nil {
				if errors.Is(err, newsletter.ErrIssueSent) {
					return fmt.Errorf("%s: nothing to do", newsletter.ErrIssueSent)
				}
				return err
			}
			stats, err := sender.SendAll(cmd.Context(), issue)
			if err != nil {
				return err
			}
			fmt.Printf("%s\ncreated %d delivery row(s); sent %d; failed %d; parked %d; total %d\n",
				summary, stats.Created, stats.Sent, stats.Failed, stats.Parked, stats.Total)
			if stats.Failed > 0 {
				fmt.Println("failed deliveries stay pending — re-run send to retry")
			}
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "render and show the plan without touching the database")
	cmd.Flags().Bool("yes", false, "skip the confirmation prompt")
	return cmd
}

func newsletterSubscribersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subscribers",
		Short: "List or count newsletter subscribers",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use: "list",
			RunE: func(cmd *cobra.Command, args []string) error {
				subs, err := db.Queries.ListSubscribers(cmd.Context())
				if err != nil {
					return err
				}
				if len(subs) == 0 {
					fmt.Println("no subscribers")
					return nil
				}
				for _, s := range subs {
					row := fmt.Sprintf("%-13s %-32s source=%q created=%s",
						s.Status, s.Email, s.Source, s.CreatedAt.Time.Format("2006-01-02"))
					if s.ConfirmedAt.Valid {
						row += " confirmed=" + s.ConfirmedAt.Time.Format("2006-01-02")
					}
					if s.UnsubscribedAt.Valid {
						row += " unsub=" + s.UnsubscribedAt.Time.Format("2006-01-02")
					}
					fmt.Println(row)
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "count",
			RunE: func(cmd *cobra.Command, args []string) error {
				rows, err := db.Queries.CountSubscribersByStatus(cmd.Context())
				if err != nil {
					return err
				}
				total := 0
				for _, r := range rows {
					fmt.Printf("%-13s %d\n", r.Status, r.N)
					total += int(r.N)
				}
				fmt.Printf("%-13s %d\n", "total", total)
				return nil
			},
		},
	)
	return cmd
}

func newsletterIssuesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "issues",
		Short: "List newsletter issues and their status",
		RunE: func(cmd *cobra.Command, args []string) error {
			issues, err := db.Queries.ListIssues(cmd.Context())
			if err != nil {
				return err
			}
			if len(issues) == 0 {
				fmt.Println("no issues")
				return nil
			}
			for _, i := range issues {
				line := fmt.Sprintf("%-14s %-7s %q", i.Slug, i.Status, i.Subject)
				if i.SentAt.Valid {
					line += " sent=" + i.SentAt.Time.Format("2006-01-02 15:04")
				}
				fmt.Println(line)
			}
			return nil
		},
	}
}

// --- shared helpers -----------------------------------------------------

// parseIssueFile reads and parses an issue markdown file; the slug is the
// file name without extension.
func parseIssueFile(path string) (*newsletter.Issue, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	slug := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	iss, err := newsletter.ParseIssue(slug, string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return iss, nil
}

// renderIssueFile resolves directives against the DB and renders both
// parts, substituting the given unsubscribe URL for the placeholder.
func renderIssueFile(cmd *cobra.Command, path, unsubURL string) (html, text, slug string, err error) {
	cfg, err := newsletter.LoadConfigEnv()
	if err != nil {
		return "", "", "", err
	}
	iss, err := parseIssueFile(path)
	if err != nil {
		return "", "", "", err
	}
	doc, err := (&newsletter.Renderer{Q: db.Queries, Cfg: cfg}).Resolve(cmd.Context(), iss)
	if err != nil {
		return "", "", "", err
	}
	if html, err = viewemail.RenderHTML(doc); err != nil {
		return "", "", "", err
	}
	text = viewemail.Text(doc)
	html = strings.ReplaceAll(html, newsletter.PlaceholderUnsubURL, unsubURL)
	text = strings.ReplaceAll(text, newsletter.PlaceholderUnsubURL, unsubURL)
	return html, text, iss.Slug, nil
}

func liveOrInert(unsub string) string {
	if unsub == previewUnsubURL {
		return "inert placeholder — address is not a subscriber"
	}
	return "live"
}

func confirmPrompt(prompt string) bool {
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	switch strings.TrimSpace(line) {
	case "y", "Y", "yes":
		return true
	}
	return false
}
