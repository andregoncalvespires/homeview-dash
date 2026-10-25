package secretbox

import (
	"errors"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	key := []byte("server-secret")
	a, err := Seal(key, "token-123")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Seal(key, "token-123")
	if a == b {
		t.Error("nonce reused: two seals are identical")
	}
	if strings.Contains(a, "token-123") {
		t.Error("plaintext visible in sealed value")
	}
	got, err := Open(key, a)
	if err != nil || got != "token-123" {
		t.Fatalf("Open = %q, %v", got, err)
	}
}

func TestOpenRejects(t *testing.T) {
	key := []byte("k1")
	sealed, _ := Seal(key, "x")
	cases := map[string]string{
		"wrong key": "",
		"empty":     "",
		"no prefix": "abc",
		"bad b64":   "v1:!!!",
		"short":     "v1:AAAA",
		"tampered":  sealed[:len(sealed)-2] + "AA",
	}
	for name, v := range cases {
		k := key
		if name == "wrong key" {
			v, k = sealed, []byte("k2")
		}
		if _, err := Open(k, v); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := Seal(nil, "x"); err == nil {
		t.Error("empty server secret accepted")
	}
}
