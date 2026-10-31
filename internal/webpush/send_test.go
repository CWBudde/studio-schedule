package webpush

import (
	"bytes"
	"cmp"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
)

// The example of RFC 8292, section 2.4 (line breaks removed).
const (
	rfc8292Token = "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9.eyJhdWQiOiJodHRwczovL3B1c2guZXhhbXBsZS5uZXQiLCJleHAiOjE0NTM1MjM3NjgsInN1YiI6Im1haWx0bzpwdXNoQGV4YW1wbGUuY29tIn0.i3CYb7t4xfxCDquptFOepC9GAu_HLGkMlMuCGSK2rpiUfnK9ojFwDXb1JrErtmysazNjjvW2L9OkSSHzvoD1oA"
	rfc8292Key   = "BA1Hxzyi1RUM1b5wjxsn7nGxAszw2u61m164i3MrAIxHF6YK5h4SDYic-dRuU_RCPCfA5aq9ojSwk5Y2EmClBPs"
)

// verifyToken checks a VAPID JWT with the base64url public key k as a push
// service would and returns its claims.
func verifyToken(t *testing.T, token, k string) vapidClaims {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q has %d parts", token, len(parts))
	}

	var header map[string]string
	if err := json.Unmarshal(b64(t, parts[0]), &header); err != nil {
		t.Fatal(err)
	}

	if header["alg"] != "ES256" || header["typ"] != "JWT" {
		t.Errorf("header %v", header)
	}

	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b64(t, k))
	if err != nil {
		t.Fatal(err)
	}

	sig := b64(t, parts[2])
	if len(sig) != 64 {
		t.Fatalf("signature of %d bytes", len(sig))
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("the signature does not verify")
	}

	var claims vapidClaims
	if err := json.Unmarshal(b64(t, parts[1]), &claims); err != nil {
		t.Fatal(err)
	}

	return claims
}

func TestVerifyRFC8292Example(t *testing.T) {
	t.Parallel()

	got := verifyToken(t, rfc8292Token, rfc8292Key)
	want := vapidClaims{Aud: "https://push.example.net", Exp: 1453523768, Sub: "mailto:push@example.com"}

	if got != want {
		t.Errorf("claims %+v, want %+v", got, want)
	}
}

func TestKeys(t *testing.T) {
	t.Parallel()

	k, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	if len(b64(t, k.PublicKey())) != PublicKeyLen || len(b64(t, k.PrivateKey())) != 32 {
		t.Fatalf("keys %q, %q", k.PublicKey(), k.PrivateKey())
	}

	parsed, err := ParseKeys(k.PublicKey(), k.PrivateKey())
	if err != nil {
		t.Fatal(err)
	}

	if parsed.PublicKey() != k.PublicKey() || parsed.PrivateKey() != k.PrivateKey() {
		t.Error("the parsed keys differ")
	}

	other, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	for name, pair := range map[string][2]string{
		"another public key":   {other.PublicKey(), k.PrivateKey()},
		"private not base64":   {k.PublicKey(), "!!"},
		"private too short":    {k.PublicKey(), "AAAA"},
		"public not base64":    {"!!", k.PrivateKey()},
		"empty private scalar": {k.PublicKey(), ""},
	} {
		if _, err := ParseKeys(pair[0], pair[1]); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: want ErrInvalidKey, got %v", name, err)
		}
	}
}

func TestValidateSubject(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"mailto:office@example.org", "https://schedule.example.org"} {
		if err := ValidateSubject(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}

	for _, bad := range []string{
		"", "office@example.org", "mailto:", "http://schedule.example.org", "https://",
		// url.Parse refuses these.
		"https://[::1", "https://schedule.example.org/%zz",
	} {
		if err := ValidateSubject(bad); !errors.Is(err, ErrConfig) {
			t.Errorf("%q: want ErrConfig, got %v", bad, err)
		}
	}
}

func TestValidateEndpoint(t *testing.T) {
	t.Parallel()

	if err := ValidateEndpoint("https://fcm.googleapis.com/fcm/send/abc"); err != nil {
		t.Error(err)
	}

	for _, bad := range []string{
		"", "http://push.example.net/x", "https:///x", "https://user:pw@push.example.net/x", "push.example.net",
		"https://push.example.net/" + strings.Repeat("x", 2048),
	} {
		if err := ValidateEndpoint(bad); !errors.Is(err, ErrInvalidEndpoint) {
			t.Errorf("%q: want ErrInvalidEndpoint, got %v", bad, err)
		}
	}
}

