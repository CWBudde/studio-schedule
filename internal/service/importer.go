package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/csvimport"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// parseFile parses an uploaded file; one that cannot be read is a
// validation error on its content.
func parseFile(f ImportFile) (csvimport.Table, error) {
	t, err := csvimport.Parse(f.Data)

	var fe *csvimport.FileError
	if errors.As(err, &fe) {
		return csvimport.Table{}, &domain.FieldError{Field: "content", Msg: fe.Msg}
	}

	return t, err
}

// ---------------------------------------------------------------- mapping

// headerSynonyms name the member fields in English and German, normalized
// (normalizeHeader). A guardian's columns are these names with a guardian
// word and its number (guardianWords).
var headerSynonyms = map[string]ImportField{
	"memberno": FieldMemberNo, "membernumber": FieldMemberNo, "memberid": FieldMemberNo,
	"membershipnumber": FieldMemberNo, "membershipno": FieldMemberNo, "mitgliedsnummer": FieldMemberNo,
	"mitgliedsnr": FieldMemberNo, "mitgliednr": FieldMemberNo, "mitgliedernummer": FieldMemberNo,
	"mitgliedsnummern": FieldMemberNo, "mitgliedernr": FieldMemberNo,

	"givenname": FieldGivenName, "firstname": FieldGivenName, "forename": FieldGivenName, "vorname": FieldGivenName,

	"familyname": FieldFamilyName, "lastname": FieldFamilyName, "surname": FieldFamilyName,
	"nachname": FieldFamilyName, "familienname": FieldFamilyName, "name": FieldFamilyName,

	"email": FieldEmail, "mail": FieldEmail, "emailaddress": FieldEmail, "emailadresse": FieldEmail,

	"phone": FieldPhone, "phonenumber": FieldPhone, "telephone": FieldPhone, "tel": FieldPhone,
	"mobile": FieldPhone, "telefon": FieldPhone, "telefonnummer": FieldPhone, "mobil": FieldPhone,
	"handy": FieldPhone, "mobilnummer": FieldPhone,

	"birthdate": FieldBirthDate, "dateofbirth": FieldBirthDate, "birthday": FieldBirthDate, "dob": FieldBirthDate,
	"geburtsdatum": FieldBirthDate, "geburtstag": FieldBirthDate, "gebdatum": FieldBirthDate,

	"notes": FieldNotes, "note": FieldNotes, "comment": FieldNotes, "comments": FieldNotes, "remarks": FieldNotes,
	"bemerkung": FieldNotes, "bemerkungen": FieldNotes, "notiz": FieldNotes, "notizen": FieldNotes,
	"kommentar": FieldNotes, "anmerkung": FieldNotes, "anmerkungen": FieldNotes,
}

// guardianWords name a guardian in a header; a word with slot 0 needs the
// number in the header, the others imply it.
var guardianWords = []struct {
	word string
	slot int
}{
	{"erziehungsberechtigter", 0},
	{"erziehungsberechtigte", 0},
	{"elternteil", 0},
	{"guardian", 0},
	{"parent", 0},
	{"eltern", 0},
	{"mother", 1},
	{"mutter", 1},
	{"father", 2},
	{"vater", 2},
}

// normalizeHeader folds a header to the form headerSynonyms use: lower
// case, letters and digits only, umlauts kept.
func normalizeHeader(h string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return unicode.ToLower(r)
		default:
			return -1
		}
	}, h)
}

// matchHeader returns the field a header names, if it names one.
func matchHeader(h string) (ImportField, bool) {
	n := normalizeHeader(h)
	if f, ok := headerSynonyms[n]; ok {
		return f, true
	}

	for _, g := range guardianWords {
		if !strings.Contains(n, g.word) {
			continue
		}

		rest, slot := strings.Replace(n, g.word, "", 1), g.slot
		if slot == 0 {
			switch {
			case strings.Contains(rest, "1"):
				slot = 1
			case strings.Contains(rest, "2"):
				slot = 2
			default:
				return "", false
			}
		}

		rest = strings.Map(func(r rune) rune {
			if unicode.IsDigit(r) {
				return -1
			}

			return r
		}, rest)

		f, ok := headerSynonyms[rest]
		if !ok {
			return "", false
		}

		fields := guardianFields(slot)

		switch f {
		case FieldGivenName:
			return fields[0], true
		case FieldFamilyName:
			return fields[1], true
		case FieldEmail:
			return fields[2], true
		case FieldPhone:
			return fields[3], true
		default:
			return "", false
		}
	}

	return "", false
}

