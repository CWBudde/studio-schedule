package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The member import (ADR 0015) reads a CSV file of members, each row a
// person with up to two guardians, and creates or updates the persons. A
// member is found again by its member number, a guardian by its email (an
// account's or a person's, ignoring case) or else by its name. An update
// writes only the mapped fields the row fills; guardian links are added,
// never removed. No accounts are created. A dry run is the same code in a
// transaction that is rolled back; applying sends the file again, so the
// server keeps nothing between the two.

// MaxImportRunsListed is how many of the latest import runs ImportRuns
// returns.
const MaxImportRunsListed = 50

// ImportSampleRows is how many data rows Inspect shows.
const ImportSampleRows = 5

// Lengths the import accepts, like the office's person form.
const (
	maxImportName  = 100
	maxImportEmail = 254
	maxImportPhone = 50
	maxImportNotes = domain.MaxNotesLength
)

// ImportField is a target of a column: a field of the member, or of one of
// its two guardians.
type ImportField string

// Target fields.
const (
	FieldMemberNo   ImportField = "memberNo"
	FieldGivenName  ImportField = "givenName"
	FieldFamilyName ImportField = "familyName"
	FieldEmail      ImportField = "email"
	FieldPhone      ImportField = "phone"
	FieldBirthDate  ImportField = "birthDate"
	FieldNotes      ImportField = "notes"

	FieldGuardian1GivenName  ImportField = "guardian1GivenName"
	FieldGuardian1FamilyName ImportField = "guardian1FamilyName"
	FieldGuardian1Email      ImportField = "guardian1Email"
	FieldGuardian1Phone      ImportField = "guardian1Phone"
	FieldGuardian2GivenName  ImportField = "guardian2GivenName"
	FieldGuardian2FamilyName ImportField = "guardian2FamilyName"
	FieldGuardian2Email      ImportField = "guardian2Email"
	FieldGuardian2Phone      ImportField = "guardian2Phone"
)

// ImportFields returns every target field, in the order of a form.
func ImportFields() []ImportField {
	return []ImportField{
		FieldMemberNo, FieldGivenName, FieldFamilyName, FieldEmail, FieldPhone, FieldBirthDate, FieldNotes,
		FieldGuardian1GivenName, FieldGuardian1FamilyName, FieldGuardian1Email, FieldGuardian1Phone,
		FieldGuardian2GivenName, FieldGuardian2FamilyName, FieldGuardian2Email, FieldGuardian2Phone,
	}
}

// Valid reports whether f is a known target field.
func (f ImportField) Valid() bool { return slices.Contains(ImportFields(), f) }

// guardianFields are the fields of guardian slot 1 or 2: given name, family
// name, email and phone.
func guardianFields(slot int) [4]ImportField {
	if slot == 1 {
		return [4]ImportField{FieldGuardian1GivenName, FieldGuardian1FamilyName, FieldGuardian1Email, FieldGuardian1Phone}
	}

	return [4]ImportField{FieldGuardian2GivenName, FieldGuardian2FamilyName, FieldGuardian2Email, FieldGuardian2Phone}
}

// ImportMapping says which column (by its header) fills which field, and
// which further columns go into the member's notes, each as a line
// "Header: value".
type ImportMapping struct {
	Columns map[ImportField]string
	Notes   []string
}

// ImportFile is an uploaded file.
type ImportFile struct {
	Name string
	Data []byte
}

// ImportInspection is what Inspect tells about a file before the mapping.
type ImportInspection struct {
	Encoding string
	// Delimiter is "semicolon", "comma" or "tab".
	Delimiter string
	Headers   []string
	Rows      int
	// Sample are the first ImportSampleRows data rows.
	Sample [][]string
	// Suggested maps the headers it recognizes (English and German names).
	Suggested ImportMapping
}

// ImportAction is what an import does with a row's member or guardian.
type ImportAction string

// Actions.
const (
	ImportCreate    ImportAction = "create"
	ImportUpdate    ImportAction = "update"
	ImportUnchanged ImportAction = "unchanged"
	ImportError     ImportAction = "error"
)

