package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// Jobs returns the background jobs of the application, built from the same
// Deps as New. The caller runs them once at startup (RunOnce) and then in
// the background (Run); see package jobs.
func Jobs(deps Deps) *jobs.Runner {
	deps = deps.withDefaults()
	lessons := service.NewLessonGenerator(deps.Store, deps.Clock, deps.IDs)

	return jobs.NewRunner(deps.Logger, jobs.Job{
		Name:  "generate-lessons",
		Every: time.Hour,
		Run: func(ctx context.Context) error {
			created, err := lessons.GenerateAhead(ctx)

			switch {
			case errors.Is(err, domain.ErrNotFound):
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
	})
}
