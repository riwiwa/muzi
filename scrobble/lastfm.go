package scrobble

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"muzi/db"
)

type LastFMHandler struct{}

func NewLastFMHandler() *LastFMHandler {
	return &LastFMHandler{}
}

func (h *LastFMHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if r.URL.Query().Get("hs") == "true" {
			h.handleHandshake(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		h.respond(w, "failed", 400, "Invalid request")
		return
	}

	method := r.PostForm.Get("method")
	apiKey := r.PostForm.Get("api_key")
	sk := r.PostForm.Get("s")
	track := r.PostForm.Get("t")

	if method != "" {
		switch method {
		case "auth.gettoken":
			h.handleGetToken(w, apiKey)
		case "auth.getsession":
			h.handleGetSession(w, r)
		case "track.updateNowPlaying":
			h.handleNowPlaying(w, r)
		case "track.scrobble":
			h.handleScrobble(w, r)
		default:
			h.respond(w, "failed", 400, fmt.Sprintf("Invalid method: %s", method))
		}
		return
	}

	if sk != "" {
		if r.PostForm.Get("a[0]") != "" && (r.PostForm.Get("t[0]") != "" || r.PostForm.Get("i[0]") != "") {
			h.handleScrobble(w, r)
			return
		}
		if track != "" {
			h.handleNowPlaying(w, r)
			return
		}
	}

	h.respond(w, "failed", 400, "Missing required parameters")
}

func (h *LastFMHandler) respond(w http.ResponseWriter, status string, code int, message string) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(fmt.Sprintf("FAILED %s", message)))
}

func (h *LastFMHandler) respondOK(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(content))
}

func (h *LastFMHandler) handleHandshake(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("u")
	token := r.URL.Query().Get("t")
	authToken := r.URL.Query().Get("a")

	if username == "" || token == "" || authToken == "" {
		w.Write([]byte("BADAUTH"))
		return
	}

	userId, err := GetUserByUsername(username)
	if err != nil {
		w.Write([]byte("BADAUTH"))
		return
	}

	sessionKey, err := GenerateSessionKey()
	if err != nil {
		w.Write([]byte("FAILED Could not generate session"))
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`UPDATE users SET api_secret = $1 WHERE pk = $2`,
		sessionKey, userId)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating session key: %v\n", err)
		w.Write([]byte("FAILED Database error"))
		return
	}

	baseURL := getBaseURL(r)
	w.Write([]byte(fmt.Sprintf("OK\n%s\n%s/2.0/\n%s/2.0/\n", sessionKey, baseURL, baseURL)))
}

func (h *LastFMHandler) handleGetToken(w http.ResponseWriter, apiKey string) {
	userId, _, err := GetUserByAPIKey(apiKey)
	if err != nil {
		h.respond(w, "failed", 10, "Invalid API key")
		return
	}

	token, err := GenerateSessionKey()
	if err != nil {
		h.respond(w, "failed", 16, "Service temporarily unavailable")
		return
	}

	h.respondOK(w, fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<lfm status="ok">
  <token>%s</token>
</lfm>`, token))
	_ = userId
}

func (h *LastFMHandler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	apiKey := r.FormValue("api_key")

	userId, username, err := GetUserByAPIKey(apiKey)
	if err != nil {
		h.respond(w, "failed", 10, "Invalid API key")
		return
	}

	sessionKey, err := GenerateSessionKey()
	if err != nil {
		h.respond(w, "failed", 16, "Service temporarily unavailable")
		return
	}

	_, err = db.Pool.Exec(context.Background(),
		`UPDATE users SET api_secret = $1 WHERE pk = $2`,
		sessionKey, userId)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating session key: %v\n", err)
		h.respond(w, "failed", 16, "Service temporarily unavailable")
		return
	}

	h.respondOK(w, fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<lfm status="ok">
  <session>
    <name>%s</name>
    <key>%s</key>
    <subscriber>0</subscriber>
  </session>
</lfm>`, username, sessionKey))
}

