package cli

import (
	"bufio"
	"bytes"
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/andregoncalvespires/homeview-dash/internal/auth"
	"github.com/andregoncalvespires/homeview-dash/internal/config"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

func run(t *testing.T, st store.Store, lang, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	cfg := config.Config{BaseURL: "https://dash.example.com", DefaultLang: lang}
	code := Admin(context.Background(), args, st, cfg, IO{In: bufio.NewReader(strings.NewReader(stdin)), Out: &out, Err: &errb})
	return code, out.String(), errb.String()
}

func TestCreateInteractive(t *testing.T) {
	st := store.NewMemStore()
	code, out, _ := run(t, st, "pt-BR", "Ana@Example.com\nAna Silva\n", "create")
	if code != 0 || !strings.Contains(out, "https://dash.example.com/login") {
		t.Fatalf("code %d, out %q", code, out)
	}
	u, err := st.UserByEmail(context.Background(), "ana@example.com")
	if err != nil || !u.IsAdmin() || u.Name != "Ana Silva" {
		t.Fatalf("user: %+v %v", u, err)
	}
}

func TestCreateWithFlagsAndErrors(t *testing.T) {
	st := store.NewMemStore()
	if code, _, _ := run(t, st, "en", "", "create", "--email", "a@example.com", "--name", "A"); code != 0 {
		t.Fatalf("create failed: %d", code)
	}
	if code, _, e := run(t, st, "en", "", "create", "--email", "a@example.com", "--name", "A"); code != 1 || !strings.Contains(e, "already exists") {
		t.Fatalf("duplicate: %d %q", code, e)
	}
	if code, _, _ := run(t, st, "en", "", "create", "--email", "nope", "--name", "A"); code != 1 {
		t.Fatal("invalid e-mail accepted")
	}
	if code, _, _ := run(t, st, "en", "\n", "create", "--email", "b@example.com"); code != 1 {
		t.Fatal("empty name accepted")
	}
	if n, _ := st.CountAdmins(context.Background()); n != 1 {
		t.Fatalf("admins = %d", n)
	}
}

func TestLoginLinkWorks(t *testing.T) {
	st := store.NewMemStore()
	run(t, st, "en", "", "create", "--email", "a@example.com", "--name", "A")
	code, out, _ := run(t, st, "en", "", "login-link", "--email", "a@example.com")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	m := regexp.MustCompile(`https://dash\.example\.com/auth/link\?token=(\S+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no link in %q", out)
	}
	tok, _ := url.QueryUnescape(m[1])
	svc := auth.New(st, nil, []byte("k"), auth.DefaultConfig())
	if _, _, err := svc.RedeemLinkToken(context.Background(), tok, "x"); err != nil {
		t.Fatalf("link does not redeem: %v", err)
	}
	if code, _, e := run(t, st, "en", "", "login-link", "--email", "ghost@example.com"); code != 1 || !strings.Contains(e, "not found") {
		t.Fatalf("unknown user: %d %q", code, e)
	}
}

func TestListAndUsage(t *testing.T) {
	st := store.NewMemStore()
	if _, out, _ := run(t, st, "en", "", "list"); !strings.Contains(out, "No users") {
		t.Fatalf("empty list: %q", out)
	}
	run(t, st, "en", "", "create", "--email", "a@example.com", "--name", "Ann")
	if _, out, _ := run(t, st, "en", "", "list"); !strings.Contains(out, "a@example.com") || !strings.Contains(out, "admin") {
		t.Fatalf("list: %q", out)
	}
	if code, _, _ := run(t, st, "en", ""); code != 2 {
		t.Fatal("missing subcommand should exit 2")
	}
	if code, _, _ := run(t, st, "en", "", "bogus"); code != 2 {
		t.Fatal("unknown subcommand should exit 2")
	}
}

func TestCreateRejectsBrokenEncoding(t *testing.T) {
	st := store.NewMemStore()
	// What a terminal without UTF-8 line editing produces: a lone 0xC3 left
	// over from a backspace on "é", followed by the retyped letters.
	code, _, e := run(t, st, "pt-BR", "ana@example.com\nAndr\xc3e Pires\n", "create")
	if code != 1 || !strings.Contains(e, "UTF-8") {
		t.Fatalf("broken name accepted or unexplained: %d %q", code, e)
	}
	if users, _ := st.ListUsers(context.Background()); len(users) != 0 {
		t.Fatalf("user stored with invalid name: %+v", users)
	}
	// Properly encoded accents are fine.
	if code, _, _ := run(t, st, "pt-BR", "ana@example.com\nAndré Pires\n", "create"); code != 0 {
		t.Fatal("valid accented name rejected")
	}
	if u, _ := st.UserByEmail(context.Background(), "ana@example.com"); u == nil || u.Name != "André Pires" {
		t.Fatalf("stored: %+v", u)
	}
}

func TestRename(t *testing.T) {
	st := store.NewMemStore()
	ctx := context.Background()
	// Simulate a user created by an older version with a corrupted name.
	if _, err := st.CreateUser(ctx, "ana@example.com", "Andr\xc3e Pires", store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, st, "pt-BR", "", "rename", "--email", "Ana@Example.com", "--name", "André Pires"); code != 0 || !strings.Contains(out, "André Pires") {
		t.Fatalf("rename: %d %q", code, out)
	}
	if u, _ := st.UserByEmail(ctx, "ana@example.com"); u.Name != "André Pires" {
		t.Fatalf("name = %q", u.Name)
	}
	// Interactive form.
	if code, _, _ := run(t, st, "en", "Jane Doe\n", "rename", "--email", "ana@example.com"); code != 0 {
		t.Fatal("interactive rename failed")
	}
	if u, _ := st.UserByEmail(ctx, "ana@example.com"); u.Name != "Jane Doe" {
		t.Fatalf("name = %q", u.Name)
	}
	// Errors.
	if code, _, e := run(t, st, "en", "", "rename", "--email", "ghost@example.com", "--name", "X"); code != 1 || !strings.Contains(e, "not found") {
		t.Fatalf("unknown user: %d %q", code, e)
	}
	if code, _, _ := run(t, st, "en", "", "rename", "--email", "ana@example.com", "--name", "Bad\xc3"); code != 1 {
		t.Fatal("invalid name accepted")
	}
	if u, _ := st.UserByEmail(ctx, "ana@example.com"); u.Name != "Jane Doe" {
		t.Fatalf("failed rename changed the name: %q", u.Name)
	}
}
