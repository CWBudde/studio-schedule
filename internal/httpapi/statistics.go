package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func registerStatistics(api huma.API, h *handlers) {
	register(api, AccessOffice, huma.Operation{
		OperationID: "getStatistics",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/statistics",
		Summary:     "Get the statistics",
		Description: fmt.Sprintf("Returns the school's figures of a range of school dates, in the school time zone: "+
			"bookings, attendance, late cancellations, trials and guests, waiting lists and utilization by week "+
			"(by month beyond 26 weeks) and by class, room, course type and instructor, the active members, and "+
			"the pass sales from the receipts, cancellations netted, and the top %d participants by active bookings "+
			"(merged and anonymized persons left out); no other person is named. "+
			"Canceled lessons count only as such. The presets end today; a custom range has at most %d days "+
			"(else a 422). The term range without a term that includes today is a 404.",
			service.TopParticipantsLimit, service.MaxStatisticsDays),
		Tags: []string{"admin"},
	}, h.getStatistics)
}

// StatisticsInput is the request of getStatistics.
type StatisticsInput struct {
	Range string      `default:"last4Weeks" doc:"The range: the current week and the 3 or 11 before it, the current term up to today, or from–to." enum:"last4Weeks,last12Weeks,term,custom" query:"range"`
	From  domain.Date `doc:"First school date of a custom range."                                format:"date" query:"from"`
	To    domain.Date `doc:"Last school date (inclusive) of a custom range."                     format:"date" query:"to"`
}

// LessonFigures are the aggregates of a set of lessons that were not
// canceled.
type LessonFigures struct {
	Lessons           int      `doc:"Lessons held or scheduled (canceled ones left out)."                      json:"lessons"`
	Capacity          int      `doc:"Their places."                                                            json:"capacity"`
	Bookings          int      `doc:"Their active (not canceled) bookings, trials and guests included."         json:"bookings"`
	Utilization       float64  `doc:"Bookings per place (0 without places); above 1 if overbooked."            json:"utilization"`
	Attended          int      `doc:"Bookings marked as attended."                                             json:"attended"`
	NoShows           int      `doc:"Bookings marked as not attended."                                         json:"noShows"`
	NoShowRate        *float64 `doc:"No-shows per booking whose attendance was taken; absent if none was."     json:"noShowRate,omitempty" required:"false"`
	LateCancellations int      `doc:"Bookings canceled after the cancellation cutoff."                         json:"lateCancellations"`
	Trials            int      `doc:"Active trial bookings."                                                   json:"trials"`
	Guests            int      `doc:"Active guest bookings (without a person)."                               json:"guests"`
	WaitlistEntries   int      `doc:"Waiting-list entries, ended ones included: the demand for full lessons." json:"waitlistEntries"`
}

func lessonFigures(c service.LessonCounts) LessonFigures {
	f := LessonFigures{
		Lessons: c.Lessons, Capacity: c.Capacity, Bookings: c.Bookings, Utilization: c.Utilization(),
		Attended: c.Attended, NoShows: c.NoShows, LateCancellations: c.LateCancellations, Trials: c.Trials,
		Guests: c.Guests, WaitlistEntries: c.WaitlistEntries,
	}
	if rate, ok := c.NoShowRate(); ok {
		f.NoShowRate = &rate
	}

	return f
}

// SalesFigures are the pass sales of a range.
type SalesFigures struct {
	PassesSold int   `doc:"Passes sold less sales canceled (Storno), by the receipts' issue dates."    json:"passesSold"`
	SalesGross int64 `doc:"Gross total of the receipts in minor units, cancellations negative."         json:"salesGross"`
}

func salesFigures(c service.SalesCounts) SalesFigures {
	return SalesFigures{PassesSold: c.PassesSold, SalesGross: int64(c.Gross)}
}

// StatisticsTotals are the figures of the whole range.
type StatisticsTotals struct {
	LessonFigures
	SalesFigures

	CanceledLessons int `doc:"Lessons the school canceled; left out of every other figure."          json:"canceledLessons"`
	ActiveMembers   int `doc:"Persons with at least one active booking (guests not counted)."        json:"activeMembers"`
}

// StatisticsPeriod is a week or month of the range.
type StatisticsPeriod struct {
	Start domain.Date `doc:"First school date of the period (cut to the range)."            format:"date" json:"start"`
	End   domain.Date `doc:"Last school date of the period (inclusive, cut to the range)." format:"date" json:"end"`
	LessonFigures
	SalesFigures
}

