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
	"net/netip"
	"runtime/debug"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
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

	// AppURL is the absolute URL of the web app that links in mails point
	// to, ending in "/" (or "#/" with hash routing). Registration fails
	// without it.
	AppURL string
	// Hasher hashes passwords; defaults to argon2id with the parameters of
	// the build target (password.New).
	Hasher service.PasswordHasher
	// Mailer delivers the mails of the outbox. Without one, mails stay in
	// the outbox and the outbox job does not run.
	Mailer mail.Mailer
	// Demo marks an installation that serves the fictional demo data (the
	// web demo, or `serve` without a database). It makes the outbox public
	// as a mailbox and lists DemoLogins, so visitors can sign in.
	Demo bool
	// DemoLogins are the seeded accounts listed when Demo is set.
	DemoLogins []seed.Login
	// InsecureCookies leaves the Secure attribute off the session cookie,
	// for local development over plain HTTP.
	InsecureCookies bool
	// HSTS sends Strict-Transport-Security on every response, telling
	// browsers to use only HTTPS for a year. Set it only when the app is
	// served over HTTPS (BASE_URL is https://).
	HSTS bool
	// TrustedProxies are the reverse proxies whose X-Forwarded-For entries
	// name the client, for the rate limits; empty: the connection's address.
	TrustedProxies []netip.Prefix
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

	if d.Hasher == nil {
		d.Hasher = password.New()
	}

	return d
}

// New builds the application handler. The returned huma.API gives access to
// the OpenAPI document and allows tests to register extra operations.
//
// Middleware order (outermost first): security headers, panic recovery,
// access log, cross-origin protection for unsafe methods, router. The
// headers come first so that every response carries them, including the
// 500 of a recovered panic.
func New(deps Deps) (http.Handler, huma.API) {
	deps = deps.withDefaults()

	router := chi.NewRouter()
	cfg := httpapi.NewConfig()
	cfg.Transformers = append(cfg.Transformers, maskServerErrors(deps.Logger))
	api := humachi.New(router, cfg)

	httpapi.Register(api, httpapi.Deps{
		Settings:       service.NewSettingsService(deps.Store),
		Catalog:        service.NewCatalogService(deps.Store, deps.IDs),
		Timetable:      service.NewTimetableService(deps.Store),
		Schedule:       service.NewScheduleService(deps.Store, deps.Clock),
		Auth:           newAuthService(deps),
		Family:         service.NewFamilyService(deps.Store, deps.Clock, deps.IDs),
		Outbox:         service.NewOutboxService(deps.Store, deps.Clock, deps.Mailer),
		Version:        deps.Version,
		Logger:         deps.Logger,
		SecureCookies:  !deps.InsecureCookies,
		Demo:           deps.Demo,
		DemoLogins:     demoLogins(deps.DemoLogins),
		TrustedProxies: deps.TrustedProxies,
		RateLimits:     httpapi.DefaultRateLimits(deps.Clock),
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
	h = securityHeaders(h, deps.HSTS)

	return h, api
}

// contentSecurityPolicy allows only the app's own scripts, styles, images and
// API calls (the WASM demo is served by GitHub Pages, without these headers).
// Scripts must stay without 'unsafe-inline' and 'unsafe-eval': the theme
// script is a file (web/public/theme.js) for that reason.
//
// Styles allow 'unsafe-inline': sonner injects its toast stylesheet as a
// <style> element, and Radix's scroll lock (react-remove-scroll) injects one
// with the computed scrollbar gap whenever a dialog, menu or select opens.
// The latter's content varies, so hashes cannot replace it, and neither
// library takes a nonce from us. Inline styles cannot run code; React
// escapes all rendered text, so they add little to an injection.
//
// The API docs page (BasePath/docs) loads its renderer from a CDN; Huma sets
// a policy of its own for it, which replaces this one there.
const contentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' data:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"object-src 'none'"

// securityHeaders sets the browser security headers on every response:
// the CSP, no MIME sniffing, no framing (X-Frame-Options for browsers
// without frame-ancestors), no URL in the Referer of cross-origin requests
// (reset and confirmation tokens are in URLs), and HSTS when hsts is set.
func securityHeaders(next http.Handler, hsts bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")

		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}

		next.ServeHTTP(w, r)
	})
}

// serverErrorDetail is all a 5xx response says; the cause is only logged.
const serverErrorDetail = "internal server error"

// maskServerErrors is a Huma transformer that keeps the details of server
// errors out of responses. The handlers map known errors themselves
// (httpapi.toHTTPError); an error that reaches Huma unmapped becomes a 500
// whose problem document would carry the error text. That text is logged
// here and replaced by serverErrorDetail.
func maskServerErrors(logger *slog.Logger) huma.Transformer {
	return func(ctx huma.Context, _ string, v any) (any, error) {
		problem, ok := v.(*huma.ErrorModel)
		if !ok || problem.Status < http.StatusInternalServerError {
			return v, nil
		}

		if problem.Detail == serverErrorDetail && len(problem.Errors) == 0 {
			return v, nil // already generic, and logged where it was mapped
		}

		causes := make([]string, 0, len(problem.Errors))
		for _, e := range problem.Errors {
			if e != nil {
				causes = append(causes, e.Message)
			}
		}

		var operation string
		if op := ctx.Operation(); op != nil {
			operation = op.OperationID
		}

		logger.ErrorContext(ctx.Context(), "unmapped server error",
			slog.String("operation", operation),
			slog.Int("status", problem.Status),
			slog.String("detail", problem.Detail),
			slog.Any("causes", causes),
		)

		return &huma.ErrorModel{
			Title:  http.StatusText(problem.Status),
			Status: problem.Status,
			Detail: serverErrorDetail,
		}, nil
	}
}

func demoLogins(logins []seed.Login) []httpapi.DemoLogin {
	out := make([]httpapi.DemoLogin, len(logins))
	for i, l := range logins {
		out[i] = httpapi.DemoLogin{
			Email: l.Email, Password: l.Password, Role: string(l.Role), GivenName: l.GivenName, FamilyName: l.FamilyName,
		}
	}

	return out
}

// newAuthService builds the AuthService of deps. The mail templates are
// embedded, so failing to parse them is a bug.
func newAuthService(deps Deps) *service.AuthService {
	renderer, err := mail.NewRenderer()
	if err != nil {
		panic("app: mail templates: " + err.Error())
	}

	return service.NewAuthService(deps.Store, deps.Clock, deps.IDs, deps.Hasher, renderer, deps.AppURL)
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
			writeProblem(w, http.StatusInternalServerError, serverErrorDetail)
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
