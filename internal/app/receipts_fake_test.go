package app_test

import (
	"context"
	"encoding/binary"
)

// fakePDF stands in for the typst renderer in the app tests: a PDF header
// and the template data, so a test can read what would be printed.
type fakePDF struct{}

func (fakePDF) Render(_ context.Context, input []byte) ([]byte, error) {
	n := binary.LittleEndian.Uint32(input)

	return append([]byte("%PDF-fake"), input[4:4+n]...), nil
}
