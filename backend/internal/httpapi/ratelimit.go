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
	// signInsPerAccountClient bounds sign-ins at one address from one client,
	// which is what stops a single machine guessing one account's password. It
	// holds back only that client, so guessing from one machine cannot lock the
	// owner out; a right password clears it.
	signInsPerAccountClient = 10
	// signInsPerAccount bounds sign-ins at one address from every client the
	// account has not been signed in from, which is what stops its password
	// being guessed from many machines. One client reaches its own limit of
	// wrong passwords long before this, so locking the owner out takes many
	// machines, and even then not on a client they signed in from before.
	signInsPerAccount = 100
	// knownClientLifetime is how long a client that signed in to an account
	// stays exempt from that account's count across clients.
	knownClientLifetime = 30 * 24 * time.Hour
	// failedSignInsPerClient bounds wrong passwords from one client across every
	// address, which is what stops one password being tried against many accounts.
	failedSignInsPerClient = 50
	// passwordChecksPerAccount bounds how often a signed-in account may type its
	// current password - to change it or to delete the account - before a right
	// one clears the count. It stops a session, left open or stolen, being used
	// to guess the password it was opened with.
	passwordChecksPerAccount = 10
	// passwordResetsPerAccount bounds recovery mail for one address across every
	// client, which prevents distributed requests from flooding one mailbox.
	passwordResetsPerAccount = 5
	// passwordResetsPerClient bounds recovery requests from one client across
	// every address, which prevents one machine from using the service to send
	// mail to many accounts.
	passwordResetsPerClient = 20
	// registrationsPerAccount bounds registrations of one address, each of
	// which may send a confirmation message, across every client.
	registrationsPerAccount = 5
	// registrationsPerClient bounds registrations from one client across every
	// address, which prevents one machine from making accounts in bulk.
	registrationsPerClient = 20
	// verificationsPerClient bounds confirmation attempts from one client
	// across every address. Each code also allows only a few wrong guesses;
	// this stops one machine trying a guess at many addresses instead.
	verificationsPerClient = 30
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

// signInLimits holds the counts a password sign-in is checked against.
type signInLimits struct {
	// perAccountClient counts sign-ins at one address, right or wrong, from one
	// client, and is cleared by a right one.
	perAccountClient *attemptLimiter
	// perAccount counts sign-ins at one address, right or wrong, from clients
	// that have not signed in to it, and is cleared by a right one.
	perAccount *attemptLimiter
	// perClient counts wrong passwords from one client at any address.
	perClient *attemptLimiter
	// known remembers the clients each address was signed in to from, until
	// the moment each stops being exempt.
	known *knownClients
}

// newSignInLimits creates the sign-in counts with the service's limits.
func newSignInLimits() *signInLimits {
	return &signInLimits{
		perAccountClient: newAttemptLimiter(signInsPerAccountClient, attemptWindow),
		perAccount:       newAttemptLimiter(signInsPerAccount, attemptWindow),
		perClient:        newAttemptLimiter(failedSignInsPerClient, attemptWindow),
		known:            newKnownClients(knownClientLifetime),
	}
}

// signInKey names one client's sign-ins at one address. The separator cannot
// appear in either part, so two different pairs never share a key.
func signInKey(client, email string) string {
	return email + "\x00" + client
}

// knownClients remembers which clients signed in to which address, so that
// somebody guessing an account's password from elsewhere does not keep its
// owner out on the machines they use.
//
// Like the counts it lives in memory: a restart forgets it, and the owner is
// then held to the count across clients until they sign in again.
type knownClients struct {
	lifetime time.Duration
	now      func() time.Time

	mu        sync.Mutex
	until     map[string]time.Time
	lastSweep time.Time
}

// newKnownClients creates an empty memory of clients kept for lifetime.
func newKnownClients(lifetime time.Duration) *knownClients {
	return &knownClients{lifetime: lifetime, now: time.Now, until: make(map[string]time.Time)}
}

// has reports whether the key signed in within the lifetime.
func (k *knownClients) has(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	until, found := k.until[key]
	return found && k.now().Before(until)
}

// remember records a sign-in under the key, dropping the entries that expired
// at most once a day so the map does not grow with every client ever seen.
func (k *knownClients) remember(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if now.Sub(k.lastSweep) >= 24*time.Hour {
		for stored, until := range k.until {
			if !now.Before(until) {
				delete(k.until, stored)
			}
		}
		k.lastSweep = now
	}
	k.until[key] = now.Add(k.lifetime)
}

// admit decides whether a sign-in may go ahead, and counts it against the
// address if so: always for this client, and across clients unless this client
// signed in to the address before.
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
	key := signInKey(client, email)
	known := l.known.has(key)
	if !known {
		if wait, ok := l.perAccount.allowed(email); !ok {
			return wait, false
		}
	}
	if wait, ok := l.perAccountClient.take(key); !ok {
		return wait, false
	}
	if !known {
		l.perAccount.record(email)
	}
	return 0, true
}

// failed counts a wrong password against the client.
func (l *signInLimits) failed(client string) {
	l.perClient.record(client)
}

// succeeded clears the address's counts and remembers the client as one the
// address is signed in from. The client's count of wrong passwords stays: one
// right password among many wrong ones is what trying one password against
// many accounts looks like.
//
// Arguments:
//   - client: the address the request came from.
//   - email: the address signed in to, already normalised.
func (l *signInLimits) succeeded(client, email string) {
	key := signInKey(client, email)
	l.perAccountClient.forget(key)
	l.perAccount.forget(email)
	l.known.remember(key)
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

// writeTooManyAttempts refuses a sign-in or a password check that has used up
// its allowance and says in Retry-After when to come back.
func (s *Server) writeTooManyAttempts(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	seconds := max(int(math.Ceil(wait.Seconds())), 1)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))

	s.logger.Warn("refused a password attempt over its limit",
		slog.String("request_id", RequestIDFrom(r.Context())),
		slog.String("client", clientAddress(r)),
		slog.Int("retry_after_seconds", seconds))

	s.writeErrorDetails(w, r, http.StatusTooManyRequests, "too_many_attempts",
		"Too many password attempts. Try again later.", map[string]any{"retry_after": seconds})
}
