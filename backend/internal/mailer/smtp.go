// Package mailer renders transactional mail and delivers a persistent outbox
// through an operator-configured SMTP server.
package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// SMTPConfig describes one SMTP connection. Enabled is deliberately absent:
// administrators control delivery through the stored instance setting.
type SMTPConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	TLS         string
	FromAddress string
	FromName    string
}

// Sender delivers rendered messages.
type Sender interface {
	Send(ctx context.Context, message domain.MailMessage) error
}

// SMTPSender delivers mail over SMTP with implicit TLS, STARTTLS or an
// explicitly configured plain connection.
type SMTPSender struct {
	config SMTPConfig
	dialer net.Dialer
}

// NewSMTPSender - creates an SMTP sender.
//
// Arguments:
//   - config: the validated SMTP connection.
//
// Returns:
//   - a sender using that connection.
func NewSMTPSender(config SMTPConfig) *SMTPSender {
	config.Host = strings.TrimSpace(config.Host)
	config.Username = strings.TrimSpace(config.Username)
	config.FromAddress = strings.TrimSpace(config.FromAddress)
	config.FromName = strings.TrimSpace(config.FromName)
	return &SMTPSender{config: config, dialer: net.Dialer{Timeout: 15 * time.Second}}
}

// Send - delivers one rendered message and waits for the SMTP server to accept it.
//
// Arguments:
//   - ctx: context bounding connection and delivery.
//   - message: the rendered recipient, subject and bodies.
//
// Returns:
//   - an error when the server does not accept the message.
func (s *SMTPSender) Send(ctx context.Context, message domain.MailMessage) error {
	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	var (
		connection net.Conn
		err        error
	)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.config.Host}
	if s.config.TLS == "tls" {
		connection, err = tls.DialWithDialer(&s.dialer, "tcp", address, tlsConfig)
	} else {
		connection, err = s.dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer func() { _ = connection.Close() }()
	deadline := time.Now().Add(30 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("bound SMTP session: %w", err)
	}

	client, err := smtp.NewClient(connection, s.config.Host)
	if err != nil {
		return fmt.Errorf("start SMTP client: %w", err)
	}
	defer func() { _ = client.Close() }()
	if s.config.TLS == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not offer STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if s.config.Username != "" {
		if s.config.TLS == "none" {
			return errors.New("SMTP authentication requires TLS")
		}
		if err := client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return fmt.Errorf("authenticate to SMTP server: %w", err)
		}
	}
	if err := client.Mail(s.config.FromAddress); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(message.Recipient); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("start SMTP data: %w", err)
	}
	if _, err := io.Copy(w, bytes.NewReader(s.messageBytes(message))); err != nil {
		_ = w.Close()
		return fmt.Errorf("write SMTP data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish SMTP data: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("finish SMTP session: %w", err)
	}
	return nil
}

// messageBytes builds an RFC 5322 message. Bodies are base64 encoded so every
// supported language travels without depending on an SMTP server's 8-bit mode.
func (s *SMTPSender) messageBytes(message domain.MailMessage) []byte {
	from := (&stdmail.Address{Name: s.config.FromName, Address: s.config.FromAddress}).String()
	to := (&stdmail.Address{Address: message.Recipient}).String()
	var out strings.Builder
	fmt.Fprintf(&out, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", from, to, encodeHeader(message.Subject))
	if message.HTMLBody == "" {
		out.WriteString("Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		writeBase64(&out, message.TextBody)
		return []byte(out.String())
	}
	boundary := "tripvault-message"
	fmt.Fprintf(&out, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	for _, part := range []struct{ kind, body string }{{"plain", message.TextBody}, {"html", message.HTMLBody}} {
		fmt.Fprintf(&out, "--%s\r\nContent-Type: text/%s; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n", boundary, part.kind)
		writeBase64(&out, part.body)
	}
	fmt.Fprintf(&out, "--%s--\r\n", boundary)
	return []byte(out.String())
}

// encodeHeader makes a subject safe for a message header.
func encodeHeader(value string) string {
	return mime.BEncoding.Encode("UTF-8", value)
}

// writeBase64 writes MIME-sized base64 lines.
func writeBase64(out *strings.Builder, value string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	for len(encoded) > 76 {
		out.WriteString(encoded[:76])
		out.WriteString("\r\n")
		encoded = encoded[76:]
	}
	out.WriteString(encoded)
	out.WriteString("\r\n")
}
