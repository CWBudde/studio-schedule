package httpapi

import (
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/requestid"
)

// Reasons of a Conflict (ADR 0007, ADR 0008, ADR 0009, ADR 0010, ADR 0012, ADR 0014, ADR 0015, ADR 0016;
// those of ADR 0013 are in settings.go).
const (
	reasonInUse           = "in-use"
	reasonDuplicate       = "duplicate"
	reasonManaged         = "managed"
	reasonHasAccount      = "has-account"
	reasonRoomTaken       = "room-taken"
	reasonPast            = "past"
	reasonGenerated       = "generated"
	reasonAlreadyBooked   = "already-booked"
	reasonFull            = "full"
	reasonClosed          = "closed"
	reasonAlreadyCanceled = "already-canceled"
	reasonNotFull         = "not-full"
	reasonAlreadyWaiting  = "already-waiting"
	reasonNotInvited      = "not-invited"
	reasonInviteExpired   = "invite-expired"
	reasonInviteUsed      = "invite-used"
	reasonNotWaiting      = "not-waiting"
	reasonAlreadyVoided   = "already-voided"
	reasonReceiptSettings = "receipt-settings-incomplete"
	reasonPassUsed        = "pass-used"
	reasonCancellation    = "cancellation"
	reasonMailLimit       = "mail-limit"
	reasonExpired         = "expired"
	reasonNotFailed       = "not-failed"
	reasonSecretLink      = "secret-link"
	reasonTwoAccounts     = "two-accounts"
	reasonGuardianCycle   = "guardian-cycle"
	reasonAnonymized      = "anonymized"
	reasonSoleGuardian    = "sole-guardian"
	reasonLastAdmin       = "last-admin"
	reasonOwnAccount      = "own-account"
)

// ErrorModel is the problem document (RFC 9457) of every error response:
// Huma's, plus the ID of the request, which support can look up in the logs.
// It keeps Huma's schema name, so the OpenAPI document and the generated
// client types name it as before.
type ErrorModel struct {
	huma.ErrorModel

	RequestID string `doc:"The ID of the request (the X-Request-Id response header), for finding it in the server's logs." json:"requestId,omitempty"`
}

func (e *ErrorModel) setRequestID(id string) { e.RequestID = id }

// dropValues removes the value of every error detail: a problem document
// never echoes what the client sent, a password least of all.
func (e *ErrorModel) dropValues() {
	for _, d := range e.Errors {
		if d != nil {
			d.Value = nil
		}
	}
}

// NewProblem is the problem document for status with detail msg and the
// details of errs, as huma.NewError builds it.
func NewProblem(status int, msg string, errs ...error) *ErrorModel {
	details := make([]*huma.ErrorDetail, 0, len(errs))

	for _, err := range errs {
		// Like huma.NewError: only the error itself, not one it wraps,
		// gives its detail.
		switch e := err.(type) { //nolint:errorlint // see above
		case nil:
		case huma.ErrorDetailer:
			details = append(details, e.ErrorDetail())
		default:
			details = append(details, &huma.ErrorDetail{Message: e.Error()})
		}
	}

	return &ErrorModel{ErrorModel: huma.ErrorModel{
		Status: status, Title: http.StatusText(status), Detail: msg, Errors: details,
	}}
}

// Every error Huma creates, from the handlers' huma.Error404NotFound to its
// own validation errors, is an ErrorModel, and the generated responses
// document it (Huma takes the error schema from NewError). The request ID
// is stamped on, and the details' values dropped, by finishProblem.
func init() { //nolint:gochecknoinits // Huma's documented way to replace its error type
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return NewProblem(status, msg, errs...)
	}
}

// finishProblem is a Huma transformer that puts the ID of the request
// (internal/requestid) into every problem document and drops the values of
// its error details (Huma's validation echoes the offending input there).
func finishProblem(ctx huma.Context, _ string, v any) (any, error) {
	p, ok := v.(interface {
		setRequestID(id string)
		dropValues()
	})
	if !ok {
		return v, nil
	}

	if id := requestid.From(ctx.Context()); id != "" {
		p.setRequestID(id)
	}

	p.dropValues()

	return v, nil
}