// SuggestMapping maps the headers it recognizes to their fields, each
// field to the first header that names it. Unrecognized headers stay
// unmapped (they may go into the notes).
func SuggestMapping(headers []string) ImportMapping {
	m := ImportMapping{Columns: map[ImportField]string{}}

	for _, h := range headers {
		if f, ok := matchHeader(h); ok {
			if _, taken := m.Columns[f]; !taken {
				m.Columns[f] = h
			}
		}
	}

	return m
}

// columns are a mapping resolved against a file's header: the cell of
// each mapped field, and the notes columns.
type columns struct {
	field map[ImportField]int
	notes []noteColumn
}

type noteColumn struct {
	header string
	index  int
}

// resolveMapping checks m against the header: every header it names is the
// file's (ignoring case) and named once, the member number and the names
// are mapped, and so are the names of a guardian whose other columns are.
func resolveMapping(header []string, m ImportMapping) (columns, error) {
	index := make(map[string]int, len(header))
	for i, h := range header {
		index[strings.ToLower(h)] = i
	}

	used := map[int]bool{}
	cols := columns{field: map[ImportField]int{}}

	column := func(at, h string) (int, error) {
		i, ok := index[strings.ToLower(strings.TrimSpace(h))]

		switch {
		case !ok:
			return 0, &domain.FieldError{Field: at, Msg: fmt.Sprintf("the file has no column %q", h)}
		case used[i]:
			return 0, &domain.FieldError{Field: at, Msg: fmt.Sprintf("the column %q is mapped twice", h)}
		}

		used[i] = true

		return i, nil
	}

	// In the order of the fields, so the first problem is always the same.
	for _, f := range ImportFields() {
		h, ok := m.Columns[f]
		if !ok || strings.TrimSpace(h) == "" {
			continue
		}

		i, err := column("mapping.columns."+string(f), h)
		if err != nil {
			return columns{}, err
		}

		cols.field[f] = i
	}

	for f := range m.Columns {
		if !f.Valid() {
			return columns{}, &domain.FieldError{Field: "mapping.columns", Msg: fmt.Sprintf("unknown field %q", f)}
		}
	}

	for _, h := range m.Notes {
		i, err := column("mapping.notes", h)
		if err != nil {
			return columns{}, err
		}

		cols.notes = append(cols.notes, noteColumn{header: header[i], index: i})
	}

	required := []ImportField{FieldMemberNo, FieldGivenName, FieldFamilyName}

	for _, slot := range []int{1, 2} {
		g := guardianFields(slot)
		if slices.ContainsFunc(g[:], func(f ImportField) bool { _, ok := cols.field[f]; return ok }) {
			required = append(required, g[0], g[1])
		}
	}

	for _, f := range required {
		if _, ok := cols.field[f]; !ok {
			return columns{}, &domain.FieldError{Field: "mapping.columns." + string(f), Msg: "the field must be mapped"}
		}
	}

	return cols, nil
}

// get returns the cell of field f in cells; empty if f is not mapped.
func (c columns) get(cells []string, f ImportField) string {
	if i, ok := c.field[f]; ok {
		return cells[i]
	}

	return ""
}

