package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"

	"muzi/migrate"
)

const (
	// Apple's export zips include App Store and payment data, so they can be large
	appleMaxUpload = 2 << 30
	appleMaxFiles  = 10
)

// POST /import/apple: imports Apple Music history from the files of Apple's privacy export
// (the downloaded zips, or extracted CSVs) and responds with a summary
func importAppleHandler(w http.ResponseWriter, r *http.Request) {
	username := getLoggedInUsername(r)
	if username == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userId, err := getUserIdByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, appleMaxUpload)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Upload failed or too large (2GB max)", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()

	uploads := r.MultipartForm.File["apple_files"]
	if len(uploads) == 0 || len(uploads) > appleMaxFiles {
		http.Error(w, fmt.Sprintf("Choose between 1 and %d files", appleMaxFiles), http.StatusBadRequest)
		return
	}

	export := migrate.NewAppleExport()
	for _, u := range uploads {
		f, err := u.Open()
		if err != nil {
			http.Error(w, "Could not read "+u.Filename, http.StatusBadRequest)
			return
		}
		if strings.EqualFold(path.Ext(u.Filename), ".zip") {
			err = export.AddZip(f, u.Size)
		} else {
			err = export.AddFile(u.Filename, f)
		}
		f.Close()
		if err != nil {
			http.Error(w, fmt.Sprintf("Could not read %s: %v", u.Filename, err), http.StatusBadRequest)
			return
		}
	}
	if !export.HasPlayActivity() {
		http.Error(w, `Couldn't find "Apple Music Play Activity.csv" in those files. Upload the zip(s) from `+
			`Apple's data export, or the extracted Apple Music Activity files.`, http.StatusBadRequest)
		return
	}

	result, err := migrate.ImportApple(userId, export)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error importing Apple Music for %s: %v\n", username, err)
		http.Error(w, "Import failed", http.StatusInternalServerError)
		return
	}
	fmt.Printf("User %s imported %d plays from Apple Music\n", username, result.Imported)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
