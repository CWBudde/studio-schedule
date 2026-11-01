package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerEventAdmin registers the office's event administration (ADR
// 0019): events, their image and sessions, registrations at the desk, the
// fees taken there and refunds.
func registerEventAdmin(api huma.API, h *handlers) {
	tags := []string{"events"}
	write := func(op huma.Operation) huma.Operation {
		op.Tags = tags

		return withConflict(api, op)
	}
	read := func(op huma.Operation) huma.Operation {
		op.Tags, op.Method = tags, http.MethodGet

		return op
	}

	const events = BasePath + "/admin/events"

	register(api, AccessOffice, read(huma.Operation{
		OperationID: "listAdminEvents",
		Path:        events,
		Summary:     "List events",
		Description: "Returns every event, drafts and canceled ones included, ordered by the start of its first " +
			"session (events without one last), with its sessions and the places taken now.",
	}), h.listAdminEvents)

	register(api, AccessOffice, read(huma.Operation{
		OperationID: "getAdminEvent",
		Path:        events + "/{eventId}",
		Summary:     "Get an event",
		Description: "Returns an event as the office sees it, with its sessions, the places taken now and its " +
			"image's type and digest.",
	}), h.getAdminEvent)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "createEvent",
		Method:      http.MethodPost,
		Path:        events,
		Summary:     "Create an event",
		Description: "Adds a draft event, which only the office sees. Without a slug the title's is taken (lowercase, " +
			"umlauts spelled out, other accents dropped, everything else a hyphen), numbered (-2, -3, …) while " +
			"another event has it; a title that gives none is a 422 on `body.slug`. A slug that is given and " +
			"taken is a 409 `duplicate`. An unknown course type is a 422 on `body.courseTypeId`.",
		DefaultStatus: http.StatusCreated,
	}), h.createEvent)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateEvent",
		Method:      http.MethodPut,
		Path:        events + "/{eventId}",
		Summary:     "Update an event",
		Description: "Replaces the editable fields of an event; its status stays, and without a slug it keeps its " +
			"own. A new capacity is written to every session that has not started, canceled ones included; " +
			"lowering it below the registrations and open invites is a 422 on `body.capacity`. The course " +
			"type of an event with sessions stays (a 422 on `body.courseTypeId`). A slug that is given and " +
			"taken is a 409 `duplicate`.",
	}), h.updateEvent)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "deleteEvent",
		Method:      http.MethodDelete,
		Path:        events + "/{eventId}",
		Summary:     "Delete an event",
		Description: "Deletes an event with its sessions and image. An event with registrations (canceled ones " +
			"included) or a session that has started is a 409 `in-use`: it is canceled instead.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteEvent)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "publishEvent",
		Method:      http.MethodPost,
		Path:        events + "/{eventId}/publish",
		Summary:     "Publish an event",
		Description: "Shows a draft event and its sessions to everyone. An event that is not a draft is a 409 " +
			"`not-draft`, one without a scheduled session a 409 `no-sessions`.",
	}), h.publishEvent)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "unpublishEvent",
		Method:      http.MethodPost,
		Path:        events + "/{eventId}/unpublish",
		Summary:     "Take an event back",
		Description: "Makes a published event a draft again, which only the office sees. One that is not " +
			"published is a 409 `not-published`, one with open registrations a 409 `in-use`.",
	}), h.unpublishEvent)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "cancelEvent",
		Method:      http.MethodPost,
		Path:        events + "/{eventId}/cancel",
		Summary:     "Cancel an event",
		Description: "Cancels an event in one transaction, with an optional note: its sessions that have not " +
			"started are canceled like lessons (without their own mails), every open registration is " +
			"canceled and a paid one refunded by a Storno of its receipt, even once the event has started, " +
			"and everyone registered gets one event-canceled mail. An event canceled already is a 409 " +
			"`closed`; incomplete receipt settings a 409 `receipt-settings-incomplete`.",
	}), h.cancelEvent)

	register(api, AccessOffice, read(huma.Operation{
		OperationID: "getAdminEventImage",
		Path:        events + "/{eventId}/image",
		Summary:     "Event image",
		Description: "Returns the event's image; a 404 without one. With the `ETag` in `If-None-Match` an " +
			"unchanged image is a 304.",
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
	}), h.getAdminEventImage)

	register(api, AccessOffice, huma.Operation{
		OperationID: "putEventImage",
		Method:      http.MethodPut,
		Path:        events + "/{eventId}/image",
		Summary:     "Set the event image",
		Description: "Sets the event's image: a JPEG, PNG or WebP image of at most 1 MiB, base64 encoded, as for " +
			"the school's background. Another type is a 422 on `body.contentType`; an empty or larger image, " +
			"or data that does not match the type, one on `body.data`.",
		Tags:          tags,
		DefaultStatus: http.StatusNoContent,
		MaxBodyBytes:  backgroundMaxBody,
	}, h.putEventImage)

	register(api, AccessOffice, huma.Operation{
		OperationID:   "deleteEventImage",
		Method:        http.MethodDelete,
		Path:          events + "/{eventId}/image",
		Summary:       "Remove the event image",
		Description:   "Removes the event's image, if it has one.",
		Tags:          tags,
		DefaultStatus: http.StatusNoContent,
	}, h.deleteEventImage)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "addEventSession",
		Method:      http.MethodPost,
		Path:        events + "/{eventId}/sessions",
		Summary:     "Add a session to an event",
		Description: "Adds a session: a public lesson of kind workshop without a class, with the event's course " +
			"type and capacity. A canceled event is a 409 `closed`. Otherwise the checks of `createLesson` " +
			"apply (422 on unknown rooms or staff, 409 `past`, 409 `room-taken`). The session is then edited, " +
			"canceled and deleted as a lesson; its capacity stays the event's.",
		DefaultStatus: http.StatusCreated,
	}), h.addEventSession)

	register(api, AccessOffice, read(huma.Operation{
		OperationID: "listEventRegistrations",
		Path:        events + "/{eventId}/registrations",
		Summary:     "List an event's registrations",
		Description: "Returns the registrations of an event, canceled ones included, in the order they were made.",
	}), h.listEventRegistrations)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "addEventRegistration",
		Method:      http.MethodPost,
		Path:        events + "/{eventId}/registrations",
		Summary:     "Register for an event",
		Description: "Registers a person or a guest (exactly one of `personId` and `guest`, a 422 on `body` " +
			"otherwise; a guest needs an email) for a published event at the desk. A draft or canceled event " +
			"is a 409 `closed`; one whose registrations and open invites fill its capacity a 409 `full` unless " +
			"`confirmOverCapacity` is set; a person registered already a 409 `duplicate`. An unknown person is " +
			"a 422 on `body.personId`. The price is the event's now; a free registration is paid, a priced " +
			"one unpaid until `markEventRegistrationPaid`.",
		DefaultStatus: http.StatusCreated,
	}), h.addEventRegistration)

	const registrations = BasePath + "/admin/event-registrations/{registrationId}"

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "markEventRegistrationPaid",
		Method:      http.MethodPost,
		Path:        registrations + "/pay",
		Summary:     "Take an event fee",
		Description: "Records that the fee of an open, unpaid registration was paid at the desk and issues its " +
			"receipt in the same transaction: one line naming the event and its dates, mailed to the payer " +
			"(default: the registered person), or to a guest's email. A paid, free or refunded registration " +
			"is a 409 `already-paid`, a canceled one a 409 `already-canceled`, one on the waiting list a 409 " +
			"`not-registered`; an unknown payer, or one for a " +
			"guest, a 422 on `body.payerId`; incomplete receipt settings a 409 `receipt-settings-incomplete`.",
	}), h.markEventRegistrationPaid)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "officeCancelEventRegistration",
		Method:      http.MethodPost,
		Path:        registrations + "/cancel",
		Summary:     "Cancel a registration",
		Description: "Cancels an open registration, after the members' deadline too. With `refund`, a fee paid " +
			"with a receipt is refunded by a Storno of the receipt, even once the event has started; without, " +
			"the payment stays. A canceled registration is a 409 `already-canceled`.",
	}), h.officeCancelEventRegistration)
}

