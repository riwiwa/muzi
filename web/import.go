package web

// Functions that the web UI uses for importing

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"

	"muzi/migrate"
)

// Global vars to hold active import jobs and a mutex to lock access to
// importJobs
var (
	importJobs = make(map[string]chan migrate.ProgressUpdate)
	jobsMu     sync.RWMutex
)

// Renders the import page
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

// Validates and parses tracks from uploaded Spotify JSON files
func parseUploads(uploads []*multipart.FileHeader, w http.ResponseWriter) []migrate.SpotifyTrack {
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

// Imports the uploaded JSON files into the database
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
	err = r.ParseMultipartForm(32 * 1024 * 1024) // 32 MiB
	if err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	allTracks := parseUploads(r.MultipartForm.File["json_files"], w)
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
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": jobID,
		"status": "started",
	})
}

// Fetch a LastFM account's scrobbles and insert them into the database
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
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": jobID,
		"status": "started",
	})
}

// Controls the progress bar for a LastFM import
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

// Controls the progress bar for a Spotify import
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
