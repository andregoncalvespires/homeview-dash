// Package secretbox encrypts small secrets (such as Proxmox API tokens) before
// they are written to the database. The key is derived from the server secret,
// so a copy of homeview.db alone does not reveal them.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

const prefix = "v1:"

// ErrInvalid is returned when a sealed value is malformed or fails
// authentication (wrong key or tampered data).
var ErrInvalid = errors.New("secretbox: invalid or undecryptable value")

func aead(serverSecret []byte) (cipher.AEAD, error) {
	if len(serverSecret) == 0 {
		return nil, errors.New("secretbox: empty server secret")
	}
	m := hmac.New(sha256.New, serverSecret)
	m.Write([]byte("homeview/secretbox/v1"))
	block, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext with AES-256-GCM and returns a printable string.
func Seal(serverSecret []byte, plaintext string) (string, error) {
	g, err := aead(serverSecret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := g.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// Open reverses Seal.
func Open(serverSecret []byte, sealed string) (string, error) {
	g, err := aead(serverSecret)
	if err != nil {
		return "", err
	}
	if len(sealed) <= len(prefix) || sealed[:len(prefix)] != prefix {
		return "", ErrInvalid
	}
	raw, err := base64.RawStdEncoding.DecodeString(sealed[len(prefix):])
	if err != nil || len(raw) < g.NonceSize() {
		return "", ErrInvalid
	}
	pt, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	if err != nil {
		return "", ErrInvalid
	}
	return string(pt), nil
}
