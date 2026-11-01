package domain

// AgeMonths is an age in months; 0 means not stated (ADR 0021).
type AgeMonths int

// MaxAgeMonths is the highest age a class can state: 100 years.
const MaxAgeMonths AgeMonths = 1200

// MaxStage is the highest stage a class can state; stages count from 1.
const MaxStage = 9

// LevelRange is the span between two levels of the school's scale. The ends
// may be in either order by position: the scale can be reordered after a
// class was saved, and the span is read lower level first. The zero
// LevelRange states no level.
type LevelRange struct{ From, To LevelID }

// IsZero reports whether the range states no level at all.
func (r LevelRange) IsZero() bool { return r.From.IsZero() && r.To.IsZero() }
