package web

import (
	"fmt"
	"net/http"
	"os"

	"muzi/scrobble"
)

type settingsData struct {
	Title            string
	LoggedInUsername string
	TemplateName     string
	APIKey           string
	APISecret        string
	SpotifyClientId  string
	SpotifyConnected bool
}

func settingsPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			http.Error(w, "User not found", http.StatusInternalServerError)
			return
		}

		user, err := scrobble.GetUserById(userId)
		if err != nil {
			http.Error(w, "Error loading user", http.StatusInternalServerError)
			return
		}

		d := settingsData{
			Title:            "muzi | Settings",
			LoggedInUsername: username,
			TemplateName:     "settings",
			APIKey:           "",
			APISecret:        "",
			SpotifyClientId:  "",
			SpotifyConnected: user.IsSpotifyConnected(),
		}

		if user.ApiKey != nil {
			d.APIKey = *user.ApiKey
		}
		if user.ApiSecret != nil {
			d.APISecret = *user.ApiSecret
		}
		if user.SpotifyClientId != nil {
			d.SpotifyClientId = *user.SpotifyClientId
		}

		err = templates.ExecuteTemplate(w, "base", d)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func generateAPIKeyHandler(w http.ResponseWriter, r *http.Request) {
	username := getLoggedInUsername(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userId, err := getUserIdByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	apiKey, err := scrobble.GenerateAPIKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating API key: %v\n", err)
		http.Error(w, "Error generating API key", http.StatusInternalServerError)
		return
	}

	apiSecret, err := scrobble.GenerateAPISecret()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating API secret: %v\n", err)
		http.Error(w, "Error generating API secret", http.StatusInternalServerError)
		return
	}

	err = scrobble.UpdateUserAPIKey(userId, apiKey, apiSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving API key: %v\n", err)
		http.Error(w, "Error saving API key", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func updateSpotifyCredentialsHandler(w http.ResponseWriter, r *http.Request) {
	username := getLoggedInUsername(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userId, err := getUserIdByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	clientId := r.FormValue("spotify_client_id")
	clientSecret := r.FormValue("spotify_client_secret")

	if clientId == "" || clientSecret == "" {
		err = scrobble.DeleteUserSpotifyCredentials(userId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error removing Spotify credentials: %v\n", err)
		}
	} else {
		err = scrobble.UpdateUserSpotifyCredentials(userId, clientId, clientSecret)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error saving Spotify credentials: %v\n", err)
			http.Error(w, "Error saving Spotify credentials", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func spotifyConnectHandler(w http.ResponseWriter, r *http.Request) {
	username := getLoggedInUsername(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userId, err := getUserIdByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	user, err := scrobble.GetUserById(userId)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spotifyConnectHandler: GetUserById error: %v\n", err)
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	fmt.Fprintf(os.Stderr, "spotifyConnectHandler: userId=%d, SpotifyClientId=%v\n", userId, user.SpotifyClientId)

	if user.SpotifyClientId == nil || *user.SpotifyClientId == "" {
		fmt.Fprintf(os.Stderr, "spotifyConnectHandler: SpotifyClientId is nil or empty, redirecting to settings\n")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/scrobble/spotify/authorize?user_id=%d", userId), http.StatusSeeOther)
}
