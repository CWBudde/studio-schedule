package service

import (
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ReleaseWindow is the state of the weekly release rule at one instant (ADR
// 0003). Regular lessons are released once they start before ReleasedUntil.
type ReleaseWindow struct {
	// LastRelease is the latest release instant at or before now.
	LastRelease time.Time
	// NextRelease is the release instant after LastRelease.
	NextRelease time.Time
	// ReleasedUntil is school-local midnight after the last released day:
	// lessons with StartsAt < ReleasedUntil are released.
	ReleasedUntil time.Time
}

// ComputeReleaseWindow applies the release rule of s in the school time
// zone loc at the instant now.
//
// Every s.ReleaseWeekday at s.ReleaseTime the schedule is released through
// the same weekday s.ReleaseWeeksAhead weeks later. Weeks are stepped on
// school dates, never as 168 hours, so a DST change does not move the
// release time. The release instant itself counts as released.
func ComputeReleaseWindow(s domain.Settings, loc *time.Location, now time.Time) ReleaseWindow {
	today := domain.DateOf(now, loc)
	day := today.AddDays(-((int(today.Weekday()) - int(s.ReleaseWeekday) + 7) % 7))

	last := day.At(s.ReleaseTime, loc)
	if last.After(now) {
		day = day.AddDays(-7)
		last = day.At(s.ReleaseTime, loc)
	}

	return ReleaseWindow{
		LastRelease:   last,
		NextRelease:   day.AddDays(7).At(s.ReleaseTime, loc),
		ReleasedUntil: day.AddDays(7*s.ReleaseWeeksAhead+1).At(domain.TimeOfDay{}, loc),
	}
}
