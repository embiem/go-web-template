package newsletter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/util"
	"github.com/embiem/indie-game-gems/view"
	viewemail "github.com/embiem/indie-game-gems/view/email"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PlaceholderUnsubURL is stored inside issue snapshots (issues.html/text)
// and swapped for the recipient's real link at delivery time. Keeping the
// snapshot recipient-independent is what makes the archive shareable.
const PlaceholderUnsubURL = "{{unsubscribe_url}}"

// minTopScore is the gem_score floor for {{top}} blocks.
const minTopScore = 75

// Renderer resolves parsed issue directives against the database into
// view/email documents. Pure rendering happens in view/email; this type is
// the DB join.
type Renderer struct {
	Q *data.Queries
	// Cfg supplies BASE_URL-derived links and the postal address.
	Cfg Config
	// Now is consulted for {{gotd}} without an argument (default: latest
	// pick on/before today). Injectable for tests.
	Now func() time.Time
}

// Directive resolution errors — preview-time, never send-time (send refuses
// to run when Resolve fails).
var (
	// ErrNoData marks directives that parse but match nothing (unknown game
	// slug, empty week, no pick yet).
	ErrNoData = errors.New("directive matches no data")
)

// Resolve builds the email document for an issue.
func (r *Renderer) Resolve(ctx context.Context, iss *Issue) (viewemail.Doc, error) {
	var doc viewemail.Doc
	doc.Subject = iss.Subject
	doc.Preheader = iss.Preheader
	doc.Footer = viewemail.Footer{
		HomeURL:       r.Cfg.BaseURL,
		BrowserURL:    r.Cfg.BaseURL + "/newsletter/issues/" + iss.Slug,
		UnsubURL:      PlaceholderUnsubURL,
		PostalAddress: r.Cfg.PostalAddress,
		Reason: "You are getting this because you subscribed at " +
			r.Cfg.BaseURL + "/newsletter.",
	}
	for _, seg := range iss.Segments {
		var blk viewemail.Block
		var err error
		switch {
		case seg.Prose != "":
			blk = viewemail.Block{ProseHTML: viewemail.Prose(seg.Prose), ProseText: viewemail.ProseText(seg.Prose)}
		case seg.Directive.Kind == KindGotd:
			blk, err = r.gotdBlock(ctx, seg.Directive.Arg)
		case seg.Directive.Kind == KindRadar:
			blk, err = r.radarBlock(ctx, seg.Directive.Arg)
		case seg.Directive.Kind == KindTop:
			blk, err = r.topBlock(ctx, seg.Directive.Arg)
		case seg.Directive.Kind == KindGame:
			blk, err = r.gameBlock(ctx, seg.Directive.Arg)
		default:
			err = fmt.Errorf("unknown directive %+v", seg.Directive)
		}
		if err != nil {
			return viewemail.Doc{}, fmt.Errorf("line %d: %w", seg.Directive.Line, err)
		}
		doc.Blocks = append(doc.Blocks, blk)
	}
	return doc, nil
}

// gotdBlock resolves Game of the Day: latest pick on/before the arg date
// (default today).
func (r *Renderer) gotdBlock(ctx context.Context, arg string) (viewemail.Block, error) {
	// UTC midnight of "today": Truncate works on absolute time, so without
	// .UTC() a local zone west of UTC would encode yesterday's date.
	date := util.TodayUTC(r.now())
	if arg != "" {
		t, err := ParseDate(arg)
		if err != nil {
			return viewemail.Block{}, err // unreachable: parse validates earlier
		}
		date = t
	}
	row, err := r.Q.GetPickOnOrBeforeWithDate(ctx, pgtype.Date{Time: date, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return viewemail.Block{}, fmt.Errorf("%w: no Game of the Day on or before %s (run: gems pick set %s <game-slug>)",
				ErrNoData, date.Format("2006-01-02"), date.Format("2006-01-02"))
		}
		return viewemail.Block{}, err
	}
	card, err := r.gameCard(ctx, row.GameCard)
	if err != nil {
		return viewemail.Block{}, err
	}
	return viewemail.Block{Gotd: &viewemail.Gotd{
		Heading:  "Game of the Day — " + row.PickDate.Time.Format("January 2"),
		Game:     card,
		NoteHTML: viewemail.Prose(row.NoteMd),
		NoteText: viewemail.ProseText(row.NoteMd),
	}}, nil
}

