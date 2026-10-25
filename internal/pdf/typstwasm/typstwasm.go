//go:build !js

// Package typstwasm renders receipt PDFs on the server (ADR 0012): it runs the
// typst WebAssembly module built from tools/receipt-pdf (receipt.wasm,
// committed with Git LFS, `just typst-wasm`) in-process with wazero, no
// external program. The web demo runs the same module in the browser instead
// (internal/wasmbridge), so cmd/wasm must not import this package
// (scripts/check-wasm-deps.sh).
package typstwasm

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/cwbudde/studio-schedule/internal/pdf"
)

//go:embed receipt.wasm
var module []byte

// Module returns the embedded typst module.
func Module() []byte { return module }

// DefaultInstances is how many renders run at once by default.
const DefaultInstances = 2

// maxMemory is the linear memory beyond which an instance is dropped after a
// render rather than reused: WebAssembly memory never shrinks.
const maxMemory = 256 << 20

// Options configure a Renderer.
type Options struct {
	// Instances is how many renders may run at once (instances are not safe
	// for concurrent use, so each render takes one); DefaultInstances if
	// zero.
	Instances int
	// CacheDir, if set, keeps the compiled module between runs, so a restart
	// skips the compilation of a few seconds.
	CacheDir string
}

// Renderer renders PDFs with the typst module. It compiles the module in the
// background when created; renders wait for that. Use it from any goroutine.
type Renderer struct {
	ready    chan struct{}
	err      error // set before ready closes
	rt       wazero.Runtime
	compiled wazero.CompiledModule

	// slots holds one token per instance that may exist; idle holds the
	// instances that exist and are free.
	slots chan struct{}
	idle  chan api.Module

	closeOnce sync.Once
}

// New returns a Renderer and starts compiling the module in the background.
func New(opts Options) *Renderer { return newRenderer(module, opts) }

func newRenderer(module []byte, opts Options) *Renderer {
	n := opts.Instances
	if n <= 0 {
		n = DefaultInstances
	}

	r := &Renderer{ready: make(chan struct{}), slots: make(chan struct{}, n), idle: make(chan api.Module, n)}
	for range n {
		r.slots <- struct{}{}
	}

	go r.compile(module, opts.CacheDir)

	return r
}

func (r *Renderer) compile(module []byte, cacheDir string) {
	defer close(r.ready)

	if !bytes.HasPrefix(module, pdf.Magic) {
		r.err = errors.New("typstwasm: receipt.wasm is not a WebAssembly module (a Git LFS pointer? run git lfs pull)")

		return
	}

	// The context only bounds the compilation; renders pass their own.
	ctx := context.Background()
	cfg := wazero.NewRuntimeConfig().WithCloseOnContextDone(true)

	if cacheDir != "" {
		cache, err := wazero.NewCompilationCacheWithDir(cacheDir)
		if err != nil {
			r.err = fmt.Errorf("typstwasm: compilation cache: %w", err)

			return
		}

		cfg = cfg.WithCompilationCache(cache)
	}

	r.rt = wazero.NewRuntimeWithConfig(ctx, cfg)
	wasi_snapshot_preview1.MustInstantiate(ctx, r.rt)

	compiled, err := r.rt.CompileModule(ctx, module)
	if err != nil {
		r.err = fmt.Errorf("typstwasm: compile: %w", err)

		return
	}

	r.compiled = compiled
}

// Ready waits until the module is compiled and reports whether that failed.
func (r *Renderer) Ready(ctx context.Context) error {
	select {
	case <-r.ready:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// zeros is an endless source of zero bytes: the module's random numbers (only
// hash seeds) are the same on every run and in the demo.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
}

// Render runs the module on an input built by pdf.Input and returns the PDF.
// A template error comes back as an error with typst's message.
func (r *Renderer) Render(ctx context.Context, input []byte) ([]byte, error) {
	if err := r.Ready(ctx); err != nil {
		return nil, err
	}

	m, err := r.acquire(ctx)
	if err != nil {
		return nil, err
	}

	out, renderErr, err := call(ctx, m, input)

	if err != nil || m.Memory().Size() > maxMemory {
		// A trapped or canceled instance is unusable, and a large one is
		// not worth keeping.
		_ = m.Close(context.WithoutCancel(ctx))
		r.slots <- struct{}{}
	} else {
		r.idle <- m
	}

	switch {
	case err != nil:
		return nil, fmt.Errorf("typstwasm: render: %w", err)
	case renderErr != "":
		return nil, fmt.Errorf("typstwasm: render: %s", renderErr)
	}

	return out, nil
}

// acquire takes an idle instance, or a slot to create one, waiting if all
// are in use.
func (r *Renderer) acquire(ctx context.Context) (api.Module, error) {
	select {
	case m := <-r.idle:
		return m, nil
	default:
	}

	select {
	case m := <-r.idle:
		return m, nil
	case <-r.slots:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	m, err := r.rt.InstantiateModule(ctx, r.compiled, wazero.NewModuleConfig().
		WithName(""). // anonymous: several instances of one module
		WithStartFunctions("_initialize").
		WithStdout(io.Discard).WithStderr(io.Discard).
		WithRandSource(zeros{}))
	if err != nil {
		r.slots <- struct{}{}

		return nil, fmt.Errorf("typstwasm: instantiate: %w", err)
	}

	return m, nil
}

// call writes input into the instance and renders it: out is the PDF, or
// renderErr typst's error text; err reports a broken instance.
func call(ctx context.Context, m api.Module, input []byte) (out []byte, renderErr string, err error) {
	ptr, err := m.ExportedFunction("alloc").Call(ctx, uint64(len(input)))
	if err != nil {
		return nil, "", err
	}

	// wasm32: pointers and lengths are 32 bits.
	if !m.Memory().Write(uint32(ptr[0]), input) { //nolint:gosec // see above
		return nil, "", errors.New("input out of the module's memory")
	}

	status, err := m.ExportedFunction("render").Call(ctx, ptr[0], uint64(len(input)))
	if err != nil {
		return nil, "", err
	}

	rp, err := m.ExportedFunction("result_ptr").Call(ctx)
	if err != nil {
		return nil, "", err
	}

	rl, err := m.ExportedFunction("result_len").Call(ctx)
	if err != nil {
		return nil, "", err
	}

	res, ok := m.Memory().Read(uint32(rp[0]), uint32(rl[0])) //nolint:gosec // wasm32 pointers
	if !ok {
		return nil, "", errors.New("result out of the module's memory")
	}

	if status[0] != 0 {
		return nil, string(res), nil
	}

	return bytes.Clone(res), "", nil
}

// Close releases the runtime and its instances; renders fail afterwards.
func (r *Renderer) Close(ctx context.Context) error {
	var err error

	r.closeOnce.Do(func() {
		<-r.ready

		if r.rt != nil {
			err = r.rt.Close(ctx)
		}
	})

	return err
}
