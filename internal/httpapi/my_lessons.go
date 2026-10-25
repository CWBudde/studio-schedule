package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerMyLessons registers "My lessons" (ADR 0009, amendment): the
// bookings and waiting-list entries of the signed-in account's family.
func registerMyLessons(api huma.API, h *handlers) {
	register(api, AccessSignedIn, huma.Operation{
		OperationID: "listMyLessons",
		Method:      http.MethodGet,
		Path:        BasePath + "/my/lessons",
		Summary:     "List my upcoming lessons",
		Description: "Returns the active bookings of the signed-in person and their dependents (managed ones " +
			"included) in lessons that are not over, by start, and their open waiting-list entries of lessons " +
			"that have not started, with their place in the list or their invite. A booking is listed whatever " +
			"its lesson's visibility: a hidden or unreleased lesson the family is booked for is still theirs. " +
			"Canceled bookings are not listed here; the history has them once their lessons are over.",
		Tags: []string{"bookings"},
	}, h.listMyLessons)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "listMyLessonHistory",
		Method:      http.MethodGet,
		Path:        BasePath + "/my/lessons/history",
		Summary:     "List my past lessons",
		Description: "Returns one page of the bookings of the signed-in person and their dependents, canceled " +
			"ones included, in lessons that are over: those that started in the " +
			strconv.Itoa(service.HistoryMonths) + " calendar months (in the school time " +
			"zone) before `before` (default and at most: now), the newest first. The next page is `before` = " +
			"`nextBefore`, as long as `hasMore` says there are older bookings; a page in between may be empty.",
		Tags: []string{"bookings"},
	}, h.listMyLessonHistory)
}

// MyLessonBooking is a booking of the caller's family with its lesson.
type MyLessonBooking struct {
	Booking    Booking       `json:"booking"`
	PersonName string        `doc:"Given and family name of the booked person." json:"personName"`
	Lesson     LessonSummary `json:"lesson"`
	// The cutoffs, as in getSchedule.
	LateCancelFrom time.Time      `doc:"From this instant on a cancellation is late: the cancellation cutoff before the start (UTC); absent for a lesson the school canceled, which is never canceled late." json:"lateCancelFrom,omitzero"`
	CanCancel      bool           `doc:"Whether the booking may be canceled now (cancelBooking): it is active and its lesson has not started." json:"canCancel"`
	Payment        BookingPayment `doc:"How the booking is paid." json:"payment"`
}

// MyWaitlistEntry is a waiting-list entry of the caller's family with its
// lesson.
type MyWaitlistEntry struct {
	Entry           WaitlistFor   `json:"entry"`
	Lesson          LessonSummary `json:"lesson"`
	BookingClosesAt time.Time     `doc:"Booking closes at this instant (UTC); no invites go out from then on." json:"bookingClosesAt"`
}

// MyLessons is the response body of listMyLessons.
type MyLessons struct {
	Timezone string            `doc:"IANA time zone of the school; school dates and times are local to it." json:"timezone"`
	Now      time.Time         `doc:"Server time the response was computed at."                             json:"now"`
	Upcoming []MyLessonBooking `doc:"Active bookings of lessons that are not over, by the lessons' start, then by booking time." json:"upcoming" nullable:"false"`
	Waitlist []MyWaitlistEntry `doc:"Open waiting-list entries of lessons that have not started, by the lessons' start, the signed-in person first." json:"waitlist" nullable:"false"`
}

// MyLessonsOutput is the response of listMyLessons.
type MyLessonsOutput struct {
	Body MyLessons
}

// MyLessonHistoryInput are the parameters of listMyLessonHistory.
type MyLessonHistoryInput struct {
	Before time.Time `doc:"End of the page (exclusive): it holds the lessons that started in the months before this instant; default and at most: now." format:"date-time" query:"before"`
}

// MyLessonHistory is the response body of listMyLessonHistory.
type MyLessonHistory struct {
	Timezone   string            `doc:"IANA time zone of the school; school dates and times are local to it." json:"timezone"`
	Now        time.Time         `doc:"Server time the response was computed at."                             json:"now"`
	Before     time.Time         `doc:"End of the page (exclusive): the before asked for, at most now."        json:"before"`
	Items      []MyLessonBooking `doc:"Bookings of lessons that are over and started in [nextBefore, before), every status, the newest lessons first." json:"items" nullable:"false"`
	NextBefore time.Time         `doc:"Start of the page (inclusive; the operation says how many months a page covers); the before of the next page." json:"nextBefore"`
	HasMore    bool              `doc:"Whether there are bookings of lessons that started before nextBefore."   json:"hasMore"`
}

// MyLessonHistoryOutput is the response of listMyLessonHistory.
type MyLessonHistoryOutput struct {
	Body MyLessonHistory
}

func (h *handlers) listMyLessons(ctx context.Context, _ *struct{}) (*MyLessonsOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	ml, err := h.deps.MyLessons.Upcoming(ctx, p)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	body := MyLessons{
		Timezone: ml.Timezone, Now: ml.Now.UTC(), Upcoming: myLessonBookings(ml.Upcoming),
		Waitlist: make([]MyWaitlistEntry, 0, len(ml.Waitlist)),
	}

	for _, w := range ml.Waitlist {
		body.Waitlist = append(body.Waitlist, MyWaitlistEntry{
			Entry: waitlistForOne(w.WaitingPerson), Lesson: lessonSummary(w.Lesson),
			BookingClosesAt: w.Lesson.BookingClosesAt.UTC(),
		})
	}

	return &MyLessonsOutput{Body: body}, nil
}

func (h *handlers) listMyLessonHistory(ctx context.Context, in *MyLessonHistoryInput) (*MyLessonHistoryOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	hist, err := h.deps.MyLessons.History(ctx, p, in.Before)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &MyLessonHistoryOutput{Body: MyLessonHistory{
		Timezone: hist.Timezone, Now: hist.Now.UTC(), Before: hist.Before.UTC(), Items: myLessonBookings(hist.Items),
		NextBefore: hist.From.UTC(), HasMore: hist.HasMore,
	}}, nil
}

func myLessonBookings(items []service.MyBooking) []MyLessonBooking {
	out := make([]MyLessonBooking, 0, len(items))

	for _, it := range items {
		out = append(out, MyLessonBooking{
			Booking: bookingBody(it.Booking), PersonName: it.Name, Lesson: lessonSummary(it.Lesson),
			LateCancelFrom: it.Lesson.LateCancelFrom.UTC(), CanCancel: it.Cancelable, Payment: paymentBody(it.Payment),
		})
	}

	return out
}
