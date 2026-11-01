// Package app wires stores, services and the HTTP API into a single
// http.Handler. It is the one entry point shared by the server binary
// (cmd/studio-schedule) and the WebAssembly build (cmd/wasm), so both serve
// exactly the same API.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/requestid"
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
	// Pusher delivers web push notifications (ADR 0017): internal/webpush
	// with the VAPID keys on the server, the worker in the web demo
	// (internal/wasmbridge). Without one push is off: nothing is queued,
	// the API reports it off and the deliver-push job does not run.
	Pusher service.PushSender
	// PDF renders receipt PDFs (ADR 0012): the typst module run by wazero
	// on the server (internal/pdf/typstwasm), by the browser in the web demo
	// (internal/wasmbridge). Without one, receipts and so pass sales fail.
	PDF service.ReceiptRenderer
	// Demo marks an installation that serves the fictional demo data (the
	// web demo, or `serve` without a database). It makes the outbox public
	// as a mailbox and lists DemoLogins, so visitors can sign in.
	Demo bool
	// DemoLogins are the seeded accounts listed when Demo is set.
	DemoLogins []seed.Login
	// InsecureCookies leaves the Secure attribute off the session cookie,
	// for local development over plain HTTP.
	InsecureCookies bool
	// HostCookie names the session cookie __Host-studio_session instead of
	// studio_session (ADR 0004, amendment). Set it when the app is served
	// over HTTPS (BASE_URL is https://); the prefix needs the Secure
	// attribute, so InsecureCookies turns it off.
	HostCookie bool
	// HSTS sends Strict-Transport-Security on every response, telling
	// browsers to use only HTTPS for a year. Set it only when the app is
	// served over HTTPS (BASE_URL is https://).
	HSTS bool
	// HSTSIncludeSubdomains extends HSTS to every subdomain of the app's
	// host (includeSubDomains). The server sets it from
	// HSTS_INCLUDE_SUBDOMAINS, on by default; it is off where other
	// subdomains of the host are still served over plain HTTP.
	HSTSIncludeSubdomains bool
	// TrustedProxies are the reverse proxies whose X-Forwarded-For entries
	// name the client, for the rate limits; empty: the connection's address.
	TrustedProxies []netip.Prefix
	// Wake connects the handler to the background jobs (Jobs): a request
	// that leaves work for a job now, such as an announcement to email,
	// wakes it instead of waiting for its next tick. Pass the same Waker to
	// New and Jobs; without one, jobs run on their ticks only.
	Wake *jobs.Waker

	// RequestIDs generates the IDs of requests that come without a trusted
	// X-Request-Id; defaults to ids.V7. It is separate from IDs so that
	// requests do not shift the entity IDs of a deterministic source.
	RequestIDs ids.Source
	// Ready reports whether the app can serve requests, for /readyz: the
	// server checks its database and schema. Nil means always ready (the
	// memory store).
	Ready func(ctx context.Context) error
	// Metrics receives the measurements of requests and jobs; defaults to
	// none. The server passes its Prometheus collectors when METRICS_ADDR
	// is set.
	Metrics Metrics
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

	if d.RequestIDs == nil {
		d.RequestIDs = ids.V7{}
	}

	if d.Metrics == nil {
		d.Metrics = noMetrics{}
	}

	return d
}

// New builds the application handler. The returned huma.API gives access to
// the OpenAPI document and allows tests to register extra operations.
//
// Middleware order (outermost first): security headers, request ID, access
// log and metrics, panic recovery, cross-origin protection for unsafe
// methods, router. The headers come first so that every response carries
// them, including the 500 of a recovered panic; the request ID comes next
// so that the recovery and access log lines and every problem document
// name it; the access log wraps the recovery so that a panic's 500 is
// logged and counted. Paths in logs and error details have their secret
// parameters redacted (redact.go). Besides the API the router serves /healthz and /readyz (see
// health.go); everything else falls through to Deps.UI.
func New(deps Deps) (http.Handler, huma.API) {
	deps = deps.withDefaults()
	// Every line logged with a request's context names the request.
	deps.Logger = slog.New(requestid.NewLogHandler(deps.Logger.Handler()))

	router := chi.NewRouter()
	// Before any route: the access log reads the matched route's pattern.
	router.Use(recordRoute)
	registerHealth(router, deps)

	cfg := httpapi.NewConfig()
	cfg.Transformers = append(cfg.Transformers, maskServerErrors(deps.Logger))
	api := humachi.New(router, cfg)

	svc := newServices(deps, newRenderer())

	httpapi.Register(api, httpapi.Deps{
		Settings:       svc.Settings,
		Catalog:        svc.Catalog,
		Timetable:      svc.Timetable,
		Schedule:       svc.Schedule,
		Lessons:        svc.Lessons,
		Bookings:       svc.Bookings,
		Waitlist:       svc.Waitlist,
		Participants:   svc.Participants,
		Teach:          svc.Teach,
		MyLessons:      svc.MyLessons,
		Feeds:          svc.Feeds,
		Passes:         svc.Passes,
		Receipts:       svc.Receipts,
		Auth:           svc.Auth,
		Family:         svc.Family,
		Persons:        svc.Persons,
		Outbox:         svc.Outbox,
		Statistics:     svc.Statistics,
		Version:        deps.Version,
		Logger:         deps.Logger,
		SecureCookies:  !deps.InsecureCookies,
		HostCookie:     deps.HostCookie,
		Demo:           deps.Demo,
		DemoLogins:     demoLogins(deps.DemoLogins),
		TrustedProxies: deps.TrustedProxies,
		RateLimits:     httpapi.DefaultRateLimits(deps.Clock),
		Announcements:  svc.Announcements,
		MailLog:        svc.MailLog,
		Imports:        svc.Imports,
		Push:           svc.Push,
	})

	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusMethodNotAllowed, "method not allowed")
	})
	paths := newPathRedactor(router)

	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if deps.UI == nil || isAPIPath(r.URL.Path) {
			writeProblem(w, r, http.StatusNotFound, "no route for "+paths.redact(r.URL.Path))

			return
		}

		deps.UI.ServeHTTP(w, r)
	})

	var h http.Handler = router

	h = crossOriginProtection(h)
	h = recoverer(h, deps.Logger, paths)
	h = accessLog(h, deps.Logger, deps.Clock, deps.Metrics, paths)
	h = requestIDs(h, deps.RequestIDs, deps.TrustedProxies)
	h = securityHeaders(h, hstsHeader(deps.HSTS, deps.HSTSIncludeSubdomains))

	return h, api
}

