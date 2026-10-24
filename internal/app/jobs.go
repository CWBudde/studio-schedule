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
// the background (Run); see package jobs.
func Jobs(deps Deps) *jobs.Runner {
	deps = deps.withDefaults()
	lessons := service.NewLessonGenerator(deps.Store, deps.Clock, deps.IDs)
	renderer := newRenderer()
	auth := newAuthService(deps, renderer)
	waitlist := service.NewWaitlistService(deps.Store, deps.Clock, deps.IDs,
		service.Mails{Renderer: renderer, AppURL: deps.AppURL})

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

	if deps.Mailer != nil {
		outbox := service.NewOutboxService(deps.Store, deps.Clock, deps.Mailer)
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

	return jobs.NewRunner(deps.Logger, all...)
}
