package handler

import (
	"encoding/gob"
	"net/http"
	"time"

	"github.com/embiem/go-web-template/data"
	"github.com/embiem/go-web-template/db"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
)

var SessionManager *scs.SessionManager

func InitSession(secureCookies bool) {
	// Register structs we want to set on the session
	gob.Register(data.User{})

	// Initialize a new session manager and configure the session lifetime.
	SessionManager = scs.New()
	SessionManager.Store = pgxstore.New(db.Pool)
	SessionManager.Lifetime = 24 * time.Hour

	// Harden the session cookie.
	SessionManager.Cookie.HttpOnly = true
	SessionManager.Cookie.SameSite = http.SameSiteLaxMode
	SessionManager.Cookie.Path = "/"
	SessionManager.Cookie.Secure = secureCookies
}
