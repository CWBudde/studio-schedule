//go:build !js

package typstwasm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/pdf"
	"github.com/cwbudde/studio-schedule/internal/pdf/pdftext"
)

var update = flag.Bool("update", false, "rewrite the golden checksum in testdata")

// shared is one renderer for the package's tests: the compilation takes a
// few seconds (more with the race detector), so it is cached on disk too.
var shared = sync.OnceValue(func() *Renderer {
	opts := Options{Instances: 2}
	if dir, err := os.UserCacheDir(); err == nil {
		opts.CacheDir = filepath.Join(dir, "studio-schedule", "wazero-test")
	}

	return New(opts)
})

// logo is a small PNG, encoded the same way on every run.
func logo(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := range 40 {
		for y := range 20 {
			img.Set(x, y, color.RGBA{R: uint8(x * 6), G: 80, B: uint8(y * 12), A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func input(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/receipt.json")
	if err != nil {
		t.Fatal(err)
	}

	in, err := pdf.Input(data, logo(t))
	if err != nil {
		t.Fatal(err)
	}

	return in
}

func render(t *testing.T, in []byte) []byte {
	t.Helper()

	out, err := shared().Render(t.Context(), in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	return out
}

func sum(b []byte) string {
	s := sha256.Sum256(b)

	return hex.EncodeToString(s[:])
}

// TestGolden renders the sample receipt: the bytes are the same on every run
// (and in the web demo, which runs the same module), and the PDF shows the
// data, umlauts and euro signs included.
func TestGolden(t *testing.T) {
	t.Parallel()

	in := input(t)
	out := render(t, in)

	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %q", out[:min(len(out), 16)])
	}

	if again := render(t, in); !bytes.Equal(out, again) {
		t.Fatal("two renders of the same data differ")
	}

	const golden = "testdata/receipt.pdf.sha256"

	if *update {
		if err := os.WriteFile(golden, []byte(sum(out)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run the test with -update to write it)", err)
	}

	if got := sum(out); got != strings.TrimSpace(string(want)) {
		t.Errorf("sha256 %s, golden %s: the template or the module changed; check the PDF and run with -update", got,
			strings.TrimSpace(string(want)))
	}

	text, err := pdftext.Text(out)
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range []string{
		"Tanzstudio Beispiel", "Übungsweg 12", "Jürgen Öztürk", "Musterstraße 3", "SD-2026-0001", "24.10.2026",
		"Quittung", "10er-Karte für Lea Öztürk", "150,00 €", "126,05 €", "Umsatzsteuer 19 %", "23,95 €",
		"Gesamtbetrag", "Steuernummer 12/345/67890",
	} {
		if !strings.Contains(text, s) {
			t.Errorf("the PDF does not show %q; its text:\n%s", s, text)
		}
	}

	if !bytes.Contains(out, []byte("/Subtype/Image")) {
		t.Error("the PDF has no image: the logo is missing")
	}
}

// TestGoldenWithoutLogo pins the sample without a logo, which
// scripts/receipt-pdf-smoke.mjs renders with Node's WebAssembly and the
// demo's WASI shim: the same module gives the same bytes there.
func TestGoldenWithoutLogo(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/receipt.json")
	if err != nil {
		t.Fatal(err)
	}

	// The template asks for no logo.
	data = bytes.Replace(data, []byte(`"logo": "png"`), []byte(`"logo": ""`), 1)

	in, err := pdf.Input(data, nil)
	if err != nil {
		t.Fatal(err)
	}

	got := sum(render(t, in))

	const golden = "testdata/receipt-nologo.pdf.sha256"

	if *update {
		if err := os.WriteFile(golden, []byte(got+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run the test with -update to write it)", err)
	}

	if got != strings.TrimSpace(string(want)) {
		t.Errorf("sha256 %s, golden %s", got, strings.TrimSpace(string(want)))
	}
}

// TestConcurrentRenders runs more renders than instances at once: each gets
// an instance of its own and the same bytes.
func TestConcurrentRenders(t *testing.T) {
	t.Parallel()

	in := input(t)
	want := sum(render(t, in))

	var wg sync.WaitGroup

	for range 6 {
		wg.Go(func() {
			out, err := shared().Render(t.Context(), in)
			if err != nil {
				t.Errorf("Render: %v", err)

				return
			}

			if got := sum(out); got != want {
				t.Errorf("concurrent render gave %s, want %s", got, want)
			}
		})
	}

	wg.Wait()
}

// TestTemplateToleratesMissingLists: lists the data leaves out or sets to
// null print nothing rather than fail.
func TestTemplateToleratesMissingLists(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/receipt.json")
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}

	doc["summary"], doc["notes"], doc["meta"], doc["logo"] = nil, nil, nil, nil
	delete(doc, "footer")

	if data, err = json.Marshal(doc); err != nil {
		t.Fatal(err)
	}

	in, err := pdf.Input(data, nil)
	if err != nil {
		t.Fatal(err)
	}

	if out := render(t, in); !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
}

func TestTemplateErrors(t *testing.T) {
	t.Parallel()

	for name, data := range map[string]string{
		"not JSON":      "{",
		"missing field": `{"title": "x"}`,
	} {
		in, err := pdf.Input([]byte(data), nil)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := shared().Render(t.Context(), in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	// The instance survives an error.
	render(t, input(t))
}

func TestCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := shared().Render(ctx, input(t)); err == nil {
		t.Fatal("a canceled render succeeded")
	}
}

func TestNotAModule(t *testing.T) {
	t.Parallel()

	r := newRenderer([]byte("version https://git-lfs.github.com/spec/v1\n"), Options{})
	defer r.Close(t.Context())

	_, err := r.Render(t.Context(), []byte{0, 0, 0, 0})
	if err == nil || !strings.Contains(err.Error(), "git lfs pull") {
		t.Fatalf("got %v, want the hint about Git LFS", err)
	}
}

func TestEmbeddedModuleMatchesPin(t *testing.T) {
	t.Parallel()

	pin, err := os.ReadFile("receipt.wasm.sha256")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := sum(Module()), strings.Fields(string(pin))[0]; got != want {
		t.Fatalf("receipt.wasm has sha256 %s, receipt.wasm.sha256 pins %s (just typst-wasm; git lfs pull)", got, want)
	}
}
