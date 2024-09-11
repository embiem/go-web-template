package handler

import (
	"encoding/gob"
	"time"

	"github.com/embiem/go-web-template/data"
	"github.com/embiem/go-web-template/db"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
)

var SessionManager *scs.SessionManager

func InitSession() {
	// Register structs we want to set on the session
	gob.Register(data.User{})

	// Initialize a new session manager and configure the session lifetime.
	SessionManager = scs.New()
	SessionManager.Store = pgxstore.New(db.Pool)
	SessionManager.Lifetime = 24 * time.Hour
}
