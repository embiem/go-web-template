/*
Package handler contains the logic for the routes.
*/
package handler

import (
	"net/http"

	"github.com/embiem/go-web-template/view"
)

func GetIndexPage(w http.ResponseWriter, r *http.Request) error {
	user, ok := CurrentUser(r)
	if !ok {
		// Redirect to login page
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}

	return view.IndexPage(user.Username).Render(r.Context(), w)
}
