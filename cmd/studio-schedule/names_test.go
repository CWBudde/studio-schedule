package main

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// TestAudienceText: the levels (a span with an en dash, one level by its
// name, or all levels), the stage and the ages in whole and half years,
// rounded down to the half, joined by " · "; nothing stated is "".
func TestAudienceText(t *testing.T) {
	t.Parallel()

	src := ids.NewSequence()
	beginner := domain.Level{ID: ids.New[domain.LevelID](src), Name: "Beginner", Position: 1}
	intermediate := domain.Level{ID: ids.New[domain.LevelID](src), Name: "Intermediate", Position: 2}

	for name, tc := range map[string]struct {
		audience service.Audience
		want     string
	}{
		"nothing":    {service.Audience{}, ""},
		"one level":  {service.Audience{Low: beginner, High: beginner}, "Beginner"},
		"a span":     {service.Audience{Low: beginner, High: intermediate}, "Beginner–Intermediate"},
		"all levels": {service.Audience{AllLevels: true}, "all levels"},
		"stage only": {service.Audience{Stage: 3}, "stage 3"},
		"ages":       {service.Audience{AgeFrom: 48, AgeTo: 72}, "4–6 yrs"},
		"from":       {service.Audience{AgeFrom: 42}, "from 3½ yrs"},
		"up to":      {service.Audience{AgeTo: 72}, "up to 6 yrs"},
		"below half": {service.Audience{AgeFrom: 40}, "from 3 yrs"},
		"past half":  {service.Audience{AgeFrom: 45}, "from 3½ yrs"},
		"a baby":     {service.Audience{AgeTo: 6}, "up to ½ yrs"},
		"everything": {
			service.Audience{Low: beginner, High: intermediate, Stage: 2, AgeFrom: 48, AgeTo: 72},
			"Beginner–Intermediate · stage 2 · 4–6 yrs",
		},
		"all levels with ages": {service.Audience{AllLevels: true, AgeFrom: 144}, "all levels · from 12 yrs"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := audienceText(tc.audience); got != tc.want {
				t.Errorf("audienceText(%+v) = %q, want %q", tc.audience, got, tc.want)
			}
		})
	}
}

// TestWithAudience: the audience follows the name in parentheses, and a
// name without one stands alone.
func TestWithAudience(t *testing.T) {
	t.Parallel()

	if got := withAudience("Contemporary", service.Audience{AllLevels: true}); got != "Contemporary (all levels)" {
		t.Errorf("withAudience = %q", got)
	}

	if got := withAudience("Contemporary", service.Audience{}); got != "Contemporary" {
		t.Errorf("withAudience without an audience = %q", got)
	}
}
