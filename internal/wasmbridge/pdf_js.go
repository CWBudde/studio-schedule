//go:build js && wasm

package wasmbridge

import (
	"context"
	"errors"
	"fmt"
	"syscall/js"
)

// PDFRenderer renders receipt PDFs in the web demo (ADR 0012): it hands the
// input to the worker's RenderPDFGlobal, which runs the same typst module as
// the server with the browser's WebAssembly, and waits for the promise. The
// waiting goroutine lets the Go runtime return to the event loop, so the
// worker can fetch and compile the module meanwhile.
type PDFRenderer struct{}

// Render implements service.ReceiptRenderer.
func (PDFRenderer) Render(ctx context.Context, input []byte) ([]byte, error) {
	fn := js.Global().Get(RenderPDFGlobal)
	if fn.Type() != js.TypeFunction {
		return nil, errors.New("wasmbridge: the worker cannot render PDFs")
	}

	type result struct {
		pdf []byte
		err error
	}

	done := make(chan result, 1)

	var onOK, onErr js.Func

	onOK = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer onOK.Release()
		defer onErr.Release()

		var v js.Value
		if len(args) > 0 {
			v = args[0]
		}

		pdf, err := bytesFromJS(v)
		done <- result{pdf: pdf, err: err}

		return nil
	})
	onErr = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer onOK.Release()
		defer onErr.Release()

		msg := "unknown error"
		if len(args) > 0 {
			msg = js.Global().Get("String").Invoke(args[0]).String()
		}

		done <- result{err: fmt.Errorf("wasmbridge: render PDF: %s", msg)}

		return nil
	})

	fn.Invoke(bytesToJS(input)).Call("then", onOK, onErr)

	select {
	case r := <-done:
		if r.err == nil && len(r.pdf) == 0 {
			return nil, errors.New("wasmbridge: render PDF: empty result")
		}

		return r.pdf, r.err
	case <-ctx.Done():
		// The callbacks release themselves when the promise settles.
		return nil, ctx.Err()
	}
}
