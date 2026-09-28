package web

// CSRF protection for logged-in browsers. Each session's token is an HMAC of its session ID,
// so a token can't be reused for another session and a cookie planted by another site can't
// produce a valid one. The token is exposed to the page in a script-readable cookie; app.js
// sends it as the X-CSRF-Token header and adds it to POST forms as the csrf_token field.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"muzi/db"
)

const (
	csrfCookie = "csrf_token"
	csrfHeader = "X-CSRF-Token"
	csrfField  = "csrf_token"
	// enough for a profile picture upload that carries the token as a form field
	csrfMaxFormBody = 6 << 20
)

// Paths that authenticate some other way (API keys) or before a session exists
var csrfExempt = map[string]bool{
	"/2.0":                 true,
	"/2.0/":                true,
	"/1/submit-listens":    true,
	"/loginsubmit":         true,
	"/createaccountsubmit": true,
}

var (
	csrfKeyOnce sync.Once
	csrfKey     []byte
)

func csrfSecret() []byte {
	csrfKeyOnce.Do(func() {
		key, err := db.GetOrCreateSecret("csrf")
		if err != nil {
			// fail closed: no key means no token will ever match
			fmt.Fprintf(os.Stderr, "Error loading CSRF secret: %v\n", err)
			return
		}
		csrfKey = key
	})
	return csrfKey
}

func csrfToken(sessionID string) string {
	key := csrfSecret()
	if key == nil {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// Checks the CSRF token on state-changing requests from a logged-in browser, and keeps the
// token cookie up to date so the page can read it.
func csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := r.Cookie("session")
		if err != nil || session.Value == "" {
			// no session: nothing to forge; handlers that need a login reject the request
			next.ServeHTTP(w, r)
			return
		}
		expected := csrfToken(session.Value)

		if c, err := r.Cookie(csrfCookie); expected != "" && (err != nil || c.Value != expected) {
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookie,
				Value:    expected,
				Path:     "/",
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400 * 30,
			})
		}

		if isUnsafeMethod(r.Method) && !csrfExempt[r.URL.Path] {
			got := r.Header.Get(csrfHeader)
			if got == "" && expected != "" {
				// plain form posts carry it as a field; cap the body before parsing it
				r.Body = http.MaxBytesReader(w, r.Body, csrfMaxFormBody)
				if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
					r.ParseMultipartForm(csrfMaxFormBody)
				}
				got = r.PostFormValue(csrfField)
			}
			if expected == "" || !hmac.Equal([]byte(got), []byte(expected)) {
				http.Error(w, "Invalid or missing CSRF token. Reload the page and try again.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
