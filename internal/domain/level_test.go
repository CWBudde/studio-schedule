package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func levelFixture() domain.Level {
	return domain.Level{ID: newID[domain.LevelID](), Name: "Beginner", Code: "B", Position: 1}
}

func TestLevelValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, levelFixture, map[string]func(*domain.Level){
		"zero id":       func(l *domain.Level) { l.ID = domain.LevelID{} },
		"no name":       func(l *domain.Level) { l.Name = "" },
		"blank name":    func(l *domain.Level) { l.Name = "  " },
		"long name":     func(l *domain.Level) { l.Name = strings.Repeat("ä", domain.MaxLevelNameLen+1) },
		"long code":     func(l *domain.Level) { l.Code = strings.Repeat("Ü", domain.MaxLevelCodeLen+1) },
		"zero position": func(l *domain.Level) { l.Position = 0 },
		"negative":      func(l *domain.Level) { l.Position = -1 },
	})
}

func TestLevelLimitsCountRunes(t *testing.T) {
	t.Parallel()

	l := levelFixture()
	// Umlauts count as one character each, so the longest name and code
	// in bytes are still valid.
	l.Name = strings.Repeat("ä", domain.MaxLevelNameLen)
	l.Code = strings.Repeat("Ü", domain.MaxLevelCodeLen)

	if err := l.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	l.Code = ""
	if err := l.Validate(); err != nil {
		t.Fatalf("without a code: Validate() = %v, want nil", err)
	}
}

func TestLevelFieldErrors(t *testing.T) {
	t.Parallel()

	level := func(f func(*domain.Level)) domain.Level { l := levelFixture(); f(&l); return l }

	for name, tc := range map[string]struct {
		level domain.Level
		field string
	}{
		"blank name": {level(func(l *domain.Level) { l.Name = " " }), "name"},
		"long name":  {level(func(l *domain.Level) { l.Name = strings.Repeat("ä", 61) }), "name"},
		"long code":  {level(func(l *domain.Level) { l.Code = strings.Repeat("x", 9) }), "code"},
		"position":   {level(func(l *domain.Level) { l.Position = 0 }), "position"},
	} {
		err := tc.level.Validate()

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, tc.field)
		}
	}
}
