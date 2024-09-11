package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/embiem/go-web-template/data"
	"github.com/embiem/go-web-template/db"
	"github.com/embiem/go-web-template/view"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
)

func GetSignupPage(w http.ResponseWriter, r *http.Request) error {
	if SessionManager.Exists(r.Context(), string(SessionKeyUser)) {
		slog.Info("User already logged in", "user",
			SessionManager.Get(r.Context(), string(SessionKeyUser)))
		// Redirect to index page
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	}

	// Enforce browser to revalidate; prevents going "back" to login form while logged in,
	// after logging in. This would be weird for the user.
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0, s-maxage=0")

	return view.SignupPage().Render(r.Context(), w)
}

func PostSignup(w http.ResponseWriter, r *http.Request) error {

	organization := r.FormValue("organization")
	if organization != "" {
		// Honeypot field detected a spam bot
		w.WriteHeader(http.StatusForbidden)
		return nil
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		return view.SignupForm(
			view.SignupInputErrors{
				PreviousName:  username,
				EmptyName:     username == "",
				EmptyPassword: password == "",
			}).Render(r.Context(), w)
	}

	_, err := db.Queries.GetUserByUsername(r.Context(), username)
	if err == nil {
		return view.SignupForm(view.SignupInputErrors{
			PreviousName:  username,
			UsernameTaken: true,
		}).Render(r.Context(), w)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("Error while hashing password", "err", err)
		return err
	}

	tx, err := db.Conn.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())

	// ~~~ START OF TX
	qtx := db.Queries.WithTx(tx)
	user, err := qtx.CreateUser(r.Context(), data.CreateUserParams{
		Username: username,
	})
	if err != nil {
		slog.Error("Error while creating user", "err", err)
		return err
	}

	_, err = qtx.CreateAccount(r.Context(), data.CreateAccountParams{
		UserID:       user.ID,
		Provider:     string(PasswordProvider),
		PasswordHash: pgtype.Text{String: string(hashedPassword), Valid: true},
	})
	if err != nil {
		slog.Error("Error while creating account", "err", err)
		return err
	}

	err = tx.Commit(r.Context())
	if err != nil {
		return err
	}
	// ~~~ END OF TX

	SessionManager.Put(r.Context(), string(SessionKeyUser), user)

	// Redirect to index page
	w.Header().Add("HX-Redirect", "/")
	w.WriteHeader(http.StatusSeeOther)
	return nil
}

func GetLoginPage(w http.ResponseWriter, r *http.Request) error {
	if SessionManager.Exists(r.Context(), string(SessionKeyUser)) {
		slog.Info("User already logged in", "user",
			SessionManager.Get(r.Context(), string(SessionKeyUser)))
		// Redirect to index page
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	}

	// Enforce browser to revalidate; prevents going "back" to login form while logged in,
	// after logging in. This would be weird for the user.
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0, s-maxage=0")

	return view.LoginPage().Render(r.Context(), w)
}

func PostLogin(w http.ResponseWriter, r *http.Request) error {
	organization := r.FormValue("organization")
	if organization != "" {
		// Honeypot field detected a spam bot
		w.WriteHeader(http.StatusForbidden)
		return nil
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		return view.LoginForm(
			view.LoginInputErrors{
				PreviousName:  username,
				EmptyName:     username == "",
				EmptyPassword: password == "",
			}).Render(r.Context(), w)
	}

	user, err := db.Queries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return view.LoginForm(view.LoginInputErrors{
				PreviousName: username,
				UnknownName:  true,
			}).Render(r.Context(), w)
		}

		return err
	}
	accounts, err := db.Queries.GetUserAccounts(r.Context(), user.ID)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return fmt.Errorf("no accounts found for user %s", user.Username)
	}
	var passwordAccount data.Account
	for _, account := range accounts {
		if account.Provider == "password" {
			passwordAccount = account
			break
		}
	}
	if !passwordAccount.ID.Valid {
		return fmt.Errorf("no account of correct provider found for user %s", user.Username)
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passwordAccount.PasswordHash.String),
		[]byte(password)); err != nil {
		return view.LoginForm(view.LoginInputErrors{
			PreviousName:      username,
			IncorrectPassword: true,
		}).Render(r.Context(), w)
	}

	SessionManager.Put(r.Context(), string(SessionKeyUser), user)

	// Redirect to index page
	w.Header().Add("HX-Redirect", "/")
	w.WriteHeader(http.StatusSeeOther)
	return nil
}

func PostLogout(w http.ResponseWriter, r *http.Request) error {
	SessionManager.Remove(r.Context(), string(SessionKeyUser))

	// Redirect to login page
	http.Redirect(w, r, "/login", http.StatusSeeOther)
	return nil
}