// Codes of the problems of a row (ImportIssue, domain.ImportRowError).
const (
	// IssueRequired: a field that must be filled is empty.
	IssueRequired = "required"
	// IssueInvalid: the value is not one the field takes (an email address,
	// a date as DD.MM.YYYY or YYYY-MM-DD, or a value too long).
	IssueInvalid = "invalid"
	// IssueFuture: the birth date lies after today.
	IssueFuture = "future"
	// IssueMemberNoRepeated: an earlier row has the member number.
	IssueMemberNoRepeated = "member-no-repeated"
	// IssueGuardianAmbiguous: several persons match the guardian.
	IssueGuardianAmbiguous = "guardian-ambiguous"
	// IssueGuardianIsMember: the guardian is the member itself.
	IssueGuardianIsMember = "guardian-is-member"
	// IssueGuardianCycle: the guardian is a dependent of the member.
	IssueGuardianCycle = "guardian-cycle"
)

// ImportIssue is a problem of a row: a code and the field it is about.
type ImportIssue struct {
	Code  string
	Field ImportField
}

// ImportGuardian is what a row does with one of its guardians.
type ImportGuardian struct {
	Slot       int
	GivenName  string
	FamilyName string
	// PersonID is the guardian found or (when applied) created; zero for
	// one the dry run creates.
	PersonID domain.PersonID
	Action   ImportAction
	// MatchedBy is "email" or "name"; empty for a new guardian.
	MatchedBy string
	// Linked tells that the row links the guardian to the member.
	Linked bool
}

// ImportRow is what an import does, or would do, with a row.
type ImportRow struct {
	Line       int
	MemberNo   string
	GivenName  string
	FamilyName string
	// PersonID is the member found, or created when applied; zero for an
	// error and for a member a dry run would create.
	PersonID domain.PersonID
	Action   ImportAction
	// Changes are the fields a create or update writes.
	Changes []ImportField
	Issues  []ImportIssue
	// Duplicates are persons a new member may be the same as: the same
	// email, or the same name and birth date (Duplicates); one an earlier
	// row of a dry run creates has a zero ID.
	Duplicates []domain.Person
	Guardians  []ImportGuardian
}

// ImportResult is the outcome of a dry run or of an applied import.
type ImportResult struct {
	Applied bool
	// RunID is the recorded run; zero for a dry run.
	RunID  domain.ImportRunID
	Counts domain.ImportCounts
	Rows   []ImportRow
}

// ImportRunEntry is a run with the account that ran it (zero for the
// command line).
type ImportRunEntry struct {
	Run   domain.ImportRun
	Actor AccountEntry
}

// ImportService imports members from CSV files. The HTTP layer admits only
// office and admin accounts; the command line uses it too.
type ImportService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
}

// NewImportService returns an ImportService.
func NewImportService(st store.Store, clk clock.Clock, src ids.Source) *ImportService {
	return &ImportService{store: st, clock: clk, ids: src}
}

// errDryRun rolls a dry run's transaction back.
var errDryRun = errors.New("dry run")

// Inspect parses a file and suggests a mapping of its headers. A file that
// cannot be read is a validation error on its content.
func (s *ImportService) Inspect(f ImportFile) (ImportInspection, error) {
	t, err := parseFile(f)
	if err != nil {
		return ImportInspection{}, fmt.Errorf("inspect %q: %w", f.Name, err)
	}

	out := ImportInspection{
		Encoding: t.Encoding, Delimiter: delimiterName(t.Delimiter), Headers: t.Header, Rows: len(t.Rows),
		Suggested: SuggestMapping(t.Header),
	}

	for _, r := range t.Rows[:min(len(t.Rows), ImportSampleRows)] {
		out.Sample = append(out.Sample, r.Cells)
	}

	return out, nil
}

func delimiterName(r rune) string {
	switch r {
	case ',':
		return "comma"
	case '\t':
		return "tab"
	default:
		return "semicolon"
	}
}