// ClassStatistics are the figures of a weekly class or a one-off lesson.
type ClassStatistics struct {
	ClassID        domain.ClassID  `doc:"The class; absent for a one-off lesson."                    json:"classId,omitzero"`
	LessonID       domain.LessonID `doc:"The one-off lesson; absent for a class."                    json:"lessonId,omitzero"`
	CourseTypeName string          `doc:"Name of the course type."                                   json:"courseTypeName"`
	Level          string          `doc:"Level of the class; absent for a one-off lesson." enum:"beginner,intermediate,advanced,professional,open" json:"level,omitempty" required:"false"`
	Weekday        *int            `doc:"Day of the week of the class, 0 = Sunday to 6 = Saturday; absent for a one-off lesson." json:"weekday,omitempty" maximum:"6" minimum:"0" required:"false"`
	StartTime      string          `doc:"Start time (HH:MM) of the class in the school time zone; absent for a one-off lesson." json:"startTime,omitempty" required:"false"`
	StartsAt       time.Time       `doc:"Start of the one-off lesson (UTC); absent for a class."     json:"startsAt,omitzero"`
	LessonFigures
}

// RoomStatistics are the figures of a room.
type RoomStatistics struct {
	RoomID       domain.RoomID `doc:"The room."            json:"roomId"`
	Name         string        `doc:"Name of the room."    json:"name"`
	LocationName string        `doc:"Name of its location." json:"locationName"`
	LessonFigures
}

// CourseTypeStatistics are the figures of a course type.
type CourseTypeStatistics struct {
	CourseTypeID domain.CourseTypeID `doc:"The course type."         json:"courseTypeId"`
	Name         string              `doc:"Name of the course type." json:"name"`
	LessonFigures
}

// InstructorStatistics are the figures of the lessons a staff member
// taught as the instructor.
type InstructorStatistics struct {
	StaffID domain.StaffID `doc:"The staff member."              json:"staffId"`
	Name    string         `doc:"Display name of the staff member." json:"name"`
	LessonFigures
}

// ParticipantStatistics are the bookings of a person in the range.
type ParticipantStatistics struct {
	PersonID          domain.PersonID `doc:"The person."                                                  json:"personId"`
	GivenName         string          `doc:"Given name."                                                  json:"givenName"`
	FamilyName        string          `doc:"Family name."                                                 json:"familyName"`
	Bookings          int             `doc:"Active (not canceled) bookings in the range, trials included." json:"bookings"`
	Attended          int             `doc:"Of those, the ones marked as attended."                       json:"attended"`
	LateCancellations int             `doc:"Bookings the person canceled after the cancellation cutoff."  json:"lateCancellations"`
}

// StatisticsTerm names the term of the term range.
type StatisticsTerm struct {
	ID   domain.TermID `doc:"The term."         json:"id"`
	Name string        `doc:"Name of the term." json:"name"`
}

// Statistics is the response body of getStatistics.
type Statistics struct {
	Timezone    string                 `doc:"IANA time zone of the school; school dates are local to it." json:"timezone"`
	Currency    string                 `doc:"ISO 4217 currency of the sales."                              json:"currency"`
	Today       domain.Date            `doc:"Today's school date, where the presets end."                  format:"date" json:"today"`
	Range       string                 `doc:"The range asked for." enum:"last4Weeks,last12Weeks,term,custom" json:"range"`
	From        domain.Date            `doc:"First school date of the range."                              format:"date" json:"from"`
	To          domain.Date            `doc:"Last school date of the range (inclusive)."                   format:"date" json:"to"`
	Term        *StatisticsTerm        `doc:"The term of the term range; absent otherwise."                json:"term,omitempty" required:"false"`
	Granularity string                 `doc:"Length of the periods." enum:"week,month"                     json:"granularity"`
	Totals      StatisticsTotals       `doc:"The figures of the whole range." json:"totals"`
	Periods     []StatisticsPeriod     `doc:"The weeks (Monday to Sunday) or months of the range, in order." json:"periods" nullable:"false"`
	Classes     []ClassStatistics      `doc:"By class, from Monday by start time; the one-off lessons after them by start." json:"classes" nullable:"false"`
	Rooms       []RoomStatistics       `doc:"By room, ordered by location and name." json:"rooms" nullable:"false"`
	CourseTypes []CourseTypeStatistics `doc:"By course type, ordered by name." json:"courseTypes" nullable:"false"`
	Instructors []InstructorStatistics `doc:"By instructor, ordered by name." json:"instructors" nullable:"false"`

	// The only persons named (ADR 0016: never an anonymized one).
	TopParticipants []ParticipantStatistics `doc:"The persons with the most active bookings (at most 20, each with at least one), by bookings (descending), family name and given name. Merged and anonymized persons are left out." json:"topParticipants" nullable:"false"`
}

