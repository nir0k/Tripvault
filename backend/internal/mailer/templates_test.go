package mailer

import (
	"strings"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestTemplatesSelectLanguageAndEscapeHTML checks mail text follows the
// recipient's locale while user-controlled values cannot become markup.
func TestTemplatesSelectLanguageAndEscapeHTML(t *testing.T) {
	content := TripInvitationMail("ru-RU", "<Север>", "Аня & Борис", "https://example.com/invite#token=x")
	if !strings.Contains(content.Subject, "Север") || !strings.Contains(content.Text, "Аня & Борис") {
		t.Fatalf("unexpected Russian content: %+v", content)
	}
	if strings.Contains(content.HTML, "<Север>") || strings.Contains(content.HTML, "Аня & Борис") {
		t.Fatalf("user content was not escaped: %s", content.HTML)
	}
	english := PasswordReset("de", "Sam", "https://example.com/reset#token=x")
	if !strings.Contains(english.Subject, "Reset") || strings.Contains(english.Subject, "Сброс") {
		t.Fatalf("unsupported locale did not fall back to English: %+v", english)
	}
}

// TestSMTPMessageEncodesHeadersAndBodies checks non-ASCII and line breaks stay
// inside encoded MIME fields rather than becoming additional headers.
func TestSMTPMessageEncodesHeadersAndBodies(t *testing.T) {
	sender := NewSMTPSender(SMTPConfig{FromAddress: "tripvault@example.com", FromName: "Tripvault"})
	raw := string(sender.messageBytes(domain.MailMessage{
		Recipient: "person@example.com",
		Subject:   "Привет\r\nBcc: attacker@example.com",
		TextBody:  "Обычный текст",
		HTMLBody:  "<p>Обычный текст</p>",
	}))
	if strings.Contains(raw, "\r\nBcc:") || !strings.Contains(raw, "Subject: =?UTF-8?") {
		t.Fatalf("unsafe or unencoded subject:\n%s", raw)
	}
	if !strings.Contains(raw, "multipart/alternative") || strings.Contains(raw, "Обычный текст") {
		t.Fatalf("message bodies were not MIME encoded:\n%s", raw)
	}
}

// TestEmailVerificationCarriesLinkAndCode checks the confirmation message
// offers both ways of confirming, in the recipient's language.
func TestEmailVerificationCarriesLinkAndCode(t *testing.T) {
	content := EmailVerification("ru", "<Аня>", "https://example.com/verify-email#token=x", "042917")
	for _, part := range []string{content.Text, content.HTML} {
		if !strings.Contains(part, "042917") || !strings.Contains(part, "verify-email#token=x") {
			t.Fatalf("message lacks its link or code: %s", part)
		}
	}
	if !strings.Contains(content.Subject, "Подтвердите") || strings.Contains(content.HTML, "<Аня>") {
		t.Fatalf("unexpected confirmation content: %+v", content)
	}
}
