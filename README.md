# Indie Game Gems

A small, curated site for discovering great indie games outside the big-store
algorithm: one hand-picked **Game of the Day**, a **Weekly Release Radar**,
**Top of the Month**, per-game and per-developer pages with one-click store
buttons (Steam, GOG, Epic, itch.io, Humble, direct), and a weekly **email
newsletter**.

Server-rendered with Go ([chi](https://github.com/go-chi/chi),
[templ](https://templ.guide)), PostgreSQL via [sqlc](https://sqlc.dev), Tailwind
CSS v4, and a little htmx. Content is curated with the `gems` CLI (cobra) and
enriched from the public Steam store API and IGDB.

## Features

- **Game of the Day** — an editorially picked game per date, with a note
  (`/game-of-the-day`, archives by date).
- **Weekly Release Radar** — games released in a given ISO week
  (`/release-radar`, `/release-radar/{week}`).
- **Top of the Month** — best games of a month by gem score (`/top`,
  `/top/{year}/{month}`).
- **Game & developer pages** — descriptions, screenshots/trailer, tags, store
  buttons, "more from this developer" (`/games/{slug}`, `/developers/{slug}`).
- **Newsletter** — double opt-in signup, markdown-authored weekly issues with
  `{{gotd}}/{{radar}}/{{top}}/{{game}}` data directives, resumable sending,
  web archive, one-click unsubscribe.
- **Sitemap, robots.txt, RSS feed** (`/sitemap.xml`, `/robots.txt`,
  `/feed.xml`).
- **Curation CLI** — `gems` (see below) imports, syncs and schedules
  everything; safe to re-run.

## Quick start

Prerequisites: Go 1.25+, Docker, and (for templ/sqlc/migrations work)
[templ](https://templ.guide), [sqlc](https://docs.sqlc.dev),
[golang-migrate](https://github.com/golang-migrate/migrate), [air](https://github.com/air-verse/air)
and the Tailwind standalone CLI.

```bash
cp .env.example .env            # defaults work for local dev
docker compose up -d            # Postgres 16, Adminer (:8080), Mailpit (:8025)
go run ./cmd/gems seed seed/games.yaml   # import the curated seed (idempotent)
go run ./cmd/gems steam sync --all       # descriptions + review stats from Steam
go run ./cmd/gems media fetch --all      # cache images into ./media
go run ./cmd/gems pick auto --days 14    # schedule the next two weeks of picks
air                              # dev server on :3000 (see .air.toml)
```

Migrations run automatically on server/CLI start (`db.Init()` applies
`db/migrations`). Tests: `go test ./...` (hermetic — no DB needed).

## Curation workflow (`gems` CLI)

```bash
go run ./cmd/gems steam releases --from 2026-09-28 --to 2026-10-04
                                        # every indie Steam game released in the range
                                        # -> tmp/steam-releases-<from>_<to>.json (no DB needed)
go run ./cmd/gems games add --steam 1145360 --slug hades --editorial 95
                                        # create a game from Steam data
go run ./cmd/gems steam sync --all      # Steam: description, reviews, release
go run ./cmd/gems igdb sync --all       # IGDB: trailer, store links, companies
go run ./cmd/gems igdb catalogue supergiant-games   # add a developer's other games
go run ./cmd/gems media fetch --all [--force]       # cache images locally
go run ./cmd/gems link set hades gog https://www.gog.com/game/hades
go run ./cmd/gems pick set 2026-10-01 hades --note "Why we love it"
go run ./cmd/gems pick auto --days 7    # fill empty days, avoiding recent repeats
go run ./cmd/gems pick list [--from YYYY-MM-DD --to YYYY-MM-DD]
go run ./cmd/gems games list            # inventory
go run ./cmd/gems rescore               # recompute all gem scores
go run ./cmd/gems refresh               # cron bundle: steam + igdb + missing media
```

`seed/games.yaml` is the curated source of truth (developers, games, tags,
store links, editorial scores). Re-run `gems seed seed/games.yaml` any time —
seed owns the curated fields and never wipes importer-owned data.

`gems steam releases` is the discovery step: it walks Steam's store search
(released games newest-first, plus the coming-soon listing for future dates),
keeps games with an exact release date inside `--from..--to` (default `--to`:
today, UTC) and English support, then adds store details, the top user tags
and the store page's review score from Steam's batched `GetItems` API (100
games per request, about 10 seconds for a week of indie releases).
`--indie=false` drops the Indie-tag filter, `--details=false` writes only
appid/name/date, `--out -` prints to stdout. Pick candidates from the JSON and
import them with `gems games add --steam <appid>`.

**IGDB access**: `gems igdb *` needs `IGDB_CLIENT_ID` / `IGDB_CLIENT_SECRET`
from a Confidential [Twitch dev console](https://dev.twitch.tv/console/apps)
app (IGDB is free for non-commercial use). Without them everything else works;
`igdb` commands fail with a clear message.

**Cron**: run `gems refresh` periodically (plus `gems newsletter send` on
publish day). In Docker: `docker compose run --rm app /gems refresh`.

## Newsletter workflow

```bash
go run ./cmd/gems newsletter new --week 2026-W40          # scaffold from template
$EDITOR newsletter/issues/2026-w40.md                      # edit markdown
go run ./cmd/gems newsletter preview newsletter/issues/2026-w40.md [--text]
go run ./cmd/gems newsletter test newsletter/issues/2026-w40.md --to you@example.com
go run ./cmd/gems newsletter send newsletter/issues/2026-w40.md --yes
go run ./cmd/gems newsletter subscribers list|count
go run ./cmd/gems newsletter issues
```

An issue is markdown with YAML front matter (`subject`, `preheader`), prose,
and **block directives on their own lines**:

| Directive | Meaning |
|---|---|
| `{{gotd 2026-09-28}}` | Game of the Day for that date (default: today) |
| `{{radar 2026-W40}}` | releases of the given ISO week |
| `{{top 2026-09}}` | best games of the given month |
| `{{game hades}}` | a single game card with store buttons |

Rendering resolves directives against the DB, produces a table-based HTML
email plus a plain-text alternative, and snapshots the issue into the web
archive. Sending is resumable — re-running `send` only mails pending
deliveries — and delivery rows are claimed atomically (claimed with
`FOR UPDATE SKIP LOCKED`), so concurrent send runs never double-send; a run
that crashes mid-mail is resumed by the next run after a 10-minute claim
timeout. The snapshot freezes once the first delivery row exists. Local dev
sends through Mailpit (UI at `:8025`).

## Gem score

`gem_score` blends editorial curation with Steam review sentiment into a 0–100
number (see `catalog/scoring.go`). Steam's review percentage only counts once
a game has at least 50 reviews; its influence then ramps linearly up to full
weight at 10,000 reviews. The blend is 60% editorial / 40% Steam; with no
usable Steam data the editorial score stands alone, and with no editorial
score the Steam percentage stands alone (a game with neither stays unscored).
Scores are recomputed by `gems seed`, `gems steam sync` and `gems rescore`.

## Environment variables

All in `.env.example`; copy to `.env`.

| Variable | Meaning |
|---|---|
| `DATABASE_URL` | Postgres connection string (app + CLI) |
| `PORT` | HTTP port (default `3000`) |
| `APP_ENV` | `development` (text logs) or `production` (JSON logs, HSTS) |
| `MEDIA_DIR` | Local media cache root, default `media` (gitignored) |
| `MEDIA_BASE_URL` | Public prefix for media URLs, default `/media`; point at a CDN to switch |
| `POSTGRES_PORT` | Host port for the compose `db` service (default `5432`) |
| `POSTGRES_PASSWORD` | Compose db + prod-app database password (only needed on first db init / by the app container) |
| `BASE_URL` | Public site origin; links/emails are absolute against it |
| `SMTP_HOST/PORT/USERNAME/PASSWORD/TLS` | SMTP relay for newsletter mail; defaults target the Mailpit dev sink (`SMTP_TLS`: `none`/`starttls`/`tls`) |
| `SMTP_RATE_PER_SEC` | Max mails per second when sending |
| `MAIL_FROM`, `MAIL_REPLY_TO`, `MAIL_POSTAL_ADDRESS` | Sender identity + imprint footer |
| `NEWSLETTER_SECRET` | HMAC key for one-click unsubscribe links — rotate = links invalidated (confirm links use random tokens stored hashed) |
| `IGDB_CLIENT_ID`, `IGDB_CLIENT_SECRET` | Optional; Twitch app credentials for IGDB |

## Deployment

`docker compose --profile prod up -d app` builds and runs the production
image: a two-stage build that compiles the web server **and** the `gems` CLI,
regenerates CSS with the pinned Tailwind CLI, and runs as non-root. The app
service gets its database URL from `POSTGRES_PASSWORD` against the `db`
service; `MEDIA_DIR` is a writable volume (`media_data:/data/media`) — never
baked into the image. Cron/one-off jobs use the same image:

```bash
docker compose run --rm app /gems refresh
docker compose run --rm app /gems pick auto --days 14
```

## Data sources & attribution

Game metadata, descriptions, review statistics and imagery come from the
public Steam store (Valve) and [IGDB](https://www.igdb.com) (Twitch; free for
non-commercial use under their terms). Images are cached locally and served
from this site's own `/media` origin. All games link back to their store
pages; trademarks belong to their respective owners.
