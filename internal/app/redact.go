package app

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

// redacted replaces a secret path segment in logs and error details.
const redacted = "<redacted>"

// routeParam matches a parameter of a chi route pattern: {name},
// {name:regexp} or a trailing *.
var routeParam = regexp.MustCompile(`\{[^}]*\}|\*$`)

// secretParam reports whether the path parameter name holds a secret, such
// as the token of a calendar feed (/ical/{token}.ics) or of a waitlist
// invitation (/api/v1/waitlist-invites/{token}): a token in the path grants
// access by itself, so it never reaches a log line or an error detail. Name
// such parameters "token" or "…Token".
func secretParam(name string) bool {
	return name == "token" || strings.HasSuffix(name, "Token")
}

// paramName returns the name of the routeParam match m.
func paramName(m string) string {
	if m == "*" {
		return "*"
	}

	name, _, _ := strings.Cut(strings.Trim(m, "{}"), ":")

	return name
}

// matchedPath is the path of a request that matched the route pattern,
// rebuilt from the pattern and the request's parameters with the secret
// ones redacted: /api/v1/waitlist-invites/<redacted>.
func matchedPath(pattern string, rctx *chi.Context) string {
	return routeParam.ReplaceAllStringFunc(pattern, func(m string) string {
		name := paramName(m)
		if secretParam(name) {
			return redacted
		}

		return rctx.URLParam(name)
	})
}

// pathRedactor redacts the secret segments of paths that match no route,
// such as a feed path without ".ics": everything from the first parameter
// of a route with a secret parameter on. It reads the router's routes when
// it is first used, after every route is registered.
type pathRedactor struct {
	secrets func() []secretRoute
}

// secretRoute is a route with a secret parameter: the static prefix of its
// pattern before the first parameter, and the static suffix of its last
// segment, if any (".ics").
type secretRoute struct {
	prefix, suffix string
}

func newPathRedactor(routes chi.Routes) *pathRedactor {
	return &pathRedactor{secrets: sync.OnceValue(func() []secretRoute {
		var out []secretRoute

		_ = chi.Walk(routes, func(_, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			params := routeParam.FindAllStringIndex(pattern, -1)
			if !slices.ContainsFunc(params, func(loc []int) bool { return secretParam(paramName(pattern[loc[0]:loc[1]])) }) {
				return nil
			}

			r := secretRoute{prefix: pattern[:params[0][0]]}
			if tail := pattern[params[len(params)-1][1]:]; !strings.Contains(tail, "/") {
				r.suffix = tail
			}

			if !slices.Contains(out, r) {
				out = append(out, r)
			}

			return nil
		})

		// The longest prefix first, and with a suffix before without one.
		slices.SortFunc(out, func(a, b secretRoute) int {
			if d := len(b.prefix) - len(a.prefix); d != 0 {
				return d
			}

			return len(b.suffix) - len(a.suffix)
		})

		return out
	})}
}

// redact returns p with the secret segments of the routes redacted.
func (p *pathRedactor) redact(path string) string {
	out := path

	for _, r := range p.secrets() {
		if !strings.HasPrefix(path, r.prefix) {
			continue
		}

		if rest := path[len(r.prefix):]; r.suffix != "" && strings.HasSuffix(rest, r.suffix) &&
			!strings.Contains(rest, "/") {
			return r.prefix + redacted + r.suffix
		}

		if out == path {
			out = r.prefix + redacted // the longest prefix, unless a suffix matches
		}
	}

	return out
}

// of returns the path of r for logs: rebuilt from its route with the
// secret parameters redacted, or redacted by redact if it matched none.
func (p *pathRedactor) of(r *http.Request) string {
	if info, _ := r.Context().Value(requestInfoKey{}).(*requestInfo); info != nil && info.path != "" {
		return info.path
	}

	return p.redact(r.URL.Path)
}
