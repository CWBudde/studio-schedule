package httpapi

import (
	"context"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/clientip"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/ratelimit"
)

// RateLimits limit guessing passwords and sending mails (ADR 0006). A nil
// limiter imposes no limit.
type RateLimits struct {
	// PasswordFailures counts wrong passwords per email address: at login
	// and when changing the password or email.
	PasswordFailures *ratelimit.Limiter
	// Logins counts login attempts per client IP.
	Logins *ratelimit.Limiter
	// Mails counts the requests that send mails, per client IP: register,
	// password reset and email change.
	Mails *ratelimit.Limiter
}

// DefaultRateLimits returns the limits of ADR 0006.
func DefaultRateLimits(clk clock.Clock) RateLimits {
	return RateLimits{
		PasswordFailures: ratelimit.New(clk, 5, 15*time.Minute),
		Logins:           ratelimit.New(clk, 20, 15*time.Minute),
		Mails:            ratelimit.New(clk, 5, time.Hour),
	}
}

// clientIPKey is the context key of the request's client IP.
type clientIPKey struct{}

// clientIPMiddleware puts the client IP of the request into the context:
// the connection's address, or the X-Forwarded-For entry written by the
// trusted proxies (clientip.From).
func (h *handlers) clientIPMiddleware(ctx huma.Context, next func(huma.Context)) {
	var header http.Header

	if len(h.deps.TrustedProxies) == 0 {
		if ctx.Header("X-Forwarded-For") != "" {
			h.untrustedProxy.Do(func() {
				h.deps.Logger.WarnContext(ctx.Context(), "request forwarded by a proxy that is not in "+
					"TRUSTED_PROXIES: rate limits treat all its clients as one")
			})
		}
	} else {
		header = http.Header{}

		ctx.EachHeader(func(name, value string) {
			if strings.EqualFold(name, "X-Forwarded-For") {
				header.Add(name, value)
			}
		})
	}

	ip := clientip.FromAddr(ctx.RemoteAddr(), header, h.deps.TrustedProxies)
	next(huma.WithValue(ctx, clientIPKey{}, ip))
}

// clientIP returns the client IP of the request as a limiter key.
func clientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(netip.Addr)

	return ip.String() // "invalid IP" for an unknown one: they share a key
}

// emailKey is the limiter key of an email address.
func emailKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// allow counts one event of key in l and returns a 429 when l's limit is
// used up.
func allow(l *ratelimit.Limiter, key string) error {
	if l == nil {
		return nil
	}

	if ok, retry := l.Allow(key); !ok {
		return tooManyRequests(retry)
	}

	return nil
}

// notBlocked returns a 429 when key has used up its limit in l, without
// counting.
func notBlocked(l *ratelimit.Limiter, key string) error {
	if l == nil {
		return nil
	}

	if blocked, retry := l.Blocked(key); blocked {
		return tooManyRequests(retry)
	}

	return nil
}

// record counts one event of key in l, e.g. a wrong password.
func record(l *ratelimit.Limiter, key string) {
	if l != nil {
		l.Add(key)
	}
}

// forget forgets the events of key in l.
func forget(l *ratelimit.Limiter, key string) {
	if l != nil {
		l.Reset(key)
	}
}

func tooManyRequests(retry time.Duration) error {
	secs := strconv.Itoa(max(1, int((retry+time.Second-1)/time.Second)))

	return huma.ErrorWithHeaders(
		huma.Error429TooManyRequests("too many attempts; try again in "+secs+" seconds"),
		http.Header{"Retry-After": []string{secs}},
	)
}