func TestIsPublic(t *testing.T) {
	t.Parallel()

	for addr, want := range map[string]bool{
		"142.250.185.74": true, "2a00:1450:4001:82b::200a": true,
		"127.0.0.1": false, "::1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "fe80::1": false, "fc00::1": false, "100.64.0.1": false, "0.0.0.0": false,
		"::ffff:127.0.0.1": false, "224.0.0.1": false,
	} {
		if got := IsPublic(netip.MustParseAddr(addr)); got != want {
			t.Errorf("IsPublic(%s) = %v, want %v", addr, got, want)
		}
	}
}

// pushService is a fake push service that checks each request like a real
// one and decrypts its payload with the browser's key. It answers status
// with body ("  details  " if empty) and, if set, a Location header.
type pushService struct {
	t        *testing.T
	ua       *ecdh.PrivateKey
	auth     []byte
	vapidKey string
	status   int
	body     string
	location string
	requests atomic.Int32 // every request, also to a wrong path
	got      chan pushRequest
}

type pushRequest struct {
	payload []byte
	header  http.Header
	claims  vapidClaims
}

func (p *pushService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := p.t

	p.requests.Add(1)

	if r.Method != http.MethodPost || r.URL.Path != "/push/abc" {
		t.Errorf("request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)

		return
	}

	scheme, params, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || scheme != "vapid" {
		t.Errorf("Authorization %q", r.Header.Get("Authorization"))
	}

	var token, k string

	for param := range strings.SplitSeq(params, ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(param), "=")

		switch name {
		case "t":
			token = value
		case "k":
			k = value
		}
	}

	if k != p.vapidKey {
		t.Errorf("k=%q, want the VAPID public key %q", k, p.vapidKey)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
	}

	p.got <- pushRequest{payload: decrypt(t, p.ua, p.auth, body), header: r.Header.Clone(), claims: verifyToken(t, token, k)}

	if p.location != "" {
		w.Header().Set("Location", p.location)
	}

	w.WriteHeader(p.status)

	_, _ = w.Write([]byte(cmp.Or(p.body, "  details  ")))
}

// assertNoRequest fails if the push service was asked anything.
func (p *pushService) assertNoRequest(t *testing.T) {
	t.Helper()

	if n := p.requests.Load(); n != 0 {
		t.Errorf("the push service got %d requests", n)
	}
}

func newPushService(t *testing.T, status int) (*pushService, *httptest.Server, Subscription, *Sender, clock.Clock) {
	t.Helper()

	ua, err := ecdh.P256().GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	keys, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	sub := Subscription{P256DH: [PublicKeyLen]byte(ua.PublicKey().Bytes())}
	copy(sub.Auth[:], "0123456789abcdef")

	svc := &pushService{
		t: t, ua: ua, auth: sub.Auth[:], vapidKey: keys.PublicKey(), status: status, got: make(chan pushRequest, 1),
	}
	srv := httptest.NewTLSServer(svc)
	t.Cleanup(srv.Close)
	sub.Endpoint = srv.URL + "/push/abc"

	clk := clock.NewFixed(time.Date(2026, time.October, 25, 9, 0, 0, 0, time.UTC))

	sender, err := New(Config{Keys: keys, Subject: "mailto:office@example.org", Clock: clk, Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}

	return svc, srv, sub, sender, clk
}

func TestSend(t *testing.T) {
	t.Parallel()

	svc, srv, sub, sender, clk := newPushService(t, http.StatusCreated)

	payload := []byte(`{"title":"A place is free","url":"https://schedule.example.org/waitlist/accept?token=x"}`)
	if err := sender.Send(t.Context(), sub, payload, Options{TTL: 90*time.Minute + 500*time.Millisecond, Urgency: UrgencyHigh}); err != nil {
		t.Fatal(err)
	}

	got := <-svc.got

	if string(got.payload) != string(payload) {
		t.Errorf("payload %q, want %q", got.payload, payload)
	}

	for name, want := range map[string]string{
		"TTL": "5400", "Content-Encoding": "aes128gcm", "Content-Type": "application/octet-stream", "Urgency": "high",
	} {
		if v := got.header.Get(name); v != want {
			t.Errorf("%s: %q, want %q", name, v, want)
		}
	}

	wantClaims := vapidClaims{Aud: srv.URL, Exp: clk.Now().Add(12 * time.Hour).Unix(), Sub: "mailto:office@example.org"}
	if got.claims != wantClaims {
		t.Errorf("claims %+v, want %+v", got.claims, wantClaims)
	}

	if sender.PublicKey() != svc.vapidKey {
		t.Error("PublicKey is not the VAPID key")
	}
}

func TestSendWithoutUrgencyOrTTL(t *testing.T) {
	t.Parallel()

	svc, _, sub, sender, _ := newPushService(t, http.StatusCreated)

	// A negative TTL is sent as 0, like none.
	for _, ttl := range []time.Duration{0, -time.Hour} {
		if err := sender.Send(t.Context(), sub, []byte("{}"), Options{TTL: ttl}); err != nil {
			t.Fatal(err)
		}

		got := <-svc.got
		if got.header.Get("TTL") != "0" || got.header.Get("Urgency") != "" {
			t.Errorf("TTL %v: header TTL %q, Urgency %q", ttl, got.header.Get("TTL"), got.header.Get("Urgency"))
		}
	}
}

func TestSendAcceptsEverySuccess(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent} {
		svc, _, sub, sender, _ := newPushService(t, status)

		if err := sender.Send(t.Context(), sub, []byte("{}"), Options{}); err != nil {
			t.Errorf("%d: %v", status, err)
		}

		<-svc.got
	}
}

func TestSendRefusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status          int
		gone, permanent bool
	}{
		// The subscription is gone: delete it.
		{http.StatusGone, true, true},
		{http.StatusNotFound, true, true},
		// The request itself is refused: sending it again cannot help.
		{http.StatusBadRequest, false, true},
		{http.StatusUnauthorized, false, true},
		{http.StatusForbidden, false, true},
		{http.StatusRequestEntityTooLarge, false, true},
		// Overloaded or failing: retry later.
		{http.StatusTooManyRequests, false, false},
		{http.StatusInternalServerError, false, false},
		{http.StatusBadGateway, false, false},
		{http.StatusServiceUnavailable, false, false},
		// A redirect without a Location is returned as it is.
		{http.StatusMovedPermanently, false, false},
	} {
		svc, _, sub, sender, _ := newPushService(t, tc.status)

		err := sender.Send(t.Context(), sub, []byte("{}"), Options{TTL: time.Minute})
		<-svc.got

		var se *StatusError
		if !errors.As(err, &se) {
			t.Fatalf("%d: want a StatusError, got %v", tc.status, err)
		}

		if se.StatusCode != tc.status || se.Gone() != tc.gone || se.Permanent() != tc.permanent || se.Body != "details" {
			t.Errorf("%d: %+v gone %v permanent %v", tc.status, se, se.Gone(), se.Permanent())
		}
	}
}

func TestSendTruncatesTheErrorBody(t *testing.T) {
	t.Parallel()

	svc, _, sub, sender, _ := newPushService(t, http.StatusBadRequest)
	// The first maxErrorBody bytes end in a space, which is trimmed; what
	// follows them is not read.
	svc.body = "\t" + strings.Repeat("e", maxErrorBody-2) + " " + strings.Repeat("z", 100)

	err := sender.Send(t.Context(), sub, []byte("{}"), Options{})
	<-svc.got

	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want a StatusError, got %v", err)
	}

	if want := strings.Repeat("e", maxErrorBody-2); se.Body != want {
		t.Errorf("body of %d bytes %q, want the %d bytes %q", len(se.Body), se.Body, len(want), want)
	}
}

