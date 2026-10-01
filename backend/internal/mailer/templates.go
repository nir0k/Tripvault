package mailer

import (
	"fmt"
	"html"
	"strings"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Content is the translated text of one transactional message.
type Content struct {
	Subject string
	Text    string
	HTML    string
}

// locale chooses a supported mail language, falling back to English.
func locale(value string) string {
	if strings.EqualFold(value, "ru") || strings.HasPrefix(strings.ToLower(value), "ru-") {
		return "ru"
	}
	return "en"
}

// linkContent builds a plain and HTML message around one secure action link.
func linkContent(subject, greeting, explanation, action, link, expiry string) Content {
	text := fmt.Sprintf("%s\n\n%s\n\n%s: %s\n\n%s\n", greeting, explanation, action, link, expiry)
	htmlBody := fmt.Sprintf("<p>%s</p><p>%s</p><p><a href=\"%s\">%s</a></p><p>%s</p>",
		html.EscapeString(greeting), html.EscapeString(explanation), html.EscapeString(link),
		html.EscapeString(action), html.EscapeString(expiry))
	return Content{Subject: subject, Text: text, HTML: htmlBody}
}

// PasswordReset - renders a password recovery message in the requested language.
//
// Arguments:
//   - language: the recipient's locale; unsupported values fall back to English.
//   - name: the recipient's display name.
//   - link: the single-use recovery address.
//
// Returns:
//   - translated plain-text and HTML content.
func PasswordReset(language, name, link string) Content {
	if locale(language) == "ru" {
		return linkContent("Сброс пароля Tripvault", "Здравствуйте, "+name+"!", "Кто-то запросил сброс пароля вашей учётной записи Tripvault. Если это были не вы, проигнорируйте письмо.", "Задать новый пароль", link, "Ссылка действует 30 минут и используется один раз.")
	}
	return linkContent("Reset your Tripvault password", "Hello, "+name+"!", "Someone requested a password reset for your Tripvault account. If this was not you, ignore this message.", "Choose a new password", link, "The link expires in 30 minutes and can be used once.")
}

// UserInvitationMail - renders an invitation to create an account.
//
// Arguments:
//   - language: the invitee's locale; unsupported values fall back to English.
//   - name: the display name proposed by the administrator.
//   - link: the single-use invitation address.
//
// Returns:
//   - translated plain-text and HTML content.
func UserInvitationMail(language, name, link string) Content {
	if locale(language) == "ru" {
		return linkContent("Приглашение в Tripvault", "Здравствуйте, "+name+"!", "Администратор приглашает вас создать учётную запись Tripvault.", "Принять приглашение", link, "Ссылка действует 7 дней и используется один раз.")
	}
	return linkContent("Invitation to Tripvault", "Hello, "+name+"!", "An administrator invited you to create a Tripvault account.", "Accept the invitation", link, "The link expires in 7 days and can be used once.")
}

// EmailVerification - renders the message that confirms a self-registered address.
//
// It carries both ways of confirming: a link to open and a code to type on the
// site, for a person who reads mail on another device.
//
// Arguments:
//   - language: the recipient's locale; unsupported values fall back to English.
//   - name: the display name the person registered with.
//   - link: the confirmation address.
//   - code: the confirmation code.
//
// Returns:
//   - translated plain-text and HTML content.
func EmailVerification(language, name, link, code string) Content {
	subject, greeting, explanation, action, codeLabel, expiry :=
		"Confirm your Tripvault email", "Hello, "+name+"!",
		"Confirm this address to finish creating your Tripvault account. If you did not register, ignore this message.",
		"Confirm the address", "Or enter this code on the site",
		"The link and the code expire in 1 hour. An account that is not confirmed within 5 days is deleted."
	if locale(language) == "ru" {
		subject, greeting, explanation, action, codeLabel, expiry =
			"Подтвердите почту Tripvault", "Здравствуйте, "+name+"!",
			"Подтвердите этот адрес, чтобы завершить создание учётной записи Tripvault. Если вы не регистрировались, проигнорируйте письмо.",
			"Подтвердить адрес", "Или введите этот код на сайте",
			"Ссылка и код действуют 1 час. Учётная запись, не подтверждённая за 5 дней, удаляется."
	}
	text := fmt.Sprintf("%s\n\n%s\n\n%s: %s\n\n%s: %s\n\n%s\n", greeting, explanation, action, link, codeLabel, code, expiry)
	htmlBody := fmt.Sprintf("<p>%s</p><p>%s</p><p><a href=\"%s\">%s</a></p><p>%s:</p>"+
		"<p style=\"font-size:24px;font-weight:bold;letter-spacing:4px\">%s</p><p>%s</p>",
		html.EscapeString(greeting), html.EscapeString(explanation), html.EscapeString(link),
		html.EscapeString(action), html.EscapeString(codeLabel), html.EscapeString(code), html.EscapeString(expiry))
	return Content{Subject: subject, Text: text, HTML: htmlBody}
}

// TripInvitationMail - renders an invitation to join a trip.
//
// Arguments:
//   - language: the invitee's locale; unsupported values fall back to English.
//   - tripTitle: the trip named by the invitation.
//   - inviter: the display name of the person sending it.
//   - link: the single-use invitation address.
//
// Returns:
//   - translated plain-text and HTML content.
func TripInvitationMail(language, tripTitle, inviter, link string) Content {
	if locale(language) == "ru" {
		return linkContent("Приглашение в поездку «"+tripTitle+"»", "Здравствуйте!", inviter+" приглашает вас присоединиться к поездке «"+tripTitle+"» в Tripvault.", "Открыть приглашение", link, "Ссылка действует 7 дней и используется один раз.")
	}
	return linkContent("Invitation to “"+tripTitle+"”", "Hello!", inviter+" invited you to join “"+tripTitle+"” in Tripvault.", "Open the invitation", link, "The link expires in 7 days and can be used once.")
}

// translatedEvent selects and renders a short notification.
func translatedEvent(language, subjectEN, subjectRU, textEN, textRU string) Content {
	if locale(language) == "ru" {
		return Content{Subject: subjectRU, Text: textRU + "\n", HTML: "<p>" + html.EscapeString(textRU) + "</p>"}
	}
	return Content{Subject: subjectEN, Text: textEN + "\n", HTML: "<p>" + html.EscapeString(textEN) + "</p>"}
}

// SMTPTest - renders the message used to verify an SMTP configuration.
//
// Arguments:
//   - language: the administrator's locale.
//
// Returns:
//   - translated test content.
func SMTPTest(language string) Content {
	return translatedEvent(language,
		"Tripvault mail test", "Проверка почты Tripvault",
		"SMTP delivery from Tripvault works.", "Отправка почты из Tripvault работает.")
}

// PasswordChangedFromProfile - renders a security notice for a profile password change.
//
// Arguments:
//   - language: the account owner's locale.
//
// Returns:
//   - translated security content.
func PasswordChangedFromProfile(language string) Content {
	return translatedEvent(language,
		"Your Tripvault password changed", "Пароль Tripvault изменён",
		"Your Tripvault password was changed from your profile.",
		"Пароль вашей учётной записи Tripvault изменён в профиле.")
}

// PasswordChangedByRecovery - renders a security notice for a recovered password.
//
// Arguments:
//   - language: the account owner's locale.
//
// Returns:
//   - translated security content.
func PasswordChangedByRecovery(language string) Content {
	return translatedEvent(language,
		"Your Tripvault password changed", "Пароль Tripvault изменён",
		"Your Tripvault password was changed through a recovery link.",
		"Пароль вашей учётной записи Tripvault изменён через ссылку восстановления.")
}

// PasswordResetByAdministrator - renders a security notice for an administrator reset.
//
// Arguments:
//   - language: the account owner's locale.
//
// Returns:
//   - translated security content.
func PasswordResetByAdministrator(language string) Content {
	return translatedEvent(language,
		"Your Tripvault password was reset", "Пароль Tripvault сброшен",
		"An administrator reset your Tripvault password. Sign in with the temporary password they gave you.",
		"Администратор сбросил ваш пароль Tripvault. Войдите с временным паролем, который он вам передал.")
}

// AccountAccessChanged - renders security notices for activation and administrator-right changes.
//
// Arguments:
//   - language: the account owner's locale.
//   - wasActive, isActive: the previous and current activation state.
//   - wasAdmin, isAdmin: the previous and current administrator state.
//
// Returns:
//   - translated security content, or empty content when neither state changed.
func AccountAccessChanged(language string, wasActive, isActive, wasAdmin, isAdmin bool) Content {
	var english, russian []string
	if wasActive != isActive {
		if isActive {
			english = append(english, "An administrator reactivated your Tripvault account.")
			russian = append(russian, "Администратор снова активировал вашу учётную запись Tripvault.")
		} else {
			english = append(english, "An administrator deactivated your Tripvault account.")
			russian = append(russian, "Администратор деактивировал вашу учётную запись Tripvault.")
		}
	}
	if wasAdmin != isAdmin {
		if isAdmin {
			english = append(english, "Administrator access was granted to your Tripvault account.")
			russian = append(russian, "Вашей учётной записи Tripvault предоставлены права администратора.")
		} else {
			english = append(english, "Administrator access was withdrawn from your Tripvault account.")
			russian = append(russian, "У вашей учётной записи Tripvault отозваны права администратора.")
		}
	}
	if len(english) == 0 {
		return Content{}
	}
	return translatedEvent(language,
		"Your Tripvault account changed", "Ваша учётная запись Tripvault изменена",
		strings.Join(english, " "), strings.Join(russian, " "))
}

// UserInvitationAccepted - renders the notification sent to the inviting administrator.
//
// Arguments:
//   - language: the administrator's locale.
//   - name: the new account's display name.
//
// Returns:
//   - translated notification content.
func UserInvitationAccepted(language, name string) Content {
	return translatedEvent(language,
		"An invitation was accepted", "Приглашение принято",
		name+" accepted the invitation to Tripvault.",
		"Приглашение пользователя "+name+" в Tripvault принято.")
}

// TripInvitationAccepted - renders the notification sent to the trip owner.
//
// Arguments:
//   - language: the owner's locale.
//   - name: the accepted account's display name.
//   - tripTitle: the trip joined by that account.
//
// Returns:
//   - translated notification content.
func TripInvitationAccepted(language, name, tripTitle string) Content {
	return translatedEvent(language,
		"A trip invitation was accepted", "Приглашение в поездку принято",
		name+" joined “"+tripTitle+"”.",
		"Приглашение пользователя "+name+" в поездку «"+tripTitle+"» принято.")
}

// TripMemberAdded - renders a notification that an existing account gained trip access.
//
// Arguments:
//   - language: the member's locale.
//   - tripTitle: the trip they can now open.
//
// Returns:
//   - translated notification content.
func TripMemberAdded(language, tripTitle string) Content {
	return translatedEvent(language,
		"You were added to “"+tripTitle+"”", "Вас добавили в поездку «"+tripTitle+"»",
		"You now have access to the trip “"+tripTitle+"” in Tripvault.",
		"Теперь у вас есть доступ к поездке «"+tripTitle+"» в Tripvault.")
}

// TripMemberRoleChanged - renders a notification that a trip role changed.
//
// Arguments:
//   - language: the member's locale.
//   - tripTitle: the trip whose role changed.
//   - role: the member's new editor or viewer role.
//
// Returns:
//   - translated notification content.
func TripMemberRoleChanged(language, tripTitle string, role domain.TripRole) Content {
	roleRU := "читатель"
	if role == domain.RoleEditor {
		roleRU = "редактор"
	}
	return translatedEvent(language,
		"Your role on “"+tripTitle+"” changed", "Ваша роль в поездке «"+tripTitle+"» изменена",
		"Your role on the Tripvault trip “"+tripTitle+"” is now "+string(role)+".",
		"Ваша роль в поездке «"+tripTitle+"» в Tripvault изменена: "+roleRU+".")
}

// TripMemberRemoved - renders a notification that trip access was withdrawn.
//
// Arguments:
//   - language: the former member's locale.
//   - tripTitle: the trip they can no longer open.
//
// Returns:
//   - translated notification content.
func TripMemberRemoved(language, tripTitle string) Content {
	return translatedEvent(language,
		"Access to “"+tripTitle+"” was removed", "Доступ к поездке «"+tripTitle+"» закрыт",
		"You no longer have access to the Tripvault trip “"+tripTitle+"”.",
		"У вас больше нет доступа к поездке «"+tripTitle+"» в Tripvault.")
}

// Message - maps translated content onto a newly identified queue message.
//
// Arguments:
//   - recipient: the destination email address.
//   - content: the already translated subject and bodies.
//
// Returns:
//   - a rendered message ready for durable queuing.
func Message(recipient string, content Content) domain.MailMessage {
	return domain.MailMessage{ID: uuid.Must(uuid.NewV7()), Recipient: recipient, Subject: content.Subject, TextBody: content.Text, HTMLBody: content.HTML}
}
