// Package config loads the server configuration from environment variables
// and command-line flags. Precedence is: explicit flag, then environment
// variable, then default. The package does not know about any flag library;
// the command binds the flags and passes the explicitly set values in Flags.
//
// Only settings with a consumer exist here. Secrets come from the environment
// (or flags) only, and a Config never prints them: String, GoString and
// LogValue all mask passwords.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/clientip"
	"github.com/cwbudde/studio-schedule/internal/webpush"
)

// Environment variable names.
const (
	EnvDatabaseURL = "DATABASE_URL"
	EnvHTTPAddr    = "HTTP_ADDR"
	EnvBaseURL     = "BASE_URL"

	// Outgoing mail (environment only). Without SMTP_HOST mails stay in the
	// outbox.
	EnvSMTPHost     = "SMTP_HOST"
	EnvSMTPPort     = "SMTP_PORT"
	EnvSMTPUsername = "SMTP_USERNAME"
	EnvSMTPPassword = "SMTP_PASSWORD"
	EnvSMTPFrom     = "SMTP_FROM"
	EnvSMTPTLS      = "SMTP_TLS"

	// EnvTrustedProxies lists the reverse proxies (CIDR prefixes or
	// addresses, comma-separated) whose X-Forwarded-For entries name the
	// client (environment only).
	EnvTrustedProxies = "TRUSTED_PROXIES"

	// EnvHSTSIncludeSubdomains (true or false, default true) extends the
	// Strict-Transport-Security header of an https:// BASE_URL to every
	// subdomain of the host (environment only).
	EnvHSTSIncludeSubdomains = "HSTS_INCLUDE_SUBDOMAINS"

	// EnvMetricsAddr is the host:port of the Prometheus metrics server;
	// empty means no metrics.
	EnvMetricsAddr = "METRICS_ADDR"
	// EnvLogFormat (text or json) and EnvLogLevel (debug, info, warn or
	// error) set the server's log output.
	EnvLogFormat = "LOG_FORMAT"
	EnvLogLevel  = "LOG_LEVEL"

	// Web push (ADR 0017, environment only): the VAPID key pair that
	// `studio-schedule vapid-keys` prints, and the contact the push
	// services see (a mailto: or https: URL). All three or none: without
	// them push is off.
	EnvVAPIDPublicKey  = "VAPID_PUBLIC_KEY"
	EnvVAPIDPrivateKey = "VAPID_PRIVATE_KEY"
	EnvVAPIDSubject    = "VAPID_SUBJECT"
)

// Flag names of the settings that a flag can set. The commands bind them;
// errors name the flag when a flag supplied the bad value.
const (
	FlagDatabaseURL = "database-url"
	FlagHTTPAddr    = "addr"
	FlagBaseURL     = "base-url"
	FlagLogFormat   = "log-format"
	FlagLogLevel    = "log-level"
	FlagMetricsAddr = "metrics-addr"
)

// Defaults when neither flag nor env sets a value.
const (
	DefaultHTTPAddr  = ":8080"
	DefaultLogFormat = "text"
	DefaultLogLevel  = "info"
)

// redactedMask replaces secrets in printed values (the same mask as
// url.URL.Redacted uses).
const redactedMask = "xxxxx"

// ErrInvalid is wrapped by every validation error returned by Load.
var ErrInvalid = errors.New("invalid configuration")

// Config is the validated server configuration.
type Config struct {
	// DatabaseURL is a PostgreSQL connection string, either a URL
	// (postgres://…) or a libpq key=value DSN. Empty means no database.
	// It may contain a password.
	DatabaseURL string
	// HTTPAddr is the host:port the HTTP server listens on.
	HTTPAddr string
	// BaseURL is the public origin of the app (scheme://host[:port], no
	// trailing slash), used to build absolute links. Empty means unset.
	BaseURL string
	// SMTP is the mail server; a zero Host means none.
	SMTP SMTP
	// Push is the web push configuration; the zero value means push is
	// off.
	Push Push
	// TrustedProxies are the reverse proxies in front of the app; empty
	// means the connection's address is the client's.
	TrustedProxies []netip.Prefix
	// HSTSIncludeSubdomains extends HSTS to the subdomains of the host
	// (includeSubDomains); the default is true.
	HSTSIncludeSubdomains bool
	// MetricsAddr is the host:port of the separate server of the
	// Prometheus metrics (/metrics). Empty means no metrics.
	MetricsAddr string

	// Log is the log output; the zero value is text at level info.
	Log Log
}

