package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.DataDir != "/data" || c.DefaultLang != "pt-BR" || c.CookieSecure || c.SMTP.Port != 587 || c.SMTP.Security != "starttls" {
		t.Fatalf("defaults: %+v", c)
	}
	if err := c.ValidateForServe(); err == nil || !strings.Contains(err.Error(), "HOMEVIEW_SMTP_HOST") {
		t.Fatalf("SMTP must be required: %v", err)
	}
}

func TestFullConfig(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"HOMEVIEW_BASE_URL":       "https://dash.example.com/",
		"HOMEVIEW_LANG":           "en-US",
		"HOMEVIEW_SMTP_HOST":      "smtp.example.com",
		"HOMEVIEW_SMTP_PORT":      "465",
		"HOMEVIEW_SMTP_SECURITY":  "TLS",
		"HOMEVIEW_SMTP_USER":      "bot@example.com",
		"HOMEVIEW_SMTP_PASS":      " secret ",
		"HOMEVIEW_INTERNAL_HOSTS": "Dash.Casa.Example.com, , nas",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://dash.example.com" || !c.CookieSecure || c.DefaultLang != "en" {
		t.Fatalf("%+v", c)
	}
	if c.SMTP.From != "bot@example.com" || c.SMTP.Pass != " secret " || c.SMTP.Security != "tls" || c.SMTP.Port != 465 {
		t.Fatalf("smtp: %+v", c.SMTP)
	}
	if len(c.InternalHosts) != 2 || c.InternalHosts[0] != "dash.casa.example.com" || c.InternalHosts[1] != "nas" {
		t.Fatalf("internal hosts: %v", c.InternalHosts)
	}
	if err := c.ValidateForServe(); err != nil {
		t.Fatal(err)
	}
}

func TestCookieSecureOverride(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"HOMEVIEW_BASE_URL": "http://x.lan", "HOMEVIEW_COOKIE_SECURE": "true"}))
	if err != nil || !c.CookieSecure {
		t.Fatalf("%v %+v", err, c)
	}
}

func TestInvalidValues(t *testing.T) {
	bad := []map[string]string{
		{"HOMEVIEW_BASE_URL": "dash.example.com"},
		{"HOMEVIEW_BASE_URL": "ftp://x"},
		{"HOMEVIEW_SMTP_PORT": "abc"},
		{"HOMEVIEW_SMTP_PORT": "70000"},
		{"HOMEVIEW_SMTP_SECURITY": "ssl"},
		{"HOMEVIEW_COOKIE_SECURE": "maybe"},
	}
	for _, m := range bad {
		if _, err := FromEnv(env(m)); err == nil {
			t.Errorf("accepted %v", m)
		}
	}
}