// EventData is the editable part of an event.
type EventData struct {
	Slug           string              `doc:"Lowercase letters and digits in groups joined by single hyphens, used in /events/{slug}. Absent: a new event's is derived from the title, an edited one keeps its own." json:"slug,omitempty" maxLength:"80" required:"false"`
	Title          string              `doc:"Title."                                             json:"title"         maxLength:"200"   minLength:"1"`
	Description    string              `doc:"Description, plain text."                            json:"description,omitempty" maxLength:"10000" required:"false"`
	CourseTypeID   domain.CourseTypeID `doc:"Course type of the event and its sessions; it stays once the event has sessions." json:"courseTypeId"`
	Capacity       int                 `doc:"Places for the whole event."                         json:"capacity"      maximum:"10000"   minimum:"1"`
	Price          int64               `doc:"Gross price in minor units of the school currency (cents); 0 for a free event." json:"price" maximum:"100000000" minimum:"0"`
	VATRate        int                 `doc:"VAT rate of the price in basis points (1900 = 19 %); 0 for none." json:"vatRate" maximum:"10000" minimum:"0"`
	SignupOpensAt  time.Time           `doc:"When members may register; absent: from publishing on." json:"signupOpensAt,omitzero" required:"false"`
	SignupClosesAt time.Time           `doc:"When registering closes; absent: at the first session's start." json:"signupClosesAt,omitzero" required:"false"`
	CancelUntil    time.Time           `doc:"Deadline of a member's own cancellation; absent: the sign-up close." json:"cancelUntil,omitzero" required:"false"`
}

