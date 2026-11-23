package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("unexpected format %s", h)
	}
	if ok, err := VerifyPassword("correct horse battery", h); err != nil || !ok {
		t.Errorf("verify good: %v %v", ok, err)
	}
	if ok, _ := VerifyPassword("wrong horse battery", h); ok {
		t.Error("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Error("salt must be random")
	}
}

func TestVerifyRejectsMalformedAndHostileHashes(t *testing.T) {
	for _, h := range []string{
		"", "plain", "$argon2i$v=19$m=1,t=1,p=1$AAAA$AAAA",
		"$argon2id$v=19$m=4294967295,t=1,p=1$AAAA$AAAA", // memory bomb
		"$argon2id$v=19$m=19456,t=99,p=1$AAAA$AAAA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$AAAA",
	} {
		if ok, err := VerifyPassword("x", h); ok || err == nil {
			t.Errorf("hash %q must be rejected (ok=%v err=%v)", h, ok, err)
		}
	}
}

func TestCheckPassword(t *testing.T) {
	tests := []struct {
		name, pw string
		wantErr  bool
	}{
		{"fine passphrase", "tulips in the garden", false},
		{"unicode counts runes", "été-précédé-éé", false},
		{"too short", "short1", true},
		{"nine chars", "123456789", true},
		{"common", "password123", true},
		{"email", "me@example.org", true},
		{"repeated", "aaaaaaaaaaaa", true},
		{"too long", strings.Repeat("a1", 200), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPassword(tt.pw, "me@example.org", "Me")
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
