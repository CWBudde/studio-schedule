package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerWaitlist registers the waiting list (ADR 0010): join and leave
// the list of a full lesson, accept or decline an invite in the app, and
// look at or accept an invite through the link of its mail, without a
// sign-in.
func registerWaitlist(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"waitlist"}
		if op.Method == "" {
			op.Method = http.MethodPost
		}

		return withConflict(api, op)
	}

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "joinWaitlist",
		Path:        BasePath + "/lessons/{lessonId}/waitlist",
		Summary:     "Join a lesson's waiting list",
		Description: "Puts the signed-in person or one of their dependents at the end of the waiting list of a " +
			"full lesson. Whenever a place frees, the oldest waiting entry is invited by mail; the invite " +
			"reserves the place until `expiresAt`. With `stayOnList`, an invite the person lets expire or " +
			"declines puts them back at the end of the list instead of ending the entry. A person outside the " +
			"family, or a lesson the caller may not see, is a 404; a lesson that cannot be booked a 409 " +
			"`closed` with `why`, as in `bookLesson`. A person booked already is a 409 `already-booked`, one " +
			"on the list already a 409 `already-waiting`, and a lesson with a free place (`freePlaces` > 0) a " +
			"409 `not-full`: book it instead.",
		DefaultStatus: http.StatusCreated,
	}), h.joinWaitlist)

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "leaveWaitlist",
		Method:      http.MethodDelete,
		Path:        BasePath + "/waitlist/{entryId}",
		Summary:     "Leave a waiting list",
		Description: "Takes an entry of the signed-in person or one of their dependents off the list (404 for " +
			"any other entry); a place its invite held goes to the next entry. An entry that is no longer " +
			"on the list is a 409 `not-waiting`.",
		DefaultStatus: http.StatusNoContent,
	}), h.leaveWaitlist)

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "acceptWaitlistInvite",
		Path:        BasePath + "/waitlist/{entryId}/accept",
		Summary:     "Accept a waiting-list invite",
		Description: "Books the place an invite of the signed-in person or one of their dependents holds (404 " +
			"for any other entry), like `bookLesson`, and returns the booking. A waiting entry is a 409 " +
			"`not-invited`, an invite that ran out a 409 `invite-expired`, one used already (accepted, or " +
			"the entry left the list) a 409 `invite-used`; a lesson that cannot be booked any more a 409 " +
			"`closed` with `why`. The signed-in account gets the confirmation mail.",
	}), h.acceptWaitlistInvite)

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "declineWaitlistInvite",
		Path:        BasePath + "/waitlist/{entryId}/decline",
		Summary:     "Decline a waiting-list invite",
		Description: "Gives up the place an invite holds, failing like `acceptWaitlistInvite`; the place goes " +
			"to the next entry. An entry with `stayOnList` goes back to the end of the list (while booking " +
			"is open), any other ends. Returns the entry as it is now.",
	}), h.declineWaitlistInvite)

	// A read, but it answers with what the link stands for.
	register(api, AccessPublic, huma.Operation{
		OperationID: "getWaitlistInvite",
		Method:      http.MethodGet,
		Path:        BasePath + "/waitlist-invites/{token}",
		Summary:     "Look at a waiting-list invite",
		Description: "Returns what the link of a `waitlist-invite` mail stands for: the lesson, the given name " +
			"of the person the place is reserved for, until when, and whether it can still be taken " +
			"(`state`, `closed`); after an invite that ended while the person went back to the list, `state` is " +
			"`waiting` with their `position`. It changes nothing, so mail scanners that open links take no place. No " +
			"sign-in is needed; an unknown token is a 404. Rate-limited per client.",
		Tags: []string{"waitlist"},
	}, h.getWaitlistInvite)

	register(api, AccessPublic, write(huma.Operation{
		OperationID: "acceptWaitlistInviteByToken",
		Path:        BasePath + "/waitlist-invites/{token}",
		Summary:     "Accept a waiting-list invite by its link",
		Description: "Books the place the invite holds without a sign-in, the token being the proof, and " +
			"returns the invite as it is now (`state` accepted). It fails like `acceptWaitlistInvite`; the link " +
			"of an entry that went back to waiting is a 409 `invite-expired`, an unknown token (or a link that a " +
			"newer invite replaced) a 404. The confirmation goes to the person's mail recipients, like the invite. " +
			"Rate-limited per client.",
	}), h.acceptWaitlistInviteByToken)
}

