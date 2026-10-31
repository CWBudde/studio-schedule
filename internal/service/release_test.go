package service

import (
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}

	return loc
}

func releaseSettings(weekday time.Weekday, tod domain.TimeOfDay, weeksAhead int) domain.Settings {
	s := seed.DemoSettings()
	s.ReleaseWeekday = weekday
	s.ReleaseTime = tod
	s.ReleaseWeeksAhead = weeksAhead

	return s
}

func TestComputeReleaseWindow(t *testing.T) {
	t.Parallel()

	berlin := mustZone(t, "Europe/Berlin")
	at := func(y int, m time.Month, d, hh, mm, ss int) time.Time {
		return time.Date(y, m, d, hh, mm, ss, 0, berlin)
	}
	utc := func(y int, m time.Month, d, hh, mm int) time.Time {
		return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
	}

	friday18x2 := releaseSettings(time.Friday, domain.TimeOfDay{Hour: 18}, 2)
	demo := seed.DemoSettings() // Sunday 18:00, one week ahead

	tests := []struct {
		name     string
		settings domain.Settings
		now      time.Time
		want     ReleaseWindow
	}{
		{
			name:     "one second before the Friday release",
			settings: friday18x2,
			now:      at(2026, time.October, 16, 17, 59, 59),
			want: ReleaseWindow{
				LastRelease:   at(2026, time.October, 9, 18, 0, 0),
				NextRelease:   at(2026, time.October, 16, 18, 0, 0),
				ReleasedUntil: at(2026, time.October, 24, 0, 0, 0),
			},
		},
		{
			name:     "the release instant counts as released",
			settings: friday18x2,
			now:      at(2026, time.October, 16, 18, 0, 0),
			want: ReleaseWindow{
				LastRelease:   at(2026, time.October, 16, 18, 0, 0),
				NextRelease:   utc(2026, time.October, 23, 16, 0), // 18:00 CEST
				ReleasedUntil: utc(2026, time.October, 30, 23, 0), // Saturday 31st 00:00 CET
			},
		},
		{
			name:     "between releases, the school date differs from the UTC date",
			settings: friday18x2,
			now:      utc(2026, time.October, 16, 23, 30), // Saturday 01:30 in Berlin
			want: ReleaseWindow{
				LastRelease:   at(2026, time.October, 16, 18, 0, 0),
				NextRelease:   at(2026, time.October, 23, 18, 0, 0),
				ReleasedUntil: at(2026, time.October, 31, 0, 0, 0),
			},
		},
		{
			name:     "next release across the end of DST keeps 18:00 school time",
			settings: friday18x2,
			now:      at(2026, time.October, 23, 18, 0, 0),
			want: ReleaseWindow{
				LastRelease:   utc(2026, time.October, 23, 16, 0), // 18:00 CEST
				NextRelease:   utc(2026, time.October, 30, 17, 0), // 18:00 CET, 169 hours later
				ReleasedUntil: utc(2026, time.November, 6, 23, 0),
			},
		},
		{
			name:     "last release found across the end of DST",
			settings: friday18x2,
			now:      at(2026, time.October, 30, 17, 59, 0),
			want: ReleaseWindow{
				LastRelease:   utc(2026, time.October, 23, 16, 0),
				NextRelease:   utc(2026, time.October, 30, 17, 0),
				ReleasedUntil: utc(2026, time.November, 6, 23, 0),
			},
		},
		{
			name:     "spring forward: release before the change",
			settings: friday18x2,
			now:      at(2027, time.March, 26, 18, 0, 0),
			want: ReleaseWindow{
				LastRelease:   utc(2027, time.March, 26, 17, 0), // 18:00 CET
				NextRelease:   utc(2027, time.April, 2, 16, 0),  // 18:00 CEST, 167 hours later
				ReleasedUntil: utc(2027, time.April, 9, 22, 0),  // Saturday April 10th 00:00 CEST
			},
		},
		{
			name:     "spring forward: one second before the release after the change",
			settings: friday18x2,
			now:      utc(2027, time.April, 2, 15, 59).Add(59 * time.Second),
			want: ReleaseWindow{
				LastRelease:   utc(2027, time.March, 26, 17, 0),
				NextRelease:   utc(2027, time.April, 2, 16, 0),
				ReleasedUntil: utc(2027, time.April, 9, 22, 0),
			},
		},
		{
			name:     "demo: Sunday 18:00, one week ahead, mid-week",
			settings: demo,
			now:      at(2026, time.October, 21, 12, 0, 0),
			want: ReleaseWindow{
				LastRelease:   at(2026, time.October, 18, 18, 0, 0),
				NextRelease:   utc(2026, time.October, 25, 17, 0), // 18:00 CET on the day DST ends
				ReleasedUntil: at(2026, time.October, 26, 0, 0, 0),
			},
		},
		{
			name:     "demo: release on the day DST ends",
			settings: demo,
			now:      utc(2026, time.October, 25, 17, 0),
			want: ReleaseWindow{
				LastRelease:   utc(2026, time.October, 25, 17, 0),
				NextRelease:   at(2026, time.November, 1, 18, 0, 0),
				ReleasedUntil: at(2026, time.November, 2, 0, 0, 0),
			},
		},
		{
			name:     "demo: one minute before the release on the day DST ends",
			settings: demo,
			now:      utc(2026, time.October, 25, 16, 59),
			want: ReleaseWindow{
				LastRelease:   at(2026, time.October, 18, 18, 0, 0),
				NextRelease:   utc(2026, time.October, 25, 17, 0),
				ReleasedUntil: at(2026, time.October, 26, 0, 0, 0),
			},
		},
		{
			name:     "release time in the DST gap moves forward by the gap",
			settings: releaseSettings(time.Sunday, domain.TimeOfDay{Hour: 2, Minute: 30}, 1),
			now:      utc(2027, time.March, 28, 1, 30), // 03:30 CEST
			want: ReleaseWindow{
				LastRelease:   utc(2027, time.March, 28, 1, 30),
				NextRelease:   at(2027, time.April, 4, 2, 30, 0),
				ReleasedUntil: at(2027, time.April, 5, 0, 0, 0),
			},
		},
		{
			name:     "release time in the DST gap, just before",
			settings: releaseSettings(time.Sunday, domain.TimeOfDay{Hour: 2, Minute: 30}, 1),
			now:      utc(2027, time.March, 28, 1, 29),
			want: ReleaseWindow{
				LastRelease:   at(2027, time.March, 21, 2, 30, 0),
				NextRelease:   utc(2027, time.March, 28, 1, 30),
				ReleasedUntil: at(2027, time.March, 29, 0, 0, 0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ComputeReleaseWindow(tt.settings, berlin, tt.now)
			if !got.LastRelease.Equal(tt.want.LastRelease) {
				t.Errorf("LastRelease = %v, want %v", got.LastRelease, tt.want.LastRelease)
			}

			if !got.NextRelease.Equal(tt.want.NextRelease) {
				t.Errorf("NextRelease = %v, want %v", got.NextRelease, tt.want.NextRelease)
			}

			if !got.ReleasedUntil.Equal(tt.want.ReleasedUntil) {
				t.Errorf("ReleasedUntil = %v, want %v", got.ReleasedUntil, tt.want.ReleasedUntil)
			}

			if got.LastRelease.After(tt.now) || !got.NextRelease.After(tt.now) {
				t.Errorf("now %v is not in [%v, %v)", tt.now, got.LastRelease, got.NextRelease)
			}
		})
	}
}

// TestReleaseWindowBoundary checks the ADR example: the Friday 16th release
// opens everything that starts before Saturday the 31st 00:00.
func TestReleaseWindowBoundary(t *testing.T) {
	t.Parallel()

	berlin := mustZone(t, "Europe/Berlin")
	w := ComputeReleaseWindow(releaseSettings(time.Friday, domain.TimeOfDay{Hour: 18}, 2), berlin,
		time.Date(2026, time.October, 16, 18, 0, 0, 0, berlin))

	lesson := func(startsAt time.Time) domain.Lesson {
		return domain.Lesson{
			ClassID:    domain.ClassID{1},
			StartsAt:   startsAt,
			Visibility: domain.VisibilityPublic,
			Kind:       domain.LessonRegular,
		}
	}

	if l := lesson(time.Date(2026, time.October, 30, 23, 0, 0, 0, berlin)); !visibleToGuest(l, w) {
		t.Error("Friday 30th 23:00 must be released")
	}

	if l := lesson(time.Date(2026, time.October, 31, 0, 0, 0, 0, berlin)); visibleToGuest(l, w) {
		t.Error("Saturday 31st 00:00 must not be released")
	}
}

// TestReleasedThroughExtendsWindow checks the manual released-through date
// (ADR 0003, amendment 2026-10-31): the window ends at the later of the
// rule's end and school-local midnight after the date; the rule's own end
// stays in RuleReleasedUntil.
func TestReleasedThroughExtendsWindow(t *testing.T) {
	t.Parallel()

	berlin := mustZone(t, "Europe/Berlin")
	at := func(y int, m time.Month, d, hh int) time.Time { return time.Date(y, m, d, hh, 0, 0, 0, berlin) }
	utc := func(y int, m time.Month, d, hh int) time.Time { return time.Date(y, m, d, hh, 0, 0, 0, time.UTC) }

	// Friday 18:00, two weeks ahead; at the release on Friday the 16th the
	// rule releases through Friday the 30th (until Saturday the 31st 00:00).
	now := at(2026, time.October, 16, 18)
	ruleEnd := utc(2026, time.October, 30, 23) // Saturday 31st 00:00 CET

	tests := []struct {
		name    string
		through domain.Date
		want    time.Time
	}{
		{name: "no date", want: ruleEnd},
		{name: "a date in the past", through: domain.Date{Year: 2026, Month: time.October, Day: 1}, want: ruleEnd},
		{name: "a date before the rule's end", through: domain.Date{Year: 2026, Month: time.October, Day: 29}, want: ruleEnd},
		{name: "the rule's last day", through: domain.Date{Year: 2026, Month: time.October, Day: 30}, want: ruleEnd},
		{
			name:    "a date after the rule's end, across the end of DST",
			through: domain.Date{Year: 2026, Month: time.November, Day: 20},
			want:    utc(2026, time.November, 20, 23), // Saturday 21st 00:00 CET
		},
		{
			name:    "a date across the start of DST: midnight CEST",
			through: domain.Date{Year: 2027, Month: time.March, Day: 28},
			want:    utc(2027, time.March, 28, 22), // Monday 29th 00:00 CEST
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := releaseSettings(time.Friday, domain.TimeOfDay{Hour: 18}, 2)
			s.ReleasedThrough = tt.through

			got := ComputeReleaseWindow(s, berlin, now)
			if !got.ReleasedUntil.Equal(tt.want) {
				t.Errorf("ReleasedUntil = %v, want %v", got.ReleasedUntil, tt.want)
			}

			if !got.RuleReleasedUntil.Equal(ruleEnd) {
				t.Errorf("RuleReleasedUntil = %v, want %v", got.RuleReleasedUntil, ruleEnd)
			}

			if !got.LastRelease.Equal(now) || !got.NextRelease.Equal(at(2026, time.October, 23, 18)) {
				t.Errorf("the date moved the releases: %v, %v", got.LastRelease, got.NextRelease)
			}
		})
	}

	// Once the weekly rule passes the date, the date has no effect.
	s := releaseSettings(time.Friday, domain.TimeOfDay{Hour: 18}, 2)
	s.ReleasedThrough = domain.Date{Year: 2026, Month: time.November, Day: 3}

	later := ComputeReleaseWindow(s, berlin, at(2026, time.October, 23, 18))
	if !later.ReleasedUntil.Equal(later.RuleReleasedUntil) {
		t.Errorf("after the rule passed the date: ReleasedUntil = %v, want the rule's %v",
			later.ReleasedUntil, later.RuleReleasedUntil)
	}
}

