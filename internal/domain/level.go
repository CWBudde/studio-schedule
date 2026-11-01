package domain

import "unicode/utf8"

// Level is a step on the school's level scale (ADR 0021). Position orders the
// scale from least to most advanced, starting at 1. Code is an optional short
// label such as "A"; the name is the school's own word, not translated.
type Level struct {
	ID       LevelID
	Name     string
	Code     string // optional
	Position int
}

// Limits of a level's name and code, in characters.
const (
	MaxLevelNameLen = 60
	MaxLevelCodeLen = 8
)

// Validate checks the level for consistency. The service trims the name and
// the code before. All errors wrap ErrValidation.
func (l Level) Validate() error {
	return validate(
		rule{!l.ID.IsZero(), "level id must be set"},
		on("name", !blank(l.Name), "level name must not be empty"),
		on("name", utf8.RuneCountInString(l.Name) <= MaxLevelNameLen, "level name is too long"),
		on("code", utf8.RuneCountInString(l.Code) <= MaxLevelCodeLen, "level code is too long"),
		on("position", l.Position > 0, "level position must be positive"),
	)
}