func (d EventData) service() service.EventData {
	return service.EventData{
		Slug: d.Slug, Title: d.Title, Description: d.Description, CourseTypeID: d.CourseTypeID, Capacity: d.Capacity,
		Price: domain.Money(d.Price), VATRate: d.VATRate, SignupOpensAt: d.SignupOpensAt,
		SignupClosesAt: d.SignupClosesAt, CancelUntil: d.CancelUntil,
	}
}

// AdminEvent is an event as the office sees it.
type AdminEvent struct {
	ID             domain.EventID      `doc:"Event ID." json:"id"`
	Slug           string              `doc:"Slug, used in /events/{slug}." json:"slug"`
	Title          string              `doc:"Title." json:"title"`
	Description    string              `doc:"Description, plain text; absent if none." json:"description,omitempty"`
	CourseTypeID   domain.CourseTypeID `doc:"Course type of the event and its sessions." json:"courseTypeId"`
	Capacity       int                 `doc:"Places for the whole event." json:"capacity"`
	Price          int64               `doc:"Gross price in minor units; 0 for a free event." json:"price"`
	VATRate        int                 `doc:"VAT rate of the price in basis points." json:"vatRate"`
	Status         string              `doc:"draft: only the office sees it; published: public with its sessions; canceled: canceled by the school." enum:"draft,published,canceled" json:"status"`
	SignupOpensAt  time.Time           `doc:"When members may register; absent: from publishing on." json:"signupOpensAt,omitzero"`
	SignupClosesAt time.Time           `doc:"When registering closes; absent: at the first session's start." json:"signupClosesAt,omitzero"`
	CancelUntil    time.Time           `doc:"Deadline of a member's own cancellation; absent: the sign-up close." json:"cancelUntil,omitzero"`
	CreatedAt      time.Time           `doc:"When the event was created." json:"createdAt"`
	UpdatedAt      time.Time           `doc:"When it was last changed."   json:"updatedAt"`
	Registered     int                 `doc:"Registrations with a place." json:"registered"`
	Invited        int                 `doc:"Open invites of the waiting list, each holding a place." json:"invited"`
	Waiting        int                 `doc:"Registrations waiting for a place." json:"waiting"`
	Sessions       []LessonSummary     `doc:"The sessions, canceled ones included, ordered by start." json:"sessions" nullable:"false"`
	Image          *EventImageInfo     `doc:"The image's type and digest; absent without one, and in listAdminEvents." json:"image,omitempty"`
}

// EventImageInfo names an event's image without its data.
type EventImageInfo struct {
	ContentType string `doc:"The image type." enum:"image/jpeg,image/png,image/webp" json:"contentType"`
	Digest      string `doc:"Digest of the image, its ETag in getAdminEventImage." json:"digest"`
}

// NewAdminEvent is the response body of the event administration for v.
func NewAdminEvent(v service.EventAdmin) AdminEvent {
	e := v.Event
	out := AdminEvent{
		ID: e.ID, Slug: e.Slug, Title: e.Title, Description: e.Description, CourseTypeID: e.CourseTypeID,
		Capacity: e.Capacity, Price: int64(e.Price), VATRate: e.VATRate, Status: string(e.Status),
		SignupOpensAt: e.SignupOpensAt.UTC(), SignupClosesAt: e.SignupClosesAt.UTC(), CancelUntil: e.CancelUntil.UTC(),
		CreatedAt: e.CreatedAt.UTC(), UpdatedAt: e.UpdatedAt.UTC(),
		Registered: v.Counts.Registered, Invited: v.Counts.Invited, Waiting: v.Counts.Waiting,
		Sessions: make([]LessonSummary, len(v.Sessions)),
	}

	for i, sl := range v.Sessions {
		out.Sessions[i] = lessonSummary(sl)
	}

	if !v.Image.IsZero() {
		out.Image = &EventImageInfo{ContentType: v.Image.ContentType, Digest: v.Image.Digest}
	}

	return out
}

