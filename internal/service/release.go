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
	// lessons with StartsAt < ReleasedUntil are released. It is the later
	// of RuleReleasedUntil and midnight after Settings.ReleasedThrough.
	ReleasedUntil time.Time
	// RuleReleasedUntil is where the weekly rule alone releases through,
	// without the manual date.
	RuleReleasedUntil time.Time
}

// ComputeReleaseWindow applies the release rule of s in the school time
// zone loc at the instant now.
//
// Every s.ReleaseWeekday at s.ReleaseTime the schedule is released through
// the same weekday s.ReleaseWeeksAhead weeks later. Weeks are stepped on
// school dates, never as 168 hours, so a DST change does not move the
// release time. The release instant itself counts as released.
//
// A manual s.ReleasedThrough only extends the window: ReleasedUntil is the
// later of the rule's end and school-local midnight after that date (ADR
// 0003, amendment 2026-10-31). This is the one place that computes the
// window; visibility, early access, booking and the calendar feeds all read
// it.
func ComputeReleaseWindow(s domain.Settings, loc *time.Location, now time.Time) ReleaseWindow {
	today := domain.DateOf(now, loc)
	day := today.AddDays(-((int(today.Weekday()) - int(s.ReleaseWeekday) + 7) % 7))

	last := day.At(s.ReleaseTime, loc)
	if last.After(now) {
		day = day.AddDays(-7)
		last = day.At(s.ReleaseTime, loc)
	}

	rule := day.AddDays(7*s.ReleaseWeeksAhead+1).At(domain.TimeOfDay{}, loc)
	until := rule

	if !s.ReleasedThrough.IsZero() {
		if manual := s.ReleasedThrough.AddDays(1).At(domain.TimeOfDay{}, loc); manual.After(until) {
			until = manual
		}
	}

	return ReleaseWindow{
		LastRelease:       last,
		NextRelease:       day.AddDays(7).At(s.ReleaseTime, loc),
		ReleasedUntil:     until,
		RuleReleasedUntil: rule,
	}
}
