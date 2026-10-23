package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestVisibleToGuest(t *testing.T) {
	t.Parallel()

	until := time.Date(2026, time.October, 25, 22, 0, 0, 0, time.UTC)
	w := ReleaseWindow{ReleasedUntil: until}

	released := until.Add(-time.Nanosecond)
	unreleased := until // the window is exclusive

	// shapes of a lesson, with whether a guest sees it once it is public
	// but not yet released ("published early")
	shapes := []struct {
		name       string
		class      domain.ClassID
		kind       domain.LessonKind
		earlyShown bool
	}{
		{"regular class lesson", domain.ClassID{1}, domain.LessonRegular, false},
		{"one-off regular lesson", domain.ClassID{}, domain.LessonRegular, true},
		{"workshop of a class", domain.ClassID{1}, domain.LessonWorkshop, true},
		{"one-off workshop", domain.ClassID{}, domain.LessonWorkshop, true},
		{"private lesson", domain.ClassID{}, domain.LessonPrivate, true},
	}

	for _, vis := range domain.LessonVisibilities() {
		for _, shape := range shapes {
			for _, status := range domain.LessonStatuses() {
				for _, rel := range []struct {
					name     string
					startsAt time.Time
				}{{"released", released}, {"unreleased", unreleased}} {
					l := domain.Lesson{
						ClassID:    shape.class,
						StartsAt:   rel.startsAt,
						Status:     status,
						Visibility: vis,
						Kind:       shape.kind,
					}

					want := false
					if vis == domain.VisibilityPublic {
						want = rel.name == "released" || shape.earlyShown
					}

					name := fmt.Sprintf("%s/%s/%s/%s", vis, shape.name, status, rel.name)
					if got := visibleToGuest(l, w); got != want {
						t.Errorf("%s: visibleToGuest = %v, want %v", name, got, want)
					}
				}
			}
		}
	}
}
