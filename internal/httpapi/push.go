package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerPush registers web push (ADR 0017): whether it is on, and the
// signed-in account's devices that get notifications.
func registerPush(api huma.API, h *handlers) {
	tags := []string{"push"}

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "getPushStatus",
		Method:      http.MethodGet,
		Path:        BasePath + "/my/push",
		Summary:     "Web push status",
		Description: "Tells whether this installation sends web push notifications (for waiting-list invites, " +
			"besides the mail), and the VAPID key a browser subscribes with.",
		Tags: tags,
	}, h.getPushStatus)

	register(api, AccessSignedIn, withInvalid(api, huma.Operation{
		OperationID: "savePushSubscription",
		Method:      http.MethodPost,
		Path:        BasePath + "/my/push/subscriptions",
		Summary:     "Turn on notifications on a device",
		Description: "Saves the browser's push subscription (`PushSubscription.toJSON()`) for the signed-in " +
			"account: the device gets the account's notifications from now on. Saving the same endpoint again " +
			"updates it; an endpoint another account saved before (a shared device) is the caller's now. A 404 " +
			"while push is off, a 403 while an admin is signed in as the account, a 422 on a field that is no " +
			"https URL or key.",
		Tags:          tags,
		DefaultStatus: http.StatusNoContent,
	}), h.savePushSubscription)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "deletePushSubscription",
		Method:      http.MethodDelete,
		Path:        BasePath + "/my/push/subscriptions",
		Summary:     "Turn off notifications on a device",
		Description: "Deletes the signed-in account's subscription of the endpoint in the body, if it has one; " +
			"the answer is the same either way. The web app calls it before it signs out.",
		Tags:          tags,
		DefaultStatus: http.StatusNoContent,
	}, h.deletePushSubscription)
}

// PushStatus is the response body of getPushStatus.
type PushStatus struct {
	Enabled   bool   `doc:"Whether the installation sends web push notifications."                                   json:"enabled"`
	PublicKey string `doc:"The VAPID public key (base64url), the applicationServerKey to subscribe with; absent while push is off, and in the web demo, which shows its notifications itself." json:"publicKey,omitempty"`
}

// PushStatusOutput is the response of getPushStatus.
type PushStatusOutput struct {
	Body PushStatus
}

func (h *handlers) getPushStatus(_ context.Context, _ *struct{}) (*PushStatusOutput, error) {
	s := h.deps.Push.Status()

	return &PushStatusOutput{Body: PushStatus{Enabled: s.Enabled, PublicKey: s.PublicKey}}, nil
}

// PushKeys are the keys of a browser's subscription.
type PushKeys struct {
	P256DH string `doc:"The browser's ECDH public key, base64url." json:"p256dh" maxLength:"100" minLength:"1"`
	Auth   string `doc:"The authentication secret, base64url."     json:"auth"   maxLength:"40"  minLength:"1"`
}

// SavePushSubscriptionInput is the request of savePushSubscription.
type SavePushSubscriptionInput struct {
	Body struct {
		Endpoint    string   `doc:"The push service's https URL of the subscription."                     json:"endpoint" maxLength:"2048" minLength:"1"`
		Keys        PushKeys `json:"keys"`
		DeviceLabel string   `doc:"A name of the device for its holder, e.g. \"Firefox on Linux\"." json:"deviceLabel,omitempty" maxLength:"100" required:"false"`
	}
}

func (h *handlers) savePushSubscription(ctx context.Context, in *SavePushSubscriptionInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	err := h.deps.Push.Subscribe(ctx, p, service.PushSubscriptionInput{
		Endpoint: in.Body.Endpoint, P256DH: in.Body.Keys.P256DH, Auth: in.Body.Keys.Auth,
		DeviceLabel: in.Body.DeviceLabel,
	})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

// DeletePushSubscriptionInput is the request of deletePushSubscription.
type DeletePushSubscriptionInput struct {
	Body struct {
		Endpoint string `doc:"The endpoint of the device's subscription." json:"endpoint" maxLength:"2048" minLength:"1"`
	}
}

func (h *handlers) deletePushSubscription(ctx context.Context, in *DeletePushSubscriptionInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	if err := h.deps.Push.Unsubscribe(ctx, p, in.Body.Endpoint); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}
