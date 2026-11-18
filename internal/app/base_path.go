package app

import (
	"net/http"
	"net/url"
	"strings"
)

// underBasePath dispatches only requests below the configured mount point.
// The inner router sees its usual paths for matching, metrics and redaction.
func underBasePath(next http.Handler, prefix string) http.Handler {
	root := prefix + "/"
	inside := http.StripPrefix(prefix, next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, root):
			inside.ServeHTTP(w, r)
		case r.URL.Path == prefix:
			target := url.URL{Path: root, RawQuery: r.URL.RawQuery, ForceQuery: r.URL.ForceQuery}
			http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
		default:
			writeProblem(w, r, http.StatusNotFound, "no route outside the app's base path")
		}
	})
}
