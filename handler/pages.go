package handler

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/db"
	"github.com/embiem/indie-game-gems/util"
	"github.com/embiem/indie-game-gems/view"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// todayUTC returns today's date at midnight UTC (site-wide convention).
func todayUTC() time.Time {
	return util.TodayUTC(time.Now())
}

// dateParam converts a time.Time into the pgtype.Date the queries expect.
func dateParam(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true}
}

// notFound renders the themed 404 page with the right status.
func notFound(w http.ResponseWriter, r *http.Request) error {
	w.WriteHeader(http.StatusNotFound)
	return view.NotFoundPage().Render(r.Context(), w)
}

// GetNotFoundPage is wired as router.NotFound.
func GetNotFoundPage(w http.ResponseWriter, r *http.Request) error {
	return notFound(w, r)
}

// ---------------------------------------------------------------------------
// Home

func GetHomePage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	today := todayUTC()

	// Game of the Day feature: today's pick, falling back to the latest
	// earlier one (picks are scheduled ahead, so "today" can be empty).
	feature, err := latestPickFeature(ctx, today)
	if err != nil {
		return err
	}

	// Weekly Release Radar: this ISO week (Monday..Sunday, UTC).
	radarMonday, err := weekStart(today)
	if err != nil {
		return err
	}
	radarCards, err := db.Queries.ListGamesReleasedBetween(ctx, data.ListGamesReleasedBetweenParams{
		ReleaseDate: dateParam(radarMonday),
		// Half-open [monday, monday+7): next Monday's releases belong to
		// next week, so they also don't double up under "Coming up".
		ReleaseDate_2: dateParam(radarMonday.AddDate(0, 0, 7)),
	})
	if err != nil {
		return err
	}

	// A week with fewer than 3 releases looks bare, so pad the section with
	// the next upcoming releases ("Coming up"), falling back to the most
	// recent ones ("Recently released"). Never both — one honest filler.
	var radarUpcoming, radarRecent []data.GameCard
	if len(radarCards) < 3 {
		fill := int32(3 - len(radarCards))
		radarUpcoming, err = db.Queries.ListUpcomingGames(ctx, data.ListUpcomingGamesParams{
			ReleaseDate: dateParam(radarMonday.AddDate(0, 0, 7)),
			Limit:       fill,
		})
		if err != nil {
			return err
		}
		if len(radarUpcoming) == 0 {
			radarRecent, err = db.Queries.ListRecentlyReleased(ctx, data.ListRecentlyReleasedParams{
				ReleaseDate: dateParam(radarMonday),
				Limit:       fill,
			})
			if err != nil {
				return err
			}
		}
	}

	// Top of the Month: the current month once it has 3+ games, otherwise
	// the previous one — always labelled truthfully.
	top, err := homeTopSection(ctx, today)
	if err != nil {
		return err
	}

	gems, err := db.Queries.ListCuratedGems(ctx, 8)
	if err != nil {
		return err
	}

	return view.HomePage(view.HomePageData{
		Meta: view.PageMeta{
			Title:       "Discover great indie games you missed",
			Description: "One curated indie gem a day, a weekly release radar and monthly tops — the alternative to the Steam front page.",
			Canonical:   "/",
		},
		Pick:  feature,
		Radar: view.RadarSection{Label: util.FormatISOWeekLabel(radarMonday), Range: weekRangeLabel(radarMonday), Cards: radarCards, Upcoming: radarUpcoming, Recent: radarRecent},
		Top:   top,
		Gems:  gems,
	}).Render(ctx, w)
}

// latestPickFeature loads the pick for `date`, or the latest earlier pick
// when `date` itself has none. Returns nil when no pick exists at all.
func latestPickFeature(ctx context.Context, date time.Time) (*view.PickFeature, error) {
	card, note, pickDate, err := pickForPage(ctx, date, date)
	if errors.Is(err, errNoPickOnDate) || errors.Is(err, errNoPicksYet) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// One-click store buttons are core; the home hero shows them like the
	// /game-of-the-day page does. A pick survives its game being deleted.
	var links []data.StoreLink
	game, err := db.Queries.GetGameBySlug(ctx, card.Slug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if links, err = db.Queries.ListStoreLinks(ctx, game.ID); err != nil {
			return nil, err
		}
	}
	return &view.PickFeature{
		Card:     card,
		NoteMd:   note,
		PickDate: pickDate,
		Stores:   view.StoreButtons(links, card.ReleaseStatus),
	}, nil
}

