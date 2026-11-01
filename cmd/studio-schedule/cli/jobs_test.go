package cli

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// nopMailer accepts every mail.
type nopMailer struct{}

func (nopMailer) Send(context.Context, mail.Message) error { return nil }

// nopPusher accepts every push message.
type nopPusher struct{}

func (nopPusher) PublicKey() string { return "BPublicKeyOfTheTests" }

func (nopPusher) Send(context.Context, domain.PushSubscription, []byte, time.Duration) error {
	return nil
}

// senders is a DeliveryFunc with both senders.
func senders(config.Config, clock.Clock, *slog.Logger) (Delivery, error) {
	return Delivery{Mailer: nopMailer{}, Pusher: nopPusher{}}, nil
}

// TestEnvJobs builds the jobs as serve does: deliver-mail and deliver-push
// only with their senders. A job runs on the command's database, under
// its lock.
func TestEnvJobs(t *testing.T) {
	t.Parallel()

	var without, with record

	if out := (harness{st: demoStore(t)}).run(t, &without, "jobs", "purge-sessions"); out.code != ExitOK {
		t.Fatalf("jobs without senders: exit %d, stderr %q", out.code, out.stderr)
	}

	if out := (harness{st: demoStore(t), delivery: senders}).run(t, &with, "jobs"); out.code != ExitOK {
		t.Fatalf("jobs with senders: exit %d, stderr %q", out.code, out.stderr)
	}

	for _, name := range []string{"deliver-mail", "deliver-push"} {
		if slices.Contains(without.jobs, name) || !slices.Contains(with.jobs, name) {
			t.Errorf("%s: without senders %v, with %v; want it only with them", name, without.jobs, with.jobs)
		}
	}

	if !slices.Contains(without.jobs, "generate-lessons") {
		t.Errorf("jobs %v lack generate-lessons", without.jobs)
	}

	// An unknown job is not found.
	if out := (harness{st: demoStore(t)}).run(t, &record{}, "jobs", "tidy-up"); out.code != ExitNotFound {
		t.Errorf("unknown job: exit %d, stderr %q", out.code, out.stderr)
	}
}

// TestEnvJobsFailures: a command that opens no database gets no jobs, and
// senders that cannot be built fail the command.
func TestEnvJobsFailures(t *testing.T) {
	t.Parallel()

	out := (harness{st: demoStore(t)}).run(t, &record{}, "jobs-without-store")
	if out.code != ExitFailed || !strings.Contains(out.stderr, "jobs-without-store opens no database") {
		t.Errorf("without a store: exit %d, stderr %q", out.code, out.stderr)
	}

	broken := func(config.Config, clock.Clock, *slog.Logger) (Delivery, error) {
		return Delivery{}, errors.New("mail server: invalid smtp configuration")
	}

	out = (harness{st: demoStore(t), delivery: broken}).run(t, &record{}, "jobs")
	if out.code != ExitFailed || !strings.Contains(out.stderr, "invalid smtp configuration") {
		t.Errorf("broken senders: exit %d, stderr %q", out.code, out.stderr)
	}
}

// TestEnvDelivery builds the System's senders, and none without a
// DeliveryFunc.
func TestEnvDelivery(t *testing.T) {
	t.Parallel()

	var none, both record

	if out := (harness{}).run(t, &none, "delivery"); out.code != ExitOK || none.delivery != (Delivery{}) {
		t.Errorf("without a DeliveryFunc: exit %d, %+v", out.code, none.delivery)
	}

	if out := (harness{delivery: senders}).run(t, &both, "delivery"); out.code != ExitOK ||
		both.delivery.Mailer == nil || both.delivery.Pusher == nil {
		t.Errorf("with one: exit %d, %+v", out.code, both.delivery)
	}
}

// countingRenderer counts its renders.
type countingRenderer struct{ renders *int }

func (c countingRenderer) Render(context.Context, []byte) ([]byte, error) {
	*c.renders++

	return []byte("%PDF-1.7\n"), nil
}

// TestLazyPDF starts the renderer on the first render only, once, and
// releases only one that started, once; without a renderer a render fails.
func TestLazyPDF(t *testing.T) {
	t.Parallel()

	var starts, renders, releases int

	start := func() (service.ReceiptRenderer, func()) {
		starts++

		return countingRenderer{&renders}, func() { releases++ }
	}

	unused := &lazyPDF{start: start}
	unused.release()

	if starts != 0 || releases != 0 {
		t.Errorf("an unused renderer: %d starts, %d releases; want none", starts, releases)
	}

	used := &lazyPDF{start: start}
	for range 2 {
		if out, err := used.Render(t.Context(), nil); err != nil || string(out) != "%PDF-1.7\n" {
			t.Fatalf("Render = %q, %v", out, err)
		}
	}

	used.release()
	used.release() // the command's release after the jobs'

	if starts != 1 || renders != 2 || releases != 1 {
		t.Errorf("%d starts, %d renders, %d releases; want 1, 2, 1", starts, renders, releases)
	}

	if _, err := (&lazyPDF{}).Render(t.Context(), nil); !errors.Is(err, errNoRenderer) {
		t.Errorf("without a renderer: %v, want errNoRenderer", err)
	}

	var none *lazyPDF
	none.release() // a System without a renderer: nothing to stop
}
