package mail

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestBuildMessage(t *testing.T) {
	date := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	msg, err := BuildMessage(`"HomeView" <noreply@example.com>`, "ana@example.com", "123456 é o seu código", "Olá, Ana!\nCódigo: 123456\n", date, "id1@example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	for _, want := range []string{
		"From: \"HomeView\" <noreply@example.com>\r\n",
		"To: ana@example.com\r\n",
		"Subject: =?utf-8?q?123456_=C3=A9_o_seu_c=C3=B3digo?=\r\n",
		"Message-ID: <id1@example.com>\r\n",
		"Content-Transfer-Encoding: base64\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	_, body, ok := strings.Cut(s, "\r\n\r\n")
	if !ok {
		t.Fatal("no body separator")
	}
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body, "\r\n", ""))
	if err != nil || string(dec) != "Olá, Ana!\nCódigo: 123456\n" {
		t.Fatalf("body = %q (%v)", dec, err)
	}
	for _, line := range strings.Split(body, "\r\n") {
		if len(line) > 76 {
			t.Errorf("line too long: %d", len(line))
		}
	}
}

func TestBuildMessageRejectsHeaderInjection(t *testing.T) {
	date := time.Now()
	cases := [][4]string{
		{"a@example.com", "b@example.com", "hi\r\nBcc: evil@example.com", "id"},
		{"a@example.com", "b@example.com\nBcc: evil@example.com", "hi", "id"},
		{"a@example.com\r\nX: y", "b@example.com", "hi", "id"},
		{"a@example.com", "b@example.com", "hi", "id\r\nX: y"},
	}
	for _, c := range cases {
		if _, err := BuildMessage(c[0], c[1], c[2], "body", date, c[3]); err == nil {
			t.Errorf("accepted injection: %q", c)
		}
	}
}

func TestLongBodyWraps(t *testing.T) {
	msg, err := BuildMessage("a@example.com", "b@example.com", "s", strings.Repeat("x", 500), time.Now(), "id")
	if err != nil {
		t.Fatal(err)
	}
	_, body, _ := strings.Cut(string(msg), "\r\n\r\n")
	dec, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(body, "\r\n", ""))
	if len(dec) != 500 {
		t.Fatalf("decoded %d bytes", len(dec))
	}
}
