package domain_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// TestFieldErrorsOfJoin: FieldErrors finds every field error of a joined
// error, in order, through wrapping.
func TestFieldErrorsOfJoin(t *testing.T) {
	t.Parallel()

	kind := &domain.FieldError{Field: "kind", Msg: "a room rental without a course type cannot become another kind"}
	date := &domain.FieldError{Field: "date", Msg: "invalid date"}
	room := &domain.FieldError{Field: "roomId", Msg: "lesson room must be set"}

	for name, tc := range map[string]struct {
		err  error
		want []*domain.FieldError
	}{
		"nil":          {nil, nil},
		"no field":     {fmt.Errorf("%w: the lesson has started", domain.ErrValidation), nil},
		"one":          {kind, []*domain.FieldError{kind}},
		"wrapped":      {fmt.Errorf("update lesson: %w", date), []*domain.FieldError{date}},
		"joined":       {errors.Join(kind, nil, date), []*domain.FieldError{kind, date}},
		"wrapped join": {fmt.Errorf("update lesson: %w", errors.Join(kind, date)), []*domain.FieldError{kind, date}},
		// A join inside a join, beside an error without a field, in order.
		"nested": {
			errors.Join(errors.Join(kind, errors.New("other")), fmt.Errorf("x: %w", room), date),
			[]*domain.FieldError{kind, room, date},
		},
		"several %w": {fmt.Errorf("%w and %w", date, room), []*domain.FieldError{date, room}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := domain.FieldErrors(tc.err); !slices.Equal(got, tc.want) {
				t.Errorf("FieldErrors = %v, want %v", got, tc.want)
			}
		})
	}
}
