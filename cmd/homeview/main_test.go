package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionHelpAndUnknown(t *testing.T) {
	env := func(string) string { return "" }
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errb, env); code != 0 || !strings.Contains(out.String(), "homeview") {
		t.Fatalf("version: %d %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errb, env); code != 0 || !strings.Contains(out.String(), "admin create") {
		t.Fatalf("help: %d", code)
	}
	if code := run([]string{"bogus"}, strings.NewReader(""), &out, &errb, env); code != 2 {
		t.Fatalf("unknown: %d", code)
	}
}

func TestServeRequiresSMTP(t *testing.T) {
	var errb bytes.Buffer
	env := func(k string) string {
		if k == "HOMEVIEW_DATA" {
			return t.TempDir()
		}
		return ""
	}
	if code := serve(&errb, env); code != 1 || !strings.Contains(errb.String(), "HOMEVIEW_SMTP_HOST") {
		t.Fatalf("serve without SMTP: %d %q", code, errb.String())
	}
}
