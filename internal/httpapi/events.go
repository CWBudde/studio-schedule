package httpapi

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerEvents registers the public and the members' event operations
// (ADR 0019): the published events and their images for everyone, and for
// signed-in members registering their family, canceling before the
// deadline and their registrations (My events).
func registerEvents(api huma.API, h *handlers) {
	tags := []string{"events"}
	read := func(op huma.Operation) huma.Operation {
		op.Tags, op.Method = tags, http.MethodGet

		return op
	}
	write := func(op huma.Operation) huma.Operation {
		op.Tags, op.Method = tags, http.MethodPost

		return withConflict(api, op)
	}

	const events = BasePath + "/events"

	register(api, AccessPublic, read(huma.Operation{
		OperationID: "listEvents",
		Path:        events,
		Summary:     "List upcoming events",
		Description: "Returns the published events with a scheduled session that has not ended, ordered by the " +
			"start of their first session, with their sessions and free places. Drafts and canceled events " +
			"are left out. It names nobody: no registrant and no count but the free places.",
	}), h.listEvents)

	register(api, AccessPublic, read(huma.Operation{
		OperationID: "getEvent",
		Path:        events + "/{slug}",
		Summary:     "Get an event",
		Description: "Returns a published or canceled event by its slug, past ones too, with its sessions " +
			"(canceled ones included) and free places. A draft is a 404, as an unknown slug is.",
	}), h.getEvent)

	register(api, AccessPublic, read(huma.Operation{
		OperationID: "getEventImage",
		Path:        events + "/{slug}/image",
		Summary:     "Event image",
		Description: "Returns the image of a published or canceled event; a 404 without one, for a draft and " +
			"for an unknown slug. Cacheable: with the `ETag` in `If-None-Match` an unchanged image is a 304.",
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The image.",
				Content: map[string]*huma.MediaType{
					domain.BackgroundJPEG: {Schema: &huma.Schema{Type: "string", Format: "binary"}},
					domain.BackgroundPNG:  {Schema: &huma.Schema{Type: "string", Format: "binary"}},
					domain.BackgroundWebP: {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
			"304":     {Description: "The image did not change since the ETag in If-None-Match."},
			"default": problem(api, reflect.TypeFor[ErrorModel](), "Error"),
		},
	}), h.getEventImage)

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "registerForEvent",
		Path:        events + "/{slug}/registrations",
		Summary:     "Register for an event",
		Description: "Registers the signed-in person or one of their dependents for the whole event. A person " +
			"outside the family, a draft and an unknown slug are a 404. An event that is canceled, or whose " +
			"sign-up window is not open (from `signupOpensAt`, or publishing, until `signupClosesAt`, or the " +
			"first session's start), is a 409 `closed`; one whose registrations and open invites fill its " +
			"capacity a 409 `full`; a person registered (or waiting) already a 409 `duplicate`. A free " +
			"registration is paid at once; a priced one is unpaid until the fee is paid at the desk. The " +
			"signed-in account gets a confirmation mail.",
		DefaultStatus: http.StatusCreated,
	}), h.registerForEvent)

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "cancelEventRegistration",
		Path:        BasePath + "/event-registrations/{registrationId}/cancel",
		Summary:     "Cancel a registration",
		Description: "Cancels a registration of the signed-in person or one of their dependents before the " +
			"event's `cancelUntil` (default: the sign-up close); a paid fee is refunded by a Storno of its " +
			"receipt. Anyone else's registration, a guest's included, is a 404. After the deadline it is a " +
			"409 `too-late` (the office still cancels); a canceled registration a 409 `already-canceled`, one " +
			"on the waiting list a 409 `not-registered`; incomplete receipt settings for a refund a 409 " +
			"`receipt-settings-incomplete`. The signed-in account gets a mail.",
	}), h.cancelEventRegistration)

	register(api, AccessSignedIn, read(huma.Operation{
		OperationID: "listMyEvents",
		Path:        BasePath + "/my/events",
		Summary:     "List my events",
		Description: "Returns the registrations of the signed-in person and their dependents, canceled ones " +
			"included, each with its event: open ones first, then by the event's first session. Their " +
			"receipts and Stornos are downloaded with `getMyReceiptPdf`, whoever paid.",
	}), h.listMyEvents)
}

