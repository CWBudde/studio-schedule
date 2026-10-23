// Package app wires stores, services and the HTTP API into a single
// http.Handler. It is the one entry point shared by the server binary
// (cmd/studio-schedule) and the WebAssembly build (cmd/wasm), so both serve
// exactly the same API.
package app

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Deps are the external dependencies of the application.
type Deps struct {
	// Store is required.
	Store store.Store
	// Clock defaults to clock.System.
	Clock clock.Clock
	// IDs generates the IDs of new entities; defaults to ids.V7.
	IDs ids.Source
	// Version is the build version reported by the health endpoint;
	// defaults to "dev".
	Version string
	// Logger defaults to a logger that discards everything.
	Logger *slog.Logger
	// UI, if set, handles every request that matches no API route
	// (typically the embedded single-page app).
	UI http.Handler
}

// withDefaults fills in the optional dependencies. It panics without a
// store.
func (d Deps) withDefaults() Deps {
	if d.Store == nil {
		panic("app: Deps.Store is required")
	}

	if d.Clock == nil {
		d.Clock = clock.System{}
	}

	if d.IDs == nil {
		d.IDs = ids.V7{}
	}

	if d.Version == "" {
		d.Version = "dev"
	}

	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}

	return d
}

// New builds the application handler. The returned huma.API gives access to
// the OpenAPI document and allows tests to register extra operations.
//
// Middleware order (outermost first): panic recovery, access log,
// cross-origin protection for unsafe methods, router.
func New(deps Deps) (http.Handler, huma.API) {
	deps = deps.withDefaults()

	router := chi.NewRouter()
	api := humachi.New(router, httpapi.NewConfig())

	httpapi.Register(api, httpapi.Deps{
		Settings: service.NewSettingsService(deps.Store),
		Version:  deps.Version,
		Logger:   deps.Logger,
	})

	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if deps.UI == nil || isAPIPath(r.URL.Path) {
			writeProblem(w, http.StatusNotFound, "no route for "+r.URL.Path)

			return
		}

		deps.UI.ServeHTTP(w, r)
	})

	var h http.Handler = router

	h = crossOriginProtection(h)
	h = accessLog(h, deps.Logger, deps.Clock)
	h = recoverer(h, deps.Logger)

	return h, api
}

// isAPIPath reports whether p belongs to the API namespace, which never
// falls back to the UI.
func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

// crossOriginProtection rejects cross-origin unsafe requests (POST, PUT,
// ...) based on Sec-Fetch-Site / Origin. Requests without either header,
// such as those from non-browser clients or the in-page WASM bridge, pass.
func crossOriginProtection(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusForbidden, "cross-origin request rejected")
	}))

	return cop.Handler(next)
}

func recoverer(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec) // let net/http abort the connection silently
			}

			logger.ErrorContext(r.Context(), "panic in handler",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Any("panic", rec),
				slog.String("stack", string(debug.Stack())),
			)
			writeProblem(w, http.StatusInternalServerError, "internal server error")
		}()

		next.ServeHTTP(w, r)
	})
}

func accessLog(next http.Handler, logger *slog.Logger, clk clock.Clock) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := clk.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		logger.DebugContext(r.Context(), "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Duration("duration", clk.Now().Sub(start)),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	w.wroteHeader = true

	return w.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// writeProblem writes a minimal RFC 9457 problem document, matching the
// format Huma uses for its own errors.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(huma.ErrorModel{
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}
