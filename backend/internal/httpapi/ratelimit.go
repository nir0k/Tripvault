package httpapi

import (
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sign-in limits. They are constants rather than settings: they exist to make
// guessing a password slow, and no legitimate use comes near them.
const (
	// attemptWindow is how long a counted attempt is remembered.
	attemptWindow = 15 * time.Minute
	// signInsPerAccount bounds sign-ins at one address from anywhere. It is what
	// stops one account's password being guessed from many addresses; a right
	// password clears it. The cost - somebody guessing can lock the owner out
	// for a window - is the trade the specification makes.
	signInsPerAccount = 10
	// failedSignInsPerClient bounds wrong passwords from one client across every
	// address, which is what stops one password being tried against many accounts.
	failedSignInsPerClient = 50
)

// attemptLimiter counts events per key over a fixed window.
//
// It lives in the process's memory: the service runs as one instance, and a
// restart forgetting the counts costs an attacker a restart they cannot cause.
type attemptLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	counts    map[string]attemptCount
	lastSweep time.Time
}

// attemptCount is how many events a key has had in its current window.
type attemptCount struct {
	events  int
	resetAt time.Time
}

// newAttemptLimiter creates a limiter allowing limit events per key per window.
func newAttemptLimiter(limit int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		counts: make(map[string]attemptCount),
	}
}

// allowed reports whether key may have another event now, without counting
// one. When it may not, the duration says how long until it can.
func (l *attemptLimiter) allowed(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.check(key, l.now())
}

// take counts an event against key if it is allowed, in one step, so that
// concurrent requests cannot all pass the check before any of them is counted.
// When it is not allowed, the duration says how long until it is.
func (l *attemptLimiter) take(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if wait, ok := l.check(key, now); !ok {
		return wait, false
	}
	l.add(key, now)
	return 0, true
}

// record counts an event against key whatever its current count.
func (l *attemptLimiter) record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.add(key, l.now())
}

// forget clears key's count.
func (l *attemptLimiter) forget(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.counts, key)
}

// check reports whether key is under its limit at now. The caller holds the lock.
func (l *attemptLimiter) check(key string, now time.Time) (time.Duration, bool) {
	count, found := l.counts[key]
	if !found || !now.Before(count.resetAt) || count.events < l.limit {
		return 0, true
	}
	return count.resetAt.Sub(now), false
}

// add counts one event against key, opening a window if it has none. The
// caller holds the lock.
func (l *attemptLimiter) add(key string, now time.Time) {
	l.sweep(now)
	count, found := l.counts[key]
	if !found || !now.Before(count.resetAt) {
		count = attemptCount{resetAt: now.Add(l.window)}
	}
	count.events++
	l.counts[key] = count
}

// sweep drops expired windows, at most once a window, so the map holds the
// clients seen recently rather than everybody who ever called. The caller
// holds the lock.
func (l *attemptLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	for key, count := range l.counts {
		if !now.Before(count.resetAt) {
			delete(l.counts, key)
		}
	}
	l.lastSweep = now
}

// signInLimits holds the two counts a password sign-in is checked against.
type signInLimits struct {
	// perAccount counts sign-ins at one address, right or wrong, from any
	// client, and is cleared by a right one.
	perAccount *attemptLimiter
	// perClient counts wrong passwords from one client at any address.
	perClient *attemptLimiter
}

// newSignInLimits creates the sign-in counts with the service's limits.
func newSignInLimits() *signInLimits {
	return &signInLimits{
		perAccount: newAttemptLimiter(signInsPerAccount, attemptWindow),
		perClient:  newAttemptLimiter(failedSignInsPerClient, attemptWindow),
	}
}

// admit decides whether a sign-in may go ahead, and counts it against the
// address if so.
//
// It runs before the password is checked and answers the same way for an
// address that has an account and one that does not, so a refusal says nothing
// about which addresses are registered.
//
// Arguments:
//   - client: the address the request came from.
//   - email: the address being signed in to, already normalised.
//
// Returns:
//   - how long to wait before trying again, when refused.
//   - true when the sign-in may go ahead.
func (l *signInLimits) admit(client, email string) (time.Duration, bool) {
	if wait, ok := l.perClient.allowed(client); !ok {
		return wait, false
	}
	return l.perAccount.take(email)
}

// failed counts a wrong password against the client.
func (l *signInLimits) failed(client string) {
	l.perClient.record(client)
}

// succeeded clears the address's count. The client's count of wrong passwords
// stays: one right password among many wrong ones is what trying one password
// against many accounts looks like.
func (l *signInLimits) succeeded(email string) {
	l.perAccount.forget(email)
}

// clientAddress names the client a request came from, for counting attempts.
//
// Behind the bundled nginx the peer is always the proxy, so the address nginx
// passes on in X-Real-IP is used; nginx sets that header itself, replacing
// whatever a client sent. Behind a further proxy or tunnel nginx reads the
// client from X-Forwarded-For once TRIPVAULT_TRUSTED_PROXY names that proxy;
// until it does, every client arrives from one address and the per-client count
// covers all of them together.
func clientAddress(r *http.Request) string {
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		return real
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeTooManyAttempts refuses a request that has used up its allowance and
// says in Retry-After when to come back.
func (s *Server) writeTooManyAttempts(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	seconds := max(int(math.Ceil(wait.Seconds())), 1)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))

	s.logger.Warn("refused a sign-in over its attempt limit",
		slog.String("request_id", RequestIDFrom(r.Context())),
		slog.String("client", clientAddress(r)),
		slog.Int("retry_after_seconds", seconds))

	s.writeErrorDetails(w, r, http.StatusTooManyRequests, "too_many_attempts",
		"Too many sign-in attempts. Try again later.", map[string]any{"retry_after": seconds})
}
