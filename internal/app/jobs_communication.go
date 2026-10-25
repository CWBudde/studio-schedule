package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// announcementsJob is the name of the job that emails announcements.
const announcementsJob = "announcements"

// communicationJobs are the jobs of announcements and the mail log (ADR
// 0014). They only queue or remove mails, so they run without a mailer too
// (the demo).
func communicationJobs(deps Deps, renderer service.MailRenderer) []jobs.Job {
	announcements := service.NewAnnouncementService(deps.Store, deps.Clock, deps.IDs, renderer, nil)
	mailLog := service.NewMailLogService(deps.Store, deps.Clock, deps.IDs)

	return []jobs.Job{{
		// Emails the announcements whose time has come, once each, in
		// batches; woken when the office publishes one to email now.
		Name:  announcementsJob,
		Every: time.Minute,
		Wake:  deps.Wake.Chan(announcementsJob),
		Run: func(ctx context.Context) error {
			n, err := announcements.SendDue(ctx)

			switch {
			case errors.Is(err, service.ErrNoSchool):
				return nil // announcements but no settings: broken data, nothing to send
			case err != nil:
				return err
			}

			if n > 0 {
				deps.Logger.InfoContext(ctx, "announcement mails queued", slog.Int("mails", n))
			}

			return nil
		},
	}, {
		// Removes the bodies of mails sent 30 days ago and the mails of 180
		// days ago.
		Name:  "purge-mail",
		Every: 24 * time.Hour,
		Run: func(ctx context.Context) error {
			bodies, deleted, err := mailLog.Purge(ctx)
			if bodies > 0 || deleted > 0 {
				deps.Logger.InfoContext(ctx, "old mails purged", slog.Int("bodies", bodies), slog.Int("deleted", deleted))
			}

			return err
		},
	}}
}