func (h *LastFMHandler) handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	sessionKey := r.PostForm.Get("s")
	if sessionKey == "" {
		h.respond(w, "failed", 9, "Invalid session")
		return
	}

	userId, _, err := GetUserBySessionKey(sessionKey)
	if err != nil {
		h.respond(w, "failed", 9, "Invalid session")
		return
	}

	artist := r.PostForm.Get("a")
	track := r.PostForm.Get("t")
	album := r.PostForm.Get("b")

	if track == "" {
		h.respondOK(w, "OK")
		return
	}

	duration := r.PostForm.Get("l")
	msPlayed := 0
	if duration != "" {
		if d, err := strconv.Atoi(duration); err == nil {
			msPlayed = d * 1000
		}
	}

	UpdateNowPlaying(NowPlaying{
		UserId:    userId,
		SongName:  track,
		Artist:    artist,
		Album:     album,
		MsPlayed:  msPlayed,
		Platform:  "lastfm_api",
		UpdatedAt: time.Now(),
	})

	h.respondOK(w, "OK")
}

func (h *LastFMHandler) handleScrobble(w http.ResponseWriter, r *http.Request) {
	sessionKey := r.PostForm.Get("s")
	if sessionKey == "" {
		h.respond(w, "failed", 9, "Invalid session")
		return
	}

	userId, _, err := GetUserBySessionKey(sessionKey)
	if err != nil {
		h.respond(w, "failed", 9, "Invalid session")
		return
	}

	scrobbles := h.parseScrobbles(r.PostForm, userId)
	if len(scrobbles) == 0 {
		h.respond(w, "failed", 1, "No scrobbles to submit")
		return
	}

	accepted, ignored := 0, 0
	for _, scrobble := range scrobbles {
		err := SaveScrobble(scrobble)
		if err != nil {
			if err.Error() == "duplicate scrobble" {
				ignored++
			}
			continue
		}
		accepted++
	}

	ClearNowPlaying(userId)

	h.respondOK(w, fmt.Sprintf("OK\n%d\n%d\n", accepted, ignored))
}

func (h *LastFMHandler) parseScrobbles(form url.Values, userId int) []Scrobble {
	var scrobbles []Scrobble

	for i := 0; i < 50; i++ {
		var artist, track, album, timestampStr string

		if i == 0 {
			artist = form.Get("a[0]")
			track = form.Get("t[0]")
			album = form.Get("b[0]")
			timestampStr = form.Get("i[0]")
		} else {
			artist = form.Get(fmt.Sprintf("a[%d]", i))
			track = form.Get(fmt.Sprintf("t[%d]", i))
			album = form.Get(fmt.Sprintf("b[%d]", i))
			timestampStr = form.Get(fmt.Sprintf("i[%d]", i))
		}

		if artist == "" || track == "" || timestampStr == "" {
			break
		}

		ts, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			continue
		}

		duration := form.Get(fmt.Sprintf("l[%d]", i))
		msPlayed := 0
		if duration != "" {
			if d, err := strconv.Atoi(duration); err == nil {
				msPlayed = d * 1000
			}
		}

		scrobbles = append(scrobbles, Scrobble{
			UserId:    userId,
			Timestamp: time.Unix(ts, 0).UTC(),
			SongName:  track,
			Artist:    artist,
			Album:     album,
			MsPlayed:  msPlayed,
			Platform:  "lastfm_api",
		})
	}

	return scrobbles
}

func SignRequest(params map[string]string, secret string) string {
	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	var str string
	for _, k := range keys {
		str += k + params[k]
	}
	str += secret

	hash := md5.Sum([]byte(str))
	return hex.EncodeToString(hash[:])
}

func SignAPIRequest(params map[string]string, secret string) string {
	var pairs []string
	for k, v := range params {
		pairs = append(pairs, k+"="+url.QueryEscape(v))
	}
	signature := SignRequest(map[string]string{"api_key": params["api_key"], "method": params["method"]}, secret)
	return signature
}

func FetchURL(client *http.Client, endpoint, method string, params map[string]string) (string, error) {
	data := url.Values{}
	for k, v := range params {
		data.Set(k, v)
	}
	req, err := http.NewRequest(method, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}