// Import runs the import of file with mapping for the account actor (zero
// for the command line): with apply, it writes and records the run; without,
// it reports what it would do and writes nothing. Rows with problems are
// left out and reported; the others are imported. A file that cannot be
// read is a validation error on its content, a mapping that does not fit
// the file one on the mapping.
func (s *ImportService) Import(
	ctx context.Context, actor domain.AccountID, f ImportFile, mapping ImportMapping, apply bool,
) (ImportResult, error) {
	t, err := parseFile(f)
	if err != nil {
		return ImportResult{}, fmt.Errorf("import %q: %w", f.Name, err)
	}

	cols, err := resolveMapping(t.Header, mapping)
	if err != nil {
		return ImportResult{}, fmt.Errorf("import %q: %w", f.Name, err)
	}

	name := strings.TrimSpace(f.Name)
	if name == "" {
		name = "import.csv"
	}

	var out ImportResult

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		started := s.clock.Now()

		im, err := newImporter(ctx, tx, s.clock, s.ids, cols, t.Rows)
		if err != nil {
			return err
		}

		out = ImportResult{Applied: apply, Rows: make([]ImportRow, 0, len(t.Rows))}

		for _, r := range t.Rows {
			row, err := im.row(ctx, cols, r)
			if err != nil {
				return fmt.Errorf("line %d: %w", r.Line, err)
			}

			countRow(&out.Counts, row)
			out.Rows = append(out.Rows, row)
		}

		if !apply {
			return errDryRun
		}

		run := domain.ImportRun{
			ID: ids.New[domain.ImportRunID](s.ids), FileName: name, ActorID: actor, StartedAt: started,
			FinishedAt: s.clock.Now(), Counts: out.Counts, Errors: rowErrors(out.Rows),
		}
		if err := run.Validate(); err != nil {
			return err
		}

		out.RunID = run.ID

		return tx.Imports().Create(ctx, run)
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return ImportResult{}, fmt.Errorf("import %q: %w", f.Name, err)
	}

	if !apply {
		clearDryRunIDs(out.Rows)
	}

	return out, nil
}

// clearDryRunIDs clears every ID of a person the dry run created, which
// was rolled back: a member or guardian a row creates, and the same person
// where a later row finds it again (a guardian of siblings, a member who is
// another's guardian) or names it as a possible duplicate.
func clearDryRunIDs(rows []ImportRow) {
	created := map[domain.PersonID]bool{}

	for _, r := range rows {
		if r.Action == ImportCreate {
			created[r.PersonID] = true
		}

		for _, g := range r.Guardians {
			if g.Action == ImportCreate {
				created[g.PersonID] = true
			}
		}
	}

	forget := func(id *domain.PersonID) {
		if created[*id] {
			*id = domain.PersonID{}
		}
	}

	for i := range rows {
		r := &rows[i]
		forget(&r.PersonID)

		for j := range r.Guardians {
			forget(&r.Guardians[j].PersonID)
		}

		for j := range r.Duplicates {
			forget(&r.Duplicates[j].ID)
		}
	}
}

// ImportRuns returns the latest MaxImportRunsListed import runs, newest
// first, with the accounts that ran them.
func (s *ImportService) ImportRuns(ctx context.Context) ([]ImportRunEntry, error) {
	var out []ImportRunEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		runs, err := tx.Imports().List(ctx, MaxImportRunsListed)
		if err != nil {
			return err
		}

		actorIDs := make([]domain.AccountID, 0, len(runs))
		for _, r := range runs {
			if !r.ActorID.IsZero() {
				actorIDs = append(actorIDs, r.ActorID)
			}
		}

		accounts, err := auditAccounts(ctx, tx, actorIDs)
		if err != nil {
			return err
		}

		out = make([]ImportRunEntry, len(runs))
		for i, r := range runs {
			out[i] = ImportRunEntry{Run: r, Actor: accounts[r.ActorID]}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("import runs: %w", err)
	}

	return out, nil
}
