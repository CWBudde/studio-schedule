package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// scheduleWeek is how many school dates the schedule tool covers without
// to: a week from from.
const scheduleWeek = 7

func scheduleTools() []tool {
	return []tool{{
		name: "schedule",
		description: "The lessons that start from one school date through another, as the account sees the " +
			"schedule (getSchedule; `lessons list --from --to` for the office): members see the lessons " +
			"released to them, staff every lesson, hidden and canceled ones included. Each lesson has its " +
			"time (UTC instants), course type, audience, room, location, instructor, capacity, status and " +
			"free places. Without --personal-data the server leaves out whom of the account's family a " +
			"lesson has booked or waitlisted, and a room rental's renter. At most " +
			strconv.Itoa(service.MaxScheduleDays) + " days.",
		access:      httpapi.AccessPublic,
		annotations: readOnly("Schedule"),
		add:         handler(schedule),
	}}
}

// scheduleInput are the arguments of the schedule tool.
type scheduleInput struct {
	From string `json:"from,omitempty" jsonschema:"The first school date (YYYY-MM-DD); default today in the school's time zone."`
	To   string `json:"to,omitempty"   jsonschema:"The last school date, inclusive (YYYY-MM-DD); default 6 days after from."`

	Location   string `json:"location,omitempty"   jsonschema:"Only lessons at this location (its ID)."`
	CourseType string `json:"courseType,omitempty" jsonschema:"Only lessons of this course type (its ID)."`
	Level      string `json:"level,omitempty"      jsonschema:"Only lessons of classes for this level (its ID): a level span that includes it, or all levels."`
	Instructor string `json:"instructor,omitempty" jsonschema:"Only lessons with this staff member (its ID) as instructor or accompanist."`
}

// schedule is the schedule tool: the body of getSchedule for p.
func schedule(ctx context.Context, s *Server, p service.Principal, in scheduleInput) (any, error) {
	q, err := s.scheduleQuery(ctx, in)
	if err != nil {
		return nil, err
	}

	sch, err := s.svc.Schedule.Schedule(ctx, p, q)
	if err != nil {
		return nil, err
	}

	body := httpapi.NewSchedule(sch)
	if !s.opts.PersonalData {
		// Person data, left out without the flag (ADR 0022): whom of the
		// family a lesson has booked or waitlisted, and a rental's renter.
		// The instructors stay, as the public schedule shows them to
		// anonymous visitors. The fields are omitempty, so their keys go.
		for i := range body.Lessons {
			l := &body.Lessons[i]
			l.BookedFor, l.Waitlist, l.Renter = nil, nil, ""
		}
	}

	return body, nil
}

// scheduleQuery is the query of in: from through to (inclusive), from
// today and a week by default, and the filters.
func (s *Server) scheduleQuery(ctx context.Context, in scheduleInput) (service.ScheduleQuery, error) {
	var (
		q    service.ScheduleQuery
		errs [4]error
	)

	q.Location, errs[0] = parseID[domain.LocationID]("location", in.Location)
	q.CourseType, errs[1] = parseID[domain.CourseTypeID]("courseType", in.CourseType)
	q.Level, errs[2] = parseID[domain.LevelID]("level", in.Level)
	q.Instructor, errs[3] = parseID[domain.StaffID]("instructor", in.Instructor)

	if err := errors.Join(errs[:]...); err != nil {
		return q, err
	}

	from, err := parseDate("from", in.From)
	if err != nil {
		return q, err
	}

	to, err := parseDate("to", in.To)
	if err != nil {
		return q, err
	}

	if from.IsZero() {
		if from, err = s.today(ctx); err != nil {
			return q, err
		}
	}

	if to.IsZero() {
		to = from.AddDays(scheduleWeek - 1)
	}

	// The service checks the range: to after from, at most MaxScheduleDays.
	q.From, q.To = from, to.AddDays(1)

	return q, nil
}

// today is the school date of now in the school's time zone; UTC without
// school settings, whose schedule the service then refuses.
func (s *Server) today(ctx context.Context) (domain.Date, error) {
	loc := time.UTC

	settings, err := s.svc.Settings.Public(ctx)

	switch {
	case errors.Is(err, domain.ErrNotFound):
	case err != nil:
		return domain.Date{}, err
	default:
		if loc, err = time.LoadLocation(settings.Timezone); err != nil {
			return domain.Date{}, fmt.Errorf("the school's time zone: %w", err)
		}
	}

	return domain.DateOf(s.clk.Now(), loc), nil
}

// parseID parses the ID argument field; empty is the zero ID. The error
// names the field, not the value.
func parseID[I domain.AnyID](field, value string) (I, error) {
	if value == "" {
		return I{}, nil
	}

	id, err := domain.ParseID[I](value)
	if err != nil {
		return I{}, &domain.FieldError{Field: field, Msg: field + " is not an ID"}
	}

	return id, nil
}

// parseDate parses the school date argument field (YYYY-MM-DD); empty is
// the zero date. The error names the field, not the value.
func parseDate(field, value string) (domain.Date, error) {
	if value == "" {
		return domain.Date{}, nil
	}

	d, err := domain.ParseDate(value)
	if err != nil {
		return domain.Date{}, &domain.FieldError{Field: field, Msg: field + " is not a date (YYYY-MM-DD)"}
	}

	return d, nil
}