// Services are the application's services. New builds them for the HTTP
// API and the command line gets them from NewServices, so a command and the
// HTTP operation it mirrors share their rules, validation and audit (ADR
// 0020).
type Services struct {
	Settings     *service.SettingsService
	Catalog      *service.CatalogService
	Timetable    *service.TimetableService
	Schedule     *service.ScheduleService
	Lessons      *service.LessonService
	Bookings     *service.BookingService
	Waitlist     *service.WaitlistService
	Participants *service.ParticipantService
	Teach        *service.TeachService
	MyLessons    *service.MyLessonsService
	Feeds        *service.CalendarFeedService
	Passes       *service.PassService
	Receipts     *service.ReceiptService
	Auth         *service.AuthService
	Family       *service.FamilyService
	Persons      *service.PersonService
	Outbox       *service.OutboxService
	Statistics   *service.StatisticsService
	// Generator creates the lessons of the weekly classes: the
	// generate-lessons job's, and `lessons generate` on the command line.
	Generator *service.LessonGenerator
	// Announcements wake the announcements job through Deps.Wake when one
	// is due now; without a Waker they wait for the job's next tick.
	Announcements *service.AnnouncementService
	MailLog       *service.MailLogService
	Imports       *service.ImportService
	Push          *service.PushService
	// Integrity checks the data for inconsistencies the database cannot
	// rule out: `db check` on the command line.
	Integrity *service.IntegrityService
}

// NewServices builds the services of deps exactly as New does. It panics
// without a store.
func NewServices(deps Deps) Services {
	return newServices(deps.withDefaults(), newRenderer())
}

// newServices builds the services of deps, which has its defaults, with
// the mail templates of renderer.
func newServices(deps Deps, renderer *mail.Renderer) Services {
	mails := mailsOf(deps, renderer)
	receipts := service.NewReceiptService(deps.Store, deps.Clock, deps.IDs, mails, deps.PDF)

	return Services{
		Settings:     service.NewSettingsService(deps.Store, deps.Clock, deps.IDs),
		Catalog:      service.NewCatalogService(deps.Store, deps.IDs),
		Timetable:    service.NewTimetableService(deps.Store, deps.Clock, deps.IDs, mails),
		Schedule:     service.NewScheduleService(deps.Store, deps.Clock),
		Lessons:      service.NewLessonService(deps.Store, deps.Clock, deps.IDs, mails),
		Bookings:     service.NewBookingService(deps.Store, deps.Clock, deps.IDs, mails),
		Waitlist:     service.NewWaitlistService(deps.Store, deps.Clock, deps.IDs, mails),
		Participants: service.NewParticipantService(deps.Store, deps.Clock, deps.IDs, mails),
		Teach:        service.NewTeachService(deps.Store, deps.Clock, deps.IDs, renderer),
		MyLessons:    service.NewMyLessonsService(deps.Store, deps.Clock),
		// A demo installation's feeds have no URL: calendars cannot reach the
		// web demo, and the demo server is for trying the app out.
		Feeds:      service.NewCalendarFeedService(deps.Store, deps.Clock, deps.IDs, deps.AppURL, !deps.Demo),
		Passes:     service.NewPassService(deps.Store, deps.Clock, deps.IDs, receipts),
		Receipts:   receipts,
		Auth:       newAuthService(deps, renderer),
		Family:     service.NewFamilyService(deps.Store, deps.Clock, deps.IDs, mails),
		Persons:    service.NewPersonService(deps.Store, deps.Clock, deps.IDs, mails),
		Outbox:     service.NewOutboxService(deps.Store, deps.Clock, deps.Mailer, receipts),
		Statistics: service.NewStatisticsService(deps.Store, deps.Clock),
		Generator:  newLessonGenerator(deps),
		Announcements: service.NewAnnouncementService(deps.Store, deps.Clock, deps.IDs, renderer,
			func() { deps.Wake.Wake(announcementsJob) }),
		MailLog:   service.NewMailLogService(deps.Store, deps.Clock, deps.IDs),
		Imports:   service.NewImportService(deps.Store, deps.Clock, deps.IDs),
		Push:      service.NewPushService(deps.Store, deps.Clock, deps.IDs, deps.Pusher),
		Integrity: service.NewIntegrityService(deps.Store),
	}
}

