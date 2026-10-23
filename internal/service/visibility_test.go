package service

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
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

func TestViewerSees(t *testing.T) {
	t.Parallel()

	released := time.Date(2026, time.October, 25, 22, 0, 0, 0, time.UTC)
	early := released.Add(72 * time.Hour)
	w := ReleaseWindow{ReleasedUntil: released}
	member := viewer{role: domain.RoleMember, earlyUntil: early}

	regular := func(vis domain.LessonVisibility, startsAt time.Time) domain.Lesson {
		return domain.Lesson{ClassID: domain.ClassID{1}, StartsAt: startsAt, Visibility: vis, Kind: domain.LessonRegular}
	}

	for _, vis := range domain.LessonVisibilities() {
		for _, at := range []struct {
			name     string
			startsAt time.Time
			inWindow bool
		}{
			{"released", released.Add(-time.Nanosecond), true},
			{"early access", early.Add(-time.Nanosecond), true},
			{"beyond early access", early, false},
		} {
			l := regular(vis, at.startsAt)
			name := fmt.Sprintf("%s/%s", vis, at.name)

			if want := vis != domain.VisibilityHidden && at.inWindow; member.sees(l, w) != want {
				t.Errorf("member: %s: sees = %v, want %v", name, !want, want)
			}

			for _, r := range []domain.Role{domain.RoleInstructor, domain.RoleOffice, domain.RoleAdmin} {
				if !(viewer{role: r}).sees(l, w) {
					t.Errorf("%s: %s: staff must see every lesson", r, name)
				}
			}

			if got, want := (viewer{}).sees(l, w), visibleToGuest(l, w); got != want {
				t.Errorf("guest: %s: sees = %v, want %v (visibleToGuest)", name, got, want)
			}
		}
	}

	// Early access never hides what is released.
	short := viewer{role: domain.RoleMember, earlyUntil: released.Add(-48 * time.Hour)}
	if !short.sees(regular(domain.VisibilityPublic, released.Add(-time.Hour)), w) {
		t.Error("a short early access hides a released lesson")
	}

	if got := short.visibleUntil(w); !got.Equal(released) {
		t.Errorf("visibleUntil = %v, want the release %v", got, released)
	}

	// Members see one-off lessons for members beyond any window.
	oneOff := domain.Lesson{StartsAt: early.Add(30 * 24 * time.Hour), Visibility: domain.VisibilityMembers, Kind: domain.LessonWorkshop}
	if !member.sees(oneOff, w) {
		t.Error("a member does not see a workshop for members")
	}
}

func TestScheduleViewers(t *testing.T) {
	t.Parallel()

	f := newScheduleFixture(t)
	q := ScheduleQuery{From: schoolDate(2026, time.October, 19), To: schoolDate(2026, time.November, 9)}

	guestSees := []string{"monBallet", "monHipHop", "wedPointeCanceled", "workshop"}
	// Demo default: 14 visible days, so early access until Thursday
	// 2026-11-05 00:00 (after the DST change on October 25).
	memberSees := []string{"monBallet", "monHipHop", "tueMembers", "wedPointeCanceled", "monBalletNextWeek", "workshop"}
	staffSees := []string{
		"monBallet", "monHipHop", "tueHidden", "tueMembers", "wedPointeCanceled", "monBalletNextWeek",
		"workshop", "hiddenWorkshop",
	}
	early := time.Date(2026, time.November, 5, 0, 0, 0, 0, f.loc)
	released := ComputeReleaseWindow(seed.DemoSettings(), f.loc, f.now).ReleasedUntil

	tests := []struct {
		name         string
		p            Principal
		want         []string
		visibleUntil time.Time
	}{
		{"guest", Principal{}, guestSees, released},
		{"member", f.principal(t, domain.RoleMember), memberSees, early},
		{"instructor", f.principal(t, domain.RoleInstructor), staffSees, time.Time{}},
		{"office", f.principal(t, domain.RoleOffice), staffSees, time.Time{}},
		{"admin", f.principal(t, domain.RoleAdmin), staffSees, time.Time{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := f.svc.Schedule(t.Context(), tt.p, q)
			if err != nil {
				t.Fatal(err)
			}

			if got := f.names(s); !slices.Equal(got, tt.want) {
				t.Errorf("lessons = %v, want %v", got, tt.want)
			}

			if !s.VisibleUntil.Equal(tt.visibleUntil) {
				t.Errorf("visibleUntil = %v, want %v", s.VisibleUntil, tt.visibleUntil)
			}

			for name, l := range f.lessons {
				_, err := f.svc.Lesson(t.Context(), tt.p, l.ID)
				if seen := slices.Contains(tt.want, name); seen != (err == nil) {
					t.Errorf("Lesson(%s): err = %v, want seen = %v", name, err, seen)
				}
			}
		})
	}
}

func TestVisibleDays(t *testing.T) {
	t.Parallel()

	q := ScheduleQuery{From: schoolDate(2026, time.October, 19), To: schoolDate(2026, time.November, 9)}

	tests := []struct {
		name         string
		visibleDays  int
		nextWeek     bool // sees the unreleased lesson on 2026-10-26
		visibleUntil time.Time
	}{
		// 3 days reach Sunday 2026-10-25 00:00, before the release:
		// early access never hides released lessons.
		{"shorter than the release", 3, false, time.Date(2026, time.October, 26, 0, 0, 0, 0, time.UTC)},
		{"default (14)", 0, true, time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)},
		{"override (5)", 5, true, time.Date(2026, time.October, 27, 0, 0, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newScheduleFixture(t)
			p := f.principal(t, domain.RoleMember)
			p.VisibleDays = tt.visibleDays // as Authenticate reads it from the account

			s, err := f.svc.Schedule(t.Context(), p, q)
			if err != nil {
				t.Fatal(err)
			}

			// School-local midnight, whatever the offset that day.
			want := domain.DateOf(tt.visibleUntil, time.UTC).At(domain.TimeOfDay{}, f.loc)
			if !s.VisibleUntil.Equal(want) {
				t.Errorf("visibleUntil = %v, want %v", s.VisibleUntil, want)
			}

			if got := slices.Contains(f.names(s), "monBalletNextWeek"); got != tt.nextWeek {
				t.Errorf("sees next week's lesson = %v, want %v", got, tt.nextWeek)
			}
		})
	}
}

// principal returns a principal of a demo account with role r.
func (f *scheduleFixture) principal(t *testing.T, r domain.Role) Principal {
	t.Helper()

	for _, a := range f.data.Accounts {
		if a.Role == r {
			return Principal{
				AccountID: a.ID, PersonID: a.PersonID, Email: a.Email, Role: a.Role, VisibleDays: a.VisibleDays,
			}
		}
	}

	t.Fatalf("no demo account with role %s", r)

	return Principal{}
}