// Log is the server's log output.
type Log struct {
	// JSON selects JSON lines instead of the text format.
	JSON bool
	// Level is the minimum level logged; the zero value is info.
	Level slog.Level
}

// SMTP is the outgoing mail server. Package mail/smtp validates it fully;
// Load only checks what it parses.
type SMTP struct {
	Host     string
	Port     int // 0: the default port of the TLS mode
	Username string
	Password string // secret
	From     string // sender address, possibly with a display name
	TLS      string // starttls (default), tls or none
}

// String implements fmt.Stringer with the password masked.
func (s SMTP) String() string { return s.GoString() }

// GoString implements fmt.GoStringer (%#v) with the password masked.
func (s SMTP) GoString() string {
	if s.Password != "" {
		s.Password = redactedMask
	}

	type plain SMTP // no methods, so %+v does not recurse

	return fmt.Sprintf("%+v", plain(s))
}

// Push configures web push (ADR 0017). Load checks that the keys parse
// and belong together.
type Push struct {
	PublicKey  string // base64url of the uncompressed P-256 point
	PrivateKey string // secret: base64url of the P-256 scalar
	Subject    string // mailto: or https: URL
}

// Enabled reports whether push is configured.
func (p Push) Enabled() bool { return p.PrivateKey != "" }

// String implements fmt.Stringer with the private key masked.
func (p Push) String() string { return p.GoString() }

// GoString implements fmt.GoStringer (%#v) with the private key masked.
func (p Push) GoString() string {
	if p.PrivateKey != "" {
		p.PrivateKey = redactedMask
	}

	type plain Push // no methods, so %+v does not recurse

	return fmt.Sprintf("%+v", plain(p))
}

// LookupFunc looks up an environment variable; os.LookupEnv satisfies it.
type LookupFunc func(key string) (string, bool)

// Flags holds the values given explicitly on the command line. A nil field
// means the flag was not given; a non-nil empty string overrides the
// environment with an empty value.
type Flags struct {
	DatabaseURL *string
	HTTPAddr    *string
	BaseURL     *string
	MetricsAddr *string
	LogFormat   *string
	LogLevel    *string
}

// Load combines flags, the environment (read through lookup; nil means an
// empty environment) and defaults, and validates the result. All problems are
// reported at once, each naming the flag or environment variable that
// supplied the bad value; the errors wrap ErrInvalid and never contain
// secrets.
func Load(lookup LookupFunc, flags Flags) (Config, error) {
	return load(lookup, flags, true)
}

// LoadWithoutLog is Load for the commands that write no log (migrate, seed,
// init, import, healthcheck): it ignores the log settings, so a mistake there
// cannot block, say, a migration. The returned Log is the zero value.
func LoadWithoutLog(lookup LookupFunc, flags Flags) (Config, error) {
	return load(lookup, flags, false)
}

// setting is a value with the name of its source: the flag (with dashes) or
// the environment variable that supplied it.
type setting struct {
	value, source string
}

