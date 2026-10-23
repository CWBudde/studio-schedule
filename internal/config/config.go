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
	"net/url"
	"regexp"
	"strconv"
	"strings"
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
)

// DefaultHTTPAddr is the listen address when neither flag nor env sets one.
const DefaultHTTPAddr = ":8080"

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

// LookupFunc looks up an environment variable; os.LookupEnv satisfies it.
type LookupFunc func(key string) (string, bool)

// Flags holds the values given explicitly on the command line. A nil field
// means the flag was not given; a non-nil empty string overrides the
// environment with an empty value.
type Flags struct {
	DatabaseURL *string
	HTTPAddr    *string
	BaseURL     *string
}

// Load combines flags, the environment (read through lookup; nil means an
// empty environment) and defaults, and validates the result. All problems are
// reported at once; the errors wrap ErrInvalid and never contain secrets.
func Load(lookup LookupFunc, flags Flags) (Config, error) {
	if lookup == nil {
		lookup = func(string) (string, bool) { return "", false }
	}

	pick := func(flag *string, key, def string) string {
		if flag != nil {
			return strings.TrimSpace(*flag)
		}

		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}

		return def
	}

	cfg := Config{
		DatabaseURL: pick(flags.DatabaseURL, EnvDatabaseURL, ""),
		HTTPAddr:    pick(flags.HTTPAddr, EnvHTTPAddr, DefaultHTTPAddr),
		BaseURL:     pick(flags.BaseURL, EnvBaseURL, ""),
		SMTP: SMTP{
			Host:     pick(nil, EnvSMTPHost, ""),
			Username: pick(nil, EnvSMTPUsername, ""),
			Password: pick(nil, EnvSMTPPassword, ""),
			From:     pick(nil, EnvSMTPFrom, ""),
			TLS:      pick(nil, EnvSMTPTLS, ""),
		},
	}

	var errs []error

	if port := pick(nil, EnvSMTPPort, ""); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			errs = append(errs, fmt.Errorf("%s: %q is not a port (1-65535)", EnvSMTPPort, port))
		}

		cfg.SMTP.Port = int(n)
	}

	if cfg.SMTP.Host == "" && (cfg.SMTP.From != "" || cfg.SMTP.Username != "" || cfg.SMTP.Password != "") {
		errs = append(errs, fmt.Errorf("%s: required when other SMTP_* variables are set", EnvSMTPHost))
	}

	if cfg.SMTP.Host != "" && cfg.SMTP.From == "" {
		errs = append(errs, fmt.Errorf("%s: required with %s", EnvSMTPFrom, EnvSMTPHost))
	}

	if err := validateDatabaseURL(cfg.DatabaseURL); err != nil {
		errs = append(errs, err)
	}

	if err := validateHTTPAddr(cfg.HTTPAddr); err != nil {
		errs = append(errs, err)
	}

	baseURL, err := normalizeBaseURL(cfg.BaseURL)
	if err != nil {
		errs = append(errs, err)
	}

	cfg.BaseURL = baseURL

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("%w: %w", ErrInvalid, errors.Join(errs...))
	}

	return cfg, nil
}

// validateDatabaseURL checks the syntax only; it does not connect. The
// errors never include the value, which may contain a password.
func validateDatabaseURL(s string) error {
	switch {
	case s == "":
		return nil
	case urlForm.MatchString(s):
		u, err := url.Parse(s)
		if err != nil {
			return fmt.Errorf("%s: not a valid URL", EnvDatabaseURL)
		}

		if u.Scheme != "postgres" && u.Scheme != "postgresql" {
			return fmt.Errorf("%s: scheme must be postgres or postgresql", EnvDatabaseURL)
		}

		return nil
	case strings.Contains(s, "="):
		// libpq key=value DSN; the driver checks the keywords.
		return nil
	default:
		return fmt.Errorf("%s: want a postgres:// URL or a key=value DSN", EnvDatabaseURL)
	}
}

func validateHTTPAddr(s string) error {
	_, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("%s: %q is not host:port", EnvHTTPAddr, s)
	}

	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("%s: %q needs a numeric port (0-65535)", EnvHTTPAddr, s)
	}

	return nil
}

// normalizeBaseURL accepts an absolute http(s) origin and strips a trailing
// slash. Paths are rejected because the app is served from the root.
func normalizeBaseURL(s string) (string, error) {
	if s == "" {
		return "", nil
	}

	u, err := url.Parse(s)

	switch {
	case err != nil:
		return "", fmt.Errorf("%s: not a valid URL", EnvBaseURL)
	case u.Scheme != "http" && u.Scheme != "https":
		return "", fmt.Errorf("%s: must be an absolute http or https URL", EnvBaseURL)
	case u.Host == "":
		return "", fmt.Errorf("%s: has no host", EnvBaseURL)
	case u.User != nil:
		return "", fmt.Errorf("%s: must not contain credentials", EnvBaseURL)
	case u.Path != "" && u.Path != "/", u.RawQuery != "", u.ForceQuery, u.Fragment != "":
		return "", fmt.Errorf("%s: must be an origin without path, query or fragment", EnvBaseURL)
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

	return c
}

// String implements fmt.Stringer with secrets masked.
func (c Config) String() string {
	r := c.Redacted()

	return fmt.Sprintf("{DatabaseURL:%s HTTPAddr:%s BaseURL:%s SMTP:%s}", r.DatabaseURL, r.HTTPAddr, r.BaseURL, r.SMTP)
}

// GoString implements fmt.GoStringer (%#v) with secrets masked.
func (c Config) GoString() string {
	r := c.Redacted()

	return fmt.Sprintf("config.Config{DatabaseURL:%q, HTTPAddr:%q, BaseURL:%q, SMTP:%s}",
		r.DatabaseURL, r.HTTPAddr, r.BaseURL, r.SMTP)
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