// Event is an event as anyone sees it (ADR 0019): no person data, only its
// free places.
type Event struct {
	Slug           string          `doc:"Slug, used in /events/{slug}." json:"slug"`
	Title          string          `doc:"Title." json:"title"`
	Description    string          `doc:"Description, plain text; absent if none." json:"description,omitempty"`
	CourseType     CourseTypeRef   `doc:"Course type of the event and its sessions." json:"courseType"`
	Capacity       int             `doc:"Places for the whole event." json:"capacity"`
	FreePlaces     int             `doc:"Places left: the capacity minus the registrations with a place and the open invites, at least 0." json:"freePlaces" minimum:"0"`
	Price          int64           `doc:"Gross price in minor units of the school currency; 0 for a free event." json:"price"`
	VATRate        int             `doc:"VAT rate of the price in basis points." json:"vatRate"`
	Status         string          `doc:"published: open for registration within its window; canceled: canceled by the school, it takes no registration." enum:"published,canceled" json:"status"`
	SignupOpensAt  time.Time       `doc:"When registering opens; absent: it is open since publishing." json:"signupOpensAt,omitzero"`
	SignupClosesAt time.Time       `doc:"When registering closes: the event's own close, else the first scheduled session's start; absent without a scheduled session." json:"signupClosesAt,omitzero"`
	CancelUntil    time.Time       `doc:"Until when members cancel their own registrations: the event's own deadline, else the sign-up close; absent without one." json:"cancelUntil,omitzero"`
	SignupOpen     bool            `doc:"Whether registering is possible now: published and within the window, full or not." json:"signupOpen"`
	HasImage       bool            `doc:"Whether getEventImage has an image." json:"hasImage"`
	Sessions       []LessonSummary `doc:"The sessions, canceled ones included, ordered by start." json:"sessions" nullable:"false"`
}

// NewEvent is the public document of v.
func NewEvent(v service.PublicEvent) Event {
	e := v.Event
	out := Event{
		Slug: e.Slug, Title: e.Title, Description: e.Description,
		CourseType: CourseTypeRef{ID: v.CourseType.ID, Name: v.CourseType.Name}, Capacity: e.Capacity,
		FreePlaces: v.FreePlaces, Price: int64(e.Price), VATRate: e.VATRate, Status: string(e.Status),
		SignupOpensAt: e.SignupOpensAt.UTC(), SignupClosesAt: v.SignupClosesAt.UTC(), CancelUntil: v.CancelUntil.UTC(),
		SignupOpen: v.SignupOpen, HasImage: v.HasImage, Sessions: make([]LessonSummary, len(v.Sessions)),
	}

	for i, sl := range v.Sessions {
		out.Sessions[i] = lessonSummary(sl)
	}

	return out
}

// EventList is the response body of listEvents.
type EventList struct {
	Items []Event `json:"items" nullable:"false"`
}

// EventListOutput is the response of listEvents.
type EventListOutput struct {
	Body EventList
}

// EventOutput is the response of getEvent.
type EventOutput struct {
	Body Event
}

// EventSlugInput names an event by its slug.
type EventSlugInput struct {
	Slug string `doc:"The event's slug." maxLength:"80" path:"slug"`
}

// EventSlugImageInput is the request of getEventImage.
type EventSlugImageInput struct {
	Slug        string `doc:"The event's slug." maxLength:"80" path:"slug"`
	IfNoneMatch string `doc:"The ETag of the image the client has; unchanged: a 304." header:"If-None-Match"`
}

// RegisterForEventInput is the request of registerForEvent.
type RegisterForEventInput struct {
	Slug string `doc:"The event's slug." maxLength:"80" path:"slug"`
	Body struct {
		PersonID domain.PersonID `doc:"The signed-in person or one of their dependents." json:"personId"`
	}
}

// EventRegistrationRefInput names a registration.
type EventRegistrationRefInput struct {
	RegistrationID domain.EventRegistrationID `doc:"The registration." path:"registrationId"`
}

// MyEventRegistration is a registration of the caller's family.
type MyEventRegistration struct {
	ID                    domain.EventRegistrationID `doc:"Registration ID." json:"id"`
	Person                PassPerson                 `doc:"The registered person." json:"person"`
	Status                string                     `doc:"waiting and invited: on the waiting list; registered: has a place; canceled." enum:"waiting,invited,registered,canceled" json:"status"`
	Payment               string                     `doc:"unpaid: the fee is owed at the desk; paid: paid with a receipt, or free; refunded: the receipt was canceled by a Storno." enum:"unpaid,paid,refunded" json:"payment"`
	Price                 int64                      `doc:"The gross price when the registration was made, in minor units." json:"price"`
	ReceiptID             domain.ReceiptID           `doc:"The receipt of the payment, for getMyReceiptPdf; absent if unpaid or free." json:"receiptId,omitzero"`
	CancellationReceiptID domain.ReceiptID           `doc:"The Storno of a refund, for getMyReceiptPdf." json:"cancellationReceiptId,omitzero"`
	Cancelable            bool                       `doc:"Whether cancelEventRegistration cancels it now: registered, and before the event's cancelUntil." json:"cancelable"`
	CreatedAt             time.Time                  `doc:"When it was made." json:"createdAt"`
	CanceledAt            time.Time                  `doc:"When it was canceled; only if canceled." json:"canceledAt,omitzero"`
}

