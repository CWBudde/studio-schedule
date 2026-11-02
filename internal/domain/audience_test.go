package domain_test

import (
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestLevelRangeIsZero(t *testing.T) {
	t.Parallel()

	id := newID[domain.LevelID]()
	for _, tc := range []struct {
		r    domain.LevelRange
		want bool
	}{
		{domain.LevelRange{}, true},
		{domain.LevelRange{From: id, To: id}, false},
		{domain.LevelRange{From: id}, false},
		{domain.LevelRange{To: id}, false},
	} {
		if got := tc.r.IsZero(); got != tc.want {
			t.Errorf("%+v.IsZero() = %v, want %v", tc.r, got, tc.want)
		}
	}
}

// TestClassAudienceValid lists audiences a class may state.
func TestClassAudienceValid(t *testing.T) {
	t.Parallel()

	a, b := newID[domain.LevelID](), newID[domain.LevelID]()
	base := domain.Class{
		ID: newID[domain.ClassID](), TermID: newID[domain.TermID](), CourseTypeID: newID[domain.CourseTypeID](),
		RoomID: newID[domain.RoomID](), InstructorID: newID[domain.StaffID](),
		Weekday: time.Monday, StartTime: domain.TimeOfDay{Hour: 18}, Duration: time.Hour, Capacity: 12,
	}

	for name, f := range map[string]func(*domain.Class){
		"nothing stated": func(*domain.Class) {},
		"both ends":      func(c *domain.Class) { c.Levels = domain.LevelRange{From: a, To: b} },
		"one level":      func(c *domain.Class) { c.Levels = domain.LevelRange{From: a, To: a} },
		"all levels":     func(c *domain.Class) { c.AllLevels = true },
		"stage 9":        func(c *domain.Class) { c.Stage = 9 },
		"ages 48 to 72":  func(c *domain.Class) { c.AgeFrom, c.AgeTo = 48, 72 },
		"equal ages":     func(c *domain.Class) { c.AgeFrom, c.AgeTo = 48, 48 },
		"up to 6":        func(c *domain.Class) { c.AgeTo = 72 },
		"from 3":         func(c *domain.Class) { c.AgeFrom = 36 },
		"max ages":       func(c *domain.Class) { c.AgeFrom, c.AgeTo = domain.MaxAgeMonths, domain.MaxAgeMonths },
	} {
		c := base
		f(&c)

		if err := c.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v, want nil", name, err)
		}
	}
}