// Conflict is the problem document of a 409 the UI can explain: Reason
// says why, without internals (ADR 0007).
type Conflict struct {
	ErrorModel

	Reason string          `doc:"in-use: something still refers to the record; duplicate: the record, or a value that must be unique such as a name, exists already; managed: a dependent with its own account, a staff link or dependents of its own, which its guardians may not edit; has-account: the person has an account already; room-taken: another lesson uses the room at that time (see lesson), the request may be repeated with confirmRoomOverlap; past: the lesson would start in the past, the request may be repeated with confirmPast; generated: a lesson of a class is canceled, not deleted; already-booked: the person has an active booking for the lesson; full: the active bookings and the places reserved for the waiting list fill the lesson's capacity, or the members fill the class's capacity; closed: the lesson cannot be booked (see why); already-canceled: the booking, or the receipt, is canceled already; not-full: the lesson has a free place, so it is booked rather than waited for; already-waiting: the person is on the lesson's waiting list already; not-invited: the waiting-list entry has no invite to accept or decline; invite-expired: the invite ran out, was declined, or the lesson started; invite-used: the invite was accepted, or its entry left the list; not-waiting: the entry is no longer on the waiting list; already-voided: the pass is voided already; receipt-settings-incomplete: receipts cannot be issued, so passes not sold, until an admin completes the receipt settings (the detail names what is missing); pass-used: the pass of the receipt paid for visits, so its sale cannot be canceled (void the pass instead); cancellation: the receipt is a cancellation, which cannot be canceled; timezone-locked: lessons exist, so the school's time zone cannot change; currency-locked: prices or receipts exist, so the currency cannot change; mail-limit: the school day has as many emailed announcements as allowed (sent and scheduled); expired: the announcement has expired, so it is no longer edited (it can be deleted); not-failed: only a failed mail is queued again; secret-link: a mail with a secret link (a confirmation, password reset, invite or waiting-list invite) is not queued again, the flow that sends a new link is used instead; two-accounts: both persons of a merge have an account, so one is disabled first; guardian-cycle: after the merge a guardian of the survivor would also be its dependent; anonymized: the person is anonymized already; sole-guardian: the person is the only guardian of a dependent, so another guardian is linked or the link removed first; last-admin: the account is the last admin who can sign in (enabled and confirmed), which is not disabled, demoted or deleted; own-account: an admin does not disable, demote or delete their own account (another admin does)." enum:"in-use,duplicate,managed,has-account,room-taken,past,generated,already-booked,full,closed,already-canceled,not-full,already-waiting,not-invited,invite-expired,invite-used,not-waiting,already-voided,receipt-settings-incomplete,pass-used,cancellation,timezone-locked,currency-locked,mail-limit,expired,not-failed,secret-link,two-accounts,guardian-cycle,anonymized,sole-guardian,last-admin,own-account" json:"reason"`
	Lesson *ConflictLesson `doc:"The lesson that uses the room; only with reason room-taken." json:"lesson,omitempty"`
	Why    string          `doc:"Why the lesson cannot be booked; only with reason closed. cutoff: the booking cutoff before the start has passed; release: members may not book the lesson yet, or at all (staff book under member rules); canceled: the school canceled the lesson; started: the lesson has started or is over; rental: the lesson is a room rental, which takes no bookings." enum:"cutoff,release,canceled,started,rental" json:"why,omitempty"`
}

// Reasons of an Invalid request (ADR 0009, amendment).
const (
	reasonNotStarted = "not-started"
	reasonDayOver    = "day-over"
)

// Invalid is the problem document of a 422 about the state of the target
// rather than the request body, which the UI can explain without its own
// clock: Reason says why. A 422 about the body has no reason.
type Invalid struct {
	ErrorModel

	Reason string `doc:"Why the target cannot be changed now; absent for a 422 about the request body. not-started: the lesson has not started, so attendance cannot be taken yet; day-over: the lesson's school day is over, so its instructor and accompanist no longer change its participants (the office does)." enum:"not-started,day-over" json:"reason,omitempty"`
}

// invalid is the 422 Invalid with reason, its message located at the path
// parameter at.
func invalid(reason, msg, at string) *Invalid {
	return &Invalid{
		ErrorModel: ErrorModel{ErrorModel: huma.ErrorModel{
			Title: http.StatusText(http.StatusUnprocessableEntity), Status: http.StatusUnprocessableEntity,
			Detail: "validation failed", Errors: []*huma.ErrorDetail{{Message: msg, Location: at}},
		}},
		Reason: reason,
	}
}

// ConflictLesson names the lesson of a room-taken conflict.
type ConflictLesson struct {
	ID         domain.LessonID `doc:"Lesson ID."          json:"id"`
	StartsAt   time.Time       `doc:"Start instant (UTC)." json:"startsAt"`
	EndsAt     time.Time       `doc:"End instant (UTC)."   json:"endsAt"`
	CourseType *CourseTypeRef  `doc:"Course type; absent for a room rental without one." json:"courseType,omitempty"`
}

func conflict(reason, detail string) *Conflict {
	return &Conflict{
		ErrorModel: ErrorModel{ErrorModel: huma.ErrorModel{
			Title: http.StatusText(http.StatusConflict), Status: http.StatusConflict, Detail: detail,
		}},
		Reason: reason,
	}
}

// withConflict documents the Conflict of an operation (a write, or a read
// that answers one, like previewCancelFollowing) as its 409 and every other
// error as the default response, which Huma would otherwise drop once an
// operation lists a response of its own.
func withConflict(api huma.API, op huma.Operation) huma.Operation {
	op.Responses = map[string]*huma.Response{
		"409":     problem(api, reflect.TypeFor[Conflict](), http.StatusText(http.StatusConflict)),
		"default": problem(api, reflect.TypeFor[ErrorModel](), "Error"),
	}

	return op
}

// withInvalid documents the Invalid of an operation as its 422, on top of
// withConflict.
func withInvalid(api huma.API, op huma.Operation) huma.Operation {
	op = withConflict(api, op)
	op.Responses["422"] = problem(api, reflect.TypeFor[Invalid](), http.StatusText(http.StatusUnprocessableEntity))

	return op
}

// problem is a problem+json response with the schema of t.
func problem(api huma.API, t reflect.Type, description string) *huma.Response {
	return &huma.Response{
		Description: description,
		Content: map[string]*huma.MediaType{
			"application/problem+json": {Schema: api.OpenAPI().Components.Schemas.Schema(t, true, t.Name())},
		},
	}
}
