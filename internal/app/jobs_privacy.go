package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// privacyJobs are the jobs of ADR 0016: the retention of personal data.
func privacyJobs(deps Deps) []jobs.Job {
	retention := service.NewRetentionService(deps.Store, deps.Clock)

	return []jobs.Job{{
		// Anonymizes the guests of lessons that ended 12 months ago and
		// deletes the audit events of 3 years ago and the import runs of a
		// year ago.
		Name:  "purge-personal-data",
		Every: 24 * time.Hour,
		Run: func(ctx context.Context) error {
			n, err := retention.Purge(ctx)
			if n != (service.Purged{}) {
				deps.Logger.InfoContext(ctx, "personal data purged", slog.Int("guests", n.Guests),
					slog.Int("auditEvents", n.AuditEvents), slog.Int("importRuns", n.ImportRuns))
			}

			return err
		},
	}}
}
