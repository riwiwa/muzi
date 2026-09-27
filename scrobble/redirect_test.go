package scrobble

import (
	"net/http/httptest"
	"testing"
)

func TestSpotifyRedirectURI(t *testing.T) {
	cases := []struct {
		host, proto, fwdHost, want string
	}{
		{"localhost:1234", "", "", "http://127.0.0.1:1234/scrobble/spotify/callback"},
		{"127.0.0.1:1234", "", "", "http://127.0.0.1:1234/scrobble/spotify/callback"},
		{"192.168.1.5:1234", "", "", "http://192.168.1.5:1234/scrobble/spotify/callback"},
		{"127.0.0.1:1234", "https", "muzi.example.com", "https://muzi.example.com/scrobble/spotify/callback"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = c.host
		if c.proto != "" {
			r.Header.Set("X-Forwarded-Proto", c.proto)
		}
		if c.fwdHost != "" {
			r.Header.Set("X-Forwarded-Host", c.fwdHost)
		}
		if got := SpotifyRedirectURI(r); got != c.want {
			t.Errorf("host %q: got %q, want %q", c.host, got, c.want)
		}
	}
}