// notesOf returns the notes of a row: the notes column, then a line
// "Header: value" for each filled notes column.
func (c columns) notesOf(cells []string) string {
	var lines []string

	if n := c.get(cells, FieldNotes); n != "" {
		lines = append(lines, n)
	}

	for _, n := range c.notes {
		if v := cells[n.index]; v != "" {
			lines = append(lines, n.header+": "+v)
		}
	}

	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------- rows

// importer runs the rows of one import in its transaction. It keeps every
// live person and the keys it finds them by, and updates both as rows
// write, so later rows find what earlier ones created.
type importer struct {
	tx    store.Tx
	ids   ids.Source
	today domain.Date

	persons   map[domain.PersonID]domain.Person
	byNo      map[string]domain.PersonID
	byEmail   map[string][]domain.PersonID // persons' own emails, lower case
	byAccount map[string]domain.PersonID   // account emails, lower case
	byName    map[string][]domain.PersonID // nameKey
	// accountEmail are the persons' account emails, lower case.
	accountEmail map[domain.PersonID]string
	// lines are the lines of the member numbers seen so far.
	lines map[string]int
	// locked are the persons the run has locked.
	locked map[domain.PersonID]bool
}

// newImporter returns the importer of the rows of a file mapped by cols.
// Before any row runs, it locks every person a row may find (its member by
// number, its guardians by email and name), together and so in ID order
// (PersonRepo.Lock), and reads the persons under those locks: two imports,
// or an import and a merge (which locks its persons first as well), take
// their person locks in one order and cannot deadlock on them. A person
// found only through what an earlier row writes is one of those, or one the
// run creates. The cycle check of a guardian link locks the member's
// dependents as it walks them, as AddGuardian does.
func newImporter(
	ctx context.Context, tx store.Tx, clk clock.Clock, src ids.Source, cols columns, rows []csvimport.Row,
) (*importer, error) {
	loc := time.UTC

	switch _, l, err := loadSchool(ctx, tx); {
	case err == nil:
		loc = l
	case !errors.Is(err, ErrNoSchool):
		return nil, err
	}

	locked := map[domain.PersonID]bool{}

	for {
		im, err := indexPersons(ctx, tx)
		if err != nil {
			return nil, err
		}

		// A person found meanwhile (an edit between the reads) is locked
		// next, and the persons are read again.
		var missing []domain.PersonID

		for _, id := range im.candidates(cols, rows) {
			if !locked[id] {
				missing = append(missing, id)
			}
		}

		if len(missing) == 0 {
			im.ids, im.today, im.locked = src, domain.DateOf(clk.Now(), loc), locked

			return im, nil
		}

		switch err := tx.Persons().Lock(ctx, missing...); {
		case errors.Is(err, domain.ErrNotFound): // deleted meanwhile: read again
		case err != nil:
			return nil, err
		}

		for _, id := range missing {
			locked[id] = true
		}
	}
}

// indexPersons returns an importer with every live person and account
// indexed.
func indexPersons(ctx context.Context, tx store.Tx) (*importer, error) {
	persons, err := tx.Persons().List(ctx)
	if err != nil {
		return nil, err
	}

	accounts, err := tx.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}

	im := &importer{
		tx: tx, persons: make(map[domain.PersonID]domain.Person, len(persons)), byNo: map[string]domain.PersonID{},
		byEmail: map[string][]domain.PersonID{}, byAccount: map[string]domain.PersonID{},
		byName: map[string][]domain.PersonID{}, accountEmail: map[domain.PersonID]string{}, lines: map[string]int{},
	}

	for _, p := range persons {
		im.index(p)
	}

	for _, a := range accounts {
		if _, live := im.persons[a.PersonID]; live {
			im.byAccount[strings.ToLower(a.Email)] = a.PersonID
			im.accountEmail[a.PersonID] = strings.ToLower(a.Email)
		}
	}

	return im, nil
}

// candidates returns every stored person the rows may find: a member by
// its number, a guardian by its email (an account's or a person's) or its
// name; more than findGuardian picks, never less.
func (im *importer) candidates(cols columns, rows []csvimport.Row) []domain.PersonID {
	seen := map[domain.PersonID]bool{}

	var out []domain.PersonID

	add := func(ids ...domain.PersonID) {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}

	for _, r := range rows {
		if id, ok := im.byNo[cols.get(r.Cells, FieldMemberNo)]; ok {
			add(id)
		}

		for _, slot := range []int{1, 2} {
			f := guardianFields(slot)

			if e := strings.ToLower(cols.get(r.Cells, f[2])); e != "" {
				if id, ok := im.byAccount[e]; ok {
					add(id)
				}

				add(im.byEmail[e]...)
			}

			if given, family := cols.get(r.Cells, f[0]), cols.get(r.Cells, f[1]); given != "" && family != "" {
				add(im.byName[nameKey(given, family)]...)
			}
		}
	}

	return out
}

