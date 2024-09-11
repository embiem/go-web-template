package handler

import (
	"log/slog"
	"net/http"
)

func Make(h func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			slog.Error("internal server error", "err", err, "path", r.URL.Path)
		}
	}
}

type AccountProvider string

const (
	PasswordProvider AccountProvider = "password"
	GoogleProvider   AccountProvider = "google"
)

type SessionKey string

const (
	SessionKeyUser SessionKey = "user"
)