// homeTopSection builds the home "Top of the Month" section: current month if
// it has at least 3 scored releases, else the previous month.
func homeTopSection(ctx context.Context, today time.Time) (view.TopSection, error) {
	year, month := today.Year(), int(today.Month())
	if n, err := db.Queries.CountGamesReleasedBetween(ctx, monthRangeParams(year, month)); err != nil {
		return view.TopSection{}, err
	} else if n < 3 {
		year, month = util.ShiftMonth(year, month, -1)
	}
	first, next := util.MonthRange(year, month)
	cards, err := db.Queries.ListTopGames(ctx, data.ListTopGamesParams{
		ReleaseDate:   dateParam(first),
		ReleaseDate_2: dateParam(next),
		Limit:         5,
	})
	if err != nil {
		return view.TopSection{}, err
	}
	return view.TopSection{
		Label: util.FormatMonthLabel(year, month),
		Href:  monthURL(year, month),
		Cards: cards,
	}, nil
}

func monthRangeParams(year, month int) data.CountGamesReleasedBetweenParams {
	first, next := util.MonthRange(year, month)
	return data.CountGamesReleasedBetweenParams{ReleaseDate: dateParam(first), ReleaseDate_2: dateParam(next)}
}

// monthURL renders "/top/2026/09" style month paths.
func monthURL(year, month int) string {
	return fmt.Sprintf("/top/%04d/%02d", year, month)
}

// ---------------------------------------------------------------------------
// Game page

func GetGamePage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	slug := chi.URLParam(r, "slug")

	game, err := db.Queries.GetGameBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound(w, r)
	}
	if err != nil {
		return err
	}

	companies, err := db.Queries.ListGameDevelopers(ctx, game.ID)
	if err != nil {
		return err
	}
	links, err := db.Queries.ListStoreLinks(ctx, game.ID)
	if err != nil {
		return err
	}
	media, err := db.Queries.ListGameMedia(ctx, game.ID)
	if err != nil {
		return err
	}
	tags, err := db.Queries.ListGameTags(ctx, game.ID)
	if err != nil {
		return err
	}

	var developers, publishers []data.ListGameDevelopersRow
	primaryDevID := pgtype.UUID{}
	for _, c := range companies {
		switch c.Role {
		case "developer":
			developers = append(developers, c)
			if !primaryDevID.Valid {
				primaryDevID = c.ID
			}
		case "publisher":
			publishers = append(publishers, c)
		}
	}

	var moreFrom []data.GameCard
	moreFromName := ""
	if primaryDevID.Valid {
		moreFrom, err = db.Queries.ListGamesByDeveloper(ctx, data.ListGamesByDeveloperParams{
			DeveloperID: primaryDevID,
			ID:          game.ID,
		})
		if err != nil {
			return err
		}
		for _, c := range developers {
			if c.ID == primaryDevID {
				moreFromName = c.Name
			}
		}
	}

	d := view.GamePageData{
		Meta: view.PageMeta{
			Title:       game.Title,
			Description: pageDescription(game.Tagline, game.GemNoteMd),
			Canonical:   "/games/" + game.Slug,
			OGImage:     absMediaURL(heroURL(media)),
		},
		Game:        game,
		Developers:  developers,
		Publishers:  publishers,
		Stores:      view.StoreButtons(links, game.ReleaseStatus),
		HeroURL:     heroURL(media),
		Screenshots: screenshots(media),
		TrailerID:   game.TrailerYoutubeID.String,
		TrailerURL:  trailerPosterURL(media, heroURL(media)),
		Tags:        tags,
		MoreFrom:    moreFrom,
		MoreFromDev: moreFromName,
		JSONLD:      gameJSONLD(game, developers, heroURL(media), tags),
	}
	return view.GamePage(d).Render(ctx, w)
}

// heroURL picks the hero image, falling back to the header ("" when neither
// exists — the views render gradient tiles instead).
func heroURL(media []data.Medium) string {
	var header string
	for _, m := range media {
		switch m.Kind {
		case "hero":
			return view.MediaURL(m.Key)
		case "header":
			header = m.Key
		}
	}
	if header != "" {
		return view.MediaURL(header)
	}
	return ""
}

// screenshots returns screenshot media ordered by position.
func screenshots(media []data.Medium) []data.Medium {
	var out []data.Medium
	for _, m := range media {
		if m.Kind == "screenshot" {
			out = append(out, m)
		}
	}
	return out
}

