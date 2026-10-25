//go:build js && wasm

package wasmbridge

import (
	"context"
	"errors"
	"syscall/js"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// PushSender is the web demo's web push sender (ADR 0017), like its mailer
// a demo-only implementation: the demo has no push service to reach, so it
// hands the payload and the endpoint to the worker's NotifyGlobal, which
// posts them to the page, and the page shows it through its service worker
// if the endpoint is still its own.
type PushSender struct{}

// PublicKey implements service.PushSender: the demo needs no VAPID key.
func (PushSender) PublicKey() string { return "" }

// Send implements service.PushSender. The page shows the notification only
// if it still holds the subscription's endpoint.
func (PushSender) Send(_ context.Context, sub domain.PushSubscription, payload []byte, _ time.Duration) error {
	fn := js.Global().Get(NotifyGlobal)
	if fn.Type() != js.TypeFunction {
		return errors.New("wasmbridge: the worker cannot show notifications")
	}

	fn.Invoke(string(payload), sub.Endpoint)

	return nil
}