// lock locks the person id unless the run has (newImporter locks every
// person a row finds in advance; this only guards that).
func (im *importer) lock(ctx context.Context, id domain.PersonID) error {
	if im.locked[id] {
		return nil
	}

	if err := im.tx.Persons().Lock(ctx, id); err != nil {
		return err
	}

	im.locked[id] = true

	return nil
}

// index adds p, replacing what it was, to the keys.
func (im *importer) index(p domain.Person) {
	if old, ok := im.persons[p.ID]; ok {
		delete(im.byNo, old.MemberNo)
		im.byEmail[strings.ToLower(old.Email)] = slices.DeleteFunc(im.byEmail[strings.ToLower(old.Email)],
			func(id domain.PersonID) bool { return id == p.ID })
		key := nameKey(old.GivenName, old.FamilyName)
		im.byName[key] = slices.DeleteFunc(im.byName[key], func(id domain.PersonID) bool { return id == p.ID })
	}

	im.persons[p.ID] = p

	if p.MemberNo != "" {
		im.byNo[p.MemberNo] = p.ID
	}

	if p.Email != "" {
		im.byEmail[strings.ToLower(p.Email)] = append(im.byEmail[strings.ToLower(p.Email)], p.ID)
	}

	key := nameKey(p.GivenName, p.FamilyName)
	im.byName[key] = append(im.byName[key], p.ID)
}

// guardianPlan is what a row does with a guardian (its result: whether it
// is new or changes, and whether the link is added) and the person as it is
// written.
type guardianPlan struct {
	result ImportGuardian
	person domain.Person
}

