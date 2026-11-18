package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/service"
)

func registerEventWaitlist(api huma.API, h *handlers) {
	const registration = BasePath + "/event-registrations/{registrationId}"
	const link = BasePath + "/event-invites/{token}"

	write := func(id, path, summary string, status int) huma.Operation {
		return withConflict(api, huma.Operation{
			OperationID: id, Method: http.MethodPost, Tags: []string{"events"},
			Path: path, Summary: summary, DefaultStatus: status,
		})
	}

	register(api, AccessSignedIn, write("joinEventWaitlist", BasePath+"/events/{slug}/waitlist",
		"Join the waiting list of a full event", http.StatusCreated), h.joinEventWaitlist)
	register(api, AccessSignedIn, write("leaveEventWaitlist", registration+"/leave",
		"Leave an event waiting list", http.StatusOK), h.leaveEventWaitlist)
	register(api, AccessSignedIn, write("acceptEventInvite", registration+"/accept",
		"Take a reserved event place", http.StatusOK), h.acceptEventInvite)
	register(api, AccessSignedIn, write("declineEventInvite", registration+"/decline",
		"Decline a reserved event place", http.StatusOK), h.declineEventInvite)

	register(api, AccessPublic, huma.Operation{
		OperationID: "getEventInvite", Method: http.MethodGet, Tags: []string{"events"},
		Path: link, Summary: "Read an event invitation by its token",
		Description: "Reading this link does not take the place. A used, expired or unknown token is a 404.",
	}, h.getEventInvite)
	register(api, AccessPublic, write("acceptEventInviteByToken", link+"/accept",
		"Take a reserved event place by its token", http.StatusNoContent), h.acceptEventInviteByToken)
	register(api, AccessPublic, write("declineEventInviteByToken", link+"/decline",
		"Decline a reserved event place by its token", http.StatusNoContent), h.declineEventInviteByToken)
}

// EventInvite is the information displayed from an active token link.
type EventInvite struct {
	GivenName string    `json:"givenName"`
	ExpiresAt time.Time `json:"expiresAt"`
	Event     Event     `json:"event"`
}

// EventInviteOutput prevents caching the private details of an invitation.
type EventInviteOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         EventInvite
}

func (h *handlers) joinEventWaitlist(ctx context.Context, in *RegisterForEventInput) (*MyEventOutput, error) {
	p, _ := principalFrom(ctx)
	id, err := h.deps.Events.PublicEventID(ctx, in.Slug)
	if err != nil {
		return nil, h.memberEventError(ctx, err)
	}

	return h.myEventOutput(ctx)(h.deps.Events.JoinWaitlist(ctx, p, id, in.Body.PersonID))
}

func (h *handlers) leaveEventWaitlist(ctx context.Context, in *EventRegistrationRefInput) (*MyEventOutput, error) {
	p, _ := principalFrom(ctx)

	return h.myEventOutput(ctx)(h.deps.Events.LeaveWaitlist(ctx, p, in.RegistrationID))
}

func (h *handlers) acceptEventInvite(ctx context.Context, in *EventRegistrationRefInput) (*MyEventOutput, error) {
	p, _ := principalFrom(ctx)

	return h.myEventOutput(ctx)(h.deps.Events.AcceptInvite(ctx, p, in.RegistrationID))
}

func (h *handlers) declineEventInvite(ctx context.Context, in *EventRegistrationRefInput) (*MyEventOutput, error) {
	p, _ := principalFrom(ctx)

	return h.myEventOutput(ctx)(h.deps.Events.DeclineInvite(ctx, p, in.RegistrationID))
}

func (h *handlers) getEventInvite(ctx context.Context, in *WaitlistTokenInput) (*EventInviteOutput, error) {
	if err := allow(h.deps.RateLimits.InviteLinks, clientIP(ctx)); err != nil {
		return nil, err
	}

	v, err := h.deps.Events.Invite(ctx, in.Token)
	if err != nil {
		return nil, h.memberEventError(ctx, err)
	}

	return &EventInviteOutput{
		CacheControl: "no-store",
		Body:         EventInvite{GivenName: v.GivenName, ExpiresAt: v.ExpiresAt.UTC(), Event: NewEvent(v.Event)},
	}, nil
}

func (h *handlers) acceptEventInviteByToken(ctx context.Context, in *WaitlistTokenInput) (*struct{}, error) {
	return h.writeEventInviteToken(ctx, in, h.deps.Events.AcceptInviteByToken)
}

func (h *handlers) declineEventInviteByToken(ctx context.Context, in *WaitlistTokenInput) (*struct{}, error) {
	return h.writeEventInviteToken(ctx, in, h.deps.Events.DeclineInviteByToken)
}

func (h *handlers) writeEventInviteToken(
	ctx context.Context, in *WaitlistTokenInput, write func(context.Context, string) (service.MyEvent, error),
) (*struct{}, error) {
	if err := allow(h.deps.RateLimits.InviteLinks, clientIP(ctx)); err != nil {
		return nil, err
	}
	if _, err := write(ctx, in.Token); err != nil {
		return nil, h.memberEventError(ctx, err)
	}

	return &struct{}{}, nil
}
