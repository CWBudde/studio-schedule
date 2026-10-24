package service

import (
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// viewer is who reads the schedule (ADR 0003): a guest (zero value), a
// member, or staff (instructor, office, admin).
type viewer struct {
	role domain.Role
	// earlyUntil gives a member early access: lessons they may see that
	// start before it are visible even if not yet released.
	earlyUntil time.Time
}

// viewerOf returns the viewer of p, a zero Principal being a guest. A
// member's early access reaches midnight after today + N days, N being
// the account's visible days or else the school's default.
func viewerOf(p Principal, s domain.Settings, loc *time.Location, now time.Time) viewer {
	switch {
	case p.AccountID.IsZero() || !p.Role.AtLeast(domain.RoleMember):
		return viewer{role: domain.RoleGuest}
	case p.Role.Staff():
		return viewer{role: p.Role}
	}

	days := s.DefaultVisibleDays
	if p.VisibleDays > 0 {
		days = p.VisibleDays
	}

	return viewer{
		role:       domain.RoleMember,
		earlyUntil: domain.DateOf(now, loc).AddDays(days+1).At(domain.TimeOfDay{}, loc),
	}
}

// sees reports whether v may see l under the release window w. Staff see
// every lesson. Members also see lessons for members, and with early
// access lessons up to earlyUntil.
func (v viewer) sees(l domain.Lesson, w ReleaseWindow) bool {
	switch {
	case v.role.Staff():
		return true
	case v.role == domain.RoleMember:
		if l.Visibility == domain.VisibilityHidden {
			return false
		}

		return publishedEarly(l) || l.StartsAt.Before(v.visibleUntil(w))
	default:
		return visibleToGuest(l, w)
	}
}

// visibleUntil is the instant before which regular lessons are visible to
// v; zero means no limit (staff).
func (v viewer) visibleUntil(w ReleaseWindow) time.Time {
	switch {
	case v.role.Staff():
		return time.Time{}
	case v.earlyUntil.After(w.ReleasedUntil):
		return v.earlyUntil
	default:
		return w.ReleasedUntil
	}
}

// visibleToGuest reports whether a visitor who is not signed in may see l
// (ADR 0003): the lesson must be public and released. Kinds other than
// regular are published early and ignore the window.
// Canceled lessons stay visible; callers show their status.
func visibleToGuest(l domain.Lesson, w ReleaseWindow) bool {
	if l.Visibility != domain.VisibilityPublic {
		return false
	}

	return l.StartsAt.Before(w.ReleasedUntil) || publishedEarly(l)
}

// publishedEarly reports whether l ignores the release window: workshops
// and private lessons, whether of a class or one-off (ADR 0003, amended
// 2026-10-24). A regular one-off lesson follows the window.
func publishedEarly(l domain.Lesson) bool {
	return l.Kind != domain.LessonRegular
}