// row plans a row, checks all of it, and then writes it, unless it has a
// problem: then it writes nothing and reports the problems. An error is a
// store error, which ends the import.
func (im *importer) row(ctx context.Context, cols columns, r csvimport.Row) (ImportRow, error) {
	get := func(f ImportField) string { return cols.get(r.Cells, f) }

	out := ImportRow{
		Line: r.Line, MemberNo: get(FieldMemberNo), GivenName: get(FieldGivenName), FamilyName: get(FieldFamilyName),
	}
	issue := func(code string, f ImportField) { out.Issues = append(out.Issues, ImportIssue{Code: code, Field: f}) }

	no := out.MemberNo

	switch first, seen := im.lines[no]; {
	case no == "":
		issue(IssueRequired, FieldMemberNo)
	case utf8.RuneCountInString(no) > domain.MaxMemberNoLength:
		issue(IssueInvalid, FieldMemberNo)
	case seen && first != r.Line:
		issue(IssueMemberNoRepeated, FieldMemberNo)
	default:
		im.lines[no] = r.Line
	}

	// The member as the row would write it.
	id, exists := im.byNo[no]
	if no == "" {
		exists = false
	}

	var (
		member domain.Person
		err    error
	)

	if exists {
		// Read under the lock: a concurrent edit of the office waits.
		if err := im.lock(ctx, id); err != nil {
			return ImportRow{}, err
		}

		if member, err = im.tx.Persons().Get(ctx, id); err != nil {
			return ImportRow{}, err
		}
	} else {
		member = domain.Person{ID: ids.New[domain.PersonID](im.ids), MemberNo: no}
	}

	before := member
	member, out.Changes = im.apply(member, map[ImportField]string{
		FieldGivenName: get(FieldGivenName), FieldFamilyName: get(FieldFamilyName), FieldEmail: get(FieldEmail),
		FieldPhone: get(FieldPhone), FieldBirthDate: get(FieldBirthDate), FieldNotes: cols.notesOf(r.Cells),
	}, issue, func(f ImportField) ImportField { return f })

	if !exists {
		if member.GivenName == "" {
			issue(IssueRequired, FieldGivenName)
		}

		if member.FamilyName == "" {
			issue(IssueRequired, FieldFamilyName)
		}

		out.Changes = slices.Insert(out.Changes, 0, FieldMemberNo)
	}

	var guardians []guardianPlan

	for _, slot := range []int{1, 2} {
		g, ok, err := im.guardian(ctx, cols, r.Cells, slot, member, exists, issue)
		if err != nil {
			return ImportRow{}, err
		}

		if ok && !slices.ContainsFunc(guardians, func(o guardianPlan) bool { return o.person.ID == g.person.ID }) {
			guardians = append(guardians, g)
		}
	}

	if !exists {
		family, err := im.familyOf(ctx, guardians)
		if err != nil {
			return ImportRow{}, err
		}

		out.Duplicates = im.duplicatesOf(member, family)
	}

	if len(out.Issues) > 0 {
		// Nothing of the row is written; its issues name the fields.
		out.Action, out.Changes, out.Duplicates = ImportError, nil, nil

		return out, nil
	}

	for _, g := range guardians {
		out.Guardians = append(out.Guardians, g.result)
	}

	switch {
	case !exists:
		out.Action = ImportCreate
		if err := im.tx.Persons().Create(ctx, member); err != nil {
			return ImportRow{}, err
		}
	case member != before:
		out.Action = ImportUpdate
		if err := im.tx.Persons().Update(ctx, member); err != nil {
			return ImportRow{}, err
		}
	default:
		out.Action = ImportUnchanged
	}

	im.index(member)
	out.PersonID = member.ID
	out.GivenName, out.FamilyName = member.GivenName, member.FamilyName

	for i, g := range guardians {
		switch g.result.Action {
		case ImportCreate:
			err = im.tx.Persons().Create(ctx, g.person)
		case ImportUpdate:
			err = im.tx.Persons().Update(ctx, g.person)
		}

		if err != nil {
			return ImportRow{}, err
		}

		im.index(g.person)

		if g.result.Linked {
			if err := im.tx.Persons().AddGuardian(ctx, domain.Guardian{
				GuardianID: g.person.ID, DependentID: member.ID,
			}); err != nil {
				return ImportRow{}, err
			}
		}

		out.Guardians[i].PersonID = g.person.ID
	}

	return out, nil
}

// apply sets the filled values of a row on p, checking each, and returns
// p and the fields it changed. at names the field of a problem: the
// member's own, or a guardian's.
func (im *importer) apply(
	p domain.Person, values map[ImportField]string, issue func(string, ImportField),
	at func(ImportField) ImportField,
) (domain.Person, []ImportField) {
	var changed []ImportField

	set := func(f ImportField, cur *string, v string, limit int) {
		switch {
		case v == "" || v == *cur:
		case utf8.RuneCountInString(v) > limit:
			issue(IssueInvalid, at(f))
		default:
			*cur = v

			changed = append(changed, f)
		}
	}

	set(FieldGivenName, &p.GivenName, values[FieldGivenName], maxImportName)
	set(FieldFamilyName, &p.FamilyName, values[FieldFamilyName], maxImportName)

	// An email that differs in case only is the same address.
	switch e := values[FieldEmail]; {
	case e != "" && !domain.ValidEmail(e):
		issue(IssueInvalid, at(FieldEmail))
	case !strings.EqualFold(e, p.Email):
		set(FieldEmail, &p.Email, e, maxImportEmail)
	}

	set(FieldPhone, &p.Phone, values[FieldPhone], maxImportPhone)
	set(FieldNotes, &p.Notes, values[FieldNotes], maxImportNotes)

	if v := values[FieldBirthDate]; v != "" {
		d, err := csvimport.ParseDate(v)

		switch {
		case err != nil:
			issue(IssueInvalid, at(FieldBirthDate))
		case d.Compare(im.today) > 0:
			issue(IssueFuture, at(FieldBirthDate))
		case d != p.BirthDate:
			p.BirthDate = d

			changed = append(changed, FieldBirthDate)
		}
	}

	return p, changed
}

