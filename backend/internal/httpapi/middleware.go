package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// contextKey is the unexported key type for values this package stores in the
// request context, so no other package can collide with or forge them.
type contextKey string

// Context keys.
const (
	requestIDKey contextKey = "request_id"
	principalKey contextKey = "principal"
)

// principal is the signed-in account behind a request and the session it used.
type principal struct {
	user      domain.User
	sessionID uuid.UUID
}

// RequestIDFrom - extracts the correlation ID of the current request.
//
// Arguments:
//   - ctx: the request context.
//
// Returns:
//   - the request ID, or an empty string outside the request-ID middleware.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// principalFrom returns the signed-in account placed on the request by
// requireUser. Handlers behind that middleware can rely on it being present.
func principalFrom(ctx context.Context) principal {
	p, _ := ctx.Value(principalKey).(principal)
	return p
}

// requestID assigns a correlation ID to every request, reusing an inbound
// X-Request-Id from the proxy, and echoes it back so a client can quote it.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			buf := make([]byte, 8)
			_, _ = rand.Read(buf)
			id = hex.EncodeToString(buf)
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// requestLogger emits one logfmt record per completed request.
//
// Only the path is logged, never the query string or headers: tokens must not
// reach the log, and a query string is where a careless client would put one.
// Health probes are logged at debug level so they do not drown real traffic.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(recorder, r)

			level := slog.LevelInfo
			switch {
			case recorder.Status() >= http.StatusInternalServerError:
				level = slog.LevelError
			case r.URL.Path == "/healthz" || r.URL.Path == "/readyz":
				level = slog.LevelDebug
			}
			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.Status()),
				slog.Int("bytes", recorder.BytesWritten()),
				slog.Duration("duration", time.Since(started)),
				slog.String("client", clientAddress(r)),
			)
		})
	}
}

// recoverPanic turns a panicking handler into a 500, keeping the server alive
// and recording the failure with its request ID.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			// A client disconnect surfaces as this sentinel and is not a fault.
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			s.logger.Error("panic recovered",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.Any("panic", recovered))
			s.writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error")
		}()
		next.ServeHTTP(w, r)
	})
}

// apiHeaders sets the security headers every API response carries. A JSON
// response is never meant to be rendered, framed or cached by a shared proxy.
func apiHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		header.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// bearerPrefix is the scheme the API accepts. Tokens travel in the
// Authorization header rather than a cookie, so a mobile client needs no
// browser-specific handling.
const bearerPrefix = "Bearer "

// requireUser rejects requests without a valid access token and places the
// signed-in account on the request.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, bearerPrefix) {
			s.writeUnauthorized(w, r, "A bearer access token is required")
			return
		}

		user, sessionID, err := s.auth.Authenticate(r.Context(), strings.TrimSpace(header[len(bearerPrefix):]))
		if errors.Is(err, domain.ErrTokenInvalid) {
			s.writeUnauthorized(w, r, "The access token is invalid or has expired")
			return
		}
		if err != nil {
			s.internalError(w, r, "authenticate", err)
			return
		}

		ctx := context.WithValue(r.Context(), principalKey, principal{user: user, sessionID: sessionID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requirePasswordChanged refuses everything but the password change itself to
// an account still holding a password an administrator chose. It is enforced
// here rather than only in the interface, so the API cannot be used to work
// around it.
func (s *Server) requirePasswordChanged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principalFrom(r.Context()).user.MustChangePassword {
			s.writeError(w, r, http.StatusForbidden, "password_change_required",
				"The temporary password must be changed before anything else")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin refuses anybody who does not administer the service. The flag
// comes from the account read on this request, not from the token, so a
// withdrawn right takes effect at once.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !principalFrom(r.Context()).user.IsAdmin {
			s.writeError(w, r, http.StatusForbidden, "forbidden", "Only an administrator can do that")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeUnauthorized renders a 401 in the standard error envelope.
func (s *Server) writeUnauthorized(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="tripvault"`)
	s.writeError(w, r, http.StatusUnauthorized, "unauthorized", message)
}

// requireBackups refuses the backup endpoints on an instance that has none.
//
// The service starts without them when its backup directory cannot be opened,
// because a server that will not start is worse than one that cannot be copied
// until somebody fixes the volume. The screens then say so rather than failing
// on a nil service.
func (s *Server) requireBackups(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.backups == nil || s.backupService == nil {
			s.writeError(w, r, http.StatusServiceUnavailable, "backups_unavailable",
				"Backups are not available on this server; check the backup directory in its configuration")
			return
		}
		next.ServeHTTP(w, r)
	})
}
