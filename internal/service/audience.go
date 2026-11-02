package service

import (
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Audience is who a class is for, its levels resolved (ADR 0021): Low and
// High are the span's ends ordered by position (then ID), both zero when the
// class states no level. A class for one level has Low == High.
type Audience struct {
	Low, High      domain.Level
	AllLevels      bool
	Stage          int
	AgeFrom, AgeTo domain.AgeMonths
}

// audienceOf resolves c's levels in levels; an unknown one is
// errBrokenReference.
func audienceOf(c domain.Class, levels map[domain.LevelID]domain.Level) (Audience, error) {
	a := Audience{AllLevels: c.AllLevels, Stage: c.Stage, AgeFrom: c.AgeFrom, AgeTo: c.AgeTo}
	if c.Levels.IsZero() {
		return a, nil
	}

	from, ok := levels[c.Levels.From]
	if !ok {
		return Audience{}, fmt.Errorf("class %v: level %v: %w", c.ID, c.Levels.From, errBrokenReference)
	}

	to, ok := levels[c.Levels.To]
	if !ok {
		return Audience{}, fmt.Errorf("class %v: level %v: %w", c.ID, c.Levels.To, errBrokenReference)
	}

	if below(to, from) {
		from, to = to, from
	}

	a.Low, a.High = from, to

	return a, nil
}

// below reports whether a comes before b on the scale: by position, then ID.
func below(a, b domain.Level) bool {
	if a.Position != b.Position {
		return a.Position < b.Position
	}

	return a.ID.String() < b.ID.String()
}

// Covers reports whether a class with this audience is for level l: all
// levels, or l's position between Low's and High's (inclusive). An audience
// without a level covers none.
func (a Audience) Covers(l domain.Level) bool {
	if a.AllLevels {
		return true
	}

	if a.Low.ID.IsZero() {
		return false
	}

	return a.Low.Position <= l.Position && l.Position <= a.High.Position
}

// levelIndex maps the levels by ID.
func levelIndex(levels []domain.Level) map[domain.LevelID]domain.Level {
	m := make(map[domain.LevelID]domain.Level, len(levels))
	for _, l := range levels {
		m[l.ID] = l
	}

	return m
}
