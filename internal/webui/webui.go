// Package webui embeds the compiled single-page app and serves it.
//
// The committed dist/index.html is a placeholder. `just build` copies the
// Vite output from web/dist over dist/ before compiling the server, so the
// release binary contains the real UI. Hashed build assets (dist/assets/) are
// git-ignored.
package webui

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded UI files, rooted at the dist directory.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // unreachable: "dist" is embedded at compile time
	}

	return sub
}

// Handler serves the embedded UI.
func Handler() http.Handler {
	return NewHandler(FS())
}

// NewHandler serves the single-page app in files:
//
//   - existing files are served as-is; files under assets/ are cached
//     immutably (their names contain content hashes);
//   - paths below /api/ and missing files under /assets/ get 404;
//   - every other GET/HEAD request gets index.html, so client-side routes
//     survive a reload.
func NewHandler(files fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

			return
		}

		urlPath := path.Clean("/" + r.URL.Path)
		if urlPath == "/api" || strings.HasPrefix(urlPath, "/api/") {
			http.NotFound(w, r)

			return
		}

		name := strings.TrimPrefix(urlPath, "/")
		if name != "" && name != "index.html" && isFile(files, name) {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}

			// name is cleaned above and fs.FS paths cannot leave files.
			http.ServeFileFS(w, r, files, name) //nolint:gosec // G703: see above

			return
		}

		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)

			return
		}

		serveIndex(w, r, files)
	})
}

func isFile(files fs.FS, name string) bool {
	info, err := fs.Stat(files, name)

	return err == nil && !info.IsDir()
}

func serveIndex(w http.ResponseWriter, r *http.Request, files fs.FS) {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, fs.ErrNotExist) {
			status = http.StatusNotFound
		}

		http.Error(w, http.StatusText(status), status)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodHead {
		_, _ = w.Write(index)
	}
}
