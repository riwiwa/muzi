// Package ratelimit limits password guessing on the login form. Failures are counted per
// client IP and per username over a sliding window; once either reaches the limit, logins for
// it are refused until its oldest failure ages out.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginWindow      = 15 * time.Minute
	loginMaxFailures = 5
	// bounds memory if someone sprays many usernames or IPs
	loginMaxTracked = 10000
)

// LoginLimiter tracks failed logins
type LoginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

// Logins is the limiter for the web login form
var Logins = &LoginLimiter{failures: map[string][]time.Time{}}

// LoginKeys are the keys a login attempt is counted under: the client and the account it targets
func LoginKeys(r *http.Request, username string) []string {
	return []string{"ip:" + ClientIP(r), "user:" + strings.ToLower(username)}
}

// ClientIP is the request's client address. X-Forwarded-For is only trusted from a proxy on this machine,
// since anyone else could send it to dodge the limit.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			return strings.TrimSpace(strings.Split(fwd, ",")[0])
		}
	}
	return host
}

// Drops failures older than the window; call with the lock held
func (l *LoginLimiter) recent(key string, now time.Time) []time.Time {
	times := l.failures[key]
	i := 0
	for i < len(times) && now.Sub(times[i]) >= loginWindow {
		i++
	}
	times = times[i:]
	if len(times) == 0 {
		delete(l.failures, key)
	} else {
		l.failures[key] = times
	}
	return times
}

// Blocked reports whether any key is locked out, and how long until the longest lockout ends
func (l *LoginLimiter) Blocked(keys []string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var wait time.Duration
	for _, key := range keys {
		times := l.recent(key, now)
		if len(times) >= loginMaxFailures {
			// locked until enough failures age out to drop back under the limit
			wait = max(wait, loginWindow-now.Sub(times[len(times)-loginMaxFailures]))
		}
	}
	return wait > 0, wait
}

// Fail records a failed login
func (l *LoginLimiter) Fail(keys []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.failures) > loginMaxTracked {
		for key := range l.failures {
			l.recent(key, now)
		}
	}
	for _, key := range keys {
		l.failures[key] = append(l.recent(key, now), now)
	}
}

// Succeed clears the failures for a successful login
func (l *LoginLimiter) Succeed(keys []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		delete(l.failures, key)
	}
}