func load(lookup LookupFunc, flags Flags, withLog bool) (Config, error) {
	if lookup == nil {
		lookup = func(string) (string, bool) { return "", false }
	}

	pick := func(flag *string, flagName, key, def string) setting {
		if flag != nil {
			return setting{strings.TrimSpace(*flag), "--" + flagName}
		}

		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return setting{strings.TrimSpace(v), key}
		}

		return setting{def, key}
	}
	env := func(key string) string { return pick(nil, "", key, "").value }

	databaseURL := pick(flags.DatabaseURL, FlagDatabaseURL, EnvDatabaseURL, "")
	httpAddr := pick(flags.HTTPAddr, FlagHTTPAddr, EnvHTTPAddr, DefaultHTTPAddr)
	baseURL := pick(flags.BaseURL, FlagBaseURL, EnvBaseURL, "")
	metricsAddr := pick(flags.MetricsAddr, FlagMetricsAddr, EnvMetricsAddr, "")

	cfg := Config{
		DatabaseURL: databaseURL.value,
		HTTPAddr:    httpAddr.value,
		MetricsAddr: metricsAddr.value,
		SMTP: SMTP{
			Host:     env(EnvSMTPHost),
			Username: env(EnvSMTPUsername),
			Password: env(EnvSMTPPassword),
			From:     env(EnvSMTPFrom),
			TLS:      env(EnvSMTPTLS),
		},
	}

	var errs []error

	if port := env(EnvSMTPPort); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			errs = append(errs, fmt.Errorf("%s: %q is not a port (1-65535)", EnvSMTPPort, port))
		}

		cfg.SMTP.Port = int(n)
	}

	trusted, err := clientip.ParseTrusted(env(EnvTrustedProxies))
	if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", EnvTrustedProxies, err))
	}

	cfg.TrustedProxies = trusted

	cfg.HSTSIncludeSubdomains = true
	if v := env(EnvHSTSIncludeSubdomains); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %q is not a boolean (true or false)", EnvHSTSIncludeSubdomains, v))
		}

		cfg.HSTSIncludeSubdomains = b
	}

	push, pushErrs := loadPush(env(EnvVAPIDPublicKey), env(EnvVAPIDPrivateKey), env(EnvVAPIDSubject))
	errs = append(errs, pushErrs...)
	cfg.Push = push

	if withLog {
		logCfg, logErrs := loadLog(pick(flags.LogFormat, FlagLogFormat, EnvLogFormat, DefaultLogFormat),
			pick(flags.LogLevel, FlagLogLevel, EnvLogLevel, DefaultLogLevel))
		errs = append(errs, logErrs...)
		cfg.Log = logCfg
	}

	if cfg.SMTP.Host == "" && (cfg.SMTP.From != "" || cfg.SMTP.Username != "" || cfg.SMTP.Password != "") {
		errs = append(errs, fmt.Errorf("%s: required when other SMTP_* variables are set", EnvSMTPHost))
	}

	if cfg.SMTP.Host != "" && cfg.SMTP.From == "" {
		errs = append(errs, fmt.Errorf("%s: required with %s", EnvSMTPFrom, EnvSMTPHost))
	}

	if err := validateDatabaseURL(databaseURL); err != nil {
		errs = append(errs, err)
	}

	if err := validateAddr(httpAddr); err != nil {
		errs = append(errs, err)
	}

	if metricsAddr.value != "" {
		if err := validateAddr(metricsAddr); err != nil {
			errs = append(errs, err)
		} else if addrsConflict(metricsAddr.value, httpAddr.value) {
			errs = append(errs, fmt.Errorf("%s: %q would listen on the port of %s %q; choose another port",
				metricsAddr.source, metricsAddr.value, httpAddr.source, httpAddr.value))
		}
	}

	cfg.BaseURL, err = normalizeBaseURL(baseURL)
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("%w: %w", ErrInvalid, errors.Join(errs...))
	}

	return cfg, nil
}

// loadPush checks the VAPID settings: all three or none, keys that parse
// and belong together, and a valid subject. The errors never contain the
// private key.
func loadPush(public, private, subject string) (Push, []error) {
	if public == "" && private == "" && subject == "" {
		return Push{}, nil
	}

	var errs []error

	for name, v := range map[string]string{
		EnvVAPIDPublicKey: public, EnvVAPIDPrivateKey: private, EnvVAPIDSubject: subject,
	} {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s: required when other VAPID_* variables are set", name))
		}
	}

	if len(errs) > 0 {
		slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })

		return Push{}, errs
	}

	if _, err := webpush.ParseKeys(public, private); err != nil {
		errs = append(errs, fmt.Errorf("%s, %s: not a VAPID key pair (run `studio-schedule vapid-keys`)",
			EnvVAPIDPublicKey, EnvVAPIDPrivateKey))
	}

	if err := webpush.ValidateSubject(subject); err != nil {
		errs = append(errs, fmt.Errorf("%s: %q is not a mailto: or https: URL", EnvVAPIDSubject, subject))
	}

	return Push{PublicKey: public, PrivateKey: private, Subject: subject}, errs
}

