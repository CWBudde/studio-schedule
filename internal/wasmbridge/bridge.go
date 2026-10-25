// Package wasmbridge lets JavaScript call the Go http.Handler in-process when
// the application is compiled to WebAssembly.
//
// The pure-Go part in this file (request building, response capturing) is
// platform-independent and unit-tested on the host. The syscall/js glue that
// exposes globalThis.StudioScheduleAPI lives in bridge_js.go.
package wasmbridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Host is the Host header of every bridged request. Bridged requests carry
// neither Origin nor Sec-Fetch-Site, so http.CrossOriginProtection treats
// them as same-origin, non-browser requests and lets them pass.
const Host = "localhost"

// Header is one header line as a [name, value] pair, matching the JS shape
// used by the bridge (an array of two-element arrays).
type Header [2]string

// Request is the transport-neutral form of a JS request object.
type Request struct {
	Method  string
	URL     string // path and optional query, e.g. "/api/v1/health?x=1"
	Headers []Header
	Body    []byte // nil for no body
}

// Response is the transport-neutral form of the JS response object.
type Response struct {
	Status  int
	Headers []Header // sorted by name; one entry per value
	Body    []byte
}

var errBadURL = errors.New("wasmbridge: url must be a path starting with '/'")

// SnapshotGlobal is the name of the global through which the worker hands
// the saved data of an earlier visit to the Go program before it starts:
// a Uint8Array, or absent.
const SnapshotGlobal = "__studioScheduleSnapshot"

// ChangedGlobal is the name of the function NotifyChanged calls: the worker
// saves the data again soon after.
const ChangedGlobal = "__studioScheduleChanged"

// RenderPDFGlobal is the name of the worker's function that renders a
// receipt PDF with the typst module in the browser:
// (input: Uint8Array) → Promise<Uint8Array>, input as built by pdf.Input.
const RenderPDFGlobal = "__studioScheduleRenderPDF"

// Persistence lets the worker keep the demo's data across page loads (see
// docs/demo-mode.md): it saves Snapshot after changes (NotifyChanged) and
// passes it back through SnapshotGlobal on the next start.
type Persistence struct {
	// Snapshot encodes the current data; nil disables saving.
	Snapshot func() ([]byte, error)
	// Restored reports whether the program started from the data the worker
	// handed over. If not, the worker drops its saved state.
	Restored bool
}

// NewHTTPRequest converts r into an *http.Request addressed to Host.
// Absolute URLs are accepted, but only their path and query are used.
func NewHTTPRequest(ctx context.Context, r Request) (*http.Request, error) {
	method := strings.ToUpper(r.Method)
	if method == "" {
		method = http.MethodGet
	}

	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, fmt.Errorf("wasmbridge: parse url: %w", err)
	}

	if !strings.HasPrefix(u.Path, "/") {
		return nil, errBadURL
	}

	target := &url.URL{Scheme: "http", Host: Host, Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(r.Body))
	if err != nil {
		return nil, fmt.Errorf("wasmbridge: build request: %w", err)
	}

	if r.Body == nil {
		req.Body = http.NoBody
		req.ContentLength = 0
	}

	for _, h := range r.Headers {
		if strings.EqualFold(h[0], "Host") {
			continue
		}

		req.Header.Add(h[0], h[1])
	}

	req.RemoteAddr = "127.0.0.1:0"
	req.RequestURI = target.RequestURI()

	return req, nil
}

// Serve runs r through h and captures the response. A panic in h is
// converted into an error; HEAD responses carry no body.
func Serve(ctx context.Context, h http.Handler, r Request) (resp Response, err error) {
	req, err := NewHTTPRequest(ctx, r)
	if err != nil {
		return Response{}, err
	}

	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("wasmbridge: handler panic: %v", rec)
		}
	}()

	w := NewResponseWriter()
	h.ServeHTTP(w, req)

	resp = w.Result()
	if req.Method == http.MethodHead {
		resp.Body = nil
	}

	return resp, nil
}

// ResponseWriter is a minimal in-memory http.ResponseWriter. Like net/http,
// it snapshots the headers when the status is written; later header changes
// are ignored.
type ResponseWriter struct {
	header      http.Header
	sent        http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

var _ http.Flusher = (*ResponseWriter)(nil)

// NewResponseWriter returns an empty ResponseWriter.
func NewResponseWriter() *ResponseWriter {
	return &ResponseWriter{header: make(http.Header)}
}

// Header implements http.ResponseWriter.
func (w *ResponseWriter) Header() http.Header { return w.header }

// WriteHeader implements http.ResponseWriter. Informational (1xx) codes are
// ignored; only the first final status counts.
func (w *ResponseWriter) WriteHeader(code int) {
	if w.wroteHeader || code < 200 {
		return
	}

	w.status = code
	w.wroteHeader = true
	w.sent = w.header.Clone()
}

// Write implements http.ResponseWriter. If no Content-Type was set it is
// sniffed from the first bytes, as net/http does.
func (w *ResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		if w.header.Get("Content-Type") == "" && len(p) > 0 {
			w.header.Set("Content-Type", http.DetectContentType(p))
		}

		w.WriteHeader(http.StatusOK)
	}

	return w.body.Write(p)
}

// Flush implements http.Flusher; it is a no-op because the whole response is
// buffered.
func (w *ResponseWriter) Flush() {}

// SetReadDeadline is a no-op so that http.ResponseController and Huma's
// body-read timeout do not report an unsupported writer.
func (w *ResponseWriter) SetReadDeadline(time.Time) error { return nil }

// SetWriteDeadline is a no-op, see SetReadDeadline.
func (w *ResponseWriter) SetWriteDeadline(time.Time) error { return nil }

// Result returns the captured response. Without any write it reports 200
// with an empty body, like net/http.
func (w *ResponseWriter) Result() Response {
	status, header := w.status, w.sent
	if !w.wroteHeader {
		status, header = http.StatusOK, w.header
	}

	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}

	slices.Sort(names)

	headers := make([]Header, 0, len(names))
	for _, name := range names {
		for _, v := range header[name] {
			headers = append(headers, Header{name, v})
		}
	}

	return Response{Status: status, Headers: headers, Body: bytes.Clone(w.body.Bytes())}
}
