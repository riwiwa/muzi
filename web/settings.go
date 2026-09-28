package web

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"muzi/db"
	"muzi/scrobble"
)

type settingsData struct {
	Title              string
	LoggedInUsername   string
	TemplateName       string
	APIKey             string
	APISecret          string
	SpotifyClientId    string
	SpotifyConnected   bool
	SpotifyRedirectURI string
	Pfp                string
	PfpError           string
	Bio                string
	BioMaxLength       int
	PublicProfile      bool
	ProfileURL         string
	AccountError       string
	AccountOK          string
}

const bioMaxLength = 500

const defaultPfp = "/files/assets/pfps/default.png"

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
			Title:              "muzi | Settings",
			LoggedInUsername:   username,
			TemplateName:       "settings",
			APIKey:             "",
			APISecret:          "",
			SpotifyClientId:    "",
			SpotifyConnected:   user.IsSpotifyConnected(),
			SpotifyRedirectURI: scrobble.SpotifyRedirectURI(r),
			Pfp:                user.Pfp,
			PfpError:           r.URL.Query().Get("pfp_error"),
			Bio:                user.Bio,
			BioMaxLength:       bioMaxLength,
			ProfileURL:         "/profile/" + username,
			AccountError:       r.URL.Query().Get("account_error"),
			AccountOK:          r.URL.Query().Get("account_ok"),
		}

		err = db.Pool.QueryRow(r.Context(), "SELECT public_profile FROM users WHERE pk = $1", userId).
			Scan(&d.PublicProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading profile visibility: %v\n", err)
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

	http.Redirect(w, r, "/settings?tab=scrobble", http.StatusSeeOther)
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

	http.Redirect(w, r, "/settings?tab=scrobble", http.StatusSeeOther)
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

	authURL, err := scrobble.SpotifyAuthorizeURL(userId, r)
	if err != nil {
		http.Redirect(w, r, "/settings?tab=scrobble", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// Sets the logged-in user's profile picture from an upload, or back to the default
// when the "remove" button was used
func updateProfilePictureHandler(w http.ResponseWriter, r *http.Request) {
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

	pfp := defaultPfp
	if r.URL.Query().Get("remove") == "" {
		pfp, err = saveUploadedImage(r)
		if err != nil {
			http.Redirect(w, r, "/settings?tab=profile&pfp_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
	}

	_, err = db.Pool.Exec(r.Context(), "UPDATE users SET pfp = $1 WHERE pk = $2", pfp, userId)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating profile picture: %v\n", err)
		http.Error(w, "Error saving profile picture", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings?tab=profile", http.StatusSeeOther)
}

// Sets the logged-in user's bio; an empty bio hides it on the profile
func updateBioHandler(w http.ResponseWriter, r *http.Request) {
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

	bio := strings.TrimSpace(strings.ReplaceAll(r.FormValue("bio"), "\r\n", "\n"))
	if utf8.RuneCountInString(bio) > bioMaxLength {
		http.Error(w, fmt.Sprintf("Bio must be %d characters or fewer", bioMaxLength), http.StatusBadRequest)
		return
	}

	_, err = db.Pool.Exec(r.Context(), "UPDATE users SET bio = $1 WHERE pk = $2", bio, userId)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating bio: %v\n", err)
		http.Error(w, "Error saving bio", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings?tab=profile", http.StatusSeeOther)
}

// Makes the logged-in user's profile visible to everyone, or only to themselves
func updateVisibilityHandler(w http.ResponseWriter, r *http.Request) {
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

	public := r.FormValue("public_profile") == "on"
	_, err = db.Pool.Exec(r.Context(), "UPDATE users SET public_profile = $1 WHERE pk = $2", public, userId)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating profile visibility: %v\n", err)
		http.Error(w, "Error saving profile visibility", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings?tab=profile", http.StatusSeeOther)
}
