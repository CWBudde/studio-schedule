package webpush

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// tokenLifetime is how long a VAPID token is valid; RFC 8292 allows at most
// 24 hours.
const tokenLifetime = 12 * time.Hour

// vapidHeader is the JOSE header of every token, encoded once.
var vapidHeader = encodeBase64URL([]byte(`{"typ":"JWT","alg":"ES256"}`))

// vapidClaims are the claims of a VAPID token (RFC 8292, section 2).
type vapidClaims struct {
	Aud string `json:"aud"`
	Exp int64  `json:"exp"`
	Sub string `json:"sub"`
}

// audience returns the origin of the push endpoint, the "aud" claim. The
// endpoint is one parseEndpoint accepted: an https URL with a host.
func audience(endpoint *url.URL) string { return endpoint.Scheme + "://" + endpoint.Host }

// token returns the VAPID JWT for the push service at aud: ES256 over the
// header and the claims, valid until exp, naming the contact subject.
func (k Keys) token(aud, subject string, exp time.Time) (string, error) {
	claims, err := json.Marshal(vapidClaims{Aud: aud, Exp: exp.Unix(), Sub: subject})
	if err != nil {
		return "", fmt.Errorf("webpush: claims: %w", err)
	}

	signingInput := vapidHeader + "." + encodeBase64URL(claims)
	digest := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, k.private, digest[:])
	if err != nil {
		return "", fmt.Errorf("webpush: sign: %w", err)
	}

	// JWS ES256: r and s as two 32-byte big-endian integers (RFC 7518).
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])

	return signingInput + "." + encodeBase64URL(sig), nil
}

// authorization returns the Authorization header value of the vapid scheme
// (RFC 8292, section 3): the token and the key that signed it.
func (k Keys) authorization(aud, subject string, exp time.Time) (string, error) {
	t, err := k.token(aud, subject, exp)
	if err != nil {
		return "", err
	}

	return "vapid t=" + t + ", k=" + k.PublicKey(), nil
}
