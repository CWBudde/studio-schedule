package domain_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

const sampleID = "01890a5d-ac96-774b-bcce-b302099a8057"

func TestParseIDRoundTrip(t *testing.T) {
	t.Parallel()

	id, err := domain.ParseID[domain.LessonID](sampleID)
	if err != nil {
		t.Fatalf("ParseID: %v", err)
	}

	if got := id.String(); got != sampleID {
		t.Fatalf("String = %q, want %q", got, sampleID)
	}

	if id.IsZero() {
		t.Fatal("parsed ID reports IsZero")
	}
}

func TestParseIDInvalid(t *testing.T) {
	t.Parallel()

	for _, s := range []string{
		"",
		"not-a-uuid",
		sampleID + "0",
		"{" + sampleID + "}",
		"urn:uuid:" + sampleID,
		"01890a5dac96774bbcceb302099a8057",
		"00000000-0000-0000-0000-000000000000",
	} {
		if _, err := domain.ParseID[domain.PersonID](s); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("ParseID(%q): want ErrValidation, got %v", s, err)
		}
	}
}

func TestIDZero(t *testing.T) {
	t.Parallel()

	var id domain.BookingID
	if !id.IsZero() {
		t.Fatal("zero value does not report IsZero")
	}
}

func TestIDJSON(t *testing.T) {
	t.Parallel()

	type payload struct {
		ID domain.ClassID `json:"id"`
	}

	in := payload{ID: mustParse[domain.ClassID](t, sampleID)}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if want := `{"id":"` + sampleID + `"}`; string(data) != want {
		t.Fatalf("Marshal = %s, want %s", data, want)
	}

	var out payload
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if out != in {
		t.Fatalf("round trip = %v, want %v", out.ID, in.ID)
	}

	if err := json.Unmarshal([]byte(`{"id":"nope"}`), &out); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Unmarshal invalid: want ErrValidation, got %v", err)
	}
}

func mustParse[I domain.AnyID](t *testing.T, s string) I {
	t.Helper()

	id, err := domain.ParseID[I](s)
	if err != nil {
		t.Fatalf("ParseID(%q): %v", s, err)
	}

	return id
}
