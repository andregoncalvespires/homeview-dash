package auth

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

type sent struct{ to, subject, body string }

type fakeMail struct {
	mu   sync.Mutex
	msgs []sent
	err  error
}

func (f *fakeMail) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.msgs = append(f.msgs, sent{to, subject, body})
	return nil
}

var codeRe = regexp.MustCompile(`\b(\d{6})\b`)

func (f *fakeMail) lastCode(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.msgs) == 0 {
		t.Fatal("no mail was sent")
	}
	m := codeRe.FindStringSubmatch(f.msgs[len(f.msgs)-1].body)
	if m == nil {
		t.Fatalf("no code in %q", f.msgs[len(f.msgs)-1].body)
	}
	return m[1]
}

func (f *fakeMail) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.msgs) }

type fixture struct {
	svc  *Service
	st   *store.MemStore
	mail *fakeMail
	user *store.User
	now  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{st: store.NewMemStore(), mail: &fakeMail{}, now: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
	f.svc = New(f.st, f.mail, []byte("0123456789abcdef0123456789abcdef"), DefaultConfig())
	f.svc.SetClock(func() time.Time { return f.now })
	u, err := f.st.CreateUser(ctx, "ana@example.com", "Ana", store.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	f.user = u
	return f
}

var ctx = context.Background()

func TestUnknownEmailSendsNothing(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.RequestCode(ctx, "ghost@example.com", "en"); err != nil {
		t.Fatal(err)
	}
	if f.mail.count() != 0 {
		t.Fatal("mail sent to unregistered address")
	}
}

func TestFullFlow(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.RequestCode(ctx, "  ANA@example.com ", "pt-BR"); err != nil {
		t.Fatal(err)
	}
	code := f.mail.lastCode(t)
	if f.mail.msgs[0].to != "ana@example.com" {
		t.Fatalf("sent to %q", f.mail.msgs[0].to)
	}

	// The raw code must never be stored.
	lc, _ := f.st.GetLoginCode(ctx, f.user.ID)
	if lc.Hash == code || len(lc.Hash) != 64 {
		t.Fatalf("code stored unsafely: %q", lc.Hash)
	}

	token, u, err := f.svc.VerifyCode(ctx, "ana@example.com", code[:3]+" "+code[3:], "Firefox · Linux")
	if err != nil || u.ID != f.user.ID || token == "" {
		t.Fatalf("verify: %v %v %q", err, u, token)
	}
	if _, _, err := f.svc.VerifyCode(ctx, "ana@example.com", code, "x"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("code reused: %v", err)
	}

	got, dev, err := f.svc.Authenticate(ctx, token)
	if err != nil || got.ID != f.user.ID || dev.Label != "Firefox · Linux" {
		t.Fatalf("authenticate: %v %+v %+v", err, got, dev)
	}
	// Only the hash of the token is stored.
	if _, err := f.st.DeviceByHash(ctx, token); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("raw token found in store")
	}
	if _, _, err := f.svc.Authenticate(ctx, "nonsense"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("bad token accepted: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("empty token accepted")
	}
}

func TestWrongCodesBurnTheCode(t *testing.T) {
	f := newFixture(t)
	_ = f.svc.RequestCode(ctx, "ana@example.com", "en")
	code := f.mail.lastCode(t)
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	for i := 0; i < DefaultConfig().MaxAttempts; i++ {
		if _, _, err := f.svc.VerifyCode(ctx, "ana@example.com", wrong, "x"); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, _, err := f.svc.VerifyCode(ctx, "ana@example.com", code, "x"); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("correct code accepted after too many failures")
	}
}

func TestCodeExpires(t *testing.T) {
	f := newFixture(t)
	_ = f.svc.RequestCode(ctx, "ana@example.com", "en")
	code := f.mail.lastCode(t)
	f.now = f.now.Add(DefaultConfig().CodeTTL + time.Second)
	if _, _, err := f.svc.VerifyCode(ctx, "ana@example.com", code, "x"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("expired code accepted: %v", err)
	}
}

func TestVerifyUnknownUser(t *testing.T) {
	f := newFixture(t)
	if _, _, err := f.svc.VerifyCode(ctx, "ghost@example.com", "123456", "x"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("got %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	f := newFixture(t)
	cfg := DefaultConfig()
	for i := 0; i < cfg.MaxSends; i++ {
		if err := f.svc.RequestCode(ctx, "ana@example.com", "en"); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := f.svc.RequestCode(ctx, "ana@example.com", "en"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limit, got %v", err)
	}
	if f.mail.count() != cfg.MaxSends {
		t.Fatalf("%d mails sent", f.mail.count())
	}
	f.now = f.now.Add(cfg.Window + time.Second)
	if err := f.svc.RequestCode(ctx, "ana@example.com", "en"); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

func TestNewCodeInvalidatesOld(t *testing.T) {
	f := newFixture(t)
	_ = f.svc.RequestCode(ctx, "ana@example.com", "en")
	first := f.mail.lastCode(t)
	_ = f.svc.RequestCode(ctx, "ana@example.com", "en")
	second := f.mail.lastCode(t)
	if first != second {
		if _, _, err := f.svc.VerifyCode(ctx, "ana@example.com", first, "x"); !errors.Is(err, ErrInvalidCode) {
			t.Fatal("old code still works")
		}
	}
	if _, _, err := f.svc.VerifyCode(ctx, "ana@example.com", second, "x"); err != nil {
		t.Fatalf("new code rejected: %v", err)
	}
}

func TestMailFailureIsReported(t *testing.T) {
	f := newFixture(t)
	f.mail.err = errors.New("smtp down")
	if err := f.svc.RequestCode(ctx, "ana@example.com", "en"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeviceExpiryAndRevocation(t *testing.T) {
	f := newFixture(t)
	_ = f.svc.RequestCode(ctx, "ana@example.com", "en")
	token, _, _ := f.svc.VerifyCode(ctx, "ana@example.com", f.mail.lastCode(t), "x")

	// Activity inside the TTL keeps the device alive and refreshes last-seen.
	f.now = f.now.Add(90 * 24 * time.Hour)
	if _, d, err := f.svc.Authenticate(ctx, token); err != nil || !d.LastSeen.Equal(f.now) {
		t.Fatalf("authenticate: %v %+v", err, d)
	}
	// Inactivity beyond the TTL expires it.
	f.now = f.now.Add(DefaultConfig().DeviceTTL + time.Hour)
	if _, _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("stale device accepted: %v", err)
	}

	// Revocation.
	_ = f.svc.RequestCode(ctx, "ana@example.com", "en")
	token2, _, _ := f.svc.VerifyCode(ctx, "ana@example.com", f.mail.lastCode(t), "x")
	_, d, _ := f.svc.Authenticate(ctx, token2)
	_ = f.st.DeleteDevice(ctx, d.ID)
	if _, _, err := f.svc.Authenticate(ctx, token2); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("revoked device accepted")
	}
}

func TestLinkTokens(t *testing.T) {
	f := newFixture(t)
	tok, err := f.svc.CreateLinkToken(ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	dev, u, err := f.svc.RedeemLinkToken(ctx, tok, "CLI")
	if err != nil || u.ID != f.user.ID || dev == "" {
		t.Fatalf("redeem: %v", err)
	}
	if _, _, err := f.svc.RedeemLinkToken(ctx, tok, "CLI"); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("link token reused")
	}
	tok2, _ := f.svc.CreateLinkToken(ctx, f.user.ID)
	f.now = f.now.Add(DefaultConfig().LinkTTL + time.Second)
	if _, _, err := f.svc.RedeemLinkToken(ctx, tok2, "CLI"); !errors.Is(err, ErrInvalidCode) {
		t.Fatal("expired link accepted")
	}
}

func TestCSRF(t *testing.T) {
	f := newFixture(t)
	a := &store.Device{ID: 1}
	b := &store.Device{ID: 2}
	tok := f.svc.CSRF(a)
	if !f.svc.CheckCSRF(a, tok) || f.svc.CheckCSRF(b, tok) || f.svc.CheckCSRF(a, "") || f.svc.CheckCSRF(nil, tok) {
		t.Fatal("CSRF check is wrong")
	}
}

func TestLoadSecretPersists(t *testing.T) {
	st := store.NewMemStore()
	a, err := LoadSecret(ctx, st)
	if err != nil || len(a) < 32 {
		t.Fatalf("secret: %v %d", err, len(a))
	}
	b, _ := LoadSecret(ctx, st)
	if string(a) != string(b) {
		t.Fatal("secret changed between loads")
	}
}