// WaitlistEntry is a person's entry on the waiting list of a lesson.
type WaitlistEntry struct {
	ID         domain.WaitlistEntryID `doc:"Entry ID."                            json:"id"`
	LessonID   domain.LessonID        `doc:"The lesson."                          json:"lessonId"`
	PersonID   domain.PersonID        `doc:"The person on the list."              json:"personId"`
	Status     string                 `doc:"waiting: for an invite; invited: a place is reserved until expiresAt; accepted: the place was taken; expired: the invite ran out or was declined, or the lesson started; withdrawn: the person left the list, or got booked otherwise." enum:"waiting,invited,accepted,expired,withdrawn" json:"status"`
	StayOnList bool                   `doc:"Whether an invite that expires or is declined puts the entry back at the end of the list." json:"stayOnList"`
	JoinedAt   time.Time              `doc:"When the entry joined the list, or went back to its end." json:"joinedAt"`
	Position   int                    `doc:"Place among the waiting entries, 1 for the next to be invited; absent unless waiting." json:"position,omitzero" minimum:"1"`
	ExpiresAt  time.Time              `doc:"Until when the invite reserves the place; only while invited." json:"expiresAt,omitzero"`
}

// WaitlistFor is someone of the caller's family on a lesson's waiting list.
type WaitlistFor struct {
	PersonID   domain.PersonID        `doc:"The person on the list."                 json:"personId"`
	Name       string                 `doc:"Given and family name of the person."   json:"name"`
	EntryID    domain.WaitlistEntryID `doc:"The entry, e.g. to leave the list or to accept its invite." json:"entryId"`
	Status     string                 `doc:"waiting: for an invite; invited: a place is reserved until expiresAt; expired: the invite has run out (the entry is about to end, or to go back to the end of the list)." enum:"waiting,invited,expired" json:"status"`
	StayOnList bool                   `doc:"Whether an invite that expires or is declined puts the entry back at the end of the list." json:"stayOnList"`
	Position   int                    `doc:"Place among the waiting entries, 1 for the next to be invited; absent unless waiting." json:"position,omitzero" minimum:"1"`
	ExpiresAt  time.Time              `doc:"Until when the invite reserves the place; absent while waiting." json:"expiresAt,omitzero"`
}

// JoinWaitlistData is the request body of joinWaitlist.
type JoinWaitlistData struct {
	PersonID   domain.PersonID `doc:"Who joins: the signed-in person or one of their dependents." json:"personId"`
	StayOnList bool            `doc:"Stay on the list, at its end, after letting an invite expire or declining it; default false." json:"stayOnList,omitempty" required:"false"`
}

// JoinWaitlistInput is the request of joinWaitlist.
type JoinWaitlistInput struct {
	LessonID domain.LessonID `doc:"The lesson." path:"lessonId"`
	Body     JoinWaitlistData
}

// WaitlistEntryRefInput is the request of the operations on one entry.
type WaitlistEntryRefInput struct {
	EntryID domain.WaitlistEntryID `doc:"The entry." path:"entryId"`
}

// WaitlistEntryOutput is the response of joinWaitlist and
// declineWaitlistInvite.
type WaitlistEntryOutput struct {
	Body WaitlistEntry
}

// WaitlistInvite is what the link of an invite stands for.
type WaitlistInvite struct {
	State     string        `doc:"invited: the place is reserved until expiresAt and can be taken (unless closed says otherwise); accepted: it was taken; expired: the invite ran out, was declined, or the lesson started, and the entry ended; withdrawn: the person left the list, or got booked otherwise; waiting: the invite ended, but the person is back on the list (see position) and gets a new link with the next invite." enum:"invited,accepted,expired,withdrawn,waiting" json:"state"`
	GivenName string        `doc:"Given name of the person the place is reserved for." json:"givenName"`
	ExpiresAt time.Time     `doc:"Until when the invite reserves (or reserved) the place; absent while waiting." json:"expiresAt,omitzero"`
	Position  int           `doc:"Place among the waiting entries, 1 for the next to be invited; only with state waiting." json:"position,omitzero" minimum:"1"`
	Closed    string        `doc:"Why the lesson cannot be booked now, if it cannot (see the closed conflict); the place cannot be taken then." enum:"cutoff,release,canceled,started" json:"closed,omitempty"`
	Lesson    LessonSummary `json:"lesson"`
}

// WaitlistTokenInput is the request of the operations on an invite link.
type WaitlistTokenInput struct {
	Token string `doc:"The token from the invite link." maxLength:"128" minLength:"1" path:"token"`
}

// WaitlistInviteOutput is the response of getWaitlistInvite and
// acceptWaitlistInviteByToken.
type WaitlistInviteOutput struct {
	Body WaitlistInvite
}

func (h *handlers) joinWaitlist(ctx context.Context, in *JoinWaitlistInput) (*WaitlistEntryOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	place, err := h.deps.Waitlist.Join(ctx, p, in.LessonID, in.Body.PersonID, in.Body.StayOnList)
	if err != nil {
		return nil, h.waitlistError(ctx, err)
	}

	return &WaitlistEntryOutput{Body: waitlistEntryBody(place)}, nil
}

