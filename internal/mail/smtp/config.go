// Package smtp delivers mail messages over SMTP.
//
// It is server only: it imports net/smtp, which the WebAssembly demo must not
// depend on (scripts/check-wasm-deps.sh). The demo keeps mail in the outbox
// instead.
package smtp

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	netmail "net/mail"
	"slices"
	"strings"
)

// TLSMode selects how the connection to the SMTP server is encrypted.
type TLSMode string

// Known TLS modes.
const (
	// TLSStartTLS connects in plain text and upgrades with STARTTLS; a server
	// that does not offer it is an error. Default port 587.
	TLSStartTLS TLSMode = "starttls"
	// TLSImplicit speaks TLS from the first byte (SMTPS). Default port 465.
	TLSImplicit TLSMode = "tls"
	// TLSNone sends in plain text, for local mail catchers such as Mailpit.
	// Default port 25. Authentication then needs a localhost host.
	TLSNone TLSMode = "none"
)

// TLSModes returns all TLS modes.
func TLSModes() []TLSMode { return []TLSMode{TLSStartTLS, TLSImplicit, TLSNone} }

// Valid reports whether m is a known TLS mode.
func (m TLSMode) Valid() bool { return slices.Contains(TLSModes(), m) }

// defaultPort returns the conventional port for m.
func (m TLSMode) defaultPort() int {
	switch m {
	case TLSImplicit:
		return 465
	case TLSNone:
		return 25
	default:
		return 587
	}
}

// ErrInvalidConfig is wrapped by every validation error returned by New.
var ErrInvalidConfig = errors.New("invalid smtp configuration")

// redactedMask replaces the password in printed values (the same mask as
// internal/config uses).
const redactedMask = "xxxxx"

// Config configures a Sender. Its String, GoString and LogValue methods mask
// the password.
type Config struct {
	// Host is the SMTP server's host name or IP address, without port.
	Host string
	// Port is the TCP port; zero means the default of the TLS mode.
	Port int
	// Username and Password enable SMTP AUTH PLAIN; both or neither.
	Username string
	Password string
	// From is the sender address, optionally with a display name
	// ("Studio Example <noreply@example.org>").
	From string
	// TLS is the encryption mode; empty means TLSStartTLS.
	TLS TLSMode
}

// withDefaults fills in the TLS mode and port.
func (c Config) withDefaults() Config {
	c.Host = strings.TrimSpace(c.Host)
	c.From = strings.TrimSpace(c.From)

	if c.TLS == "" {
		c.TLS = TLSStartTLS
	}

	if c.Port == 0 {
		c.Port = c.TLS.defaultPort()
	}

	return c
}

// validate checks c (after withDefaults) and returns the parsed sender
// address. All problems are reported at once; the errors wrap
// ErrInvalidConfig and never contain the password.
func (c Config) validate() (*netmail.Address, error) {
	var errs []error

	if c.Host == "" {
		errs = append(errs, errors.New("host must be set"))
	} else if _, _, err := net.SplitHostPort(c.Host); err == nil || strings.ContainsAny(c.Host, " \t\r\n/") {
		errs = append(errs, errors.New("host must be a bare host name without port"))
	}

	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("port %d out of range", c.Port))
	}

	if !c.TLS.Valid() {
		errs = append(errs, fmt.Errorf("unknown TLS mode %q (want starttls, tls or none)", c.TLS))
	}

	from, err := netmail.ParseAddress(c.From)
	if err != nil || strings.ContainsAny(c.From, "\r\n") {
		errs = append(errs, errors.New("from must be a single email address"))
	}

	if (c.Username == "") != (c.Password == "") {
		errs = append(errs, errors.New("username and password must be set together"))
	}

	if c.Username != "" && c.TLS == TLSNone && !isLocalhost(c.Host) {
		errs = append(errs, errors.New("authentication without TLS is only allowed for localhost"))
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, errors.Join(errs...))
	}

	return from, nil
}

// isLocalhost mirrors the hosts for which net/smtp's PlainAuth accepts an
// unencrypted connection.
func isLocalhost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Redacted returns a copy of c with the password masked.
func (c Config) Redacted() Config {
	if c.Password != "" {
		c.Password = redactedMask
	}

	return c
}

// String implements fmt.Stringer with the password masked.
func (c Config) String() string {
	r := c.Redacted()

	return fmt.Sprintf("{Host:%s Port:%d Username:%s Password:%s From:%s TLS:%s}",
		r.Host, r.Port, r.Username, r.Password, r.From, r.TLS)
}

// GoString implements fmt.GoStringer (%#v) with the password masked.
func (c Config) GoString() string {
	r := c.Redacted()

	return fmt.Sprintf("smtp.Config{Host:%q, Port:%d, Username:%q, Password:%q, From:%q, TLS:%q}",
		r.Host, r.Port, r.Username, r.Password, r.From, r.TLS)
}

// LogValue implements slog.LogValuer with the password masked.
func (c Config) LogValue() slog.Value {
	r := c.Redacted()

	return slog.GroupValue(
		slog.String("host", r.Host),
		slog.Int("port", r.Port),
		slog.String("username", r.Username),
		slog.String("password", r.Password),
		slog.String("from", r.From),
		slog.String("tls", string(r.TLS)),
	)
}
