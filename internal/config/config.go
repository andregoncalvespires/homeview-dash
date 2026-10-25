// Package config reads HomeView's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	// DiscoveryInterval is how often Proxmox connections are re-read in the
	// background. Zero disables the loop (the "Sync now" button still works).
	DiscoveryInterval time.Duration
	SMTP              mail.SMTP
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

	c.DiscoveryInterval = 10 * time.Minute
	if v := strings.TrimSpace(getenv("HOMEVIEW_DISCOVERY_INTERVAL")); v != "" {
		if v == "0" || strings.EqualFold(v, "off") {
			c.DiscoveryInterval = 0
		} else {
			d, err := time.ParseDuration(v)
			if err != nil || d < time.Minute {
				return c, fmt.Errorf("HOMEVIEW_DISCOVERY_INTERVAL must be a duration of at least 1m such as 10m or 1h (or 0 to disable), got %q", v)
			}
			c.DiscoveryInterval = d
		}
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