// StatisticsOutput is the response of getStatistics.
type StatisticsOutput struct {
	Body Statistics
}

func (h *handlers) getStatistics(ctx context.Context, in *StatisticsInput) (*StatisticsOutput, error) {
	st, err := h.deps.Statistics.Statistics(ctx, service.StatisticsQuery{
		Range: service.StatisticsRange(in.Range), From: in.From, To: in.To,
	})

	switch {
	case errors.Is(err, service.ErrNoCurrentTerm):
		return nil, huma.Error404NotFound("no term includes today")
	case err != nil:
		return nil, h.toHTTPError(ctx, err)
	}

	body := Statistics{
		Timezone: st.Timezone, Currency: st.Currency, Today: st.Today, Range: string(st.Range), From: st.From, To: st.To,
		Granularity: string(st.Granularity),
		Totals: StatisticsTotals{
			LessonFigures: lessonFigures(st.Totals.LessonCounts), SalesFigures: salesFigures(st.Totals.SalesCounts),
			CanceledLessons: st.Totals.CanceledLessons, ActiveMembers: st.Totals.ActiveMembers,
		},
		Periods:     make([]StatisticsPeriod, 0, len(st.Periods)),
		Classes:     make([]ClassStatistics, 0, len(st.Classes)),
		Rooms:       make([]RoomStatistics, 0, len(st.Rooms)),
		CourseTypes: make([]CourseTypeStatistics, 0, len(st.CourseTypes)),
		Instructors: make([]InstructorStatistics, 0, len(st.Instructors)),
	}

	if !st.Term.ID.IsZero() {
		body.Term = &StatisticsTerm{ID: st.Term.ID, Name: st.Term.Name}
	}

	for _, p := range st.Periods {
		body.Periods = append(body.Periods, StatisticsPeriod{
			Start: p.Start, End: p.End, LessonFigures: lessonFigures(p.LessonCounts), SalesFigures: salesFigures(p.SalesCounts),
		})
	}

	for _, c := range st.Classes {
		row := ClassStatistics{CourseTypeName: c.CourseType.Name, LessonFigures: lessonFigures(c.LessonCounts)}
		if c.Class.ID.IsZero() {
			row.LessonID, row.StartsAt = c.Lesson.ID, c.Lesson.StartsAt.UTC()
		} else {
			weekday := int(c.Class.Weekday)
			row.ClassID, row.Level, row.Weekday = c.Class.ID, string(c.Class.Level), &weekday
			row.StartTime = c.Class.StartTime.String()
		}

		body.Classes = append(body.Classes, row)
	}

	for _, r := range st.Rooms {
		body.Rooms = append(body.Rooms, RoomStatistics{
			RoomID: r.Room.ID, Name: r.Room.Name, LocationName: r.Location.Name, LessonFigures: lessonFigures(r.LessonCounts),
		})
	}

	for _, c := range st.CourseTypes {
		body.CourseTypes = append(body.CourseTypes, CourseTypeStatistics{
			CourseTypeID: c.CourseType.ID, Name: c.CourseType.Name, LessonFigures: lessonFigures(c.LessonCounts),
		})
	}

	for _, i := range st.Instructors {
		body.Instructors = append(body.Instructors, InstructorStatistics{
			StaffID: i.Staff.ID, Name: i.Staff.DisplayName, LessonFigures: lessonFigures(i.LessonCounts),
		})
	}

	body.TopParticipants = make([]ParticipantStatistics, 0, len(st.TopParticipants))
	for _, p := range st.TopParticipants {
		body.TopParticipants = append(body.TopParticipants, ParticipantStatistics{
			PersonID: p.Person.ID, GivenName: p.Person.GivenName, FamilyName: p.Person.FamilyName,
			Bookings: p.Bookings, Attended: p.Attended, LateCancellations: p.LateCancellations,
		})
	}

	return &StatisticsOutput{Body: body}, nil
}