// loadLog parses the log format and level; an empty value (an explicitly
// empty flag) means the default.
func loadLog(format, level setting) (Log, []error) {
	var (
		l    Log
		errs []error
	)

	switch strings.ToLower(format.value) {
	case "", DefaultLogFormat:
	case "json":
		l.JSON = true
	default:
		errs = append(errs, fmt.Errorf("%s: %q is not a log format (text or json)", format.source, format.value))
	}

	if level.value != "" {
		if err := l.Level.UnmarshalText([]byte(level.value)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %q is not a log level (debug, info, warn or error)", level.source, level.value))
		}
	}

	return l, errs
}

// validateDatabaseURL checks the syntax only; it does not connect. The
// errors never include the value, which may contain a password.
func validateDatabaseURL(v setting) error {
	s, name := v.value, v.source

	switch {
	case s == "":
		return nil
	case urlForm.MatchString(s):
		u, err := url.Parse(s)
		if err != nil {
			return fmt.Errorf("%s: not a valid URL", name)
		}

		if u.Scheme != "postgres" && u.Scheme != "postgresql" {
			return fmt.Errorf("%s: scheme must be postgres or postgresql", name)
		}

		return nil
	case strings.Contains(s, "="):
		// libpq key=value DSN; the driver checks the keywords.
		return nil
	default:
		return fmt.Errorf("%s: want a postgres:// URL or a key=value DSN", name)
	}
}

// validateAddr checks the listen address v.
func validateAddr(v setting) error {
	s, name := v.value, v.source

	_, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("%s: %q is not host:port", name, s)
	}

	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("%s: %q needs a numeric port (0-65535)", name, s)
	}

	return nil
}

// addrsConflict reports whether the valid listen addresses a and b would
// take the same socket: the same port (not 0, which the system picks) on
// the same host, or on any host where either listens on every interface
// (an empty host, 0.0.0.0 or ::). localhost is 127.0.0.1 and ::1; other
// host names are compared by name, without resolving them.
func addrsConflict(a, b string) bool {
	hostA, portA, _ := net.SplitHostPort(a)
	hostB, portB, _ := net.SplitHostPort(b)

	pa, _ := strconv.ParseUint(portA, 10, 16)
	pb, _ := strconv.ParseUint(portB, 10, 16)

	if pa != pb || pa == 0 {
		return false
	}

	keysA, keysB := listenHosts(hostA), listenHosts(hostB)
	if keysA == nil || keysB == nil {
		return true // every interface
	}

	for _, k := range keysA {
		if slices.Contains(keysB, k) {
			return true
		}
	}

	return false
}

// listenHosts returns the hosts a listener on host binds, or nil for every
// interface.
func listenHosts(host string) []string {
	if strings.EqualFold(host, "localhost") {
		return []string{"127.0.0.1", "::1"}
	}

	if host == "" {
		return nil
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return []string{strings.ToLower(host)}
	}

	addr = addr.Unmap()
	if addr.IsUnspecified() {
		return nil
	}

	return []string{addr.String()}
}

// normalizeBaseURL accepts an absolute http(s) origin and strips a trailing
// slash. Paths are rejected because the app is served from the root.
func normalizeBaseURL(v setting) (string, error) {
	s, name := v.value, v.source
	if s == "" {
		return "", nil
	}

	u, err := url.Parse(s)

	switch {
	case err != nil:
		return "", fmt.Errorf("%s: not a valid URL", name)
	case u.Scheme != "http" && u.Scheme != "https":
		return "", fmt.Errorf("%s: must be an absolute http or https URL", name)
	case u.Host == "":
		return "", fmt.Errorf("%s: has no host", name)
	case u.User != nil:
		return "", fmt.Errorf("%s: must not contain credentials", name)
	case u.Path != "" && u.Path != "/", u.RawQuery != "", u.ForceQuery, u.Fragment != "":
		return "", fmt.Errorf("%s: must be an origin without path, query or fragment", name)
	}

	return strings.TrimSuffix(s, "/"), nil
}

