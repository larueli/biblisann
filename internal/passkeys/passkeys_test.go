package passkeys_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/passkeys"
)

const origin = "https://books.example.org"

var b64 = base64.RawURLEncoding

// authenticator is a minimal software authenticator: ES256, "none" attestation, user verified.
type authenticator struct {
	key     *ecdsa.PrivateKey
	credID  []byte
	counter uint32
}

func newAuthenticator(t *testing.T) *authenticator {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &authenticator{key: k, credID: id}
}

func rpHash() []byte { h := sha256.Sum256([]byte("books.example.org")); return h[:] }

func clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return b
}

func (a *authenticator) register(t *testing.T, options json.RawMessage) []byte {
	var o struct {
		PublicKey struct{ Challenge string } `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &o); err != nil || o.PublicKey.Challenge == "" {
		t.Fatalf("options: %s", options)
	}
	pub, _ := a.key.PublicKey.Bytes() // uncompressed point: 0x04 || X || Y
	x, y := pub[1:33], pub[33:65]
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	authData := append(rpHash(), 0x45) // user present, user verified, attested credential data
	authData = binary.BigEndian.AppendUint32(authData, 0)
	authData = append(authData, make([]byte, 16)...) // AAGUID
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(a.credID)))
	authData = append(authData, a.credID...)
	authData = append(authData, cose...)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	cd := clientData("webauthn.create", o.PublicKey.Challenge)
	out, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd), "attestationObject": b64.EncodeToString(att)}})
	return out
}

func (a *authenticator) assert(t *testing.T, options json.RawMessage, userHandle []byte, counter uint32) []byte {
	var o struct {
		PublicKey struct{ Challenge string } `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &o); err != nil {
		t.Fatal(err)
	}
	a.counter = counter
	authData := append(rpHash(), 0x05)
	authData = binary.BigEndian.AppendUint32(authData, a.counter)
	cd := clientData("webauthn.get", o.PublicKey.Challenge)
	ch := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, authData...), ch[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd), "authenticatorData": b64.EncodeToString(authData),
			"signature": b64.EncodeToString(sig), "userHandle": b64.EncodeToString(userHandle)}})
	return out
}

func TestPasskeyRegistrationAndLogin(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "me@example.org", "tulips in the garden", "Me")
	base, _ := url.Parse(origin)
	svc, err := passkeys.New(pool, base, "Biblisann")
	if err != nil {
		t.Fatal(err)
	}
	dev := newAuthenticator(t)

	sid, opts, err := svc.BeginRegistration(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.FinishRegistration(ctx, u.ID, sid, "Phone", dev.register(t, opts)); err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	if err := svc.FinishRegistration(ctx, u.ID, sid, "Phone", dev.register(t, opts)); !errors.Is(err, passkeys.ErrBadCeremony) {
		t.Errorf("ceremony reused: %v", err)
	}
	list, _ := svc.List(ctx, u.ID)
	if len(list) != 1 || list[0].Name != "Phone" {
		t.Fatalf("list: %+v", list)
	}

	// username-less login
	handle, _ := u.ID.MarshalBinary()
	sid, opts, err = svc.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := svc.FinishLogin(ctx, sid, dev.assert(t, opts, handle, 1))
	if err != nil || uid != u.ID {
		t.Fatalf("login: %v %v", uid, err)
	}
	// a replayed assertion (same challenge) fails: the ceremony is single use
	if _, err := svc.FinishLogin(ctx, sid, dev.assert(t, opts, handle, 2)); !errors.Is(err, passkeys.ErrBadCeremony) {
		t.Errorf("replayed ceremony: %v", err)
	}
	// another device cannot sign in as this user
	sid, opts, _ = svc.BeginLogin(ctx)
	if _, err := svc.FinishLogin(ctx, sid, newAuthenticator(t).assert(t, opts, handle, 1)); err == nil {
		t.Error("unregistered authenticator accepted")
	}
	// wrong signature
	sid, opts, _ = svc.BeginLogin(ctx)
	bad := dev.assert(t, opts, handle, 3)
	var m map[string]any
	_ = json.Unmarshal(bad, &m)
	m["response"].(map[string]any)["signature"] = b64.EncodeToString([]byte("garbage"))
	bad, _ = json.Marshal(m)
	if _, err := svc.FinishLogin(ctx, sid, bad); err == nil {
		t.Error("garbage signature accepted")
	}

	// removal
	if err := svc.Delete(ctx, u.ID, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, u.ID, list[0].ID); !errors.Is(err, passkeys.ErrUnknown) {
		t.Errorf("double delete: %v", err)
	}
}
