package service

import (
	"github.com/cwbudde/studio-schedule/internal/domain"
)

// visibleToGuest reports whether a visitor who is not signed in may see l
// (ADR 0003): the lesson must be public and released. One-off lessons and
// kinds other than regular are published early and ignore the window.
// Canceled lessons stay visible; callers show their status.
func visibleToGuest(l domain.Lesson, w ReleaseWindow) bool {
	if l.Visibility != domain.VisibilityPublic {
		return false
	}

	return l.StartsAt.Before(w.ReleasedUntil) || l.ClassID.IsZero() || l.Kind != domain.LessonRegular
}
