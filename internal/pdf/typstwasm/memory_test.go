//go:build !js

package typstwasm

import (
	"testing"

	"github.com/tetratelabs/wazero/api"
)

// pagesMemory is a memory of a given number of pages; only Grow(0), the
// page count, and Size work. Size overflows like wazero's: 0 at 65536 pages.
type pagesMemory struct {
	api.Memory
	pages uint32
}

func (m pagesMemory) Grow(delta uint32) (uint32, bool) {
	if delta != 0 {
		panic("pagesMemory only counts")
	}

	return m.pages, true
}

func (m pagesMemory) Size() uint32 { return m.pages << 16 }

// TestTooLarge pins the rule that drops an instance after a good render:
// memory beyond maxMemory, counted in pages, as Memory.Size overflows to 0
// at the 4 GiB maximum. (A crafted module that really grows that far makes
// the test process 0.5 GB larger for 256 MiB, 4 GB for 4 GiB.)
func TestTooLarge(t *testing.T) {
	t.Parallel()

	for pages, want := range map[uint32]bool{1: false, 4096: false, 4097: true, 65536: true} {
		if got := tooLarge(pagesMemory{pages: pages}); got != want {
			t.Errorf("%d pages: tooLarge %v, want %v", pages, got, want)
		}
	}
}