func (r *Renderer) radarBlock(ctx context.Context, arg string) (viewemail.Block, error) {
	start, end, err := ParseISOWeek(arg)
	if err != nil {
		return viewemail.Block{}, err
	}
	cards, err := r.Q.ListGamesReleasedBetween(ctx, data.ListGamesReleasedBetweenParams{
		ReleaseDate:   pgtype.Date{Time: start, Valid: true},
		ReleaseDate_2: pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		return viewemail.Block{}, err
	}
	if len(cards) == 0 {
		return viewemail.Block{}, fmt.Errorf("%w: no releases between %s and %s — fix the week or drop the block",
			ErrNoData, start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	items, err := r.gameCards(ctx, cards)
	if err != nil {
		return viewemail.Block{}, err
	}
	return viewemail.Block{Radar: &viewemail.Section{
		Heading: "Release Radar — " + arg,
		Items:   items,
	}}, nil
}

func (r *Renderer) topBlock(ctx context.Context, arg string) (viewemail.Block, error) {
	start, end, err := ParseMonth(arg)
	if err != nil {
		return viewemail.Block{}, err
	}
	cards, err := r.Q.ListTopGamesBetween(ctx, data.ListTopGamesBetweenParams{
		ReleaseDate:   pgtype.Date{Time: start, Valid: true},
		ReleaseDate_2: pgtype.Date{Time: end, Valid: true},
		GemScore:      pgtype.Float4{Float32: minTopScore, Valid: true},
	})
	if err != nil {
		return viewemail.Block{}, err
	}
	if len(cards) == 0 {
		return viewemail.Block{}, fmt.Errorf("%w: no games scored ≥%d released in %s",
			ErrNoData, minTopScore, arg)
	}
	items, err := r.gameCards(ctx, cards)
	if err != nil {
		return viewemail.Block{}, err
	}
	month := start.Format("January 2006")
	return viewemail.Block{Top: &viewemail.Section{Heading: "Top of the Month — " + month, Items: items}}, nil
}

func (r *Renderer) gameBlock(ctx context.Context, slug string) (viewemail.Block, error) {
	card, err := r.Q.GetGameCardBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return viewemail.Block{}, fmt.Errorf("%w: unknown game slug %q", ErrNoData, slug)
		}
		return viewemail.Block{}, err
	}
	c, err := r.gameCard(ctx, card)
	if err != nil {
		return viewemail.Block{}, err
	}
	return viewemail.Block{Game: &c}, nil
}

// gameCard enriches a game_cards row with store buttons, hero image and
// display lines.
func (r *Renderer) gameCard(ctx context.Context, c data.GameCard) (viewemail.GameCard, error) {
	links, err := r.Q.ListStoreLinks(ctx, c.ID)
	if err != nil {
		return viewemail.GameCard{}, err
	}
	stores := storeButtons(links)
	img := r.gameImageURL(c)
	return viewemail.GameCard{
		Title:       c.Title,
		Tagline:     c.Tagline,
		Developer:   c.DeveloperName,
		GameURL:     r.Cfg.GameURL(c.Slug),
		ImageURL:    img,
		ReleaseLine: releaseLine(c),
		ScoreLine:   scoreLine(c),
		Stores:      stores,
	}, nil
}

func (r *Renderer) gameCards(ctx context.Context, cards []data.GameCard) ([]viewemail.GameCard, error) {
	out := make([]viewemail.GameCard, 0, len(cards))
	for _, c := range cards {
		g, err := r.gameCard(ctx, c)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// storeButtons maps DB store links to labeled buttons in a fixed order.
func storeButtons(links []data.StoreLink) []viewemail.StoreButton {
	label := map[string]string{
		"steam": "Steam", "gog": "GOG", "epic": "Epic",
		"itch": "itch.io", "humble": "Humble", "direct": "Website",
	}
	order := []string{"steam", "gog", "epic", "itch", "humble", "direct"}
	url := make(map[string]string, len(links))
	for _, l := range links {
		url[l.Store] = l.Url
	}
	var out []viewemail.StoreButton
	for _, s := range order {
		if u, ok := url[s]; ok {
			out = append(out, viewemail.StoreButton{Label: label[s], URL: u})
		}
	}
	return out
}

// gameImageURL resolves the card's hero image to an absolute URL:
// BASE_URL + view.MediaURL(key). Empty when the game has no cached image —
// the design reads fine without it (clients block images by default).
func (r *Renderer) gameImageURL(c data.GameCard) string {
	if c.MediaKey == "" {
		return ""
	}
	return strings.TrimSuffix(r.Cfg.BaseURL, "/") + view.MediaURL(c.MediaKey)
}

// releaseLine renders the human release descriptor.
func releaseLine(c data.GameCard) string {
	label := map[string]string{
		"released": "Released", "early_access": "Early access",
		"upcoming": "Upcoming", "announced": "Announced",
	}[c.ReleaseStatus]
	if label == "" {
		label = c.ReleaseStatus
	}
	if c.ReleaseDate.Valid && c.ReleaseStatus != "announced" {
		label += " " + c.ReleaseDate.Time.Format("Jan 2, 2006")
	}
	return label
}

func scoreLine(c data.GameCard) string {
	if c.GemScore.Valid {
		return fmt.Sprintf("Gem score %d", int(c.GemScore.Float32+0.5))
	}
	return ""
}

func (r *Renderer) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
