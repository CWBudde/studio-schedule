//go:build js && wasm

package wasmbridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"syscall/js"
)

// GlobalName is the name of the object registered on globalThis.
const GlobalName = "StudioScheduleAPI"

// funcs keeps every exported js.Func alive for the lifetime of the program.
var funcs []js.Func

var errBadRequest = errors.New("wasmbridge: request must be an object")

// Register exposes h as globalThis.StudioScheduleAPI:
//
//	fetch(req: {method, url, headers: [name, value][], body: Uint8Array | null})
//	  → Promise<{status, headers: [name, value][], body: Uint8Array}>
//	snapshot() → Promise<Uint8Array> (only if p.Snapshot is set)
//	restored: boolean (p.Restored)
//	version: string
//	ready: true (set last)
//
// After registration it calls globalThis.__studioScheduleReady() if that is
// a function. The caller must keep the Go program alive (select {}).
func Register(h http.Handler, version string, p Persistence) {
	fetch := js.FuncOf(func(_ js.Value, args []js.Value) (result any) {
		defer func() {
			if rec := recover(); rec != nil {
				result = rejected(fmt.Errorf("wasmbridge: fetch: %v", rec))
			}
		}()

		if len(args) < 1 {
			return rejected(errBadRequest)
		}

		req, err := requestFromJS(args[0])
		if err != nil {
			return rejected(err)
		}

		return newPromise(func(resolve, reject js.Value) {
			go func() {
				defer func() {
					if rec := recover(); rec != nil {
						reject.Invoke(jsError(fmt.Errorf("wasmbridge: %v", rec)))
					}
				}()

				resp, err := Serve(context.Background(), h, req)
				if err != nil {
					reject.Invoke(jsError(err))

					return
				}

				resolve.Invoke(responseToJS(resp))
			}()
		})
	})
	funcs = append(funcs, fetch)

	api := js.Global().Get("Object").New()
	api.Set("fetch", fetch)

	if p.Snapshot != nil {
		snapshot := js.FuncOf(func(js.Value, []js.Value) any {
			return newPromise(func(resolve, reject js.Value) {
				// Encoding takes a moment; let the promise return first.
				go func() {
					data, err := p.Snapshot()
					if err != nil {
						reject.Invoke(jsError(err))

						return
					}

					resolve.Invoke(bytesToJS(data))
				}()
			})
		})
		funcs = append(funcs, snapshot)
		api.Set("snapshot", snapshot)
	}

	api.Set("restored", p.Restored)
	api.Set("version", version)
	api.Set("ready", true)
	js.Global().Set(GlobalName, api)

	if cb := js.Global().Get("__studioScheduleReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}
}

// TakeSnapshot returns the saved data the worker left in SnapshotGlobal, or
// nil, and removes the global.
func TakeSnapshot() ([]byte, error) {
	v := js.Global().Get(SnapshotGlobal)
	js.Global().Delete(SnapshotGlobal)

	return bytesFromJS(v)
}

// NotifyChanged tells the worker that data changed, by calling
// globalThis.__studioScheduleChanged() if it is a function. It only schedules
// a save there, so it may run while the store is locked.
func NotifyChanged() {
	if fn := js.Global().Get(ChangedGlobal); fn.Type() == js.TypeFunction {
		fn.Invoke()
	}
}

// newPromise creates a JS Promise. executor runs synchronously inside the
// Promise constructor and must not block; it hands resolve/reject to a
// goroutine.
func newPromise(executor func(resolve, reject js.Value)) js.Value {
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		executor(args[0], args[1])

		return nil
	})
	defer fn.Release() // the constructor has called it by the time New returns

	return js.Global().Get("Promise").New(fn)
}

func rejected(err error) js.Value {
	return js.Global().Get("Promise").Call("reject", jsError(err))
}

func jsError(err error) js.Value {
	return js.Global().Get("Error").New(err.Error())
}

func requestFromJS(v js.Value) (Request, error) {
	if v.Type() != js.TypeObject {
		return Request{}, errBadRequest
	}

	var r Request

	if m := v.Get("method"); m.Type() == js.TypeString {
		r.Method = m.String()
	}

	if u := v.Get("url"); u.Type() == js.TypeString {
		r.URL = u.String()
	}

	if hs := v.Get("headers"); hs.Type() == js.TypeObject {
		n := hs.Length()
		for i := range n {
			pair := hs.Index(i)
			if pair.Type() != js.TypeObject || pair.Length() < 2 {
				continue
			}

			r.Headers = append(r.Headers, Header{pair.Index(0).String(), pair.Index(1).String()})
		}
	}

	body, err := bytesFromJS(v.Get("body"))
	if err != nil {
		return Request{}, err
	}

	r.Body = body

	return r, nil
}

// bytesFromJS accepts null/undefined, a string, an ArrayBuffer or any
// Uint8Array.
func bytesFromJS(v js.Value) ([]byte, error) {
	switch v.Type() {
	case js.TypeNull, js.TypeUndefined:
		return nil, nil
	case js.TypeString:
		return []byte(v.String()), nil
	case js.TypeObject:
		uint8Array := js.Global().Get("Uint8Array")
		if v.InstanceOf(js.Global().Get("ArrayBuffer")) {
			v = uint8Array.New(v)
		}

		if !v.InstanceOf(uint8Array) {
			return nil, errors.New("wasmbridge: body must be a Uint8Array, ArrayBuffer, string or null")
		}

		b := make([]byte, v.Length())
		js.CopyBytesToGo(b, v)

		return b, nil
	default:
		return nil, fmt.Errorf("wasmbridge: unsupported body type %s", v.Type())
	}
}

func responseToJS(r Response) js.Value {
	headers := js.Global().Get("Array").New(len(r.Headers))
	for i, h := range r.Headers {
		headers.SetIndex(i, js.ValueOf([]any{h[0], h[1]}))
	}

	res := js.Global().Get("Object").New()
	res.Set("status", r.Status)
	res.Set("headers", headers)
	res.Set("body", bytesToJS(r.Body))

	return res
}

func bytesToJS(b []byte) js.Value {
	out := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(out, b)

	return out
}
