package scrobble

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type ListenbrainzHandler struct{}

func NewListenbrainzHandler() *ListenbrainzHandler {
	return &ListenbrainzHandler{}
}

type SubmitListensRequest struct {
	ListenType string          `json:"listen_type"`
	Payload    []ListenPayload `json:"payload"`
}

type ListenPayload struct {
	ListenedAt    int64         `json:"listened_at"`
	TrackMetadata TrackMetadata `json:"track_metadata"`
}

type TrackMetadata struct {
	ArtistName     string         `json:"artist_name"`
	TrackName      string         `json:"track_name"`
	ReleaseName    string         `json:"release_name"`
	AdditionalInfo AdditionalInfo `json:"additional_info"`
}

type AdditionalInfo struct {
	Duration int `json:"duration"`
}

func (h *ListenbrainzHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	apiKey := r.Header.Get("Authorization")
	if apiKey == "" {
		apiKey = r.URL.Query().Get("token")
	}

	if apiKey == "" {
		h.respondError(w, "No authorization token provided", 401)
		return
	}

	apiKey = stripBearer(apiKey)

	userId, _, err := GetUserByAPIKey(apiKey)
	if err != nil {
		h.respondError(w, "Invalid authorization token", 401)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.respondError(w, "Invalid request body", 400)
		return
	}
	defer r.Body.Close()

	var req SubmitListensRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.respondError(w, "Invalid JSON", 400)
		return
	}

	switch req.ListenType {
	case "single":
		h.handleScrobbles(w, userId, req.Payload)
	case "playing_now":
		h.handleNowPlaying(w, userId, req.Payload)
	case "import":
		h.handleScrobbles(w, userId, req.Payload)
	default:
		h.respondError(w, "Invalid listen_type", 400)
	}
}

func (h *ListenbrainzHandler) respondError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": message,
	})
}

func (h *ListenbrainzHandler) respondOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (h *ListenbrainzHandler) handleScrobbles(w http.ResponseWriter, userId int, payload []ListenPayload) {
	if len(payload) == 0 {
		h.respondError(w, "No listens provided", 400)
		return
	}

	scrobbles := make([]Scrobble, 0, len(payload))
	for _, p := range payload {
		duration := 0
		if p.TrackMetadata.AdditionalInfo.Duration > 0 {
			duration = p.TrackMetadata.AdditionalInfo.Duration * 1000
		}

		scrobbles = append(scrobbles, Scrobble{
			UserId:    userId,
			Timestamp: time.Unix(p.ListenedAt, 0).UTC(),
			SongName:  p.TrackMetadata.TrackName,
			Artist:    p.TrackMetadata.ArtistName,
			Album:     p.TrackMetadata.ReleaseName,
			MsPlayed:  duration,
			Platform:  "listenbrainz",
		})
	}

	accepted, ignored, err := SaveScrobbles(scrobbles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving scrobbles: %v\n", err)
		h.respondError(w, "Error saving scrobbles", 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"accepted":     accepted,
		"ignored":      ignored,
		"mbids":        []string{},
		"submit_token": "",
	})
}

func (h *ListenbrainzHandler) handleNowPlaying(w http.ResponseWriter, userId int, payload []ListenPayload) {
	if len(payload) == 0 {
		h.respondError(w, "No payload provided", 400)
		return
	}

	p := payload[0]
	duration := 0
	if p.TrackMetadata.AdditionalInfo.Duration > 0 {
		duration = p.TrackMetadata.AdditionalInfo.Duration * 1000
	}

	UpdateNowPlaying(NowPlaying{
		UserId:    userId,
		SongName:  p.TrackMetadata.TrackName,
		Artist:    p.TrackMetadata.ArtistName,
		Album:     p.TrackMetadata.ReleaseName,
		MsPlayed:  duration,
		Platform:  "listenbrainz",
		UpdatedAt: time.Now(),
	})

	h.respondOK(w)
}

func stripBearer(token string) string {
	if len(token) > 7 && strings.HasPrefix(token, "Bearer ") {
		return token[7:]
	}
	if len(token) > 6 && strings.HasPrefix(token, "Token ") {
		return token[6:]
	}
	return token
}

func ParseTimestamp(ts interface{}) (time.Time, error) {
	switch v := ts.(type) {
	case float64:
		return time.Unix(int64(v), 0).UTC(), nil
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(i, 0).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unknown timestamp type")
	}
}
