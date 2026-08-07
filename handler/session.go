package handler

import (
	"encoding/gob"
	"net/http"
	"time"

	"github.com/embiem/go-web-template/data"
	"github.com/embiem/go-web-template/db"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

var SessionManager *scs.SessionManager

func InitSession(secureCookies bool) {
	// Register structs we want to set on the session
	gob.Register(pgtype.UUID{})

	// Initialize a new session manager and configure the session lifetime.
	SessionManager = scs.New()
	SessionManager.Store = pgxstore.New(db.Pool)
	SessionManager.Lifetime = 24 * time.Hour
	// An abandoned session dies early while active users keep working.
	SessionManager.IdleTimeout = 2 * time.Hour

	// Harden the session cookie.
	SessionManager.Cookie.HttpOnly = true
	SessionManager.Cookie.SameSite = http.SameSiteLaxMode
	SessionManager.Cookie.Path = "/"
	SessionManager.Cookie.Secure = secureCookies
	if secureCookies {
		// __Host- pins the cookie to this exact host over HTTPS. Requires
		// Secure, Path=/ and no Domain, all satisfied above.
		SessionManager.Cookie.Name = "__Host-session"
	}
}

// CurrentUser loads the logged-in user for this request, reporting false when
// there is no session. Only the user ID lives in the session, so profile
// changes and deletions take effect on the next request instead of lingering
// until the session expires.
func CurrentUser(r *http.Request) (data.User, bool) {
	id, ok := SessionManager.Get(r.Context(), string(SessionKeyUser)).(pgtype.UUID)
	if !ok {
		return data.User{}, false
	}

	user, err := db.Queries.GetUserById(r.Context(), id)
	if err != nil {
		// The session outlived its user (deleted, or a stale session shape).
		_ = SessionManager.Destroy(r.Context())
		return data.User{}, false
	}

	return user, true
}
