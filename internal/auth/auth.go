// Package auth implements passwordless sign-in: an e-mail verification code
// that, once confirmed, authorises the browser/device with a long-lived cookie.
//
// Security notes:
//   - Only pre-registered users can sign in; unknown e-mails look exactly like
//     known ones to the caller (no account enumeration).
//   - Codes are stored as HMAC hashes, expire quickly, allow few attempts and
//     are rate limited per user.
//   - Device tokens are 256-bit random values; only their SHA-256 is stored, so
//     a leaked database cannot be replayed as cookies. Every device can be
//     revoked individually.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/andregoncalvespires/homeview-dash/internal/i18n"
	"github.com/andregoncalvespires/homeview-dash/internal/store"
)

var (
	// ErrInvalidCode covers unknown user, wrong, expired and exhausted codes.
	ErrInvalidCode = errors.New("auth: invalid or expired code")
	// ErrRateLimited is returned (and only logged) when too many codes were requested.
	ErrRateLimited = errors.New("auth: too many codes requested")
	// ErrUnauthenticated means the device token is missing, unknown or expired.
	ErrUnauthenticated = errors.New("auth: not authenticated")
)

// Mailer sends plain-text e-mail.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Config tunes the security parameters.
type Config struct {
	CodeTTL     time.Duration // how long a code is valid
	MaxAttempts int           // wrong guesses before the code is burned
	MaxSends    int           // codes per user per Window
	Window      time.Duration // rate-limit window
	DeviceTTL   time.Duration // inactivity period after which a device expires
	LinkTTL     time.Duration // validity of CLI login links
}

// DefaultConfig returns the production defaults.
func DefaultConfig() Config {
	return Config{
		CodeTTL:     10 * time.Minute,
		MaxAttempts: 5,
		MaxSends:    3,
		Window:      15 * time.Minute,
		DeviceTTL:   180 * 24 * time.Hour,
		LinkTTL:     15 * time.Minute,
	}
}

// Service is the authentication service.
type Service struct {
	st     store.Store
	mail   Mailer
	secret []byte
	cfg    Config
	now    func() time.Time
}

// New creates a Service. secret keys the code hashes and CSRF tokens.
func New(st store.Store, mail Mailer, secret []byte, cfg Config) *Service {
	return &Service{st: st, mail: mail, secret: secret, cfg: cfg, now: time.Now}
}

// SetClock replaces the time source (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Config returns the active configuration.
func (s *Service) Config() Config { return s.cfg }

