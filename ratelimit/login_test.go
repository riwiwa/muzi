package ratelimit

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimiter(t *testing.T) {
	l := &LoginLimiter{failures: map[string][]time.Time{}}
	keys := []string{"ip:1.2.3.4", "user:r"}

	for i := 0; i < loginMaxFailures-1; i++ {
		l.Fail(keys)
	}
	if locked, _ := l.Blocked(keys); locked {
		t.Fatalf("locked after %d failures, limit is %d", loginMaxFailures-1, loginMaxFailures)
	}
	l.Fail(keys)
	locked, wait := l.Blocked(keys)
	if !locked || wait <= 0 || wait > loginWindow {
		t.Fatalf("expected a lockout of up to %v, got locked=%v wait=%v", loginWindow, locked, wait)
	}

	// the username stays locked from any other address
	if locked, _ := l.Blocked([]string{"ip:5.6.7.8", "user:r"}); !locked {
		t.Error("username should be locked regardless of IP")
	}
	// other accounts from other addresses are unaffected
	if locked, _ := l.Blocked([]string{"ip:5.6.7.8", "user:someone"}); locked {
		t.Error("unrelated login should not be locked")
	}

	// failures older than the window stop counting
	for _, key := range keys {
		for i := range l.failures[key] {
			l.failures[key][i] = l.failures[key][i].Add(-loginWindow)
		}
	}
	if locked, _ := l.Blocked(keys); locked {
		t.Error("lockout should end once failures age out")
	}

	l.Fail(keys)
	l.Succeed(keys)
	if len(l.failures) != 0 {
		t.Errorf("a successful login should clear failures, still tracking %v", l.failures)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("POST", "/loginsubmit", nil)
	r.RemoteAddr = "10.0.0.9:5555"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	if got := ClientIP(r); got != "10.0.0.9" {
		t.Errorf("X-Forwarded-For from a non-local client must be ignored, got %s", got)
	}
	r.RemoteAddr = "127.0.0.1:5555"
	if got := ClientIP(r); got != "9.9.9.9" {
		t.Errorf("X-Forwarded-For from a local proxy should be used, got %s", got)
	}
}