// MyEvent is a registration of the caller's family with its event.
type MyEvent struct {
	Registration MyEventRegistration `json:"registration"`
	Event        Event               `json:"event"`
}

// NewMyEvent is the document of v.
func NewMyEvent(v service.MyEvent) MyEvent {
	r := v.Registration.Registration

	return MyEvent{
		Registration: MyEventRegistration{
			ID: r.ID, Person: PassPerson{ID: r.PersonID, Name: v.Registration.Name, Anonymized: v.Registration.Anonymized},
			Status: string(r.Status), Payment: string(r.Payment), Price: int64(r.Price), ReceiptID: r.ReceiptID,
			CancellationReceiptID: r.CancellationReceiptID, Cancelable: v.Cancelable, CreatedAt: r.CreatedAt.UTC(),
			CanceledAt: r.CanceledAt.UTC(),
		},
		Event: NewEvent(v.Event),
	}
}

// MyEventOutput is the response of registerForEvent and
// cancelEventRegistration.
type MyEventOutput struct {
	Body MyEvent
}

// MyEventList is the response body of listMyEvents.
type MyEventList struct {
	Items []MyEvent `json:"items" nullable:"false"`
}

// MyEventListOutput is the response of listMyEvents.
type MyEventListOutput struct {
	Body MyEventList
}

func (h *handlers) listEvents(ctx context.Context, _ *struct{}) (*EventListOutput, error) {
	events, err := h.deps.Events.Published(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &EventListOutput{Body: EventList{Items: make([]Event, len(events))}}
	for i, e := range events {
		out.Body.Items[i] = NewEvent(e)
	}

	return out, nil
}

func (h *handlers) getEvent(ctx context.Context, in *EventSlugInput) (*EventOutput, error) {
	e, err := h.deps.Events.BySlug(ctx, in.Slug)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &EventOutput{Body: NewEvent(e)}, nil
}

func (h *handlers) getEventImage(ctx context.Context, in *EventSlugImageInput) (*SchoolLogoOutput, error) {
	img, err := h.deps.Events.PublicImage(ctx, in.Slug)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return imageOutput(img, in.IfNoneMatch)
}

func (h *handlers) registerForEvent(ctx context.Context, in *RegisterForEventInput) (*MyEventOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	id, err := h.deps.Events.PublicEventID(ctx, in.Slug)
	if err != nil {
		return nil, h.memberEventError(ctx, err)
	}

	return h.myEventOutput(ctx)(h.deps.Events.Register(ctx, p, id, in.Body.PersonID))
}

func (h *handlers) cancelEventRegistration(ctx context.Context, in *EventRegistrationRefInput) (*MyEventOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	return h.myEventOutput(ctx)(h.deps.Events.CancelOwn(ctx, p, in.RegistrationID))
}

func (h *handlers) listMyEvents(ctx context.Context, _ *struct{}) (*MyEventListOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	mine, err := h.deps.Events.Mine(ctx, p)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &MyEventListOutput{Body: MyEventList{Items: make([]MyEvent, len(mine))}}
	for i, m := range mine {
		out.Body.Items[i] = NewMyEvent(m)
	}

	return out, nil
}

func (h *handlers) myEventOutput(ctx context.Context) func(service.MyEvent, error) (*MyEventOutput, error) {
	return func(m service.MyEvent, err error) (*MyEventOutput, error) {
		if err != nil {
			return nil, h.memberEventError(ctx, err)
		}

		return &MyEventOutput{Body: NewMyEvent(m)}, nil
	}
}

// memberEventError maps the members' too-late, then the conflicts of the
// event administration (eventError).
func (h *handlers) memberEventError(ctx context.Context, err error) error {
	if errors.Is(err, service.ErrTooLate) {
		return conflict(reasonTooLate, "the cancellation deadline has passed")
	}

	return h.eventError(ctx, err)
}
