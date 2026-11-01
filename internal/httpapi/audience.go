package httpapi

import (
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// Audience is who a class is for (ADR 0021), embedded in a class, a lesson
// and a class's statistics row. Each field is absent when the class does not
// state it; a lesson without a class has none.
type Audience struct {
	LevelFrom     *LevelRef `doc:"Lower end (by position) of the levels the class is for; absent without a level." json:"levelFrom,omitempty"`
	LevelTo       *LevelRef `doc:"Upper end of the levels the class is for, the same as levelFrom for one level; absent without a level." json:"levelTo,omitempty"`
	AllLevels     bool      `doc:"Whether the class is for every level; never with levelFrom." json:"allLevels,omitempty"`
	Stage         int       `doc:"Stage of the class, from 1."                                 json:"stage,omitempty"         maximum:"9"    minimum:"1"`
	AgeFromMonths int       `doc:"Youngest age the class is for, in months."                   json:"ageFromMonths,omitempty" maximum:"1200" minimum:"1"`
	AgeToMonths   int       `doc:"Oldest age the class is for, in months."                     json:"ageToMonths,omitempty"   maximum:"1200" minimum:"1"`
}

// LevelRef names a level of the school's scale.
type LevelRef struct {
	ID   domain.LevelID `json:"id"`
	Name string         `json:"name"`
	Code string         `doc:"Short label, if any." json:"code,omitempty"`
}

// NewAudience is the API's form of a class's audience; the command line's
// JSON output uses it too (ADR 0020).
func NewAudience(a service.Audience) Audience {
	out := Audience{
		AllLevels: a.AllLevels, Stage: a.Stage, AgeFromMonths: int(a.AgeFrom), AgeToMonths: int(a.AgeTo),
	}

	if !a.Low.ID.IsZero() {
		out.LevelFrom, out.LevelTo = levelRef(a.Low), levelRef(a.High)
	}

	return out
}

func levelRef(l domain.Level) *LevelRef {
	return &LevelRef{ID: l.ID, Name: l.Name, Code: l.Code}
}
