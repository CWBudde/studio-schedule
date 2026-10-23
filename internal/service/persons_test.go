package service_test

import (
	"context"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestSearchPersons(t *testing.T) {
	t.Parallel()

	svc := service.NewPersonService(catalogDemoStore(t))
	ctx := t.Context()

	for query, want := range map[string]string{
		"AMBERLY clara":               "Clara Amberly",
		"olga.ledgerby@example":       "Olga Ledgerby",
		"  felix   keys  ":            "Felix Keysworth",
		"clara amberly nobody-at-all": "",
	} {
		found, err := svc.SearchPersons(ctx, query)
		if err != nil {
			t.Fatal(err)
		}

		var names []string
		for _, p := range found {
			names = append(names, p.GivenName+" "+p.FamilyName)
		}

		if (want == "" && len(found) != 0) || (want != "" && (len(found) != 1 || names[0] != want)) {
			t.Errorf("SearchPersons(%q) = %v, want [%s]", query, names, want)
		}
	}

	everyone, err := svc.SearchPersons(ctx, "")
	if err != nil || len(everyone) == 0 {
		t.Fatalf("empty query: %d persons, %v", len(everyone), err)
	}
}

func TestSearchPersonsLimit(t *testing.T) {
	t.Parallel()

	st := memory.New()
	seq := ids.NewSequence()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for range service.MaxPersonMatches + 5 {
			p := domain.Person{ID: ids.New[domain.PersonID](seq), GivenName: "Sam", FamilyName: "Example"}
			if err := tx.Persons().Create(ctx, p); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	found, err := service.NewPersonService(st).SearchPersons(t.Context(), "sam")
	if err != nil || len(found) != service.MaxPersonMatches {
		t.Errorf("found %d, %v; want %d", len(found), err, service.MaxPersonMatches)
	}
}
