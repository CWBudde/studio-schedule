package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Duplicate detection (ADR 0015): the office's list of pairs of persons that
// may be the same human, computed live from every person that is not
// merged. A pair the office dismissed ("not a duplicate") is left out, and
// a family (a guardian and its dependent, or two dependents of one
// guardian) does not match by a shared email alone.

// MaxDuplicatePairs is the most pairs Duplicates returns.
const MaxDuplicatePairs = 200

// DuplicateStrength says how sure a pair is.
type DuplicateStrength string

// Strengths, the surer first.
const (
	// DuplicateStrong: the same email, ignoring case (the person's own or
	// its account's), or the same name and birth date.
	DuplicateStrong DuplicateStrength = "strong"
	// DuplicateWeak: the same name, and at least one birth date unknown.
	DuplicateWeak DuplicateStrength = "weak"
)

// DuplicateReason is why a pair matched.
type DuplicateReason string

// Reasons.
const (
	ReasonSameEmail         DuplicateReason = "email"
	ReasonSameNameBirthDate DuplicateReason = "name-birth-date"
	ReasonSameName          DuplicateReason = "name"
)

// DuplicateCandidate is a person of a pair with what helps to tell it apart.
type DuplicateCandidate struct {
	Person domain.Person
	// Account is the person's login; nil if it has none.
	Account *domain.Account
}

// DuplicatePair is two persons that may be the same, A ordered before B like
// PersonRepo.List.
type DuplicatePair struct {
	A, B     DuplicateCandidate
	Strength DuplicateStrength
	Reasons  []DuplicateReason
}

// ErrSamePerson rejects a pair of a person with itself.
var ErrSamePerson error = &domain.FieldError{Field: "personB", Msg: "the persons must differ"}

// Duplicates returns the pairs of persons that may be duplicates, strong
// ones first, then by the first person's name; at most MaxDuplicatePairs.
func (s *PersonService) Duplicates(ctx context.Context) ([]DuplicatePair, error) {
	var out []DuplicatePair

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		persons, err := tx.Persons().List(ctx)
		if err != nil {
			return err
		}

		accounts, err := tx.Accounts().List(ctx)
		if err != nil {
			return err
		}

		dismissals, err := tx.DuplicateDismissals().List(ctx)
		if err != nil {
			return err
		}

		links, err := tx.Persons().ListGuardianLinks(ctx, mapSlice(persons, func(p domain.Person) domain.PersonID {
			return p.ID
		}))
		if err != nil {
			return err
		}

		out = findDuplicates(persons, accounts, dismissals, links)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("duplicates: %w", err)
	}

	return out, nil
}

