//go:build !js

package typstwasm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/sys"
)

// TestMain creates a runtime before the tests do in parallel: the first
// runtime caches wazero's version in a package variable without a lock
// (internal/version.GetWazeroVersion), a data race between two renderers
// that compile at once. The server has one renderer.
func TestMain(m *testing.M) {
	ctx := context.Background()
	_ = wazero.NewRuntime(ctx).Close(ctx)

	os.Exit(m.Run())
}

// crafted returns a compiled renderer of m with one instance, so a test sees
// whether an instance is reused, dropped or given back.
func crafted(t *testing.T, m *wasmModule) *Renderer {
	t.Helper()

	r := newRenderer(m.encode(), Options{Instances: 1})
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	if err := r.Ready(t.Context()); err != nil {
		t.Fatalf("Ready: %v", err)
	}

	return r
}

// renderSoon renders with a deadline: a render that waits for an instance
// which never comes back fails with context.DeadlineExceeded, not hangs.
func renderSoon(t *testing.T, r *Renderer, in []byte) ([]byte, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	return r.Render(ctx, in)
}

// hostEnv instantiates the host module "env" with fn as its function name
// into the renderer's runtime; the module's instances are created lazily, so
// this works until the first render.
func hostEnv(t *testing.T, r *Renderer, name string, fn any) {
	t.Helper()

	if _, err := r.rt.NewHostModuleBuilder("env").NewFunctionBuilder().WithFunc(fn).Export(name).
		Instantiate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestCacheDirUnusable(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	r := newRenderer(abiModule().encode(), Options{CacheDir: file})

	err := r.Ready(t.Context())
	if err == nil || !strings.HasPrefix(err.Error(), "typstwasm: compilation cache: ") {
		t.Fatalf("Ready: %v, want the compilation cache error", err)
	}

	if _, got := r.Render(t.Context(), nil); !errors.Is(got, err) {
		t.Errorf("Render: %v, want %v", got, err)
	}

	if err := r.Close(t.Context()); err != nil {
		t.Errorf("Close without a runtime: %v", err)
	}
}

func TestCompileError(t *testing.T) {
	t.Parallel()

	// The magic is there, the module is cut off.
	mod := abiModule().encode()
	r := newRenderer(mod[:len(mod)-1], Options{})

	err := r.Ready(t.Context())
	if err == nil || !strings.HasPrefix(err.Error(), "typstwasm: compile: ") {
		t.Fatalf("Ready: %v, want a compile error", err)
	}

	if _, got := r.Render(t.Context(), nil); !errors.Is(got, err) {
		t.Errorf("Render: %v, want %v", got, err)
	}

	// The runtime exists and closes.
	if err := r.Close(t.Context()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestInstantiateError: an instance whose _initialize traps is not created,
// and its slot is given back, so the next render tries again rather than
// waiting forever.
func TestInstantiateError(t *testing.T) {
	t.Parallel()

	r := crafted(t, abiModule().set("_initialize", op(opUnreachable)))

	for i := range 2 {
		_, err := renderSoon(t, r, nil)
		if err == nil || !strings.HasPrefix(err.Error(), "typstwasm: instantiate: ") ||
			!strings.Contains(err.Error(), "unreachable") {
			t.Fatalf("render %d: %v, want the instantiate error", i+1, err)
		}
	}
}

// TestStartFunction: _initialize runs before the first render, and a module
// without it (as every other crafted one) is instantiated all the same.
func TestStartFunction(t *testing.T) {
	t.Parallel()

	m := abiModule().set("render",
		op(opGlobalGet, 0, opI32Eqz, opIf, blockEmpty, opUnreachable, opEnd), // trap unless initialized
		i32(0))
	m.globals = []int32{0}

	if _, err := renderSoon(t, crafted(t, m), nil); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("without _initialize: %v, want render to trap", err)
	}

	m.set("_initialize", i32(1), op(opGlobalSet, 0))

	if _, err := renderSoon(t, crafted(t, m), nil); err != nil {
		t.Fatalf("with _initialize: %v", err)
	}
}

// TestModuleFaults: a module that traps or points outside its memory fails
// the render, and the broken instance's slot is given back (with one
// instance, the second render would wait for its deadline otherwise).
func TestModuleFaults(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		fn   string
		body []byte
		want string
	}{
		"alloc traps":        {"alloc", op(opUnreachable), "unreachable"},
		"alloc out of range": {"alloc", i32(-1), "input out of the module's memory"},
		// The input's last two bytes would be past the one page.
		"alloc at the end":       {"alloc", i32(64<<10 - 2), "input out of the module's memory"},
		"render traps":           {"render", op(opUnreachable), "unreachable"},
		"result_ptr traps":       {"result_ptr", op(opUnreachable), "unreachable"},
		"result_len traps":       {"result_len", op(opUnreachable), "unreachable"},
		"result_ptr past end":    {"result_ptr", i32(-1), "result out of the module's memory"},
		"result_len past end":    {"result_len", i32(64<<10 + 1), "result out of the module's memory"},
		"result_len negative":    {"result_len", i32(-1), "result out of the module's memory"},
		"result past end by one": {"result_ptr", i32(64<<10 - int32(len(renderOK)) + 1), "result out of the module's memory"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := crafted(t, abiModule().set(tc.fn, tc.body))

			for i := range 2 {
				out, err := renderSoon(t, r, []byte("data"))
				if err == nil || !strings.HasPrefix(err.Error(), "typstwasm: render: ") ||
					!strings.Contains(err.Error(), tc.want) {
					t.Fatalf("render %d: %q, %v; want an error with %q", i+1, out, err, tc.want)
				}
			}
		})
	}
}

