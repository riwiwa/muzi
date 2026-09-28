package web

import (
	"net"
	"net/http"
	"strings"

	"muzi/config"
)

// Whether cookies should be marked Secure (sent only over HTTPS): the request arrived over TLS,
// a reverse proxy on this machine says the client used HTTPS, or public_url is https.
func secureCookies(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.HasPrefix(strings.ToLower(config.Get().Server.PublicUrl), "https://") {
		return true
	}
	// only a proxy on this machine is trusted to report the scheme
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
	}
	return false
}