// trailerPosterURL prefers a dedicated trailer poster, else the hero art.
func trailerPosterURL(media []data.Medium, fallback string) string {
	for _, m := range media {
		if m.Kind == "trailer" {
			return view.MediaURL(m.Key)
		}
	}
	return fallback
}

// absMediaURL turns a site-relative media URL into an absolute one for
// social previews ("" stays "").
func absMediaURL(url string) string {
	if url == "" {
		return ""
	}
	return view.AbsURL(url)
}

// pageDescription prefers the tagline, then the first sentence/paragraph of
// the gem note.
func pageDescription(tagline, gemNote string) string {
	if tagline != "" {
		return tagline
	}
	if gemNote != "" {
		if i := strings.Index(gemNote, "\n\n"); i > 0 {
			return gemNote[:i]
		}
		if i := strings.Index(gemNote, ". "); i > 0 {
			return gemNote[:i+1]
		}
		return gemNote
	}
	return ""
}

// gameJSONLD builds the VideoGame structured data for the game page.
func gameJSONLD(game data.Game, developers []data.ListGameDevelopersRow, heroURL string, tags []data.Tag) map[string]any {
	ld := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "VideoGame",
		"name":        game.Title,
		"url":         view.AbsURL("/games/" + game.Slug),
		"description": pageDescription(game.Tagline, game.GemNoteMd),
	}
	if heroURL != "" {
		ld["image"] = absMediaURL(heroURL)
	}
	if game.ReleaseDate.Valid {
		ld["datePublished"] = game.ReleaseDate.Time.Format("2006-01-02")
	}
	var devNames, genre []any
	for _, d := range developers {
		if d.Role == "developer" {
			devNames = append(devNames, map[string]any{"@type": "Organization", "name": d.Name})
		}
	}
	if devNames != nil {
		ld["author"] = devNames
	}
	for _, t := range tags {
		genre = append(genre, t.Name)
	}
	if genre != nil {
		ld["genre"] = genre
	}
	if game.GemScore.Valid {
		note := game.GemNoteMd
		if len(note) > 280 {
			note = note[:280]
		}
		ld["review"] = map[string]any{
			"@type":        "Review",
			"author":       map[string]any{"@type": "Organization", "name": "Indie Game Gems"},
			"reviewBody":   note,
			"reviewRating": map[string]any{"@type": "Rating", "ratingValue": fmt.Sprintf("%.0f", game.GemScore.Float32), "bestRating": "100", "worstRating": "0"},
		}
	}
	return ld
}

// ---------------------------------------------------------------------------
// Developer page

func GetDeveloperPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	slug := chi.URLParam(r, "slug")

	dev, err := db.Queries.GetDeveloperBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound(w, r)
	}
	if err != nil {
		return err
	}

	games, err := db.Queries.ListDeveloperGames(ctx, dev.ID)
	if err != nil {
		return err
	}
	sortDeveloperGames(games)

	return view.DeveloperPage(view.DeveloperPageData{
		Meta: view.PageMeta{
			Title:       dev.Name,
			Description: firstLine(dev.BioMd, dev.Name+" — games, biography and store links on Indie Game Gems."),
			Canonical:   "/developers/" + dev.Slug,
		},
		Developer: dev,
		Socials:   view.DeveloperSocials(dev.Socials),
		Games:     games,
	}).Render(ctx, w)
}