// familyOf returns the persons that are family of a new member with the
// guardians of its row: the guardians and their other dependents.
func (im *importer) familyOf(ctx context.Context, guardians []guardianPlan) (map[domain.PersonID]bool, error) {
	family := map[domain.PersonID]bool{}

	for _, g := range guardians {
		if g.person.ID.IsZero() {
			continue // an error
		}

		family[g.person.ID] = true

		if g.result.Action == ImportCreate {
			continue
		}

		dependents, err := im.tx.Persons().ListDependents(ctx, g.person.ID)
		if err != nil {
			return nil, err
		}

		for _, d := range dependents {
			family[d.ID] = true
		}
	}

	return family, nil
}

// duplicatesOf returns the persons a new person may be the same as: the
// same email (a person's or an account's, ignoring case), or the same name
// and birth date. Its family (familyOf) shares an email often: they do not
// match by the email alone (as in Duplicates).
func (im *importer) duplicatesOf(p domain.Person, family map[domain.PersonID]bool) []domain.Person {
	var found []domain.PersonID

	if p.Email != "" {
		e := strings.ToLower(p.Email)
		byEmail := slices.Clone(im.byEmail[e])

		if id, ok := im.byAccount[e]; ok {
			byEmail = append(byEmail, id)
		}

		found = slices.DeleteFunc(byEmail, func(id domain.PersonID) bool { return family[id] })
	}

	if !p.BirthDate.IsZero() {
		for _, id := range im.byName[nameKey(p.GivenName, p.FamilyName)] {
			if im.persons[id].BirthDate == p.BirthDate {
				found = append(found, id)
			}
		}
	}

	slices.SortFunc(found, compareIDs)
	found = slices.Compact(found)

	out := make([]domain.Person, len(found))
	for i, id := range found {
		out[i] = im.persons[id]
	}

	return out
}

// guardian plans guardian slot of a row whose member is member (exists if
// it is stored already). ok is false for a slot the row leaves empty.
func (im *importer) guardian(
	ctx context.Context, cols columns, cells []string, slot int, member domain.Person, exists bool,
	issue func(string, ImportField),
) (guardianPlan, bool, error) {
	fields := guardianFields(slot)
	given, family := cols.get(cells, fields[0]), cols.get(cells, fields[1])
	email, phone := cols.get(cells, fields[2]), cols.get(cells, fields[3])

	if given == "" && family == "" && email == "" && phone == "" {
		return guardianPlan{}, false, nil
	}

	at := func(f ImportField) ImportField {
		switch f {
		case FieldGivenName:
			return fields[0]
		case FieldFamilyName:
			return fields[1]
		case FieldEmail:
			return fields[2]
		default:
			return fields[3]
		}
	}

	g := guardianPlan{result: ImportGuardian{Slot: slot, GivenName: given, FamilyName: family}}

	id, by, ambiguous := im.findGuardian(given, family, email)

	switch {
	case ambiguous:
		if email != "" {
			issue(IssueGuardianAmbiguous, fields[2])
		} else {
			issue(IssueGuardianAmbiguous, fields[0])
		}

		g.result.Action = ImportError

		return g, true, nil
	case id.IsZero():
		if given == "" {
			issue(IssueRequired, fields[0])
		}

		if family == "" {
			issue(IssueRequired, fields[1])
		}

		g.person = domain.Person{ID: ids.New[domain.PersonID](im.ids)}
		g.result.Action, g.result.Linked = ImportCreate, true
	case id == member.ID:
		issue(IssueGuardianIsMember, fields[0])
		g.result.Action = ImportError

		return g, true, nil
	default:
		if err := im.lock(ctx, id); err != nil {
			return guardianPlan{}, false, err
		}

		p, err := im.tx.Persons().Get(ctx, id)
		if err != nil {
			return guardianPlan{}, false, err
		}

		g.person, g.result.MatchedBy, g.result.Action = p, by, ImportUnchanged
	}

	before := g.person

	var changed []ImportField

	g.person, changed = im.apply(g.person, map[ImportField]string{
		FieldGivenName: given, FieldFamilyName: family, FieldEmail: email, FieldPhone: phone,
	}, issue, at)

	if g.result.Action == ImportUnchanged && len(changed) > 0 && g.person != before {
		g.result.Action = ImportUpdate
	}

	g.result.GivenName, g.result.FamilyName = g.person.GivenName, g.person.FamilyName

	if g.result.Action == ImportCreate || !exists {
		g.result.Linked = true

		return g, true, nil
	}

	// Both are stored: link them unless they are, and unless the link
	// would close a cycle. The member is locked above.
	guardians, err := im.tx.Persons().ListGuardians(ctx, member.ID)
	if err != nil {
		return guardianPlan{}, false, err
	}

	if slices.ContainsFunc(guardians, func(p domain.Person) bool { return p.ID == g.person.ID }) {
		return g, true, nil
	}

	switch cycle, err := isDependentOf(ctx, im.tx, g.person.ID, member.ID); {
	case err != nil:
		return guardianPlan{}, false, err
	case cycle:
		issue(IssueGuardianCycle, fields[0])
	default:
		g.result.Linked = true
	}

	return g, true, nil
}

