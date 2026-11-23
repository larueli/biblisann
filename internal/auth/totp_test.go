package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B test vectors (SHA-1, secret "12345678901234567890"), 6-digit truncation.
func TestTOTPVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		if got := totpAt(secret, unix/30); got != want {
			t.Errorf("t=%d: %s, want %s", unix, got, want)
		}
	}
}

func TestMatchWindow(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111109, 0)
	if matchTOTP(secret, "081804", now) == 0 {
		t.Error("current code rejected")
	}
	if matchTOTP(secret, "081804", now.Add(30*time.Second)) == 0 {
		t.Error("previous step must be tolerated")
	}
	if matchTOTP(secret, "081804", now.Add(5*time.Minute)) != 0 {
		t.Error("stale code accepted")
	}
	for _, bad := range []string{"", "12345", "abcdef", "0818045"} {
		if matchTOTP(secret, bad, now) != 0 {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSealOpen(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	s, err := seal(key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := open(key, s); err != nil || string(got) != "secret" {
		t.Errorf("%q %v", got, err)
	}
	s[len(s)-1] ^= 1
	if _, err := open(key, s); err == nil {
		t.Error("tampered ciphertext accepted")
	}
	if _, err := seal([]byte("short"), nil); err == nil {
		t.Error("short key accepted")
	}
	if !strings.HasPrefix(totpURI("Biblisann", "a@b.c", []byte("x")), "otpauth://totp/Biblisann:") {
		t.Error("uri")
	}
}