// Redacted returns a copy of c with passwords in DatabaseURL and SMTP
// masked.
func (c Config) Redacted() Config {
	c.DatabaseURL = redactDatabaseURL(c.DatabaseURL)
	if c.SMTP.Password != "" {
		c.SMTP.Password = redactedMask
	}

	if c.Push.PrivateKey != "" {
		c.Push.PrivateKey = redactedMask
	}

	return c
}

// String implements fmt.Stringer with secrets masked.
func (c Config) String() string {
	r := c.Redacted()

	return fmt.Sprintf("{DatabaseURL:%s HTTPAddr:%s BaseURL:%s SMTP:%s Push:%s MetricsAddr:%s}",
		r.DatabaseURL, r.HTTPAddr, r.BaseURL, r.SMTP, r.Push, r.MetricsAddr)
}

// GoString implements fmt.GoStringer (%#v) with secrets masked.
func (c Config) GoString() string {
	r := c.Redacted()

	return fmt.Sprintf("config.Config{DatabaseURL:%q, HTTPAddr:%q, BaseURL:%q, SMTP:%s, Push:%s, MetricsAddr:%q}",
		r.DatabaseURL, r.HTTPAddr, r.BaseURL, r.SMTP, r.Push, r.MetricsAddr)
}

// LogValue implements slog.LogValuer with secrets masked.
func (c Config) LogValue() slog.Value {
	r := c.Redacted()

	return slog.GroupValue(
		slog.String("databaseURL", r.DatabaseURL),
		slog.String("httpAddr", r.HTTPAddr),
		slog.String("baseURL", r.BaseURL),
		slog.Group("smtp",
			slog.String("host", r.SMTP.Host),
			slog.Int("port", r.SMTP.Port),
			slog.String("username", r.SMTP.Username),
			slog.String("password", r.SMTP.Password),
			slog.String("from", r.SMTP.From),
			slog.String("tls", r.SMTP.TLS),
		),
		slog.Group("push",
			slog.String("publicKey", r.Push.PublicKey),
			slog.String("privateKey", r.Push.PrivateKey),
			slog.String("subject", r.Push.Subject),
		),
		slog.String("metricsAddr", r.MetricsAddr),
		slog.Bool("hstsIncludeSubdomains", r.HSTSIncludeSubdomains),
		slog.Group("log",
			slog.Bool("json", r.Log.JSON),
			slog.String("level", r.Log.Level.String()),
		),
	)
}

// urlForm tells a connection URL from a key=value DSN (whose values may
// contain "://" as well).
var urlForm = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// dsnPassword matches the password keywords of a libpq key=value DSN with a
// single-quoted (possibly unterminated) or bare value.
var dsnPassword = regexp.MustCompile(`(^|\s)((?:ssl)?password\s*=\s*)('(?:[^'\\]|\\.)*(?:'|$)|(?:[^\s\\]|\\.)*)`)

// secretParams are connection parameters that hold secrets in either form.
var secretParams = []string{"password", "sslpassword"}

func redactDatabaseURL(s string) string {
	if s == "" {
		return ""
	}

	if !urlForm.MatchString(s) {
		return dsnPassword.ReplaceAllString(s, "${1}${2}"+redactedMask)
	}

	u, err := url.Parse(s)
	if err != nil {
		return redactedMask
	}

	if q := u.Query(); len(q) > 0 {
		changed := false

		for _, k := range secretParams {
			if q.Has(k) {
				q.Set(k, redactedMask)

				changed = true
			}
		}

		if changed {
			u.RawQuery = q.Encode()
		}
	}

	return u.Redacted()
}
