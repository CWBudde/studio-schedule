package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// MaxPersonMatches is the most persons SearchPersons returns.
const MaxPersonMatches = 50

// PersonService lets the office find persons, e.g. to link a staff member
// or add a class member. The HTTP layer admits only office and admin
// accounts.
type PersonService struct {
	store store.Store
}

// NewPersonService returns a PersonService backed by st.
func NewPersonService(st store.Store) *PersonService {
	return &PersonService{store: st}
}

// SearchPersons returns the persons whose given name, family name or email
// contains every word of query, ignoring case, ordered by family name and
// given name; at most MaxPersonMatches. An empty query matches everyone.
func (s *PersonService) SearchPersons(ctx context.Context, query string) ([]domain.Person, error) {
	var persons []domain.Person

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		persons, err = tx.Persons().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("search persons: %w", err)
	}

	words := strings.Fields(strings.ToLower(query))
	matches := make([]domain.Person, 0, min(len(persons), MaxPersonMatches))

	for _, p := range persons {
		if len(matches) == MaxPersonMatches {
			break
		}

		text := strings.ToLower(p.GivenName + " " + p.FamilyName + " " + p.Email)
		if containsAll(text, words) {
			matches = append(matches, p)
		}
	}

	return matches, nil
}

func containsAll(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}

	return true
}
