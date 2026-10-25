// Package mail sends plain-text e-mail over SMTP (STARTTLS, implicit TLS or,
// for local relays only, no encryption).
package mail

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Security modes.
const (
	StartTLS = "starttls" // port 587 (default)
	TLS      = "tls"      // implicit TLS, port 465
	None     = "none"     // plain text; only for trusted local relays
)

// SMTP is a Mailer that delivers through an SMTP server.
type SMTP struct {
	Host     string
	Port     int
	User     string
	Pass     string
	From     string // "Name <addr@host>" or "addr@host"
	Security string
	Timeout  time.Duration
}

// Send delivers one message.
func (m *SMTP) Send(ctx context.Context, to, subject, body string) error {
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("invalid From address: %w", err)
	}
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	msg, err := BuildMessage(from.String(), rcpt.String(), subject, body, time.Now(),
		hex.EncodeToString(id)+"@"+m.Host)
	if err != nil {
		return err
	}

	timeout := m.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	tlsCfg := &tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}
	if m.Security == TLS {
		conn = tls.Client(conn, tlsCfg)
	}
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if m.Security == StartTLS || m.Security == "" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("server does not offer STARTTLS (set HOMEVIEW_SMTP_SECURITY=none only for trusted local relays)")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if m.User != "" {
		// PlainAuth refuses to send credentials over an unencrypted connection
		// (except to localhost), which is exactly what we want.
		if err := c.Auth(smtp.PlainAuth("", m.User, m.Pass, m.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// BuildMessage assembles an RFC 5322 message with a base64 UTF-8 body.
// It rejects header values containing line breaks (header injection).
func BuildMessage(from, to, subject, body string, date time.Time, msgID string) ([]byte, error) {
	for _, v := range []string{from, to, subject, msgID} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, errors.New("header value contains a line break")
		}
	}
	var b strings.Builder
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from)
	h("To", to)
	h("Subject", mime.QEncoding.Encode("utf-8", subject))
	h("Date", date.Format(time.RFC1123Z))
	h("Message-ID", "<"+msgID+">")
	h("MIME-Version", "1.0")
	h("Content-Type", `text/plain; charset="utf-8"`)
	h("Content-Transfer-Encoding", "base64")
	h("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")

	enc := base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(body, "\r\n", "\n")))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return []byte(b.String()), nil
}