// TestReleasedThroughVisibility: guests see regular lessons through the
// date; members' visibleUntil is the later of the date and their early
// access, which only adds to it.
func TestReleasedThroughVisibility(t *testing.T) {
	t.Parallel()

	berlin := mustZone(t, "Europe/Berlin")
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, berlin)

	s := releaseSettings(time.Sunday, domain.TimeOfDay{Hour: 18}, 1) // released until Monday the 26th
	s.DefaultVisibleDays = 14                                        // members until Thursday, November 5th
	s.ReleasedThrough = domain.Date{Year: 2026, Month: time.November, Day: 20}
	w := ComputeReleaseWindow(s, berlin, now)

	regular := func(startsAt time.Time) domain.Lesson {
		return domain.Lesson{StartsAt: startsAt, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular}
	}

	if !visibleToGuest(regular(time.Date(2026, time.November, 20, 23, 59, 0, 0, berlin)), w) {
		t.Error("a guest must see the last minute of the released-through date")
	}

	if visibleToGuest(regular(time.Date(2026, time.November, 21, 0, 0, 0, 0, berlin)), w) {
		t.Error("a guest must not see the day after the released-through date")
	}

	member := Principal{AccountID: domain.AccountID{1}, Role: domain.RoleMember}
	if got, want := viewerOf(member, s, berlin, now).visibleUntil(w), w.ReleasedUntil; !got.Equal(want) {
		t.Errorf("member visibleUntil = %v, want the released-through end %v", got, want)
	}

	member.VisibleDays = 60 // early access beyond the date still adds
	if got, want := viewerOf(member, s, berlin, now).visibleUntil(w),
		time.Date(2026, time.December, 21, 0, 0, 0, 0, berlin); !got.Equal(want) {
		t.Errorf("member with 60 days: visibleUntil = %v, want %v", got, want)
	}
}
