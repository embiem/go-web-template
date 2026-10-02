package igdb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// gameFields is the Apicalypse field list for game queries (research §5b).
const gameFields = `fields name,slug,summary,first_release_date,game_type,game_status,
 cover.image_id, screenshots.image_id, videos.video_id, videos.name,
 websites.type, websites.url, websites.trusted,
 involved_companies.company.id, involved_companies.company.name, involved_companies.company.slug,
 involved_companies.company.description, involved_companies.company.logo.image_id,
 involved_companies.company.websites.type, involved_companies.company.websites.url,
 involved_companies.developer, involved_companies.publisher,
 external_games.uid, external_games.external_game_source, external_games.url;`

// companyFields is the field list for company queries.
const companyFields = `fields name,slug,description,
 websites.type, websites.url, logo.image_id;`

// GameBySteamAppID finds the IGDB game for a Steam appid via external_games
// (two-step, per docs), then fetches the full game record.
func (c *Client) GameBySteamAppID(ctx context.Context, appid int64) (*Game, error) {
	uid := SteamUID(appid)
	var ext []ExternalGame
	err := c.Query(ctx, "external_games",
		fmt.Sprintf(`fields game; where external_game_source = %d & uid = %q;`, SourceSteam, uid), &ext)
	if err != nil {
		return nil, fmt.Errorf("igdb external_games lookup for steam %d: %w", appid, err)
	}
	if len(ext) == 0 {
		return nil, fmt.Errorf("no IGDB game for steam appid %d", appid)
	}
	var games []*Game
	q := gameFields + ` where id = ` + strconv.FormatInt(ext[0].Game, 10) + `;`
	if err := c.Query(ctx, "games", q, &games); err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("igdb game %d vanished", ext[0].Game)
	}
	return games[0], nil
}

// Game fetches a full game record by IGDB id.
func (c *Client) Game(ctx context.Context, id int64) (*Game, error) {
	var games []*Game
	q := gameFields + ` where id = ` + strconv.FormatInt(id, 10) + `;`
	if err := c.Query(ctx, "games", q, &games); err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("igdb game %d not found", id)
	}
	return games[0], nil
}

// Company fetches a company by IGDB id.
func (c *Client) Company(ctx context.Context, id int64) (*Company, error) {
	var companies []*Company
	q := companyFields + ` where id = ` + strconv.FormatInt(id, 10) + `;`
	if err := c.Query(ctx, "companies", q, &companies); err != nil {
		return nil, err
	}
	if len(companies) == 0 {
		return nil, fmt.Errorf("igdb company %d not found", id)
	}
	return companies[0], nil
}

// CompanyCatalogue returns the main games a company developed, excluding
// DLC/add-ons, bundles, episodes, mods and cancelled projects. Paginates via
// offset at the documented max limit of 500 (catalogues are smaller in
// practice; the loop stops on a short page).
func (c *Client) CompanyCatalogue(ctx context.Context, companyID int64) ([]*Game, error) {
	excludedTypes := []int{1, 2, 3, 5, 6, 7, 13, 14} // dlc, expansion, bundle, mod, episode, season, pack, update
	where := fmt.Sprintf(
		`where involved_companies.company = %d & involved_companies.developer = true & game_type != (%s) & (game_status = null | game_status != %d);`,
		companyID, intList(excludedTypes), GameStatusCancelled)

	var all []*Game
	const limit = 500
	for offset := 0; ; offset += limit {
		var games []*Game
		q := fmt.Sprintf("fields name,slug,summary,first_release_date,game_type,game_status,cover.image_id;\n%s\nsort first_release_date desc; limit %d; offset %d;",
			where, limit, offset)
		if err := c.Query(ctx, "games", q, &games); err != nil {
			return nil, err
		}
		all = append(all, games...)
		if len(games) < limit {
			return all, nil
		}
	}
}

func intList(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}