// AdminEventList is the response body of listAdminEvents.
type AdminEventList struct {
	Items []AdminEvent `json:"items" nullable:"false"`
}

// AdminEventListOutput is the response of listAdminEvents.
type AdminEventListOutput struct {
	Body AdminEventList
}

// AdminEventOutput is the response of the event reads and writes.
type AdminEventOutput struct {
	Body AdminEvent
}

// EventRefInput names an event.
type EventRefInput struct {
	EventID domain.EventID `doc:"The event." path:"eventId"`
}

// EventCreateInput is the request of createEvent.
type EventCreateInput struct {
	Body EventData
}

// EventUpdateInput is the request of updateEvent.
type EventUpdateInput struct {
	EventID domain.EventID `doc:"The event." path:"eventId"`
	Body    EventData
}

// EventCancelInput is the request of cancelEvent.
type EventCancelInput struct {
	EventID domain.EventID `doc:"The event." path:"eventId"`
	Body    LessonCancelData
}

// EventImageInput is the request of getAdminEventImage.
type EventImageInput struct {
	EventID     domain.EventID `doc:"The event." path:"eventId"`
	IfNoneMatch string         `doc:"The ETag of the image the client has; unchanged: a 304." header:"If-None-Match"`
}

// EventImagePutInput is the request of putEventImage. The maxLength is the
// base64 length of domain.MaxBackgroundBytes.
type EventImagePutInput struct {
	EventID domain.EventID `doc:"The event." path:"eventId"`
	Body    struct {
		ContentType string `doc:"The image type." enum:"image/jpeg,image/png,image/webp" json:"contentType"`
		Data        []byte `doc:"The image, base64 encoded; at most 1 MiB." json:"data" maxLength:"1398104"`
	}
}

// EventSessionData is the request body of addEventSession.
type EventSessionData struct {
	Date               domain.Date      `doc:"School date of the session."                  format:"date"   json:"date"`
	StartTime          domain.TimeOfDay `doc:"Start time (HH:MM) in the school time zone." example:"10:00" json:"startTime" pattern:"^[0-2][0-9]:[0-5][0-9]$"`
	DurationMinutes    int              `doc:"Length in minutes; the session ends on its day." json:"durationMinutes" maximum:"1440" minimum:"1"`
	RoomID             domain.RoomID    `doc:"Room."        json:"roomId"`
	InstructorID       domain.StaffID   `doc:"Instructor."  json:"instructorId"`
	AccompanistID      domain.StaffID   `doc:"Accompanist, if any." json:"accompanistId,omitzero" required:"false"`
	ConfirmRoomOverlap bool             `doc:"Save even if another lesson uses the room at an overlapping time (skips the 409 room-taken)." json:"confirmRoomOverlap,omitempty" required:"false"`
	ConfirmPast        bool             `doc:"Save even if the session starts in the past (skips the 409 past)." json:"confirmPast,omitempty" required:"false"`
}

// EventSessionInput is the request of addEventSession.
type EventSessionInput struct {
	EventID domain.EventID `doc:"The event." path:"eventId"`
	Body    EventSessionData
}

