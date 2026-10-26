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

// Ready fetches and compiles the module (once), so the service can wait for
// it before the transaction that renders a receipt.
func (PDFRenderer) Ready(ctx context.Context) error {
	fn := js.Global().Get(LoadPDFGlobal)
	if fn.Type() != js.TypeFunction {
		return errors.New("wasmbridge: the worker cannot render PDFs")
	}

	if _, err := await(ctx, fn.Invoke()); err != nil {
		return fmt.Errorf("wasmbridge: load the PDF renderer: %w", err)
	}

	return nil
}

// Render implements service.ReceiptRenderer.
func (PDFRenderer) Render(ctx context.Context, input []byte) ([]byte, error) {
	fn := js.Global().Get(RenderPDFGlobal)
	if fn.Type() != js.TypeFunction {
		return nil, errors.New("wasmbridge: the worker cannot render PDFs")
	}

	v, err := await(ctx, fn.Invoke(bytesToJS(input)))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}

		return nil, fmt.Errorf("wasmbridge: render PDF: %w", err)
	}

	pdf, err := bytesFromJS(v)
	if err == nil && len(pdf) == 0 {
		return nil, errors.New("wasmbridge: render PDF: empty result")
	}

	return pdf, err
}

// await waits for promise to settle and returns its value, or its rejection
// as an error, or ctx's error first.
func await(ctx context.Context, promise js.Value) (js.Value, error) {
	type result struct {
		v   js.Value
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

		done <- result{v: v}

		return nil
	})
	onErr = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer onOK.Release()
		defer onErr.Release()

		msg := "unknown error"
		if len(args) > 0 {
			msg = js.Global().Get("String").Invoke(args[0]).String()
		}

		done <- result{err: errors.New(msg)}

		return nil
	})

	promise.Call("then", onOK, onErr)

	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		// The callbacks release themselves when the promise settles.
		return js.Value{}, ctx.Err()
	}
}
