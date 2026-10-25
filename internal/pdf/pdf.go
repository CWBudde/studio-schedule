// Package pdf holds what both receipt renderers share (ADR 0012): the input
// format of the typst WebAssembly module built from tools/receipt-pdf. The
// server runs the module with wazero (package typstwasm), the web demo with
// the browser's WebAssembly (internal/wasmbridge and the worker), so both
// produce the same bytes from the same input.
//
// The package compiles for both targets and must stay free of the module
// itself, which only the server embeds.
package pdf

import (
	"encoding/binary"
	"errors"
	"math"
)

// ErrInputTooLarge reports data beyond the size the module's input format can
// describe.
var ErrInputTooLarge = errors.New("pdf: input too large")

// Input returns the module's input buffer: the length of the JSON data as a
// little-endian uint32, the data (the template's /data.json), then the bytes
// of an optional logo (/logo.png or /logo.jpg).
func Input(data, logo []byte) ([]byte, error) {
	if len(data) > math.MaxUint32 || len(data)+len(logo) > math.MaxInt32-4 {
		return nil, ErrInputTooLarge
	}

	out := make([]byte, 4, 4+len(data)+len(logo))
	binary.LittleEndian.PutUint32(out, uint32(len(data))) //nolint:gosec // bounded above
	out = append(out, data...)

	return append(out, logo...), nil
}

// Magic starts every WebAssembly module. A checkout without Git LFS has a
// small text pointer in place of the module, which this tells apart.
var Magic = []byte("\x00asm")
