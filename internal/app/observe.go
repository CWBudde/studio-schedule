package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cwbudde/studio-schedule/internal/clientip"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/requestid"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// Metrics receives the measurements of the application (Deps.Metrics). The
// server implements it with Prometheus collectors; the methods are called
// on the request's or job's goroutine and must be cheap.
type Metrics interface {
	// ObserveRequest records an answered HTTP request. route is the pattern
	// of the matched route, such as "/api/v1/lessons/{lessonId}", or
	// RouteOther for every request that matched none (the web app's pages
	// and files, unknown paths), so its values are bounded. method is one
	// of the standard methods or "OTHER".
	ObserveRequest(method, route string, status int, d time.Duration)
	// ObserveJob records a run of the background job name; err is what it
	// returned (a panic counts as an error).
	ObserveJob(name string, err error, d time.Duration)
}

// RouteOther is the route of the requests that match no route.
const RouteOther = "other"

// UnsentMails returns a function that counts the unsent mails of the
// outbox in deps.Store, for a metrics collector that reads it when it is
// scraped rather than on every request.
func UnsentMails(deps Deps) func(ctx context.Context) (service.MailCounts, error) {
	deps = deps.withDefaults()

	return service.NewMailLogService(deps.Store, deps.Clock, deps.IDs).Unsent
}

type noMetrics struct{}

func (noMetrics) ObserveRequest(string, string, int, time.Duration) {}
func (noMetrics) ObserveJob(string, error, time.Duration)           {}

// requestIDs gives every request an ID (internal/requestid): the
// X-Request-Id of a trusted proxy (a direct peer in trusted) if it is a
// valid ID, otherwise a new one from src. The ID is in the request's context
// and in the X-Request-Id response header.
func requestIDs(next http.Handler, src ids.Source, trusted []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestid.Header)
		if !requestid.Valid(id) || !clientip.PeerTrusted(r, trusted) {
			id = src.NewUUID().String()
		}

		w.Header().Set(requestid.Header, id)
		next.ServeHTTP(w, r.WithContext(requestid.NewContext(r.Context(), id)))
	})
}

// recoverer answers a panicking request with a 500 problem and logs the
// panic. It runs inside the access log, so that the 500 is logged and
// counted like any other answer.
func recoverer(next http.Handler, logger *slog.Logger, paths *pathRedactor) http.Handler {
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
				slog.String("path", paths.of(r)),
				slog.Any("panic", rec),
				slog.String("stack", string(debug.Stack())),
			)
			writeProblem(w, r, http.StatusInternalServerError, serverErrorDetail)
		}()

		next.ServeHTTP(w, r)
	})
}

// requestInfo collects what the access log learns of a request inside the
// router (recordRoute): the pattern of the matched route and the request's
// path with its secret parameters redacted.
type requestInfo struct {
	route string
	path  string
}

type requestInfoKey struct{}

// recordRoute is a router middleware that notes the matched route in the
// request's requestInfo once the route has answered, also when its handler
// panics.
func recordRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			info, _ := r.Context().Value(requestInfoKey{}).(*requestInfo)

			rctx := chi.RouteContext(r.Context())
			if info == nil || rctx == nil {
				return
			}

			if route := rctx.RoutePattern(); route != "" && route != "/*" {
				info.route = route
				info.path = matchedPath(route, rctx)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// accessLog logs every request with its method, redacted path, status and
// duration, and reports it to metrics; see accessLevel for the level. The
// request ID comes from the context (requestid.LogHandler). A request whose
// handler panics past the recoverer (http.ErrAbortHandler) is logged too,
// as a 500 unless it had answered already.
func accessLog(
	next http.Handler, logger *slog.Logger, clk clock.Clock, metrics Metrics, paths *pathRedactor,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := clk.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		info := &requestInfo{}
		completed := false

		defer func() {
			status := sw.status
			if !completed && !sw.wroteHeader {
				status = http.StatusInternalServerError
			}

			d := clk.Now().Sub(start)

			route := info.route
			if route == "" {
				route = RouteOther
			}

			metrics.ObserveRequest(metricMethod(r.Method), route, status, d)

			path := info.path
			if path == "" {
				path = paths.redact(r.URL.Path)
			}

			logger.Log(r.Context(), accessLevel(r.URL.Path, info.route != "", status), "http request",
				slog.String("method", r.Method),
				slog.String("path", path),
				slog.Int("status", status),
				slog.Duration("duration", d),
			)
		}()

		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))

		completed = true
	})
}

// accessLevel is the level of a request's access log line: Info for the
// API (the routes and the API paths that match none) and for every server
// error; Debug for the health probes, which come every few seconds, and for
// the web app's pages and files, which come by the dozen per visit.
func accessLevel(path string, matched bool, status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelInfo
	case isProbe(path):
		return slog.LevelDebug
	case matched:
		return slog.LevelInfo
	case isAPIPath(path) && status >= http.StatusBadRequest:
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}

// metricMethod bounds the method label: the standard methods, else OTHER.
func metricMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions:
		return m
	default:
		return "OTHER"
	}
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
// format Huma uses for its own errors, with the request's ID.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	p := httpapi.NewProblem(status, detail)
	p.RequestID = requestid.From(r.Context())

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
