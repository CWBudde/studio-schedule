package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/postgres"
)

const controlPath = "/api/test"

func checkTestControl(cfg config.Config) error {
	if !cfg.TestControl {
		return nil
	}

	host, _, _ := net.SplitHostPort(cfg.HTTPAddr) // validated by config.Load
	ip, _ := netip.ParseAddr(host)

	switch {
	case cfg.BaseURL == "" || isHTTPS(cfg.BaseURL):
		return fmt.Errorf("%s needs an explicit http:// %s", config.EnvTestControl, config.EnvBaseURL)
	case cfg.DatabaseURL == "":
		return fmt.Errorf("%s needs a %s for an isolated fixture schema", config.EnvTestControl, config.EnvDatabaseURL)
	case host != "localhost" && !ip.IsLoopback():
		return fmt.Errorf("%s needs a loopback %s", config.EnvTestControl, config.EnvHTTPAddr)
	case len(cfg.TestControlToken) < 32:
		return fmt.Errorf("%s needs a %s of at least 32 characters", config.EnvTestControl, config.EnvTestControlToken)
	default:
		return nil
	}
}

func openServeStore(
	ctx context.Context, cfg config.Config, logger *slog.Logger,
) (store.Store, func(), func(context.Context) error, error) {
	if !cfg.TestControl {
		st, closeStore, err := openStore(ctx, cfg, logger)

		return st, closeStore, nil, err
	}

	fixture, err := postgres.OpenFixture(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, err
	}

	closeStore := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		if err := fixture.Close(cleanup); err != nil {
			logger.Warn("cleaning up conformance schema", slog.Any("err", err))
		}
	}

	return fixture.Store, closeStore, fixture.Reset, nil
}

// testControl wraps the real app in a fixture server. It serializes control
// mutations with app requests. Reset also replaces process-local state such
// as the rate limits. The test clock remains set until changed explicitly.
type testControl struct {
	deps     app.Deps
	token    string
	prefix   string
	reset    func(context.Context) error
	outbox   *service.OutboxService
	now      atomic.Pointer[time.Time]
	mu       sync.RWMutex
	handler  http.Handler
	controls *http.ServeMux
}

func newTestControl(deps app.Deps, token string, reset func(context.Context) error) *testControl {
	c := &testControl{
		deps: deps, token: token, prefix: deps.BasePath + controlPath, reset: reset,
		controls: http.NewServeMux(),
	}
	now := deps.Clock.Now()
	c.now.Store(&now)
	c.deps.Clock = c
	c.handler, _ = app.New(c.deps)
	c.outbox = service.NewOutboxService(deps.Store, c, nil, nil)
	c.controls.HandleFunc("PUT "+c.prefix+"/clock", c.clockSet)
	c.controls.HandleFunc("POST "+c.prefix+"/reset", c.databaseReset)
	c.controls.HandleFunc("POST "+c.prefix+"/seed", c.loadSeed)
	c.controls.HandleFunc("GET "+c.prefix+"/outbox", c.readOutbox)

	return c
}

func (c *testControl) Now() time.Time { return *c.now.Load() }

func (c *testControl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != c.prefix && !strings.HasPrefix(r.URL.Path, c.prefix+"/") {
		c.mu.RLock()
		defer c.mu.RUnlock()

		if c.handler == nil {
			controlError(w, http.StatusServiceUnavailable)
		} else {
			c.handler.ServeHTTP(w, r)
		}

		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(c.token)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		controlError(w, http.StatusUnauthorized)

		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.handler == nil && r.URL.Path != c.prefix+"/reset" && r.URL.Path != c.prefix+"/clock" {
		controlError(w, http.StatusServiceUnavailable)

		return
	}

	c.controls.ServeHTTP(w, r)
}

func (c *testControl) clockSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Now time.Time `json:"now"`
	}

	if !decodeControl(w, r, &in) || in.Now.IsZero() {
		controlError(w, http.StatusBadRequest)

		return
	}

	now := in.Now.UTC()
	c.now.Store(&now)
	w.WriteHeader(http.StatusNoContent)
}

func (c *testControl) databaseReset(w http.ResponseWriter, r *http.Request) {
	// A failed migration can leave a partial fixture schema. Keep the app
	// unavailable until a subsequent reset succeeds.
	c.handler = nil
	if err := c.reset(r.Context()); err != nil {
		c.fail(w, r, "reset conformance database", err)

		return
	}

	c.handler, _ = app.New(c.deps)
	w.WriteHeader(http.StatusNoContent)
}

func (c *testControl) loadSeed(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Anchor string      `json:"anchor"`
		School seed.School `json:"school"`
	}

	if !decodeControl(w, r, &in) {
		controlError(w, http.StatusBadRequest)

		return
	}

	anchor, err := domain.ParseDate(in.Anchor)
	if err != nil || !in.School.Valid() {
		controlError(w, http.StatusBadRequest)

		return
	}

	switch err := seed.LoadDemoSchoolAt(r.Context(), c.deps.Store, anchor, in.School); {
	case errors.Is(err, domain.ErrConflict):
		controlError(w, http.StatusConflict)
	case err != nil:
		c.fail(w, r, "seed conformance database", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type fixtureMail struct {
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Text      string    `json:"text"`
	HTML      string    `json:"html,omitempty"`
	Template  string    `json:"template"`
	CreatedAt time.Time `json:"createdAt"`
}

func (c *testControl) readOutbox(w http.ResponseWriter, r *http.Request) {
	const outboxLimit = 1000
	mails, err := c.outbox.List(r.Context(), outboxLimit)
	if err != nil {
		c.fail(w, r, "read conformance outbox", err)

		return
	}

	out := struct {
		Mails []fixtureMail `json:"mails"`
	}{Mails: make([]fixtureMail, len(mails))}

	for i, entry := range mails {
		m := entry.Mail
		out.Mails[i] = fixtureMail{
			To: m.To, Subject: m.Subject, Text: m.Text, HTML: m.HTML,
			Template: m.Template, CreatedAt: m.CreatedAt.UTC(),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (c *testControl) fail(w http.ResponseWriter, r *http.Request, action string, err error) {
	c.deps.Logger.ErrorContext(r.Context(), action, slog.Any("err", err))
	controlError(w, http.StatusInternalServerError)
}

func decodeControl(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()

	return dec.Decode(dst) == nil && errors.Is(dec.Decode(new(any)), io.EOF)
}

func controlError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "title": http.StatusText(status)})
}