// firstLine returns the first non-empty line of a markdown blob.
func firstLine(md, fallback string) string {
	for _, line := range strings.Split(strings.TrimSpace(md), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Game of the Day

func GetGameOfTheDayPage(w http.ResponseWriter, r *http.Request) error {
	return renderPickPage(w, r, todayUTC())
}

// GetGameOfTheDayDatePage serves /game-of-the-day/{YYYY-MM-DD}. Dates in the
// future always 404: picks are scheduled ahead and must stay embargoed.
func GetGameOfTheDayDatePage(w http.ResponseWriter, r *http.Request) error {
	date, err := util.ParseDate(chi.URLParam(r, "date"))
	if err != nil {
		return notFound(w, r)
	}
	if date.After(todayUTC()) {
		return notFound(w, r)
	}
	return renderPickPage(w, r, date)
}

func renderPickPage(w http.ResponseWriter, r *http.Request, date time.Time) error {
	ctx := r.Context()
	today := todayUTC()

	card, note, pickDate, err := pickForPage(ctx, date, today)
	if errors.Is(err, errNoPickOnDate) || errors.Is(err, errNoPicksYet) {
		// Future/unknown dates 404; "no picks at all" gets the same friendly page.
		w.WriteHeader(http.StatusNotFound)
		return view.NotFoundPage().Render(ctx, w)
	}
	if err != nil {
		return err
	}

	var links []data.StoreLink
	game, err := db.Queries.GetGameBySlug(ctx, card.Slug)
	switch {
	case errors.Is(err, pgx.ErrNoRows): // pick survives while the game is gone
	case err != nil:
		return err
	default:
		if links, err = db.Queries.ListStoreLinks(ctx, game.ID); err != nil {
			return err
		}
	}

	pick := view.PickFeature{
		Card:     card,
		PickDate: pickDate,
		NoteMd:   note,
		Stores:   view.StoreButtons(links, card.ReleaseStatus),
	}

	var prevURL, nextURL string
	prev, err := db.Queries.GetPreviousPickDate(ctx, dateParam(pickDate))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if prev.Valid {
		prevURL = "/game-of-the-day/" + prev.Time.Format("2006-01-02")
	}
	next, err := db.Queries.GetNextPickDate(ctx, dateParam(pickDate))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	// Never surface scheduled future picks in the navigation.
	if next.Valid && !next.Time.After(today) {
		nextURL = "/game-of-the-day/" + next.Time.Format("2006-01-02")
	}

	archive, err := db.Queries.ListRecentPicks(ctx, data.ListRecentPicksParams{
		PickDate: dateParam(today),
		Limit:    10,
	})
	if err != nil {
		return err
	}

	return view.PickPage(view.PickPageData{
		Meta: view.PageMeta{
			Title:       "Game of the Day — " + card.Title,
			Description: pageDescription(card.Tagline, note),
			Canonical:   "/game-of-the-day/" + pickDate.Format("2006-01-02"),
			OGImage:     absMediaURL(mediaKeyURL(card.MediaKey)),
		},
		Pick:    pick,
		Prev:    prevURL,
		Next:    nextURL,
		Archive: archive,
	}).Render(ctx, w)
}

// Sentinel errors for the pick lookup paths.
var (
	errNoPickOnDate = errors.New("no pick on this date")
	errNoPicksYet   = errors.New("no picks published at all")
)

// sortDeveloperGames orders a developer's games newest-first, dated releases
// before undated ones, title as tiebreak.
func sortDeveloperGames(games []data.GameCard) {
	sort.SliceStable(games, func(i, j int) bool {
		a, b := games[i].ReleaseDate, games[j].ReleaseDate
		switch {
		case a.Valid && b.Valid && !a.Time.Equal(b.Time):
			return a.Time.After(b.Time)
		case a.Valid != b.Valid:
			return a.Valid
		default:
			return games[i].Title < games[j].Title
		}
	})
}

// pickForPage returns the pick for the requested date. Only the undated
// "today" route falls back to the latest earlier pick (picks are scheduled
// ahead, so today can legitimately have none); it reports the date the
// fallback actually landed on.
func pickForPage(ctx context.Context, date, today time.Time) (data.GameCard, string, time.Time, error) {
	row, err := db.Queries.GetDailyPick(ctx, dateParam(date))
	if err == nil {
		return row.GameCard, row.NoteMd, date, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return data.GameCard{}, "", time.Time{}, err
	}
	if !date.Equal(today) {
		return data.GameCard{}, "", time.Time{}, errNoPickOnDate
	}
	latestDate, err := db.Queries.GetLatestPickDateOnOrBefore(ctx, dateParam(today))
	if err != nil {
		return data.GameCard{}, "", time.Time{}, err
	}
	// MAX over an empty set returns one NULL row (no ErrNoRows), so the
	// validity flag is the real "no picks yet" signal.
	if !latestDate.Valid {
		return data.GameCard{}, "", time.Time{}, errNoPicksYet
	}
	fallback, err := db.Queries.GetDailyPick(ctx, latestDate)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return data.GameCard{}, "", time.Time{}, errNoPicksYet
		}
		return data.GameCard{}, "", time.Time{}, err
	}
	return fallback.GameCard, fallback.NoteMd, latestDate.Time, nil
}

func mediaKeyURL(key string) string {
	if key == "" {
		return ""
	}
	return view.MediaURL(key)
}

// ---------------------------------------------------------------------------
// Release Radar