// findDuplicates pairs the persons (ordered like PersonRepo.List) by the
// rules of Duplicates. A family shares an email often: a guardian and its
// dependent, or two dependents of one guardian (links), never match by the
// email alone.
func findDuplicates(
	persons []domain.Person, accounts []domain.Account, dismissals []domain.DuplicateDismissal,
	links []domain.Guardian,
) []DuplicatePair {
	accountOf := make(map[domain.PersonID]*domain.Account, len(accounts))
	for i := range accounts {
		accountOf[accounts[i].PersonID] = &accounts[i]
	}

	dismissed := make(map[[2]domain.PersonID]bool, len(dismissals))
	for _, d := range dismissals {
		dismissed[[2]domain.PersonID{d.PersonA, d.PersonB}] = true
	}

	index := make(map[domain.PersonID]int, len(persons))
	for i, p := range persons {
		index[p.ID] = i
	}

	// Group by each key; a pair collects the reasons of every group it
	// shares.
	type pairKey struct{ a, b int }

	reasons := map[pairKey][]DuplicateReason{}
	groups := func(key func(domain.Person) []string, reason DuplicateReason) {
		byKey := map[string][]int{}

		for i, p := range persons {
			for _, k := range key(p) {
				if k != "" && !slices.Contains(byKey[k], i) {
					byKey[k] = append(byKey[k], i)
				}
			}
		}

		for _, members := range byKey {
			for x, i := range members {
				for _, j := range members[x+1:] {
					k := pairKey{min(i, j), max(i, j)}
					if !slices.Contains(reasons[k], reason) {
						reasons[k] = append(reasons[k], reason)
					}
				}
			}
		}
	}

	groups(func(p domain.Person) []string {
		keys := []string{strings.ToLower(p.Email)}
		if a := accountOf[p.ID]; a != nil {
			keys = append(keys, strings.ToLower(a.Email))
		}

		return keys
	}, ReasonSameEmail)
	groups(func(p domain.Person) []string {
		if p.BirthDate.IsZero() {
			return nil
		}

		return []string{nameKey(p.GivenName, p.FamilyName) + "|" + p.BirthDate.String()}
	}, ReasonSameNameBirthDate)

	// The same name with a birth date missing on either side.
	byName := map[string][]int{}
	for i, p := range persons {
		k := nameKey(p.GivenName, p.FamilyName)
		byName[k] = append(byName[k], i)
	}

	for _, members := range byName {
		for x, i := range members {
			for _, j := range members[x+1:] {
				if persons[i].BirthDate.IsZero() || persons[j].BirthDate.IsZero() {
					k := pairKey{min(i, j), max(i, j)}
					reasons[k] = append(reasons[k], ReasonSameName)
				}
			}
		}
	}

	family := familyPairs(links)
	out := make([]DuplicatePair, 0, len(reasons))

	for k, rs := range reasons {
		a, b := persons[k.a], persons[k.b]
		if dismissed[[2]domain.PersonID{a.ID, b.ID}] || dismissed[[2]domain.PersonID{b.ID, a.ID}] {
			continue
		}

		if family[[2]domain.PersonID{a.ID, b.ID}] || family[[2]domain.PersonID{b.ID, a.ID}] {
			if rs = slices.DeleteFunc(rs, func(r DuplicateReason) bool { return r == ReasonSameEmail }); len(rs) == 0 {
				continue
			}
		}

		strength := DuplicateWeak
		if slices.Contains(rs, ReasonSameEmail) || slices.Contains(rs, ReasonSameNameBirthDate) {
			strength = DuplicateStrong
		}

		slices.Sort(rs)
		out = append(out, DuplicatePair{
			A: DuplicateCandidate{Person: a, Account: accountOf[a.ID]}, B: DuplicateCandidate{Person: b, Account: accountOf[b.ID]},
			Strength: strength, Reasons: rs,
		})
	}

	rank := func(s DuplicateStrength) int {
		if s == DuplicateStrong {
			return 0
		}

		return 1
	}

	slices.SortFunc(out, func(x, y DuplicatePair) int {
		return cmp.Or(
			cmp.Compare(rank(x.Strength), rank(y.Strength)),
			cmp.Compare(index[x.A.Person.ID], index[y.A.Person.ID]),
			cmp.Compare(index[x.B.Person.ID], index[y.B.Person.ID]),
		)
	})

	return out[:min(len(out), MaxDuplicatePairs)]
}

// familyPairs returns the pairs of persons the links make a family: each
// guardian with each of its dependents, and the dependents of a guardian
// with each other.
func familyPairs(links []domain.Guardian) map[[2]domain.PersonID]bool {
	pairs := make(map[[2]domain.PersonID]bool, len(links))
	children := map[domain.PersonID][]domain.PersonID{}

	for _, l := range links {
		pairs[[2]domain.PersonID{l.GuardianID, l.DependentID}] = true

		for _, sibling := range children[l.GuardianID] {
			pairs[[2]domain.PersonID{sibling, l.DependentID}] = true
		}

		children[l.GuardianID] = append(children[l.GuardianID], l.DependentID)
	}

	return pairs
}

// DismissDuplicate records that a and b are not the same person, so the
// pair is no longer listed; dismissing it again changes nothing. A person
// that does not exist, or is merged, is domain.ErrNotFound; a and b the same
// ErrSamePerson.
func (s *PersonService) DismissDuplicate(ctx context.Context, p Principal, a, b domain.PersonID) error {
	if a == b {
		return ErrSamePerson
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := lockLivePersons(ctx, tx, a, b); err != nil {
			return err
		}

		d := domain.NewDuplicateDismissal(a, b, actorOf(p), s.clock.Now())
		if err := d.Validate(); err != nil {
			return err
		}

		return tx.DuplicateDismissals().Create(ctx, d)
	})
	if err != nil {
		return fmt.Errorf("dismiss duplicate %s/%s: %w", a, b, err)
	}

	return nil
}

// nameReplacer folds what makes the same name look different: German
// umlauts and ß spelled out, common accents dropped, hyphens as spaces.
var nameReplacer = strings.NewReplacer(
	"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
	"á", "a", "à", "a", "â", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ø", "o",
	"ú", "u", "ù", "u", "û", "u",
	"ç", "c", "ñ", "n", "ý", "y", "ÿ", "y",
	"-", " ",
)

// nameKey is the normalised full name by which the duplicate detection and
// the import compare persons: lower case, folded by nameReplacer, single
// spaces.
func nameKey(given, family string) string {
	return strings.Join(strings.Fields(nameReplacer.Replace(strings.ToLower(given+" "+family))), " ")
}
