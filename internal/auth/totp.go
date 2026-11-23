package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 specifies HMAC-SHA1 for compatibility with authenticator apps
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpDigits = 6
	totpPeriod = 30
	totpWindow = 1 // accept one step either side to tolerate clock drift
)

// newTOTPSecret returns 20 random bytes.
func newTOTPSecret() ([]byte, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("auth: totp secret: %w", err)
	}
	return b, nil
}

func totpAt(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // step is a positive time counter
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%0*d", totpDigits, code)
}

// matchTOTP returns the time step the code matches within the window, or 0.
func matchTOTP(secret []byte, code string, now time.Time) int64 {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0
	}
	cur := now.Unix() / totpPeriod
	for d := int64(-totpWindow); d <= totpWindow; d++ {
		if hmac.Equal([]byte(totpAt(secret, cur+d)), []byte(code)) {
			return cur + d
		}
	}
	return 0
}

func totpURI(issuer, account string, secret []byte) string {
	v := url.Values{}
	v.Set("secret", base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret))
	v.Set("issuer", issuer)
	return fmt.Sprintf("otpauth://totp/%s:%s?%s", url.PathEscape(issuer), url.PathEscape(account), v.Encode())
}

// seal encrypts with AES-256-GCM; the nonce is prepended.
func seal(key, plain []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func open(key, sealed []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("auth: sealed value too short")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("auth: secret key must be 32 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// CurrentTOTP returns the valid code for a base32 secret at time t (used by tests and tooling).
func CurrentTOTP(b32 string, t time.Time) string {
	raw, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(b32)
	return totpAt(raw, t.Unix()/totpPeriod)
}
