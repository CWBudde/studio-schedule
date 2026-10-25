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
// hands the payload to the worker's NotifyGlobal, which posts it to the
// page, and the page shows it through its service worker. Every
// subscription of the demo is this browser's.
type PushSender struct{}

// PublicKey implements service.PushSender: the demo needs no VAPID key.
func (PushSender) PublicKey() string { return "" }

// Send implements service.PushSender.
func (PushSender) Send(_ context.Context, _ domain.PushSubscription, payload []byte, _ time.Duration) error {
	fn := js.Global().Get(NotifyGlobal)
	if fn.Type() != js.TypeFunction {
		return errors.New("wasmbridge: the worker cannot show notifications")
	}

	fn.Invoke(string(payload))

	return nil
}