func GetReleaseRadarPage(w http.ResponseWriter, r *http.Request) error {
	monday, err := weekStart(todayUTC())
	if err != nil {
		return err
	}
	return renderRadarPage(w, r, monday)
}

// GetReleaseRadarWeekPage serves /release-radar/{YYYY}-W{WW}; anything that
// is not a valid ISO week (including week 53 in 52-week years) is a 404.
func GetReleaseRadarWeekPage(w http.ResponseWriter, r *http.Request) error {
	monday, err := util.ParseISOWeek(chi.URLParam(r, "week"))
	if err != nil {
		return notFound(w, r)
	}
	return renderRadarPage(w, r, monday)
}

func renderRadarPage(w http.ResponseWriter, r *http.Request, monday time.Time) error {
	ctx := r.Context()

	cards, err := db.Queries.ListGamesReleasedBetween(ctx, data.ListGamesReleasedBetweenParams{
		ReleaseDate: dateParam(monday),
		// Half-open [monday, monday+7): next Monday leaks would be listed twice.
		ReleaseDate_2: dateParam(monday.AddDate(0, 0, 7)),
	})
	if err != nil {
		return err
	}

	days := groupByDay(cards, monday)

	return view.RadarPage(view.RadarPageData{
		Meta: view.PageMeta{
			Title:       "Release Radar " + util.FormatISOWeekLabel(monday),
			Description: "Every tracked indie release of " + weekRangeLabel(monday) + ", grouped by day.",
			Canonical:   "/release-radar/" + util.FormatISOWeekLabel(monday),
		},
		Label: util.FormatISOWeekLabel(monday),
		Range: weekRangeLabel(monday),
		Prev:  view.PagerLink{Label: util.FormatISOWeekLabel(monday.AddDate(0, 0, -7)), Href: "/release-radar/" + util.FormatISOWeekLabel(monday.AddDate(0, 0, -7))},
		Next:  view.PagerLink{Label: util.FormatISOWeekLabel(monday.AddDate(0, 0, 7)), Href: "/release-radar/" + util.FormatISOWeekLabel(monday.AddDate(0, 0, 7))},
		Days:  days,
	}).Render(ctx, w)
}

// groupByDay buckets week cards into per-day groups (Mon..Sun), skipping
// empty days.
func groupByDay(cards []data.GameCard, monday time.Time) []view.PickDay {
	byDay := map[int][]data.GameCard{}
	for _, c := range cards {
		if !c.ReleaseDate.Valid {
			continue
		}
		offset := int(c.ReleaseDate.Time.Sub(monday).Hours() / 24)
		if offset < 0 || offset > 6 {
			continue
		}
		byDay[offset] = append(byDay[offset], c)
	}
	days := make([]view.PickDay, 0, len(byDay))
	for i := range 7 {
		if cards, ok := byDay[i]; ok {
			days = append(days, view.PickDay{Date: monday.AddDate(0, 0, i), Cards: cards})
		}
	}
	return days
}

// weekStart returns the Monday 00:00 UTC starting the week containing t.
func weekStart(t time.Time) (time.Time, error) {
	year, week := t.ISOWeek()
	return util.ParseISOWeek(fmt.Sprintf("%04d-W%02d", year, week))
}

// weekRangeLabel renders "Sep 28 – Oct 4" (or same-month "Sep 1 – Sep 7").
func weekRangeLabel(monday time.Time) string {
	sunday := monday.AddDate(0, 0, 6)
	if monday.Month() == sunday.Month() {
		return monday.Format("Jan 2") + " – " + sunday.Format("2")
	}
	return monday.Format("Jan 2") + " – " + sunday.Format("Jan 2")
}

// ---------------------------------------------------------------------------
// Top of the Month

func GetTopPage(w http.ResponseWriter, r *http.Request) error {
	year, month, _ := todayUTC().Date()
	return renderTopPage(w, r, year, int(month))
}

// GetTopMonthPage serves /top/{YYYY}/{MM}; anything else is a 404.
func GetTopMonthPage(w http.ResponseWriter, r *http.Request) error {
	year, month, err := util.ParseYearMonth(chi.URLParam(r, "year"), chi.URLParam(r, "month"))
	if err != nil {
		return notFound(w, r)
	}
	return renderTopPage(w, r, year, month)
}

