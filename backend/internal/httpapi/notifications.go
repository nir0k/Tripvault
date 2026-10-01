package httpapi

import (
	"net/http"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/mailer"
)

// queueSecurityMail queues a message that cannot be disabled in user preferences.
func (s *Server) queueSecurityMail(r *http.Request, user domain.User, content mailer.Content) {
	if !s.mailEnabled(r) || s.mail == nil {
		return
	}
	if err := s.mail.EnqueueMail(r.Context(), mailer.Message(user.Email, content)); err != nil {
		s.logger.Warn("queue security mail failed", "error", err, "user_id", user.ID)
		return
	}
	if s.wakeMail != nil {
		s.wakeMail()
	}
}

// queueNotificationMail queues an ordinary event only when the recipient wants it.
func (s *Server) queueNotificationMail(r *http.Request, user domain.User, content mailer.Content) {
	if !user.EmailNotifications {
		return
	}
	s.queueSecurityMail(r, user, content)
}