// TestResultAtTheEnd: a result that ends exactly at the end of the memory
// is read.
func TestResultAtTheEnd(t *testing.T) {
	t.Parallel()

	m := abiModule().set("result_ptr", i32(64<<10-int32(len(renderOK))))
	m.data = []wasmData{{64<<10 - int32(len(renderOK)), renderOK}}

	if out, err := renderSoon(t, crafted(t, m), nil); err != nil || string(out) != renderOK {
		t.Fatalf("got %q, %v", out, err)
	}
}

// TestRenderErrorText: status 1 makes the result typst's error text.
func TestRenderErrorText(t *testing.T) {
	t.Parallel()

	const text = "template exploded"

	m := abiModule().set("render", i32(1)).set("result_ptr", i32(64)).set("result_len", i32(int32(len(text))))
	m.data = append(m.data, wasmData{64, text})

	_, err := renderSoon(t, crafted(t, m), nil)
	if err == nil || err.Error() != "typstwasm: render: "+text {
		t.Fatalf("got %v, want %q", err, "typstwasm: render: "+text)
	}
}

// TestResultIsCopied: the PDF is the caller's, not a view of the instance's
// memory that the next render overwrites.
func TestResultIsCopied(t *testing.T) {
	t.Parallel()

	r := crafted(t, abiModule())

	out, err := renderSoon(t, r, nil)
	if err != nil || string(out) != renderOK {
		t.Fatalf("got %q, %v", out, err)
	}

	out[0] = 'X'

	if again, err := renderSoon(t, r, nil); err != nil || string(again) != renderOK {
		t.Fatalf("second render: %q, %v", again, err)
	}
}

// reuseProbe is a render that traps on an instance that rendered before:
// it sets global 0 and traps if it was set. extra runs before the status.
func reuseProbe(extra ...[]byte) *wasmModule {
	body := append([][]byte{
		op(opGlobalGet, 0, opIf, blockEmpty, opUnreachable, opEnd),
		i32(1), op(opGlobalSet, 0),
	}, extra...)

	m := abiModule().set("render", append(body, i32(0))...)
	m.globals = []int32{0}

	return m
}

// TestInstanceReuseAndDrop: an instance is kept after a render, and dropped
// after a trap.
func TestInstanceReuseAndDrop(t *testing.T) {
	t.Parallel()

	r := crafted(t, reuseProbe())

	if _, err := renderSoon(t, r, nil); err != nil {
		t.Fatalf("first render: %v", err)
	}

	// The same instance again: the probe traps.
	if _, err := renderSoon(t, r, nil); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("second render: %v, want the probe's trap (the instance was not reused)", err)
	}

	// A new instance: the probe passes.
	if _, err := renderSoon(t, r, nil); err != nil {
		t.Fatalf("third render: %v (the trapped instance was reused)", err)
	}
}

