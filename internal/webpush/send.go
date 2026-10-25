package webpush

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
)

// Urgency is the Urgency header of a message (RFC 8030, section 5.3).
type Urgency string

// Known urgencies.
const (
	UrgencyVeryLow Urgency = "very-low"
	UrgencyLow     Urgency = "low"
	UrgencyNormal  Urgency = "normal"
	UrgencyHigh    Urgency = "high"
)

// Options are the delivery options of one message.
type Options struct {
	// TTL is how long the push service keeps the message for a browser
	// that is offline; 0 delivers it only to a browser that is reachable
	// now. It is sent in whole seconds.
	TTL time.Duration
	// Urgency is sent if not empty.
	Urgency Urgency
}

// Config configures a Sender.
type Config struct {
	// Keys is the VAPID key pair; required.
	Keys Keys
	// Subject is the contact of the application server, a mailto: or an
	// https: URL (the VAPID "sub" claim); required.
	Subject string
	// Clock dates the tokens; defaults to clock.System.
	Clock clock.Clock
	// Client sends the requests; defaults to a client that connects only to
	// public addresses and follows no redirects (see NewClient).
	Client *http.Client
}

// Sender sends push messages. It is safe for concurrent use.
type Sender struct {
	keys    Keys
	subject string
	clock   clock.Clock
	client  *http.Client
}

// ErrConfig is wrapped by the errors of an invalid Config.
var ErrConfig = errors.New("webpush: invalid configuration")

// New returns a Sender for cfg.
func New(cfg Config) (*Sender, error) {
	if cfg.Keys.IsZero() {
		return nil, fmt.Errorf("%w: no VAPID keys", ErrConfig)
	}

	if err := ValidateSubject(cfg.Subject); err != nil {
		return nil, err
	}

	if cfg.Clock == nil {
		cfg.Clock = clock.System{}
	}

	if cfg.Client == nil {
		cfg.Client = NewClient()
	}

	return &Sender{keys: cfg.Keys, subject: cfg.Subject, clock: cfg.Clock, client: cfg.Client}, nil
}

// ValidateSubject checks a VAPID subject: a mailto: URL with an address, or
// an https: URL with a host.
func ValidateSubject(subject string) error {
	u, err := url.Parse(subject)

	switch {
	case err != nil:
	case u.Scheme == "mailto" && strings.Contains(u.Opaque, "@"):
		return nil
	case u.Scheme == "https" && u.Host != "":
		return nil
	}

	return fmt.Errorf("%w: the subject must be a mailto: or https: URL, got %q", ErrConfig, subject)
}

// PublicKey returns the applicationServerKey browsers subscribe with
// (Keys.PublicKey).
func (s *Sender) PublicKey() string { return s.keys.PublicKey() }

// StatusError is a push service's answer other than success.
type StatusError struct {
	StatusCode int
	// Body is the start of the answer, for the log.
	Body string
}

// Error implements error.
func (e *StatusError) Error() string {
	msg := fmt.Sprintf("webpush: push service answered %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	if e.Body != "" {
		msg += ": " + e.Body
	}

	return msg
}

// Gone reports whether the subscription no longer exists (404 or 410):
// the browser unsubscribed or the subscription expired, and it should be
// deleted.
func (e *StatusError) Gone() bool {
	return e.StatusCode == http.StatusNotFound || e.StatusCode == http.StatusGone
}

// Permanent reports whether sending the message again cannot succeed: the
// subscription is gone, or the push service refuses the request itself (a
// bad request, the wrong VAPID key, a payload too large).
func (e *StatusError) Permanent() bool {
	switch e.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestEntityTooLarge:
		return true
	default:
		return e.Gone()
	}
}

// maxErrorBody bounds the answer read for a StatusError.
const maxErrorBody = 512

// Send encrypts payload for sub and posts it to the subscription's
// endpoint. A push service's refusal is a *StatusError.
func (s *Sender) Send(ctx context.Context, sub Subscription, payload []byte, opts Options) error {
	if err := ValidateEndpoint(sub.Endpoint); err != nil {
		return err
	}

	aud, err := audience(sub.Endpoint)
	if err != nil {
		return err
	}

	body, err := Encrypt(sub, payload)
	if err != nil {
		return err
	}

	auth, err := s.keys.authorization(aud, s.subject, s.clock.Now().Add(tokenLifetime))
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webpush: request: %w", err)
	}

	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.FormatInt(int64(max(opts.TTL, 0)/time.Second), 10))

	if opts.Urgency != "" {
		req.Header.Set("Urgency", string(opts.Urgency))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("webpush: send: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))

		return nil
	}

	msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	return &StatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
}

// ValidateEndpoint checks a subscription endpoint: an absolute https URL
// with a host name, without credentials, at most 2048 bytes long.
func ValidateEndpoint(endpoint string) error {
	const maxLen = 2048

	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > maxLen || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("%w: the endpoint must be an https URL", ErrInvalidEndpoint)
	}

	return nil
}

// ErrInvalidEndpoint is wrapped by the errors of ValidateEndpoint.
var ErrInvalidEndpoint = errors.New("webpush: invalid endpoint")

// sendTimeout bounds one request to a push service.
const sendTimeout = 30 * time.Second

// NewClient returns the default HTTP client of a Sender. The endpoints come
// from browsers, that is from users, so the client refuses to connect to
// anything but public unicast addresses (no loopback, private, link-local
// or shared networks, checked on the resolved address, so DNS cannot
// redirect it inside), follows no redirects and uses no proxy.
func NewClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: refusePrivate}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   sendTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// ErrPrivateAddress is returned for an endpoint that resolves to an
// address that is not public.
var ErrPrivateAddress = errors.New("webpush: the endpoint is not a public address")

// sharedAddressSpace is the carrier-grade NAT range (RFC 6598).
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// refusePrivate is the dialer's Control: it runs on the resolved address
// just before the connection is made.
func refusePrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, address)
	}

	ip, err := netip.ParseAddr(host)
	if err != nil || !IsPublic(ip) {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, address)
	}

	return nil
}

// IsPublic reports whether ip is a public unicast address.
func IsPublic(ip netip.Addr) bool {
	ip = ip.Unmap()

	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() &&
		!sharedAddressSpace.Contains(ip)
}