// EventRegistration is a registration for an event.
type EventRegistration struct {
	ID                    domain.EventRegistrationID `doc:"Registration ID." json:"id"`
	EventID               domain.EventID             `doc:"The event." json:"eventId"`
	Person                *PassPerson                `doc:"The registered person; absent for a guest." json:"person,omitempty"`
	Guest                 *GuestData                 `doc:"The registered guest; absent for a person. An anonymized guest is named Guest, without an email." json:"guest,omitempty"`
	Status                string                     `doc:"waiting and invited: on the waiting list (an invite holds a place); registered: has a place; canceled." enum:"waiting,invited,registered,canceled" json:"status"`
	Payment               string                     `doc:"unpaid: the fee is owed; paid: paid at the desk with a receipt, or free; refunded: the receipt was canceled by a Storno." enum:"unpaid,paid,refunded" json:"payment"`
	Price                 int64                      `doc:"The event's gross price when the registration was made, in minor units." json:"price"`
	VATRate               int                        `doc:"Its VAT rate in basis points." json:"vatRate"`
	ReceiptID             domain.ReceiptID           `doc:"The receipt of the payment; absent if unpaid or free." json:"receiptId,omitzero"`
	CancellationReceiptID domain.ReceiptID           `doc:"The Storno of a refund." json:"cancellationReceiptId,omitzero"`
	InviteExpiresAt       time.Time                  `doc:"When an open invite runs out; only while invited." json:"inviteExpiresAt,omitzero"`
	CreatedAt             time.Time                  `doc:"When it was made: its place in the waiting list." json:"createdAt"`
	UpdatedAt             time.Time                  `doc:"When it was last changed." json:"updatedAt"`
	CanceledAt            time.Time                  `doc:"When it was canceled; only if canceled." json:"canceledAt,omitzero"`
}

// NewEventRegistration is the body of the registration v.
func NewEventRegistration(v service.RegistrationView) EventRegistration {
	r := v.Registration
	out := EventRegistration{
		ID: r.ID, EventID: r.EventID, Status: string(r.Status), Payment: string(r.Payment), Price: int64(r.Price),
		VATRate: r.VATRate, ReceiptID: r.ReceiptID, CancellationReceiptID: r.CancellationReceiptID,
		InviteExpiresAt: r.ExpiresAt.UTC(), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
		CanceledAt: r.CanceledAt.UTC(),
	}

	if r.PersonID.IsZero() {
		out.Guest = &GuestData{Name: r.GuestName, Email: r.GuestEmail}
	} else {
		out.Person = &PassPerson{ID: r.PersonID, Name: v.Name, Anonymized: v.Anonymized}
	}

	return out
}

// EventRegistrationList is the response body of listEventRegistrations.
type EventRegistrationList struct {
	Items []EventRegistration `json:"items" nullable:"false"`
}

// EventRegistrationListOutput is the response of listEventRegistrations.
type EventRegistrationListOutput struct {
	Body EventRegistrationList
}

// EventRegistrationOutput is the response of the registration writes.
type EventRegistrationOutput struct {
	Body EventRegistration
}

// AddEventRegistrationData is the request body of addEventRegistration:
// exactly one of personId and guest.
type AddEventRegistrationData struct {
	PersonID            domain.PersonID `doc:"The person to register."                  json:"personId,omitzero" required:"false"`
	Guest               *GuestData      `doc:"A guest to register instead of a person; an email is required." json:"guest,omitempty" required:"false"`
	ConfirmOverCapacity bool            `doc:"Register even if the event is full (skips the 409 full)." json:"confirmOverCapacity,omitempty" required:"false"`
}

// AddEventRegistrationInput is the request of addEventRegistration.
type AddEventRegistrationInput struct {
	EventID domain.EventID `doc:"The event." path:"eventId"`
	Body    AddEventRegistrationData
}

// EventPaymentData is the request body of markEventRegistrationPaid.
type EventPaymentData struct {
	PaymentMethod    string          `default:"cash" doc:"How it was paid; printed on the receipt." enum:"cash,transfer,card" json:"paymentMethod,omitempty" required:"false"`
	PayerID          domain.PersonID `doc:"Who paid; default: the registered person. A guest pays for themself." json:"payerId,omitzero" required:"false"`
	RecipientName    string          `doc:"The name the receipt prints as the payer; default: the payer's or the guest's name." json:"recipientName,omitempty" maxLength:"200" required:"false"`
	RecipientAddress string          `doc:"The payer's address printed on the receipt, one line per printed line; none by default." json:"recipientAddress,omitempty" maxLength:"500" required:"false"`
	RecipientEmail   string          `doc:"Where the receipt is mailed if the payer has no account; a guest's default is their email." json:"recipientEmail,omitempty" maxLength:"254" required:"false"`
}

// EventPaymentInput is the request of markEventRegistrationPaid.
type EventPaymentInput struct {
	RegistrationID domain.EventRegistrationID `doc:"The registration." path:"registrationId"`
	Body           EventPaymentData
}

