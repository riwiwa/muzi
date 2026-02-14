package web

// Functions used for user profiles in the web UI

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"muzi/db"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgtype"
)

type ProfileData struct {
	Username            string
	Bio                 string
	Pfp                 string
	AllowDuplicateEdits bool
	ScrobbleCount       int
	ArtistCount         int
	Artists             []string
	Titles              []string
	Times               []string
	Page                int
}

// Render a page of the profile in the URL
func profilePageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		pageStr := r.URL.Query().Get("page")
		var pageInt int
		if pageStr == "" {
			pageInt = 1
		} else {
			pageInt, err = strconv.Atoi(pageStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot convert page URL query from string to int: %v\n", err)
				pageInt = 1
			}
		}

		lim := 15
		off := (pageInt - 1) * lim

		var profileData ProfileData
		profileData.Username = username
		profileData.Page = pageInt

		err = db.Pool.QueryRow(
			r.Context(),
			`SELECT bio, pfp, allow_duplicate_edits,
				(SELECT COUNT(*) FROM history WHERE user_id = $1) as scrobble_count,
				(SELECT COUNT(DISTINCT artist) FROM history WHERE user_id = $1) as artist_count
			FROM users WHERE pk = $1;`,
			userId,
		).Scan(&profileData.Bio, &profileData.Pfp, &profileData.AllowDuplicateEdits, &profileData.ScrobbleCount, &profileData.ArtistCount)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get profile for %s: %v\n", username, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		rows, err := db.Pool.Query(
			r.Context(),
			"SELECT artist, song_name, timestamp FROM history WHERE user_id = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3;",
			userId,
			lim,
			off,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SELECT history failed: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var artist, title string
			var time pgtype.Timestamptz
			err = rows.Scan(&artist, &title, &time)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Scanning history row failed: %v\n", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			profileData.Artists = append(profileData.Artists, artist)
			profileData.Titles = append(profileData.Titles, title)
			profileData.Times = append(profileData.Times, time.Time.String())
		}

		err = templates.ExecuteTemplate(w, "profile.gohtml", profileData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
