package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeClock returns a limiter whose time is moved by hand.
func fakeClock(limit int, window time.Duration) (*attemptLimiter, *time.Time) {
	current := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	limiter := newAttemptLimiter(limit, window)
	limiter.now = func() time.Time { return current }
	return limiter, &current
}

// TestAttemptLimiterRefusesPastTheLimitUntilTheWindowEnds checks the basic
// promise: the allowance is spent, refused with the time left, and restored
// when the window closes.
func TestAttemptLimiterRefusesPastTheLimitUntilTheWindowEnds(t *testing.T) {
	limiter, now := fakeClock(3, time.Minute)

	for i := range 3 {
		if _, ok := limiter.take("client"); !ok {
			t.Fatalf("attempt %d was refused inside the allowance", i+1)
		}
	}

	*now = now.Add(20 * time.Second)
	wait, ok := limiter.take("client")
	if ok {
		t.Fatal("the attempt past the limit was allowed")
	}
	if wait != 40*time.Second {
		t.Errorf("wait = %v, want the 40s left in the window", wait)
	}

	// Another key is counted on its own.
	if _, ok := limiter.take("somebody else"); !ok {
		t.Error("a different key was refused")
	}

	*now = now.Add(40 * time.Second)
	if _, ok := limiter.take("client"); !ok {
		t.Error("the allowance did not come back when the window ended")
	}
}

// TestAttemptLimiterForgetClearsAKey checks that a cleared key starts afresh.
func TestAttemptLimiterForgetClearsAKey(t *testing.T) {
	limiter, _ := fakeClock(1, time.Minute)
	limiter.take("client")
	if _, ok := limiter.allowed("client"); ok {
		t.Fatal("the key was still allowed after its only attempt")
	}
	limiter.forget("client")
	if _, ok := limiter.allowed("client"); !ok {
		t.Error("the key was still refused after being forgotten")
	}
}

// TestAttemptLimiterSweepsExpiredWindows checks that the map does not keep
// every client that ever called.
func TestAttemptLimiterSweepsExpiredWindows(t *testing.T) {
	limiter, now := fakeClock(5, time.Minute)
	limiter.record("old")

	*now = now.Add(2 * time.Minute)
	limiter.record("new")

	if _, found := limiter.counts["old"]; found {
		t.Error("an expired window was kept")
	}
	if _, found := limiter.counts["new"]; !found {
		t.Error("a live window was dropped")
	}
}

// TestSignInLimitsStopGuessingOneAccount checks the per-address count holds
// across clients and that a right password clears it.
func TestSignInLimitsStopGuessingOneAccount(t *testing.T) {
	limits := newSignInLimits()

	for i := range signInsPerAccount {
		// Every attempt from a different client: rotating addresses must not
		// buy more guesses at one account.
		client := "10.0.0." + string(rune('a'+i))
		if _, ok := limits.admit(client, "owner@example.com"); !ok {
			t.Fatalf("attempt %d was refused inside the allowance", i+1)
		}
		limits.failed(client)
	}
	if _, ok := limits.admit("10.0.1.1", "owner@example.com"); ok {
		t.Fatal("an attempt past the limit was allowed from a fresh client")
	}
	if _, ok := limits.admit("10.0.1.1", "other@example.com"); !ok {
		t.Error("another address was refused")
	}

	limits.succeeded("owner@example.com")
	if _, ok := limits.admit("10.0.1.1", "owner@example.com"); !ok {
		t.Error("a right password did not clear the count")
	}
}

// TestSignInLimitsStopOneClientTryingManyAccounts checks the per-client count
// of wrong passwords.
func TestSignInLimitsStopOneClientTryingManyAccounts(t *testing.T) {
	limits := newSignInLimits()
	for range failedSignInsPerClient {
		limits.failed("10.0.0.1")
	}
	if _, ok := limits.admit("10.0.0.1", "fresh@example.com"); ok {
		t.Error("a client past its wrong-password limit was allowed a new address")
	}
	if _, ok := limits.admit("10.0.0.2", "fresh@example.com"); !ok {
		t.Error("another client was refused")
	}
}

// TestClientAddressPrefersTheProxysHeader checks where the client address is
// read from.
func TestClientAddressPrefersTheProxysHeader(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "172.18.0.5:41234"
	if got := clientAddress(request); got != "172.18.0.5" {
		t.Errorf("without X-Real-IP: got %q, want the peer's host", got)
	}
	request.Header.Set("X-Real-IP", "203.0.113.7")
	if got := clientAddress(request); got != "203.0.113.7" {
		t.Errorf("with X-Real-IP: got %q, want the header", got)
	}
}