// EventPayment is the response body of markEventRegistrationPaid.
type EventPayment struct {
	Registration EventRegistration `json:"registration"`
	Receipt      ReceiptRef        `doc:"The receipt issued." json:"receipt"`
}

// EventPaymentOutput is the response of markEventRegistrationPaid.
type EventPaymentOutput struct {
	Body EventPayment
}

// EventRegistrationCancelInput is the request of
// officeCancelEventRegistration.
type EventRegistrationCancelInput struct {
	RegistrationID domain.EventRegistrationID `doc:"The registration." path:"registrationId"`
	Body           struct {
		Refund bool `doc:"Refund a fee paid with a receipt by a Storno of the receipt." json:"refund,omitempty" required:"false"`
	}
}

func (h *handlers) listAdminEvents(ctx context.Context, _ *struct{}) (*AdminEventListOutput, error) {
	events, err := h.deps.Events.List(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &AdminEventListOutput{Body: AdminEventList{Items: make([]AdminEvent, len(events))}}
	for i, e := range events {
		out.Body.Items[i] = NewAdminEvent(e)
	}

	return out, nil
}

func (h *handlers) getAdminEvent(ctx context.Context, in *EventRefInput) (*AdminEventOutput, error) {
	return h.eventOutput(ctx)(h.deps.Events.Get(ctx, in.EventID))
}

func (h *handlers) createEvent(ctx context.Context, in *EventCreateInput) (*AdminEventOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	return h.eventOutput(ctx)(h.deps.Events.Create(ctx, p, in.Body.service()))
}

func (h *handlers) updateEvent(ctx context.Context, in *EventUpdateInput) (*AdminEventOutput, error) {
	return h.eventOutput(ctx)(h.deps.Events.Update(ctx, in.EventID, in.Body.service()))
}

func (h *handlers) deleteEvent(ctx context.Context, in *EventRefInput) (*struct{}, error) {
	if err := h.deps.Events.Delete(ctx, in.EventID); err != nil {
		return nil, h.eventError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) publishEvent(ctx context.Context, in *EventRefInput) (*AdminEventOutput, error) {
	return h.eventOutput(ctx)(h.deps.Events.Publish(ctx, in.EventID))
}

func (h *handlers) unpublishEvent(ctx context.Context, in *EventRefInput) (*AdminEventOutput, error) {
	return h.eventOutput(ctx)(h.deps.Events.Unpublish(ctx, in.EventID))
}

func (h *handlers) cancelEvent(ctx context.Context, in *EventCancelInput) (*AdminEventOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	return h.eventOutput(ctx)(h.deps.Events.Cancel(ctx, p, in.EventID, in.Body.Note))
}

func (h *handlers) getAdminEventImage(ctx context.Context, in *EventImageInput) (*SchoolLogoOutput, error) {
	img, err := h.deps.Events.Image(ctx, in.EventID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	if img.IsZero() {
		return nil, huma.Error404NotFound("no image")
	}

	// Inline: the page shows the image (the API's answers default to an
	// attachment).
	out := &SchoolLogoOutput{
		ETag: logoETag(img.Digest()), CacheControl: LogoCacheControl, ContentDisposition: "inline",
		Status: http.StatusNotModified,
	}

	if !etagMatches(in.IfNoneMatch, out.ETag) {
		out.Status, out.ContentType, out.Body = http.StatusOK, img.ContentType, img.Data
	}

	return out, nil
}

func (h *handlers) putEventImage(ctx context.Context, in *EventImagePutInput) (*struct{}, error) {
	err := h.deps.Events.PutImage(ctx, in.EventID, domain.Background{ContentType: in.Body.ContentType, Data: in.Body.Data})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) deleteEventImage(ctx context.Context, in *EventRefInput) (*struct{}, error) {
	if err := h.deps.Events.DeleteImage(ctx, in.EventID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) addEventSession(ctx context.Context, in *EventSessionInput) (*LessonOutput, error) {
	b := in.Body

	sl, err := h.deps.Events.AddSession(ctx, in.EventID, service.SessionData{
		Date: b.Date, StartTime: b.StartTime, Duration: time.Duration(b.DurationMinutes) * time.Minute,
		RoomID: b.RoomID, InstructorID: b.InstructorID, AccompanistID: b.AccompanistID,
		ConfirmRoomOverlap: b.ConfirmRoomOverlap, ConfirmPast: b.ConfirmPast,
	})
	if err != nil {
		return nil, h.eventError(ctx, err)
	}

	return &LessonOutput{Body: NewLessonDetail(sl)}, nil
}

func (h *handlers) listEventRegistrations(ctx context.Context, in *EventRefInput) (*EventRegistrationListOutput, error) {
	regs, err := h.deps.Events.ListRegistrations(ctx, in.EventID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &EventRegistrationListOutput{Body: EventRegistrationList{Items: make([]EventRegistration, len(regs))}}
	for i, r := range regs {
		out.Body.Items[i] = NewEventRegistration(r)
	}

	return out, nil
}

func (h *handlers) addEventRegistration(
	ctx context.Context, in *AddEventRegistrationInput,
) (*EventRegistrationOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	if in.Body.PersonID.IsZero() == (in.Body.Guest == nil) {
		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "give either a person or a guest", Location: "body",
		})
	}

	nr := service.NewRegistration{PersonID: in.Body.PersonID, ConfirmOverCapacity: in.Body.ConfirmOverCapacity}
	if g := in.Body.Guest; g != nil {
		nr.GuestName, nr.GuestEmail = g.Name, g.Email
	}

	return h.registrationOutput(ctx)(h.deps.Events.AddRegistration(ctx, p, in.EventID, nr))
}

func (h *handlers) markEventRegistrationPaid(ctx context.Context, in *EventPaymentInput) (*EventPaymentOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	b := in.Body

	paid, err := h.deps.Events.MarkPaid(ctx, p, in.RegistrationID, service.EventPayment{
		Method: domain.PaymentMethod(cmp.Or(b.PaymentMethod, string(domain.PaymentCash))), PayerID: b.PayerID,
		Recipient: service.ReceiptRecipient{Name: b.RecipientName, Address: b.RecipientAddress, Email: b.RecipientEmail},
	})
	if err != nil {
		return nil, h.eventError(ctx, err)
	}

	return &EventPaymentOutput{Body: EventPayment{
		Registration: NewEventRegistration(paid.RegistrationView), Receipt: *receiptRef(&paid.Receipt),
	}}, nil
}

func (h *handlers) officeCancelEventRegistration(
	ctx context.Context, in *EventRegistrationCancelInput,
) (*EventRegistrationOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	return h.registrationOutput(ctx)(h.deps.Events.CancelRegistration(ctx, p, in.RegistrationID, in.Body.Refund))
}

func (h *handlers) eventOutput(ctx context.Context) func(service.EventAdmin, error) (*AdminEventOutput, error) {
	return func(e service.EventAdmin, err error) (*AdminEventOutput, error) {
		if err != nil {
			return nil, h.eventError(ctx, err)
		}

		return &AdminEventOutput{Body: NewAdminEvent(e)}, nil
	}
}

func (h *handlers) registrationOutput(
	ctx context.Context,
) func(service.RegistrationView, error) (*EventRegistrationOutput, error) {
	return func(r service.RegistrationView, err error) (*EventRegistrationOutput, error) {
		if err != nil {
			return nil, h.eventError(ctx, err)
		}

		return &EventRegistrationOutput{Body: NewEventRegistration(r)}, nil
	}
}

// eventError maps the conflicts of the event administration to their
// reasons, those of the receipts (receiptError) and of the sessions
// (lessonError), and everything else like toHTTPError.
func (h *handlers) eventError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrEventNoSessions):
		return conflict(reasonNoSessions, "the event has no scheduled session")
	case errors.Is(err, service.ErrEventNotDraft):
		return conflict(reasonNotDraft, "the event is not a draft")
	case errors.Is(err, service.ErrEventNotPublished):
		return conflict(reasonNotPublished, "the event is not published")
	case errors.Is(err, service.ErrEventClosed):
		return conflict(reasonClosed, "the event takes no registrations")
	case errors.Is(err, service.ErrEventFull):
		return conflict(reasonFull, "the event is full")
	case errors.Is(err, service.ErrRegistrationCanceled):
		return conflict(reasonAlreadyCanceled, "the registration is canceled already")
	case errors.Is(err, service.ErrAlreadyPaid):
		return conflict(reasonAlreadyPaid, "the registration is paid already")
	case errors.Is(err, service.ErrNotRegistered):
		return conflict(reasonNotRegistered, "the registration is on the waiting list")
	case errors.Is(err, service.ErrReceiptSettingsIncomplete):
		return h.receiptError(ctx, err)
	}

	return h.lessonError(ctx, err)
}
