package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cwbudde/studio-schedule/internal/clock"
)

// The health probes of orchestrators and load balancers. They are plain
// routes outside the API (not in the OpenAPI document), public and never
// cached, for GET and HEAD. Both targets serve them; the web demo never
// asks.
const (
	// healthzPath answers 200 while the process serves requests (liveness).
	healthzPath = "/healthz"
	// readyzPath answers 200 when the app can serve requests: Deps.Ready
	// passes (readiness). Otherwise it answers 503.
	readyzPath = "/readyz"
)

const (
	// readyTimeout bounds a readiness check, so a hanging database fails the
	// probe instead of piling up probes.
	readyTimeout = 3 * time.Second
	// readyCacheTTL is how long a readiness result answers the probes: the
	// probe is public, so a flood of it costs one check per period. A change
	// shows within readyCacheTTL (plus readyTimeout for a hanging database).
	readyCacheTTL = 2 * time.Second
)

func registerHealth(router chi.Router, deps Deps) {
	ready := &readiness{check: deps.Ready, clock: deps.Clock, logger: deps.Logger}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		router.MethodFunc(method, healthzPath, func(w http.ResponseWriter, _ *http.Request) {
			writeProbe(w)
		})

		router.MethodFunc(method, readyzPath, func(w http.ResponseWriter, r *http.Request) {
			if err := ready.get(r.Context()); err != nil {
				w.Header().Set("Cache-Control", "no-store")
				writeProblem(w, r, http.StatusServiceUnavailable, "not ready")

				return
			}

			writeProbe(w)
		})
	}
}

func writeProbe(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// isProbe reports whether p is the path of a health probe.
func isProbe(p string) bool {
	return p == healthzPath || p == readyzPath
}

// readiness runs the readiness check for /readyz. A result answers the
// probes for readyCacheTTL; concurrent probes without a fresh result share
// one check. It logs when the app turns unready (Warn, with the cause) and
// ready again (Info), not on every probe.
type readiness struct {
	check  func(ctx context.Context) error // nil: always ready
	clock  clock.Clock
	logger *slog.Logger

	mu        sync.Mutex
	err       error
	checkedAt time.Time
	checked   bool
	flight    *readyFlight // the running check, if any
	unready   bool         // the state last logged
}

// readyFlight is a running readiness check; done is closed when err is set.
type readyFlight struct {
	done chan struct{}
	err  error
}

// get returns the result of a check no older than readyCacheTTL, running
// one if there is none.
func (r *readiness) get(ctx context.Context) error {
	if r.check == nil {
		return nil
	}

	r.mu.Lock()

	if r.checked && r.clock.Now().Sub(r.checkedAt) < readyCacheTTL {
		err := r.err
		r.mu.Unlock()

		return err
	}

	if f := r.flight; f != nil {
		r.mu.Unlock()

		select {
		case <-f.done:
			return f.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	f := &readyFlight{done: make(chan struct{})}
	r.flight = f
	r.mu.Unlock()

	f.err = errReadyPanicked // unless the check returns
	defer r.finish(ctx, f)

	// The check outlives a probe that gives up: the others wait for it.
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readyTimeout)
	defer cancel()

	f.err = r.check(checkCtx)

	return f.err
}

// errReadyPanicked is the result of a readiness check that panicked.
var errReadyPanicked = errors.New("app: the readiness check panicked")

// finish stores the result of the check f, wakes the probes waiting for it
// and logs a change of the state.
func (r *readiness) finish(ctx context.Context, f *readyFlight) {
	r.mu.Lock()
	r.err, r.checkedAt, r.checked, r.flight = f.err, r.clock.Now(), true, nil
	changed := (f.err != nil) != r.unready
	r.unready = f.err != nil
	r.mu.Unlock()
	close(f.done)

	switch {
	case changed && f.err != nil:
		r.logger.WarnContext(ctx, "not ready", slog.Any("err", f.err))
	case changed:
		r.logger.InfoContext(ctx, "ready again")
	}
}