// LoadSecret returns the server secret, generating and persisting it on first use.
func LoadSecret(ctx context.Context, st store.Store) ([]byte, error) {
	const key = "secret"
	v, err := st.GetSetting(ctx, key)
	if err == nil {
		if b, derr := hex.DecodeString(v); derr == nil && len(b) >= 32 {
			return b, nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := st.SetSetting(ctx, key, hex.EncodeToString(b)); err != nil {
		return nil, err
	}
	return b, nil
}

// RequestCode e-mails a login code when email belongs to a registered user.
// For unknown addresses it does nothing and returns nil.
func (s *Service) RequestCode(ctx context.Context, email, lang string) error {
	email = store.NormalizeEmail(email)
	u, err := s.st.UserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	now := s.now()
	sent, windowStart := 1, now
	if prev, err := s.st.GetLoginCode(ctx, u.ID); err == nil {
		if now.Sub(prev.WindowStart) < s.cfg.Window {
			if prev.Sent >= s.cfg.MaxSends {
				return ErrRateLimited
			}
			sent, windowStart = prev.Sent+1, prev.WindowStart
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	code, err := randomCode()
	if err != nil {
		return err
	}
	if err := s.st.PutLoginCode(ctx, store.LoginCode{
		UserID:      u.ID,
		Hash:        s.hashCode(email, code),
		ExpiresAt:   now.Add(s.cfg.CodeTTL),
		WindowStart: windowStart,
		Sent:        sent,
	}); err != nil {
		return err
	}

	minutes := int(s.cfg.CodeTTL / time.Minute)
	subject := fmt.Sprintf(i18n.T(lang, "mail.code.subject"), code)
	body := fmt.Sprintf(i18n.T(lang, "mail.code.body"), u.Name, code, minutes)
	if err := s.mail.Send(ctx, u.Email, subject, body); err != nil {
		return fmt.Errorf("send login code: %w", err)
	}
	return nil
}

// VerifyCode checks the code and, on success, creates a device. It returns the
// raw device token to be placed in the cookie.
func (s *Service) VerifyCode(ctx context.Context, email, code, label string) (string, *store.User, error) {
	email = store.NormalizeEmail(email)
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	u, err := s.st.UserByEmail(ctx, email)
	if err != nil {
		return "", nil, ErrInvalidCode
	}
	lc, err := s.st.GetLoginCode(ctx, u.ID)
	if err != nil {
		return "", nil, ErrInvalidCode
	}
	now := s.now()
	if now.After(lc.ExpiresAt) || lc.Attempts >= s.cfg.MaxAttempts {
		_ = s.st.DeleteLoginCode(ctx, u.ID)
		return "", nil, ErrInvalidCode
	}
	if subtle.ConstantTimeCompare([]byte(lc.Hash), []byte(s.hashCode(email, code))) != 1 {
		lc.Attempts++
		if lc.Attempts >= s.cfg.MaxAttempts {
			_ = s.st.DeleteLoginCode(ctx, u.ID)
		} else {
			_ = s.st.PutLoginCode(ctx, *lc)
		}
		return "", nil, ErrInvalidCode
	}
	_ = s.st.DeleteLoginCode(ctx, u.ID)
	token, err := s.newDevice(ctx, u, label)
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// CreateLinkToken makes a one-time login token (used by the CLI as a recovery
// path when e-mail is unavailable).
func (s *Service) CreateLinkToken(ctx context.Context, userID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := s.st.PutLinkToken(ctx, hashToken(token), userID, s.now().Add(s.cfg.LinkTTL)); err != nil {
		return "", err
	}
	return token, nil
}

// RedeemLinkToken consumes a one-time token and authorises a new device.
func (s *Service) RedeemLinkToken(ctx context.Context, token, label string) (string, *store.User, error) {
	uid, err := s.st.TakeLinkToken(ctx, hashToken(token), s.now())
	if err != nil {
		return "", nil, ErrInvalidCode
	}
	u, err := s.st.UserByID(ctx, uid)
	if err != nil {
		return "", nil, ErrInvalidCode
	}
	dev, err := s.newDevice(ctx, u, label)
	if err != nil {
		return "", nil, err
	}
	return dev, u, nil
}

func (s *Service) newDevice(ctx context.Context, u *store.User, label string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if _, err := s.st.CreateDevice(ctx, u.ID, hashToken(token), label, s.now()); err != nil {
		return "", err
	}
	return token, nil
}

// Authenticate resolves a cookie token to its user and device.
func (s *Service) Authenticate(ctx context.Context, token string) (*store.User, *store.Device, error) {
	if token == "" || len(token) > 128 {
		return nil, nil, ErrUnauthenticated
	}
	d, err := s.st.DeviceByHash(ctx, hashToken(token))
	if err != nil {
		return nil, nil, ErrUnauthenticated
	}
	now := s.now()
	if now.Sub(d.LastSeen) > s.cfg.DeviceTTL {
		_ = s.st.DeleteDevice(ctx, d.ID)
		return nil, nil, ErrUnauthenticated
	}
	u, err := s.st.UserByID(ctx, d.UserID)
	if err != nil {
		return nil, nil, ErrUnauthenticated
	}
	if now.Sub(d.LastSeen) > time.Hour { // avoid a write on every request
		if err := s.st.TouchDevice(ctx, d.ID, now); err == nil {
			d.LastSeen = now
		}
	}
	return u, d, nil
}

// CSRF returns the anti-forgery token bound to a device.
func (s *Service) CSRF(d *store.Device) string {
	return s.mac("csrf:" + strconv.FormatInt(d.ID, 10))
}

// CheckCSRF validates a submitted anti-forgery token.
func (s *Service) CheckCSRF(d *store.Device, got string) bool {
	return d != nil && hmac.Equal([]byte(s.CSRF(d)), []byte(got))
}

func (s *Service) hashCode(email, code string) string { return s.mac("code:" + email + ":" + code) }

func (s *Service) mac(msg string) string {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