func (h *handlers) leaveWaitlist(ctx context.Context, in *WaitlistEntryRefInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	if err := h.deps.Waitlist.Leave(ctx, p, in.EntryID); err != nil {
		return nil, h.waitlistError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) acceptWaitlistInvite(ctx context.Context, in *WaitlistEntryRefInput) (*BookingOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	b, err := h.deps.Waitlist.Accept(ctx, p, in.EntryID)
	if err != nil {
		return nil, h.waitlistError(ctx, err)
	}

	return &BookingOutput{Body: bookingBody(b)}, nil
}

func (h *handlers) declineWaitlistInvite(ctx context.Context, in *WaitlistEntryRefInput) (*WaitlistEntryOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	place, err := h.deps.Waitlist.Decline(ctx, p, in.EntryID)
	if err != nil {
		return nil, h.waitlistError(ctx, err)
	}

	return &WaitlistEntryOutput{Body: waitlistEntryBody(place)}, nil
}

func (h *handlers) getWaitlistInvite(ctx context.Context, in *WaitlistTokenInput) (*WaitlistInviteOutput, error) {
	if err := allow(h.deps.RateLimits.InviteLinks, clientIP(ctx)); err != nil {
		return nil, err
	}

	return h.inviteOutput(ctx)(h.deps.Waitlist.Invite(ctx, in.Token))
}

func (h *handlers) acceptWaitlistInviteByToken(
	ctx context.Context, in *WaitlistTokenInput,
) (*WaitlistInviteOutput, error) {
	if err := allow(h.deps.RateLimits.InviteLinks, clientIP(ctx)); err != nil {
		return nil, err
	}

	return h.inviteOutput(ctx)(h.deps.Waitlist.AcceptInvite(ctx, in.Token))
}

func (h *handlers) inviteOutput(ctx context.Context) func(service.InviteView, error) (*WaitlistInviteOutput, error) {
	return func(v service.InviteView, err error) (*WaitlistInviteOutput, error) {
		if err != nil {
			return nil, h.waitlistError(ctx, err)
		}

		return &WaitlistInviteOutput{Body: WaitlistInvite{
			State: string(v.State), GivenName: v.Person.GivenName, ExpiresAt: v.Entry.ExpiresAt.UTC(),
			Position: v.Position, Closed: string(v.Closed), Lesson: lessonSummary(v.Lesson),
		}}, nil
	}
}

func waitlistEntryBody(place service.WaitlistPlace) WaitlistEntry {
	e := place.Entry
	out := WaitlistEntry{
		ID: e.ID, LessonID: e.LessonID, PersonID: e.PersonID, Status: string(e.Status), StayOnList: e.StayOnList,
		JoinedAt: e.JoinedAt.UTC(), Position: place.Position,
	}

	if e.Status == domain.WaitlistInvited {
		out.ExpiresAt = e.ExpiresAt.UTC()
	}

	return out
}

// waitlistFor returns the family's entries of a lesson as the schedule
// shows them.
func waitlistFor(waiting []service.WaitingPerson) []WaitlistFor {
	var out []WaitlistFor
	for _, w := range waiting {
		out = append(out, waitlistForOne(w))
	}

	return out
}

// waitlistForOne returns the entry of w as the family sees it.
func waitlistForOne(w service.WaitingPerson) WaitlistFor {
	e := w.Entry
	f := WaitlistFor{
		PersonID: w.Person.ID, Name: w.Name, EntryID: e.ID, Status: string(w.Status), StayOnList: e.StayOnList,
		Position: w.Position,
	}

	if e.Status == domain.WaitlistInvited {
		f.ExpiresAt = e.ExpiresAt.UTC()
	}

	return f
}

// waitlistError maps the conflicts of the waiting list to their reasons,
// those it shares with the bookings like bookingError, and everything else
// like toHTTPError.
func (h *handlers) waitlistError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrNotFull):
		return conflict(reasonNotFull, "the lesson has a free place; book it instead")
	case errors.Is(err, service.ErrAlreadyWaiting):
		return conflict(reasonAlreadyWaiting, "the person is on the waiting list already")
	case errors.Is(err, service.ErrNotInvited):
		return conflict(reasonNotInvited, "the entry is waiting for an invite")
	case errors.Is(err, service.ErrInviteExpired):
		return conflict(reasonInviteExpired, "the invite has expired")
	case errors.Is(err, service.ErrInviteUsed):
		return conflict(reasonInviteUsed, "the invite has been used")
	case errors.Is(err, service.ErrNotWaiting):
		return conflict(reasonNotWaiting, "the entry is no longer on the waiting list")
	}

	return h.bookingError(ctx, err)
}
