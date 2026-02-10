package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"muzi/db"
	"muzi/migrate"

	"golang.org/x/crypto/bcrypt"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgtype"
)

// 50 MB
const maxHeaderSize int64 = 50 * 1024 * 1024

// will add permissions later
type Session struct {
	Username string
}

var (
	importJobs = make(map[string]chan migrate.ProgressUpdate)
	jobsMu     sync.RWMutex
	templates  *template.Template
)

func init() {
	funcMap := template.FuncMap{
		"sub":       sub,
		"add":       add,
		"formatInt": formatInt,
	}
	templates = template.Must(template.New("").Funcs(funcMap).ParseGlob("./templates/*.gohtml"))
}

func generateID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func createSession(username string) string {
	sessionID, err := generateID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating sessionID: %v\n", err)
		return ""
	}
	_, err = db.Pool.Exec(
		context.Background(),
		"INSERT INTO sessions (session_id, username, expires_at) VALUES ($1, $2, NOW() + INTERVAL '30 days');",
		sessionID,
		username,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
		return ""
	}
	return sessionID
}

func getSession(ctx context.Context, sessionID string) *Session {
	var username string
	err := db.Pool.QueryRow(
		ctx,
		"SELECT username FROM sessions WHERE session_id = $1 AND expires_at > NOW();",
		sessionID,
	).Scan(&username)
	if err != nil {
		return nil
	}
	return &Session{Username: username}
}

// for account deletion later
func deleteSession(sessionID string) {
	_, err := db.Pool.Exec(
		context.Background(),
		"DELETE FROM sessions WHERE session_id = $1;",
		sessionID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting session: %v\n", err)
	}
}

func getLoggedInUsername(r *http.Request) string {
	cookie, err := r.Cookie("session")
	if err != nil {
		return ""
	}
	session := getSession(r.Context(), cookie.Value)
	if session == nil {
		return ""
	}
	return session.Username
}

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

func sub(a int, b int) int {
	return a - b
}

func add(a int, b int) int {
	return a + b
}

func formatInt(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	} else {
		return formatInt(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
	}
}

func getUserIdByUsername(ctx context.Context, username string) (int, error) {
	var userId int
	err := db.Pool.QueryRow(ctx, "SELECT pk FROM users WHERE username = $1;", username).
		Scan(&userId)
	return userId, err
}

func hashPassword(pass []byte) (string, error) {
	if len([]rune(string(pass))) < 8 || len(pass) > 64 {
		return "", errors.New("Error: Password must be greater than 8 chars.")
	}
	hashedPassword, err := bcrypt.GenerateFromPassword(pass, bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't hash password: %v\n", err)
		return "", err
	}
	return string(hashedPassword), nil
}

func verifyPassword(hashedPassword string, enteredPassword []byte) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), enteredPassword)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while comparing passwords: %v\n", err)
		return false
	}
	return true
}

func createAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		username := r.FormValue("uname")
		if len([]rune(string(username))) == 0 {
			http.Redirect(w, r, "/createaccount?error=userlength", http.StatusSeeOther)
			return
		}
		var usertaken bool
		err = db.Pool.QueryRow(r.Context(),
			"SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)", username).
			Scan(&usertaken)
		if usertaken == true {
			http.Redirect(w, r, "/createaccount?error=usertaken", http.StatusSeeOther)
			return
		}
		hashedPassword, err := hashPassword([]byte(r.FormValue("pass")))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing password: %v\n", err)
			http.Redirect(w, r, "/createaccount?error=passlength", http.StatusSeeOther)
			return
		}

		_, err = db.Pool.Exec(
			r.Context(),
			`INSERT INTO users (username, password) VALUES ($1, $2);`,
			username,
			hashedPassword,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot add new user to users table: %v\n", err)
			http.Redirect(w, r, "/createaccount", http.StatusSeeOther)
		} else {
			sessionID := createSession(username)
			if sessionID == "" {
				http.Redirect(w, r, "/login?error=session", http.StatusSeeOther)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     "session",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400 * 30, // 30 days
			})
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
		}
	}
}

func createAccountPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type data struct {
			Error string
		}
		d := data{Error: "len"}
		err := templates.ExecuteTemplate(w, "create_account.gohtml", d)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func loginSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		username := r.FormValue("uname")
		if username == "" {
			http.Redirect(w, r, "/login?error=invalid-creds", http.StatusSeeOther)
			return
		}
		password := r.FormValue("pass")
		var storedPassword string
		err = db.Pool.QueryRow(r.Context(), "SELECT password FROM users WHERE username = $1;", username).
			Scan(&storedPassword)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get password for entered username: %v\n", err)
		}

		if verifyPassword(storedPassword, []byte(password)) {
			sessionID := createSession(username)
			if sessionID == "" {
				http.Redirect(w, r, "/login?error=session", http.StatusSeeOther)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     "session",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400 * 30, // 30 days
			})
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/login?error=invalid-creds", http.StatusSeeOther)
		}
	}
}

func loginPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type data struct {
			Error string
		}
		d := data{Error: r.URL.Query().Get("error")}
		err := templates.ExecuteTemplate(w, "login.gohtml", d)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

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

func updateDuplicateEditsSetting(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		username := r.FormValue("username")
		allow := r.FormValue("allow") == "true"

		_, err = db.Pool.Exec(
			r.Context(),
			`UPDATE users SET allow_duplicate_edits = $1 WHERE username = $2;`,
			allow,
			username,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating setting: %v\n", err)
		}
		http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
	}
}

func importPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		type ImportData struct {
			Username string
		}
		data := ImportData{Username: username}

		err := templates.ExecuteTemplate(w, "import.gohtml", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func checkUploads(uploads []*multipart.FileHeader, w http.ResponseWriter) []migrate.SpotifyTrack {
	if len(uploads) < 1 {
		http.Error(w, "No files uploaded", http.StatusBadRequest)
		return nil
	}

	if len(uploads) > 30 {
		http.Error(w, "Too many files uploaded (30 max)", http.StatusBadRequest)
		return nil
	}

	var allTracks []migrate.SpotifyTrack

	for _, u := range uploads {
		if u.Size > maxHeaderSize {
			fmt.Fprintf(os.Stderr, "File too large: %s\n", u.Filename)
			continue
		}

		if strings.Contains(u.Filename, "..") ||
			strings.Contains(u.Filename, "/") ||
			strings.Contains(u.Filename, "\x00") {
			fmt.Fprintf(os.Stderr, "Invalid filename: %s\n", u.Filename)
			continue
		}

		file, err := u.Open()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening %s: %v\n", u.Filename, err)
			continue
		}

		reader := io.LimitReader(file, maxHeaderSize)
		data, err := io.ReadAll(reader)
		file.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", u.Filename, err)
			continue
		}

		if !json.Valid(data) {
			http.Error(w, fmt.Sprintf("Invalid JSON in %s", u.Filename),
				http.StatusBadRequest)
			return nil
		}

		var tracks []migrate.SpotifyTrack
		if err := json.Unmarshal(data, &tracks); err != nil {
			fmt.Fprintf(os.Stderr,
				"Error parsing %s: %v\n", u.Filename, err)
			continue
		}

		allTracks = append(allTracks, tracks...)
	}
	return allTracks
}

func importSpotifyHandler(w http.ResponseWriter, r *http.Request) {
	username := getLoggedInUsername(r)
	if username == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userId, err := getUserIdByUsername(r.Context(), username)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	// 32MB memory max
	err = r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	allTracks := checkUploads(r.MultipartForm.File["json_files"], w)
	if allTracks == nil {
		return
	}

	jobID, err := generateID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating jobID: %v\n", err)
		http.Error(w, "Error generating jobID", http.StatusBadRequest)
		return
	}
	progressChan := make(chan migrate.ProgressUpdate, 100)

	jobsMu.Lock()
	importJobs[jobID] = progressChan
	jobsMu.Unlock()

	go func() {
		migrate.ImportSpotify(allTracks, userId, progressChan)

		jobsMu.Lock()
		delete(importJobs, jobID)
		jobsMu.Unlock()
		close(progressChan)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": jobID,
		"status": "started",
	})
}

func importLastFMHandler(w http.ResponseWriter, r *http.Request) {
	username := getLoggedInUsername(r)
	if username == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userId, err := getUserIdByUsername(r.Context(), username)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	err = r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	lastfmUsername := template.HTMLEscapeString(r.FormValue("lastfm_username"))
	lastfmAPIKey := template.HTMLEscapeString(r.FormValue("lastfm_api_key"))

	if lastfmUsername == "" || lastfmAPIKey == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	jobID, err := generateID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating jobID: %v\n", err)
		http.Error(w, "Error generating jobID", http.StatusBadRequest)
		return
	}
	progressChan := make(chan migrate.ProgressUpdate, 100)

	jobsMu.Lock()
	importJobs[jobID] = progressChan
	jobsMu.Unlock()

	go func() {
		migrate.ImportLastFM(lastfmUsername, lastfmAPIKey, userId, progressChan,
			username)

		jobsMu.Lock()
		delete(importJobs, jobID)
		jobsMu.Unlock()
		close(progressChan)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": jobID,
		"status": "started",
	})
}

func importLastFMProgressHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job")
	if jobID == "" {
		http.Error(w, "Missing job ID", http.StatusBadRequest)
		return
	}

	jobsMu.RLock()
	job, exists := importJobs[jobID]
	jobsMu.RUnlock()

	if !exists {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "data: %s\n\n", `{"status":"connected"}`)
	flusher.Flush()

	for update := range job {
		data, err := json.Marshal(update)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()

		if update.Status == "completed" || update.Status == "error" {
			return
		}
	}
}

func importSpotifyProgressHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job")
	if jobID == "" {
		http.Error(w, "Missing job ID", http.StatusBadRequest)
		return
	}

	jobsMu.RLock()
	job, exists := importJobs[jobID]
	jobsMu.RUnlock()

	if !exists {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "data: %s\n\n", `{"status":"connected"}`)
	flusher.Flush()

	for update := range job {
		data, err := json.Marshal(update)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()

		if update.Status == "completed" || update.Status == "error" {
			return
		}
	}
}

func Start() {
	addr := ":1234"
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Handle("/files/*", http.StripPrefix("/files", http.FileServer(http.Dir("./static"))))
	r.Get("/login", loginPageHandler())
	r.Get("/createaccount", createAccountPageHandler())
	r.Get("/profile/{username}", profilePageHandler())
	r.Get("/import", importPageHandler())
	r.Post("/loginsubmit", loginSubmit)
	r.Post("/createaccountsubmit", createAccount)
	r.Post("/settings/duplicate-edits", updateDuplicateEditsSetting)
	r.Post("/import/lastfm", importLastFMHandler)
	r.Post("/import/spotify", importSpotifyHandler)
	r.Get("/import/lastfm/progress", importLastFMProgressHandler)
	r.Get("/import/spotify/progress", importSpotifyProgressHandler)
	fmt.Printf("WebUI starting on %s\n", addr)
	prot := http.NewCrossOriginProtection()
	http.ListenAndServe(addr, prot.Handler(r))
}
