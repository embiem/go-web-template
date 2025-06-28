/*
Package handler contains the logic for the routes.
*/
package handler

import (
	"net/http"

	"github.com/embiem/go-web-template/data"
	"github.com/embiem/go-web-template/view"
)

func GetIndexPage(w http.ResponseWriter, r *http.Request) error {
	if !SessionManager.Exists(r.Context(), string(SessionKeyUser)) {
		// Redirect to login page
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
		return nil
	}
	user := SessionManager.Get(r.Context(), string(SessionKeyUser)).(data.User)

	return view.IndexPage(user.Username).Render(r.Context(), w)
}
