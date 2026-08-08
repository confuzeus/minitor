package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	CookieName      = "minitool_session"
	SessionDuration = 24 * time.Hour
	payloadPrefix   = "admin:"
)

var (
	ErrEmptySigningKey = errors.New("signing key must not be empty")
	ErrMalformedCookie = errors.New("malformed session cookie")
)

// CreateSessionCookie builds a session cookie valid for SessionDuration.
// The cookie value has the form base64(payload) + "." + hex(hmac) where the
// payload is "admin:<unix expiry>" and the hmac is HMAC-SHA256 of the
// base64-encoded payload keyed by signingKey. CookieOption values customize
// the returned cookie, e.g. WithSecure.
func CreateSessionCookie(signingKey []byte, opts ...CookieOption) (*http.Cookie, error) {
	if len(signingKey) == 0 {
		return nil, ErrEmptySigningKey
	}

	expiry := time.Now().Add(SessionDuration).Unix()
	payload := fmt.Sprintf("%s%d", payloadPrefix, expiry)
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))

	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(encoded))
	sig := hex.EncodeToString(mac.Sum(nil))

	c := &http.Cookie{
		Name:     CookieName,
		Value:    encoded + "." + sig,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionDuration.Seconds()),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// CookieOption customizes the cookie returned by CreateSessionCookie.
type CookieOption func(*http.Cookie)

// WithSecure sets the Secure flag on the cookie, restricting it to HTTPS
// connections.
func WithSecure(secure bool) CookieOption {
	return func(c *http.Cookie) {
		c.Secure = secure
	}
}

// ValidateSessionCookie reports whether the request carries a valid, unexpired
// session cookie. The signature and password comparison are done in
// constant time.
func ValidateSessionCookie(r *http.Request, signingKey []byte) bool {
	if len(signingKey) == 0 {
		return false
	}

	c, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}

	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return false
	}
	encoded, sigHex := parts[0], parts[1]

	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(encoded))
	wantSig := mac.Sum(nil)

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	if subtle.ConstantTimeCompare(wantSig, sig) != 1 {
		return false
	}

	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}

	expiry, err := parseExpiry(string(payload))
	if err != nil {
		return false
	}

	return time.Now().Unix() <= expiry
}

func parseExpiry(payload string) (int64, error) {
	if !strings.HasPrefix(payload, payloadPrefix) {
		return 0, fmt.Errorf("%w: invalid payload prefix", ErrMalformedCookie)
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(payload, payloadPrefix), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid expiry timestamp", ErrMalformedCookie)
	}
	return n, nil
}

// ValidatePassword compares given against stored in constant time so that
// timing cannot leak information about the stored password.
func ValidatePassword(given, stored string) bool {
	if stored == "" {
		return false
	}
	g := sha256.Sum256([]byte(given))
	s := sha256.Sum256([]byte(stored))
	return subtle.ConstantTimeCompare(g[:], s[:]) == 1
}
