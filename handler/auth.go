package handler

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/embiem/go-web-template/data"
	"github.com/embiem/go-web-template/db"
	"github.com/embiem/go-web-template/view"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordLen follows NIST SP 800-63B: length is the only rule.
	MinPasswordLen = 8
	// MaxPasswordLen is bcrypt's hard limit; it silently ignores anything
	// past 72 bytes, so reject instead of truncating.
	MaxPasswordLen = 72
	MaxUsernameLen = 64
)

// usernameRe caps length and charset so nothing unexpected reaches the DB and
// look-alike names can't be registered.
var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

// dummyHash is compared against when no password account matched, so a login
// attempt costs the same whether or not the username exists. Generated with
// bcrypt.GenerateFromPassword at bcrypt.DefaultCost.
var dummyHash = []byte("$2a$10$e5nDnGctj0BfDp5OKrlngO2cha.gAZ2GvkWoafbw/kidhqmTvOES.")

func GetSignupPage(w http.ResponseWriter, r *http.Request) error {
	if SessionManager.Exists(r.Context(), string(SessionKeyUser)) {
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
	if r.FormValue("organization") != "" {
		// Honeypot field detected a spam bot
		w.WriteHeader(http.StatusForbidden)
		return nil
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	nameErr, passwordErr := validateCredentials(username, password)
	if nameErr != "" || passwordErr != "" {
		return view.SignupForm(view.SignupInputErrors{
			PreviousName:  username,
			NameError:     nameErr,
			PasswordError: passwordErr,
		}).Render(r.Context(), w)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	tx, err := db.Pool.Begin(r.Context())
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
		// The unique index on LOWER(username) is the only thing that can
		// decide this race; an application-level pre-check cannot.
		if isUniqueViolation(err) {
			return view.SignupForm(view.SignupInputErrors{
				PreviousName: username,
				NameError:    "This username is unavailable!",
			}).Render(r.Context(), w)
		}
		return err
	}

	_, err = qtx.CreateAccount(r.Context(), data.CreateAccountParams{
		UserID:       user.ID,
		Provider:     string(PasswordProvider),
		PasswordHash: pgtype.Text{String: string(hashedPassword), Valid: true},
	})
	if err != nil {
		return err
	}

	err = tx.Commit(r.Context())
	if err != nil {
		return err
	}
	// ~~~ END OF TX

	if err := SessionManager.RenewToken(r.Context()); err != nil {
		return err
	}
	SessionManager.Put(r.Context(), string(SessionKeyUser), user.ID)

	// Redirect to index page
	w.Header().Add("HX-Redirect", "/")
	w.WriteHeader(http.StatusSeeOther)
	return nil
}

func GetLoginPage(w http.ResponseWriter, r *http.Request) error {
	if SessionManager.Exists(r.Context(), string(SessionKeyUser)) {
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
	if r.FormValue("organization") != "" {
		// Honeypot field detected a spam bot
		w.WriteHeader(http.StatusForbidden)
		return nil
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		return view.LoginForm(view.LoginInputErrors{
			PreviousName:  username,
			NameError:     msgIf(username == "", "Please enter your username!"),
			PasswordError: msgIf(password == "", "Please enter your password!"),
		}).Render(r.Context(), w)
	}

	// Look up the password account, but always run bcrypt afterwards - against
	// a dummy hash when there is none - so response time doesn't reveal which
	// usernames exist.
	hash := dummyHash
	var user data.User
	found := false

	if u, err := db.Queries.GetUserByUsername(r.Context(), username); err == nil {
		accounts, err := db.Queries.GetUserAccounts(r.Context(), u.ID)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			if account.Provider == string(PasswordProvider) && account.PasswordHash.Valid {
				user, hash, found = u, []byte(account.PasswordHash.String), true
				break
			}
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// Unknown user, no password account (e.g. OAuth-only) and wrong password
	// all render the same message; anything else is an enumeration oracle.
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil || !found {
		return view.LoginForm(view.LoginInputErrors{
			PreviousName: username,
			FormError:    "Invalid username or password.",
		}).Render(r.Context(), w)
	}

	if err := SessionManager.RenewToken(r.Context()); err != nil {
		return err
	}
	SessionManager.Put(r.Context(), string(SessionKeyUser), user.ID)

	// Redirect to index page
	w.Header().Add("HX-Redirect", "/")
	w.WriteHeader(http.StatusSeeOther)
	return nil
}

func PostLogout(w http.ResponseWriter, r *http.Request) error {
	// Destroy, not Remove: the token and its DB row must die with the logout,
	// otherwise a stolen cookie stays valid for the rest of the lifetime.
	if err := SessionManager.Destroy(r.Context()); err != nil {
		return err
	}

	// Redirect to login page
	http.Redirect(w, r, "/login", http.StatusSeeOther)
	return nil
}

// validateCredentials returns a message per field, empty when the field is fine.
func validateCredentials(username, password string) (nameErr, passwordErr string) {
	switch {
	case username == "":
		nameErr = "Please enter a username!"
	case !usernameRe.MatchString(username):
		nameErr = fmt.Sprintf(
			"Username may only contain letters, digits, '.', '_' and '-', and be at most %d characters.",
			MaxUsernameLen)
	}

	switch {
	case password == "":
		passwordErr = "Please enter a password!"
	case utf8.RuneCountInString(password) < MinPasswordLen:
		passwordErr = fmt.Sprintf("Password must be at least %d characters.", MinPasswordLen)
	case len(password) > MaxPasswordLen:
		passwordErr = fmt.Sprintf("Password must be at most %d bytes.", MaxPasswordLen)
	}

	return nameErr, passwordErr
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func msgIf(cond bool, msg string) string {
	if cond {
		return msg
	}
	return ""
}