func renderTopPage(w http.ResponseWriter, r *http.Request, year, month int) error {
	ctx := r.Context()
	first, next := util.MonthRange(year, month)

	cards, err := db.Queries.ListTopGames(ctx, data.ListTopGamesParams{
		ReleaseDate:   dateParam(first),
		ReleaseDate_2: dateParam(next),
		Limit:         100,
	})
	if err != nil {
		return err
	}

	prevY, prevM := util.ShiftMonth(year, month, -1)
	nextY, nextM := util.ShiftMonth(year, month, 1)

	return view.TopPage(view.TopPageData{
		Meta: view.PageMeta{
			Title:       "Top of the Month — " + util.FormatMonthLabel(year, month),
			Description: "The best indie games released in " + util.FormatMonthLabel(year, month) + ", ranked by gem score.",
			Canonical:   monthURL(year, month),
		},
		Label: util.FormatMonthLabel(year, month),
		Prev:  view.PagerLink{Label: util.FormatMonthLabel(prevY, prevM), Href: monthURL(prevY, prevM)},
		Next:  view.PagerLink{Label: util.FormatMonthLabel(nextY, nextM), Href: monthURL(nextY, nextM)},
		Cards: cards,
	}).Render(ctx, w)
}

// ---------------------------------------------------------------------------
// Sitemap, robots, RSS feed

func GetSitemap(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	gameSlugs, err := db.Queries.ListAllGameSlugs(ctx)
	if err != nil {
		return err
	}
	devSlugs, err := db.Queries.ListAllDeveloperSlugs(ctx)
	if err != nil {
		return err
	}

	type sitemapURL struct {
		Loc string `xml:"loc"`
	}
	urlset := struct {
		XMLName   xml.Name     `xml:"urlset"`
		Namespace string       `xml:"xmlns,attr"`
		URLs      []sitemapURL `xml:"url"`
	}{
		Namespace: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:      []sitemapURL{},
	}
	for _, p := range []string{"/", "/game-of-the-day", "/release-radar", "/top"} {
		urlset.URLs = append(urlset.URLs, sitemapURL{Loc: view.AbsURL(p)})
	}
	for _, s := range gameSlugs {
		urlset.URLs = append(urlset.URLs, sitemapURL{Loc: view.AbsURL("/games/" + s)})
	}
	for _, s := range devSlugs {
		urlset.URLs = append(urlset.URLs, sitemapURL{Loc: view.AbsURL("/developers/" + s)})
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(xml.Header))
	if err != nil {
		return err
	}
	return xml.NewEncoder(w).Encode(urlset)
}

func GetRobots(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := fmt.Fprintf(w, "User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n", view.BaseURL())
	return err
}

// feedItem is one RSS entry; encoding/xml escapes the HTML description,
// which is valid RSS that readers unescape.
type feedItem struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        string   `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
	Description string   `xml:"description"`
}

func GetFeed(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	picks, err := db.Queries.ListRecentPicks(ctx, data.ListRecentPicksParams{
		PickDate: dateParam(todayUTC()),
		Limit:    20,
	})
	if err != nil {
		return err
	}

	items := make([]feedItem, 0, len(picks))
	for _, p := range picks {
		link := view.AbsURL("/game-of-the-day/" + p.PickDate.Time.Format("2006-01-02"))
		description := p.NoteMd
		if description == "" {
			description = p.GameCard.Tagline
		}
		items = append(items, feedItem{
			Title:       fmt.Sprintf("%s — %s", p.GameCard.Title, p.PickDate.Time.Format("Jan 2, 2006")),
			Link:        link,
			GUID:        link,
			PubDate:     p.PickDate.Time.Format(time.RFC1123Z),
			Description: string(util.MdToHTML(description)),
		})
	}

	feed := struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel struct {
			Title       string     `xml:"title"`
			Link        string     `xml:"link"`
			Description string     `xml:"description"`
			Language    string     `xml:"language"`
			LastBuild   string     `xml:"lastBuildDate"`
			Items       []feedItem `xml:"item"`
		} `xml:"channel"`
	}{Version: "2.0"}
	feed.Channel.Title = "Indie Game Gems — Game of the Day"
	feed.Channel.Link = view.AbsURL("/game-of-the-day")
	feed.Channel.Description = "One curated indie gem every day."
	feed.Channel.Language = "en"
	feed.Channel.Items = items
	if len(picks) > 0 {
		feed.Channel.LastBuild = picks[0].PickDate.Time.Format(time.RFC1123Z)
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(xml.Header))
	if err != nil {
		return err
	}
	return xml.NewEncoder(w).Encode(feed)
}
