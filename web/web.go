package web

// Main web UI controller

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"

	"muzi/db"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// 50 MiB
const maxHeaderSize int64 = 50 * 1024 * 1024

// Holds all the parsed HTML templates
var templates *template.Template

// Declares all functions for the HTML templates and parses them
func init() {
	funcMap := template.FuncMap{
		"sub":       sub,
		"add":       add,
		"formatInt": formatInt,
	}
	templates = template.Must(template.New("").Funcs(funcMap).ParseGlob("./templates/*.gohtml"))
}

// Returns T/F if a user is found in the users table
func hasUsers(ctx context.Context) bool {
	var count int
	err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM users;").Scan(&count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking for users: %v\n", err)
		return false
	}
	return count > 0
}

// Controls what is displayed at the root URL.
// If logged in: Logged in user's profile page.
// If logged out: Login page.
// If no users in DB yet: Create account page.
func rootHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !hasUsers(r.Context()) {
			http.Redirect(w, r, "/createaccount", http.StatusSeeOther)
			return
		}

		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
	}
}

// Serves all pages at the specified address.
func Start() {
	addr := ":1234"
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Handle("/files/*", http.StripPrefix("/files", http.FileServer(http.Dir("./static"))))
	r.Get("/", rootHandler())
	r.Get("/login", loginPageHandler())
	r.Get("/createaccount", createAccountPageHandler())
	r.Get("/profile/{username}", profilePageHandler())
	r.Get("/import", importPageHandler())
	r.Post("/loginsubmit", loginSubmit)
	r.Post("/createaccountsubmit", createAccount)
	r.Post("/import/lastfm", importLastFMHandler)
	r.Post("/import/spotify", importSpotifyHandler)
	r.Get("/import/lastfm/progress", importLastFMProgressHandler)
	r.Get("/import/spotify/progress", importSpotifyProgressHandler)
	r.Get("/settings", settingsPageHandler())
	fmt.Printf("WebUI starting on %s\n", addr)
	prot := http.NewCrossOriginProtection()
	http.ListenAndServe(addr, prot.Handler(r))
}
