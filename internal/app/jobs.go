package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// Jobs returns the background jobs of the application, built from the same
// Deps as New. The caller runs them once at startup (RunOnce) and then in
// the background (Run); see package jobs. Every run takes the job's lock
// in deps.Store, so two processes never run one job at once, and is
// recorded there (ADR 0020). The command line lists the jobs and their
// latest runs (Runner.Jobs, Runner.Statuses) and runs one on request
// (Runner.RunNow).
func Jobs(deps Deps) *jobs.Runner {
	deps = deps.withDefaults()
	lessons := service.NewLessonGenerator(deps.Store, deps.Clock, deps.IDs)
	renderer := newRenderer()
	auth := newAuthService(deps, renderer)
	waitlist := service.NewWaitlistService(deps.Store, deps.Clock, deps.IDs, mailsOf(deps, renderer))

	all := []jobs.Job{{
		Name:  "generate-lessons",
		Every: time.Hour,
		Run: func(ctx context.Context) error {
			created, err := lessons.GenerateAhead(ctx)

			switch {
			case errors.Is(err, service.ErrNoSchool):
				// No school settings yet: there is no school to generate for.
				// The server warns about this at startup.
				deps.Logger.DebugContext(ctx, "lesson generation skipped: no school settings")

				return nil
			case err != nil:
				return err
			}

			level := slog.LevelDebug
			if created > 0 {
				level = slog.LevelInfo
			}

			deps.Logger.Log(ctx, level, "lessons generated", slog.Int("created", created))

			return nil
		},
	}, {
		Name:  "purge-sessions",
		Every: time.Hour,
		Run: func(ctx context.Context) error {
			n, err := auth.PurgeExpired(ctx)
			if n > 0 {
				deps.Logger.InfoContext(ctx, "expired sessions and links deleted", slog.Int("deleted", n))
			}

			return err
		},
	}, {
		// Ends the waiting-list invites that ran out and the entries of
		// lessons that started, and invites the next (ADR 0010). It only
		// queues mails, so it runs without a mailer too (the demo).
		Name:  "waitlist-expiry",
		Every: time.Minute,
		Run: func(ctx context.Context) error {
			n, err := waitlist.ExpireDue(ctx)

			switch {
			case errors.Is(err, service.ErrNoSchool):
				return nil // entries but no settings: broken data, nothing to settle
			case err != nil:
				return err
			}

			if n > 0 {
				deps.Logger.InfoContext(ctx, "waiting lists settled", slog.Int("entries", n))
			}

			return nil
		},
	}}

	all = append(all, communicationJobs(deps, renderer)...)
	all = append(all, privacyJobs(deps)...)

	if deps.Mailer != nil {
		// Mails attach receipt PDFs, which a seeded receipt gets rendered
		// on first use; the receipts mail nothing themselves here.
		receipts := service.NewReceiptService(deps.Store, deps.Clock, deps.IDs, service.Mails{}, deps.PDF)
		outbox := service.NewOutboxService(deps.Store, deps.Clock, deps.Mailer, receipts)
		all = append(all, jobs.Job{
			Name:  "deliver-mail",
			Every: time.Minute,
			// A slow mail server must not delay startup.
			SkipAtStartup: true,
			Run: func(ctx context.Context) error {
				n, err := outbox.Deliver(ctx)
				if n > 0 {
					deps.Logger.InfoContext(ctx, "mails sent", slog.Int("sent", n))
				}

				return err
			},
		})
	}

	if deps.Pusher != nil {
		all = append(all, deliverPush(deps))
	}

	for i := range all {
		all[i].Run = measured(all[i], deps)
	}

	return jobs.NewRunner(jobs.Config{Logger: deps.Logger, Store: deps.Store, Clock: deps.Clock, IDs: deps.IDs}, all...)
}

// deliverPushJob is the name of the job that sends web push messages.
const deliverPushJob = "deliver-push"

// deliverPush sends the queued web push messages (ADR 0017), every minute
// and when a request queues one (Wake).
func deliverPush(deps Deps) jobs.Job {
	push := service.NewPushService(deps.Store, deps.Clock, deps.IDs, deps.Pusher)

	return jobs.Job{
		Name:  deliverPushJob,
		Every: time.Minute,
		Wake:  deps.Wake.Chan(deliverPushJob),
		// A slow push service must not delay startup.
		SkipAtStartup: true,
		Run: func(ctx context.Context) error {
			n, err := push.Deliver(ctx)
			if n > 0 {
				deps.Logger.InfoContext(ctx, "push messages sent", slog.Int("sent", n))
			}

			return err
		},
	}
}

// measured returns the Run of j reporting each run to deps.Metrics: its
// duration and whether it failed or panicked (the panic goes on to the
// runner, which recovers and logs it). It runs inside the job's lock, so
// a skipped run is not observed.
func measured(j jobs.Job, deps Deps) func(ctx context.Context) error {
	return func(ctx context.Context) (err error) {
		start := deps.Clock.Now()

		defer func() {
			if rec := recover(); rec != nil {
				deps.Metrics.ObserveJob(j.Name, errJobPanicked, deps.Clock.Now().Sub(start))
				panic(rec)
			}

			deps.Metrics.ObserveJob(j.Name, err, deps.Clock.Now().Sub(start))
		}()

		return j.Run(ctx)
	}
}

// errJobPanicked is the error a panicking job reports to Metrics.
var errJobPanicked = errors.New("job panicked")