// newLessonGenerator is the LessonGenerator of deps: Services.Generator and
// the generate-lessons job's.
func newLessonGenerator(deps Deps) *service.LessonGenerator {
	return service.NewLessonGenerator(deps.Store, deps.Clock, deps.IDs)
}

// contentSecurityPolicy allows only the app's own scripts, styles, images and
// API calls (the WASM demo is served by GitHub Pages, without these headers).
// The service worker and the manifest (ADR 0017) are named explicitly: both
// come from the app's origin only.
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
	"worker-src 'self'; " +
	"manifest-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"object-src 'none'"

// securityHeaders sets the browser security headers on every response:
// the CSP, no MIME sniffing, no framing (X-Frame-Options for browsers
// without frame-ancestors), no URL in the Referer of cross-origin requests
// (reset and confirmation tokens are in URLs), Strict-Transport-Security
// when hsts is not empty, and no caching on the API's answers.
func securityHeaders(next http.Handler, hsts string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")

		if hsts != "" {
			h.Set("Strict-Transport-Security", hsts)
		}

		if isAPIAnswer(r.URL.Path) {
			// Personal data must not stay in a browser's or proxy's cache, and
			// a JSON answer opened as a page is a file to save, never content
			// to render (reflected file download). Operations that serve a
			// file (receipt PDFs, the logos) set their own; the calendar
			// feeds are not API answers.
			h.Set("Cache-Control", "no-store")
			h.Set("Content-Disposition", `attachment; filename="api.json"`)
		}

		next.ServeHTTP(w, r)
	})
}

// hstsHeader is the Strict-Transport-Security value: use only HTTPS for a
// year, for the app's host and, with subdomains, every subdomain of it;
// empty without hsts.
func hstsHeader(hsts, subdomains bool) string {
	switch {
	case !hsts:
		return ""
	case subdomains:
		return "max-age=31536000; includeSubDomains"
	default:
		return "max-age=31536000"
	}
}

// isAPIAnswer reports whether p is a path of the API's answers: the API
// namespace (isAPIPath) without the calendar feeds, which calendars fetch
// as files, and without the API's documentation (the OpenAPI document and
// the docs page), which is meant to be read in a browser.
func isAPIAnswer(p string) bool {
	return isAPIPath(p) && !strings.HasPrefix(p, httpapi.FeedPathPrefix) &&
		p != httpapi.BasePath+"/docs" && !strings.HasPrefix(p, httpapi.BasePath+"/openapi")
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
		problem, ok := v.(*httpapi.ErrorModel)
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

		// The request ID stays: it finds the line logged above.
		masked := httpapi.NewProblem(problem.Status, serverErrorDetail)
		masked.RequestID = problem.RequestID

		return masked, nil
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

// mailsOf is how the services of deps mail: rendered by renderer, with
// links into the app, and with web push notifications if deps has a
// Pusher, which a queued notification wakes.
func mailsOf(deps Deps, renderer *mail.Renderer) service.Mails {
	m := service.Mails{Renderer: renderer, AppURL: deps.AppURL}
	if deps.Pusher != nil {
		m.Push = service.PushQueue{On: true, Wake: func() { deps.Wake.Wake(deliverPushJob) }}
	}

	return m
}

// newAuthService builds the AuthService of deps. The mail templates are
// embedded, so failing to parse them is a bug.
// newRenderer parses the mail templates; they are embedded, so an error is
// a bug of the build.
func newRenderer() *mail.Renderer {
	renderer, err := mail.NewRenderer()
	if err != nil {
		panic("app: mail templates: " + err.Error())
	}

	return renderer
}

func newAuthService(deps Deps, renderer *mail.Renderer) *service.AuthService {
	return service.NewAuthService(deps.Store, deps.Clock, deps.IDs, deps.Hasher, renderer, deps.AppURL)
}

// isAPIPath reports whether p belongs to the API namespace, which never
// falls back to the UI: the API and the calendar feeds (a feed URL without
// ".ics", say, is a 404 problem rather than the app's page).
func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, httpapi.FeedPathPrefix)
}

// crossOriginProtection rejects cross-origin unsafe requests (POST, PUT,
// ...) based on Sec-Fetch-Site / Origin. Requests without either header,
// such as those from non-browser clients or the in-page WASM bridge, pass.
func crossOriginProtection(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusForbidden, "cross-origin request rejected")
	}))

	return cop.Handler(next)
}
