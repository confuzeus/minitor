package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCreateSessionCookie(t *testing.T) {
	key := []byte("test-secret-key")
	c, err := CreateSessionCookie(key)
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}
	if c.Name != CookieName {
		t.Errorf("Name = %q, want %q", c.Name, CookieName)
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if want := int(SessionDuration.Seconds()); c.MaxAge < want-2 || c.MaxAge > want {
		t.Errorf("MaxAge = %d, want ~%d", c.MaxAge, want)
	}

	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		t.Fatalf("cookie value %q has %d parts, want 2", c.Value, len(parts))
	}
	if parts[0] == "" || parts[1] == "" {
		t.Fatalf("cookie value %q has empty payload or signature", c.Value)
	}
}

func TestCreateSessionCookieEmptyKey(t *testing.T) {
	if _, err := CreateSessionCookie(nil); err == nil {
		t.Fatal("CreateSessionCookie(nil) returned nil error, want error")
	}
}

func TestCreateSessionCookieSecureOption(t *testing.T) {
	key := []byte("test-secret-key")

	c, err := CreateSessionCookie(key)
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}
	if c.Secure {
		t.Error("Secure = true by default, want false")
	}

	c, err = CreateSessionCookie(key, WithSecure(true))
	if err != nil {
		t.Fatalf("CreateSessionCookie with WithSecure returned error: %v", err)
	}
	if !c.Secure {
		t.Error("Secure = false with WithSecure(true), want true")
	}
}

func TestCreateSessionCookiePayloadRoundTrip(t *testing.T) {
	key := []byte("test-secret-key")
	before := time.Now()
	c, err := CreateSessionCookie(key)
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}
	after := time.Now()

	encoded := strings.Split(c.Value, ".")[0]
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode payload: %v", err)
	}
	payload := string(raw)
	if !strings.HasPrefix(payload, payloadPrefix) {
		t.Errorf("payload %q does not start with %q", payload, payloadPrefix)
	}
	expiry, err := strconv.ParseInt(strings.TrimPrefix(payload, payloadPrefix), 10, 64)
	if err != nil {
		t.Fatalf("parse expiry: %v", err)
	}
	exp := time.Unix(expiry, 0)
	if exp.Before(before.Add(SessionDuration).Add(-time.Second)) || exp.After(after.Add(SessionDuration)) {
		t.Errorf("expiry %v not within [now+duration-1s, now+duration]", exp)
	}
}

func TestValidateSessionCookieValid(t *testing.T) {
	key := []byte("test-secret-key")
	c, err := CreateSessionCookie(key)
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	if !ValidateSessionCookie(r, key) {
		t.Fatal("ValidateSessionCookie = false, want true")
	}
}

func TestValidateSessionCookieExpired(t *testing.T) {
	key := []byte("test-secret-key")
	c := expiredCookie(key)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	if ValidateSessionCookie(r, key) {
		t.Fatal("ValidateSessionCookie = true for expired cookie, want false")
	}
}

func TestValidateSessionCookieTamperedPayload(t *testing.T) {
	key := []byte("test-secret-key")
	c, err := CreateSessionCookie(key)
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}

	parts := strings.Split(c.Value, ".")
	raw, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("base64 decode payload: %v", err)
	}
	raw[len(raw)-1] ^= 0x01
	c.Value = base64.StdEncoding.EncodeToString(raw) + "." + parts[1]

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	if ValidateSessionCookie(r, key) {
		t.Fatal("ValidateSessionCookie = true for tampered payload, want false")
	}
}

func TestValidateSessionCookieTamperedSignature(t *testing.T) {
	key := []byte("test-secret-key")
	c, err := CreateSessionCookie(key)
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}

	parts := strings.Split(c.Value, ".")
	sig, err := hex.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("hex decode signature: %v", err)
	}
	sig[len(sig)-1] ^= 0x01
	c.Value = parts[0] + "." + hex.EncodeToString(sig)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	if ValidateSessionCookie(r, key) {
		t.Fatal("ValidateSessionCookie = true for tampered signature, want false")
	}
}

func TestValidateSessionCookieMissingCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if ValidateSessionCookie(r, []byte("test-secret-key")) {
		t.Fatal("ValidateSessionCookie = true without cookie, want false")
	}
}

func TestValidateSessionCookieWrongSigningKey(t *testing.T) {
	c, err := CreateSessionCookie([]byte("correct-key"))
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	if ValidateSessionCookie(r, []byte("wrong-key")) {
		t.Fatal("ValidateSessionCookie = true with wrong signing key, want false")
	}
}

func TestValidateSessionCookieMalformed(t *testing.T) {
	key := []byte("test-secret-key")
	cases := []struct {
		name  string
		value string
	}{
		{"no dot", "abc"},
		{"too many parts", "a.b.c"},
		{"bad base64", "%%.sig"},
		{"bad hex sig", "payload.nothex"},
		{"wrong payload prefix", "somethingelse:12345.sig"},
		{"non-numeric expiry", base64.StdEncoding.EncodeToString([]byte("admin:notanumber")) + ".sig"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.AddCookie(&http.Cookie{Name: CookieName, Value: tc.value})
			if ValidateSessionCookie(r, key) {
				t.Fatalf("ValidateSessionCookie = true for malformed value %q, want false", tc.value)
			}
		})
	}
}

func TestValidateSessionCookieEmptyKey(t *testing.T) {
	c, err := CreateSessionCookie([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("CreateSessionCookie returned error: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	if ValidateSessionCookie(r, nil) {
		t.Fatal("ValidateSessionCookie = true with empty signing key, want false")
	}
}

func TestValidatePassword(t *testing.T) {
	if !ValidatePassword("hunter2", "hunter2") {
		t.Error("ValidatePassword = false for matching passwords, want true")
	}
	if ValidatePassword("hunter2", "hunter3") {
		t.Error("ValidatePassword = true for non-matching passwords, want false")
	}
	if ValidatePassword("hunter2", "") {
		t.Error("ValidatePassword = true when stored is empty, want false")
	}
	if ValidatePassword("", "") {
		t.Error("ValidatePassword = true when both empty, want false")
	}
}

func expiredCookie(key []byte) *http.Cookie {
	payload := fmt.Sprintf("%s%d", payloadPrefix, time.Now().Add(-time.Hour).Unix())
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encoded))
	sig := hex.EncodeToString(mac.Sum(nil))

	return &http.Cookie{
		Name:  CookieName,
		Value: encoded + "." + sig,
		Path:  "/",
	}
}
