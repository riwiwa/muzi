package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"muzi/db"
	"muzi/internal/dbtest"
)

func requestAs(username, method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	if username != "" {
		r.AddCookie(&http.Cookie{Name: "session", Value: createSession(username)})
	}
	return r
}

func TestRequireOwner(t *testing.T) {
	dbtest.Setup(t)
	owner, ownerName := dbtest.CreateUser(t)
	_, otherName := dbtest.CreateUser(t)
	artistId, _, err := db.GetOrCreateArtist(owner, "Some Artist")
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		who    string
		ok     bool
		status int
	}{
		{ownerName, true, http.StatusOK},
		{otherName, false, http.StatusNotFound},
		{"", false, http.StatusUnauthorized},
	} {
		w := httptest.NewRecorder()
		if got := requireOwner(w, requestAs(c.who, "PATCH", "/"), "artists", artistId); got != c.ok || w.Code != c.status {
			t.Errorf("user %q: allowed=%v status=%d, want %v %d", c.who, got, w.Code, c.ok, c.status)
		}
	}
}

func TestCanViewProfile(t *testing.T) {
	dbtest.Setup(t)
	owner, ownerName := dbtest.CreateUser(t)
	_, otherName := dbtest.CreateUser(t)

	// profiles start private
	if !canViewProfile(requestAs(ownerName, "GET", "/"), owner) {
		t.Error("owners must see their own private profile")
	}
	if canViewProfile(requestAs(otherName, "GET", "/"), owner) || canViewProfile(requestAs("", "GET", "/"), owner) {
		t.Error("a private profile was visible to someone else")
	}
	db.Pool.Exec(context.Background(), "UPDATE users SET public_profile = true WHERE pk = $1", owner)
	if !canViewProfile(requestAs("", "GET", "/"), owner) {
		t.Error("a public profile should be visible to everyone")
	}
}

func TestCSRFMiddleware(t *testing.T) {
	dbtest.Setup(t)
	_, name := dbtest.CreateUser(t)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := csrfMiddleware(ok)
	serve := func(r *http.Request) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	r := requestAs(name, "POST", "/settings/update-bio")
	session, _ := r.Cookie("session")
	token := csrfToken(session.Value)

	if code := serve(r); code != http.StatusForbidden {
		t.Errorf("POST without a token: %d, want 403", code)
	}
	r = requestAs("", "POST", "/settings/update-bio")
	r.AddCookie(session)
	r.Header.Set("X-CSRF-Token", token)
	if code := serve(r); code != http.StatusOK {
		t.Errorf("POST with the session's token: %d, want 200", code)
	}

	// another session's token doesn't work
	other := requestAs(name, "POST", "/")
	otherSession, _ := other.Cookie("session")
	r = requestAs("", "POST", "/settings/update-bio")
	r.AddCookie(session)
	r.Header.Set("X-CSRF-Token", csrfToken(otherSession.Value))
	if code := serve(r); code != http.StatusForbidden {
		t.Errorf("POST with another session's token: %d, want 403", code)
	}

	// form field instead of header
	r = httptest.NewRequest("POST", "/settings/update-bio", strings.NewReader("bio=x&csrf_token="+token))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(session)
	if code := serve(r); code != http.StatusOK {
		t.Errorf("POST with the token as a form field: %d, want 200", code)
	}

	// API-key endpoints and reads don't need a token
	if code := serve(requestAs(name, "POST", "/1/submit-listens")); code != http.StatusOK {
		t.Errorf("exempt API endpoint: %d, want 200", code)
	}
	if code := serve(requestAs(name, "GET", "/settings")); code != http.StatusOK {
		t.Errorf("GET: %d, want 200", code)
	}
}
