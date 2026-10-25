// Package config reads HomeView's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
	"github.com/andregoncalvespires/homeview-dash/internal/mail"
)

// Config holds all runtime settings.
type Config struct {
	Addr         string // listen address
	DataDir      string // where homeview.db lives
	BaseURL      string // public URL, used in CLI login links
	DefaultLang  string // "pt-BR" or "en"
	CookieSecure bool   // set the Secure flag on the device cookie
	// InternalHosts are extra host names that mean "the user is on the local
	// network" (besides private IPs and *.lan/*.local names), e.g. a split-DNS
	// name like dash.casa.example.com.
	InternalHosts []string
	SMTP          mail.SMTP
}

// FromEnv builds a Config from getenv (os.Getenv in production).
func FromEnv(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	c := Config{
		Addr:        get("HOMEVIEW_ADDR", ":8080"),
		DataDir:     get("HOMEVIEW_DATA", "/data"),
		BaseURL:     strings.TrimRight(get("HOMEVIEW_BASE_URL", "http://localhost:8080"), "/"),
		DefaultLang: i18n.Normalize(get("HOMEVIEW_LANG", "pt-BR")),
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, fmt.Errorf("HOMEVIEW_BASE_URL must be a full http(s) URL, got %q", c.BaseURL)
	}
	c.CookieSecure = u.Scheme == "https"
	if v := strings.TrimSpace(getenv("HOMEVIEW_COOKIE_SECURE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("HOMEVIEW_COOKIE_SECURE: %w", err)
		}
		c.CookieSecure = b
	}

	port, err := strconv.Atoi(get("HOMEVIEW_SMTP_PORT", "587"))
	if err != nil || port < 1 || port > 65535 {
		return c, errors.New("HOMEVIEW_SMTP_PORT must be a valid port number")
	}
	sec := strings.ToLower(get("HOMEVIEW_SMTP_SECURITY", mail.StartTLS))
	switch sec {
	case mail.StartTLS, mail.TLS, mail.None:
	default:
		return c, fmt.Errorf("HOMEVIEW_SMTP_SECURITY must be starttls, tls or none, got %q", sec)
	}
	for _, h := range strings.Split(getenv("HOMEVIEW_INTERNAL_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			c.InternalHosts = append(c.InternalHosts, h)
		}
	}
	c.SMTP = mail.SMTP{
		Host:     get("HOMEVIEW_SMTP_HOST", ""),
		Port:     port,
		User:     get("HOMEVIEW_SMTP_USER", ""),
		Pass:     getenv("HOMEVIEW_SMTP_PASS"),
		From:     get("HOMEVIEW_SMTP_FROM", ""),
		Security: sec,
	}
	if c.SMTP.From == "" {
		c.SMTP.From = c.SMTP.User
	}
	return c, nil
}

// ValidateForServe checks what the web server needs (SMTP is mandatory).
func (c Config) ValidateForServe() error {
	var missing []string
	if c.SMTP.Host == "" {
		missing = append(missing, "HOMEVIEW_SMTP_HOST")
	}
	if c.SMTP.From == "" {
		missing = append(missing, "HOMEVIEW_SMTP_FROM (or HOMEVIEW_SMTP_USER)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("SMTP is required to send login codes; missing: %s", strings.Join(missing, ", "))
	}
	return nil
}