func TestStatusErrorMessage(t *testing.T) {
	t.Parallel()

	for err, want := range map[*StatusError]string{
		{StatusCode: http.StatusGone, Body: "details"}: "webpush: push service answered 410 Gone: details",
		{StatusCode: http.StatusServiceUnavailable}:    "webpush: push service answered 503 Service Unavailable",
	} {
		if got := err.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// TestSendFollowsNoRedirects runs the client of NewClient, with only its
// transport swapped for one that trusts the test server and may reach the
// loopback address, against redirects to another path of the push service.
func TestSendFollowsNoRedirects(t *testing.T) {
	t.Parallel()

	for _, status := range []int{
		http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	} {
		svc, srv, sub, defaultSender, clk := newPushService(t, status)
		svc.location = srv.URL + "/elsewhere"

		client := NewClient()
		client.Transport = srv.Client().Transport

		sender, err := New(Config{Keys: defaultSender.keys, Subject: "mailto:office@example.org", Clock: clk, Client: client})
		if err != nil {
			t.Fatal(err)
		}

		err = sender.Send(t.Context(), sub, []byte("{}"), Options{})
		<-svc.got

		var se *StatusError
		if !errors.As(err, &se) || se.StatusCode != status || se.Permanent() || se.Gone() {
			t.Errorf("%d: want a StatusError of the redirect, got %v", status, err)
		}

		// The handler counts /elsewhere too (and fails the test for it).
		if n := svc.requests.Load(); n != 1 {
			t.Errorf("%d: the push service got %d requests, want 1", status, n)
		}
	}
}

func TestSendRefusesInvalidMessages(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		p256dh  func(*[PublicKeyLen]byte)
		payload []byte
		want    error
	}{
		"p256dh all zeros": {p256dh: func(k *[PublicKeyLen]byte) { *k = [PublicKeyLen]byte{} }, want: ErrInvalidKey},
		"p256dh not on the curve": {
			// An uncompressed point (0x04, x, y) with x = y = 1.
			p256dh: func(k *[PublicKeyLen]byte) {
				*k = [PublicKeyLen]byte{0: 0x04, 32: 1, 64: 1}
			},
			want: ErrInvalidKey,
		},
		"payload too large": {payload: bytes.Repeat([]byte("x"), MaxPayload+1), want: ErrPayloadTooLarge},
	} {
		svc, _, sub, sender, _ := newPushService(t, http.StatusCreated)

		if tc.p256dh != nil {
			tc.p256dh(&sub.P256DH)
		}

		payload := tc.payload
		if payload == nil {
			payload = []byte("{}")
		}

		if _, err := Encrypt(sub, payload); !errors.Is(err, tc.want) {
			t.Errorf("%s: Encrypt: want %v, got %v", name, tc.want, err)
		}

		if err := sender.Send(t.Context(), sub, payload, Options{}); !errors.Is(err, tc.want) {
			t.Errorf("%s: Send: want %v, got %v", name, tc.want, err)
		}

		svc.assertNoRequest(t)
	}
}

func TestSendWithoutContext(t *testing.T) {
	t.Parallel()

	svc, _, sub, sender, _ := newPushService(t, http.StatusCreated)

	var ctx context.Context // a caller's bug: no context at all

	if err := sender.Send(ctx, sub, []byte("{}"), Options{}); err == nil {
		t.Error("a nil context: want an error")
	}

	svc.assertNoRequest(t)
}

func TestRefusePrivate(t *testing.T) {
	t.Parallel()

	// Documentation addresses (RFC 5737, RFC 3849) count as public for
	// IsPublic: it rules out what is reachable inside, not what is unrouted.
	for _, address := range []string{"203.0.113.7:443", "[2001:db8::1]:443"} {
		if err := refusePrivate("tcp", address, nil); err != nil {
			t.Errorf("%s: %v", address, err)
		}
	}

	for _, address := range []string{
		"127.0.0.1:443", "[::1]:443", "10.1.2.3:443", "[::ffff:192.168.1.1]:443", "169.254.169.254:80",
		"203.0.113.7",          // no port
		"push.example.net:443", // a name, not a resolved address
	} {
		if err := refusePrivate("tcp", address, nil); !errors.Is(err, ErrPrivateAddress) {
			t.Errorf("%s: want ErrPrivateAddress, got %v", address, err)
		}
	}
}

func TestSendRefusesPrivateAddresses(t *testing.T) {
	t.Parallel()

	svc, _, sub, _, _ := newPushService(t, http.StatusCreated)

	keys, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	// The default client: the test server listens on the loopback address.
	sender, err := New(Config{Keys: keys, Subject: "mailto:office@example.org"})
	if err != nil {
		t.Fatal(err)
	}

	if err := sender.Send(t.Context(), sub, []byte("{}"), Options{}); !errors.Is(err, ErrPrivateAddress) {
		t.Errorf("want ErrPrivateAddress, got %v", err)
	}

	select {
	case <-svc.got:
		t.Error("the request reached the server")
	default:
	}

	sub.Endpoint = "http://push.example.net/x"
	if err := sender.Send(t.Context(), sub, []byte("{}"), Options{}); !errors.Is(err, ErrInvalidEndpoint) {
		t.Errorf("plain http: want ErrInvalidEndpoint, got %v", err)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{Subject: "mailto:office@example.org"}); !errors.Is(err, ErrConfig) {
		t.Errorf("no keys: want ErrConfig, got %v", err)
	}

	keys, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := New(Config{Keys: keys, Subject: "office"}); !errors.Is(err, ErrConfig) {
		t.Errorf("bad subject: want ErrConfig, got %v", err)
	}
}

func TestEncodeBase64URLHasNoPadding(t *testing.T) {
	t.Parallel()

	if got := encodeBase64URL([]byte{0xfb, 0xff}); got != base64.RawURLEncoding.EncodeToString([]byte{0xfb, 0xff}) ||
		strings.Contains(got, "=") {
		t.Errorf("got %q", got)
	}
}