// TestAcquireWaits: with every instance busy, a render waits for one until
// its context ends, and gets the instance a finished render gives back.
func TestAcquireWaits(t *testing.T) {
	t.Parallel()

	m := abiModule().set("render", op(opCall, 0), i32(0))
	m.imports = []wasmImport{{module: "env", name: "block"}}
	r := crafted(t, m)

	entered := make(chan struct{}, 2)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })

	t.Cleanup(release)
	hostEnv(t, r, "block", func(context.Context) {
		entered <- struct{}{}
		<-gate
	})

	a := make(chan error, 1)

	go func() {
		_, err := r.Render(context.Background(), nil)
		a <- err
	}()

	<-entered // A holds the only instance.

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	_, err := r.Render(ctx, nil)

	cancel()

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("B: %v, want context.DeadlineExceeded", err)
	}

	c := make(chan error, 1)

	go func() {
		_, err := r.Render(t.Context(), nil)
		c <- err
	}()

	select {
	case err := <-c:
		t.Fatalf("C did not wait for A: %v", err)
	case <-entered:
		t.Fatal("C ran beside A")
	case <-time.After(20 * time.Millisecond):
	}

	release()

	if err := <-a; err != nil {
		t.Fatalf("A: %v", err)
	}

	if err := <-c; err != nil {
		t.Fatalf("C: %v", err)
	}
}

// TestCanceledWhileRendering: canceling a render stops a module that loops
// forever (WithCloseOnContextDone); the error is context.Canceled, and the
// closed instance is dropped.
func TestCanceledWhileRendering(t *testing.T) {
	t.Parallel()

	// render: if env.first() { loop { br 0 } }; status 0
	m := abiModule().set("render",
		op(opCall, 0, opIf, blockEmpty, opLoop, blockEmpty, opBr, 0, opEnd, opEnd), i32(0))
	m.imports = []wasmImport{{module: "env", name: "first", results: 1}}
	r := crafted(t, m)

	started := make(chan struct{})

	var calls atomic.Int32

	hostEnv(t, r, "first", func(context.Context) uint32 {
		if calls.Add(1) == 1 {
			close(started)

			return 1
		}

		return 0
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errc := make(chan error, 1)

	go func() {
		_, err := r.Render(ctx, nil)
		errc <- err
	}()

	<-started
	cancel()

	var err error

	select {
	case err = <-errc:
	case <-time.After(10 * time.Second):
		t.Fatal("the canceled render still runs")
	}

	if err == nil || err.Error() != "typstwasm: render: module closed with context canceled" {
		t.Errorf("got %v", err)
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(%v, context.Canceled) = false", err)
	}

	var exit *sys.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != sys.ExitCodeContextCanceled {
		t.Errorf("got %#v, want a *sys.ExitError with ExitCodeContextCanceled", err)
	}

	if out, err := renderSoon(t, r, nil); err != nil || string(out) != renderOK {
		t.Fatalf("render after the cancellation: %q, %v", out, err)
	}
}

// TestClose: Close of a compiled renderer closes the runtime once; renders
// fail afterwards, with an idle instance or without.
func TestClose(t *testing.T) {
	t.Parallel()

	used := newRenderer(abiModule().encode(), Options{Instances: 1})
	if _, err := used.Render(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	unused := newRenderer(abiModule().encode(), Options{Instances: 1})
	if err := unused.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		r    *Renderer
		want string
	}{
		"idle instance": {used, "typstwasm: render: module closed with exit_code(0)"},
		"no instance":   {unused, "typstwasm: instantiate: runtime closed"},
	} {
		for i := range 2 {
			if err := tc.r.Close(t.Context()); err != nil {
				t.Fatalf("%s: Close %d: %v", name, i+1, err)
			}
		}

		if _, err := renderSoon(t, tc.r, nil); err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%s: Render after Close: %v, want %q", name, err, tc.want)
		}
	}
}

// TestMalformedInput: the real module reports an input pdf.Input would not
// build as an error text, and its instance renders on.
func TestMalformedInput(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in   []byte
		want string
	}{
		"empty":              {nil, "input too short"},
		"short length":       {[]byte{1, 0, 0}, "input too short"},
		"length beyond data": {[]byte{3, 0, 0, 0, '{', '}'}, "data length exceeds the input"},
		"length 2^32-1":      {[]byte{0xff, 0xff, 0xff, 0xff}, "data length exceeds the input"},
		"no data":            {[]byte{0, 0, 0, 0}, "failed to parse JSON"},
	} {
		if _, err := shared().Render(t.Context(), tc.in); err == nil ||
			!strings.HasPrefix(err.Error(), "typstwasm: render: "+tc.want) {
			t.Errorf("%s: %v, want the render error %q", name, err, tc.want)
		}
	}

	render(t, input(t))
}
