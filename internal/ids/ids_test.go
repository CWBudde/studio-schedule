package ids_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
)

func TestV7(t *testing.T) {
	t.Parallel()

	var src ids.V7

	prev := src.NewUUID()
	for range 1000 {
		u := src.NewUUID()

		if v := u[6] >> 4; v != 7 {
			t.Fatalf("%s: version %d, want 7", u, v)
		}

		if u.Compare(prev) <= 0 {
			t.Fatalf("%s is not greater than previous %s", u, prev)
		}

		prev = u
	}
}

func TestSequence(t *testing.T) {
	t.Parallel()

	a, b := ids.NewSequence(), ids.NewSequence()

	first := a.NewUUID()
	if got, want := first.String(), "00000000-0000-7000-8000-000000000001"; got != want {
		t.Fatalf("first = %s, want %s", got, want)
	}

	if got := b.NewUUID(); got != first {
		t.Fatalf("second sequence starts with %s, want %s", got, first)
	}

	prev := first
	for range 100 {
		u := a.NewUUID()

		if u.Compare(prev) <= 0 {
			t.Fatalf("%s is not greater than previous %s", u, prev)
		}

		prev = u
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	id := ids.New[domain.PersonID](ids.NewSequence())

	if got, want := id.String(), "00000000-0000-7000-8000-000000000001"; got != want {
		t.Fatalf("New = %s, want %s", got, want)
	}
}