// findGuardian finds the person a guardian's columns name: by the email,
// an account's first, then a person's; else by the name, among the persons
// whose email does not say otherwise. When the row names the guardian, an
// email match counts only with the same name: a family shares an address
// often, and a member or a sibling with the guardian's email is not the
// guardian. by is "email" or "name"; ambiguous reports several matches; a
// zero id and false a new person.
func (im *importer) findGuardian(given, family, email string) (id domain.PersonID, by string, ambiguous bool) {
	key := nameKey(given, family)
	named := given != "" && family != ""
	e := strings.ToLower(email)

	if e != "" {
		other := func(id domain.PersonID) bool {
			p := im.persons[id]

			return named && nameKey(p.GivenName, p.FamilyName) != key
		}

		if id, ok := im.byAccount[e]; ok && !other(id) {
			return id, "email", false
		}

		switch found := slices.DeleteFunc(slices.Clone(im.byEmail[e]), other); {
		case len(found) == 1:
			return found[0], "email", false
		case len(found) > 1:
			return domain.PersonID{}, "", true
		}
	}

	if !named {
		return domain.PersonID{}, "", false
	}

	byName := slices.DeleteFunc(slices.Clone(im.byName[key]), func(id domain.PersonID) bool {
		p := im.persons[id]
		other := cmp.Or(im.accountEmail[id], strings.ToLower(p.Email))

		return e != "" && other != "" && other != e
	})

	switch len(byName) {
	case 0:
		return domain.PersonID{}, "", false
	case 1:
		return byName[0], "name", false
	default:
		return domain.PersonID{}, "", true
	}
}

// countRow adds a row to the counts.
func countRow(c *domain.ImportCounts, row ImportRow) {
	c.Rows++

	switch row.Action {
	case ImportCreate:
		c.Created++
	case ImportUpdate:
		c.Updated++
	case ImportUnchanged:
		c.Unchanged++
	default:
		c.Failed++

		return
	}

	for _, g := range row.Guardians {
		switch g.Action {
		case ImportCreate:
			c.GuardiansCreated++
		case ImportUpdate:
			c.GuardiansUpdated++
		}

		if g.Linked {
			c.LinksAdded++
		}
	}
}

// rowErrors returns the first problem of each failed row, at most
// domain.MaxImportErrors.
func rowErrors(rows []ImportRow) []domain.ImportRowError {
	var out []domain.ImportRowError

	for _, r := range rows {
		if r.Action == ImportError && len(out) < domain.MaxImportErrors {
			out = append(out, domain.ImportRowError{Line: r.Line, Code: r.Issues[0].Code, Field: string(r.Issues[0].Field)})
		}
	}

	return out
}
