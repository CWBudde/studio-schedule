package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/csvimport"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// importMaxBody is the largest request of the import: the file base64
// encoded, plus the mapping.
const importMaxBody = (csvimport.MaxBytes+2)/3*4 + 64<<10

// registerImports registers the member import (ADR 0015): reading a file's
// headers, the dry run and the import itself, and the run history.
func registerImports(api huma.API, h *handlers) {
	register(api, AccessOffice, huma.Operation{
		OperationID: "inspectImport",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/imports/inspect",
		Summary:     "Read a member file",
		Description: fmt.Sprintf("Reads a CSV file (UTF-8 or Windows-1252; separated by semicolons, commas or "+
			"tabs; at most %d KiB and %d rows) and returns its headers, its first rows and a mapping of the "+
			"headers it recognizes by their English or German names. A file it cannot read is a 422 on "+
			"`body.content`. Nothing is stored.", csvimport.MaxBytes>>10, csvimport.MaxRows),
		Tags:         []string{"admin"},
		MaxBodyBytes: importMaxBody,
	}, h.inspectImport)

	register(api, AccessOffice, huma.Operation{
		OperationID: "runImport",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/imports",
		Summary:     "Import members",
		Description: "Imports the members of a CSV file with a mapping of its columns. Each row is a member " +
			"found by its member number, and up to two guardians found by their email (an account's or a " +
			"person's, ignoring case) or else by their name; what is not found is created. An update writes " +
			"only the mapped fields the row fills; guardian links are added, never removed; no accounts are " +
			"created. Rows with problems are left out and reported. Without `apply` it is a dry run that " +
			"writes nothing; with it, the import is recorded in the run history. A mapping that does not fit " +
			"the file is a 422 on `body.mapping`, an unreadable file one on `body.content`.",
		Tags:         []string{"admin"},
		MaxBodyBytes: importMaxBody,
	}, h.runImport)

	register(api, AccessOffice, huma.Operation{
		OperationID: "listImportRuns",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/imports",
		Summary:     "List import runs",
		Description: fmt.Sprintf("Returns the latest %d applied imports, newest first, with who ran them, "+
			"their counts and the rows they could not take.", service.MaxImportRunsListed),
		Tags: []string{"admin"},
	}, h.listImportRuns)
}

// ImportFileBody is an uploaded file.
type ImportFileBody struct {
	FileName string `doc:"The file's name, for the run history."       json:"fileName" maxLength:"255" minLength:"1"`
	Content  []byte `doc:"The file, base64 encoded; at most 1 MiB." json:"content"  maxLength:"1398104"`
}

// ImportColumns names the column (by its header) that fills each field;
// an absent field is not imported.
type ImportColumns struct {
	MemberNo            string `doc:"The member number, by which a member is found again; required." json:"memberNo,omitempty" maxLength:"200"`
	GivenName           string `doc:"Given name; required."                                       json:"givenName,omitempty" maxLength:"200"`
	FamilyName          string `doc:"Family name; required."                                      json:"familyName,omitempty" maxLength:"200"`
	Email               string `doc:"Email address."                                              json:"email,omitempty" maxLength:"200"`
	Phone               string `doc:"Phone number."                                               json:"phone,omitempty" maxLength:"200"`
	BirthDate           string `doc:"Birth date, DD.MM.YYYY or YYYY-MM-DD."                       json:"birthDate,omitempty" maxLength:"200"`
	Notes               string `doc:"Notes."                                                      json:"notes,omitempty" maxLength:"200"`
	Guardian1GivenName  string `doc:"First guardian's given name; required with any of its columns." json:"guardian1GivenName,omitempty" maxLength:"200"`
	Guardian1FamilyName string `doc:"First guardian's family name; required with any of its columns." json:"guardian1FamilyName,omitempty" maxLength:"200"`
	Guardian1Email      string `doc:"First guardian's email address."                             json:"guardian1Email,omitempty" maxLength:"200"`
	Guardian1Phone      string `doc:"First guardian's phone number."                              json:"guardian1Phone,omitempty" maxLength:"200"`
	Guardian2GivenName  string `doc:"Second guardian's given name; required with any of its columns." json:"guardian2GivenName,omitempty" maxLength:"200"`
	Guardian2FamilyName string `doc:"Second guardian's family name; required with any of its columns." json:"guardian2FamilyName,omitempty" maxLength:"200"`
	Guardian2Email      string `doc:"Second guardian's email address."                            json:"guardian2Email,omitempty" maxLength:"200"`
	Guardian2Phone      string `doc:"Second guardian's phone number."                             json:"guardian2Phone,omitempty" maxLength:"200"`
}

// ImportMapping maps the columns of a file.
type ImportMapping struct {
	Columns ImportColumns `json:"columns"`
	Notes   []string      `doc:"Further columns whose values go into the member's notes, each as a line \"Header: value\"." json:"notes,omitempty" maxItems:"100"`
}

// ImportInspection is the response body of inspectImport.
type ImportInspection struct {
	Encoding  string        `doc:"How the file was decoded." enum:"utf-8,windows-1252" json:"encoding"`
	Delimiter string        `doc:"What separates the cells." enum:"semicolon,comma,tab" json:"delimiter"`
	Headers   []string      `doc:"The column names, in file order." json:"headers" nullable:"false"`
	Rows      int           `doc:"How many data rows the file has." json:"rows"`
	Sample    [][]string    `doc:"The first data rows, a cell per header." json:"sample" nullable:"false"`
	Suggested ImportMapping `doc:"The headers recognized by their English or German names." json:"suggested"`
}

// ImportInspectInput is the request of inspectImport.
type ImportInspectInput struct {
	Body ImportFileBody
}

// ImportInspectionOutput is the response of inspectImport.
type ImportInspectionOutput struct {
	Body ImportInspection
}

// ImportRequest is the request body of runImport.
type ImportRequest struct {
	ImportFileBody

	Mapping ImportMapping `json:"mapping"`
	Apply   bool          `doc:"Import; without it, a dry run that writes nothing." json:"apply,omitempty"`
}

// ImportRunInput is the request of runImport.
type ImportRunInput struct {
	Body ImportRequest
}

// ImportCounts count what an import does.
type ImportCounts struct {
	Rows             int `doc:"Data rows of the file." json:"rows"`
	Created          int `doc:"Members created."       json:"created"`
	Updated          int `doc:"Members updated."       json:"updated"`
	Unchanged        int `doc:"Members unchanged."     json:"unchanged"`
	Failed           int `doc:"Rows left out for their problems." json:"failed"`
	GuardiansCreated int `doc:"Guardians created."     json:"guardiansCreated"`
	GuardiansUpdated int `doc:"Guardians updated."     json:"guardiansUpdated"`
	LinksAdded       int `doc:"Guardian links added."  json:"linksAdded"`
}

// ImportIssue is a problem of a row.
type ImportIssue struct {
	Code  string `doc:"required: the field is empty; invalid: not an email address, not a date as DD.MM.YYYY or YYYY-MM-DD, or too long; future: a birth date after today; member-no-repeated: an earlier row has the member number; guardian-ambiguous: several persons match the guardian; guardian-is-member: the guardian is the member; guardian-cycle: the guardian is a dependent of the member." enum:"required,invalid,future,member-no-repeated,guardian-ambiguous,guardian-is-member,guardian-cycle" json:"code"`
	Field string `doc:"The field it is about." json:"field"`
}

// ImportGuardian is what a row does with a guardian.
type ImportGuardian struct {
	Slot       int             `doc:"1 or 2."                                          json:"slot"`
	GivenName  string          `doc:"Given name."                                      json:"givenName"`
	FamilyName string          `doc:"Family name."                                     json:"familyName"`
	PersonID   domain.PersonID `doc:"The person; absent if the dry run would create it." json:"personId,omitzero"`
	Action     string          `doc:"What happens to the guardian." enum:"create,update,unchanged,error" json:"action"`
	MatchedBy  string          `doc:"How an existing guardian was found." enum:"email,name" json:"matchedBy,omitempty"`
	Linked     bool            `doc:"The row links the guardian to the member."         json:"linked"`
}

// ImportDuplicate is a person a new member may be the same as.
type ImportDuplicate struct {
	ID         domain.PersonID `doc:"The person; absent for one an earlier row of the dry run would create." json:"id,omitzero"`
	GivenName  string          `doc:"Given name."               json:"givenName"`
	FamilyName string          `doc:"Family name."              json:"familyName"`
	Email      string          `doc:"Email address, if known." json:"email,omitempty"`
	MemberNo   string          `doc:"Member number, if any."   json:"memberNo,omitempty"`
}

// ImportRow is what an import does with a row.
type ImportRow struct {
	Line       int               `doc:"The row's line in the file; the header is line 1." json:"line"`
	MemberNo   string            `doc:"The member number."                                  json:"memberNo"`
	GivenName  string            `doc:"Given name."                                         json:"givenName"`
	FamilyName string            `doc:"Family name."                                        json:"familyName"`
	PersonID   domain.PersonID   `doc:"The member; absent for an error, and for a member the dry run would create." json:"personId,omitzero"`
	Action     string            `doc:"What happens to the member." enum:"create,update,unchanged,error" json:"action"`
	Changes    []string          `doc:"The fields a create or update writes."               json:"changes" nullable:"false"`
	Issues     []ImportIssue     `doc:"The row's problems; a row with any is left out."     json:"issues" nullable:"false"`
	Duplicates []ImportDuplicate `doc:"Persons a new member may be the same as (the same email, or name and birth date); a family's shared email does not count." json:"duplicates" nullable:"false"`
	Guardians  []ImportGuardian  `doc:"The row's guardians."                                json:"guardians" nullable:"false"`
}

// ImportResult is the response body of runImport.
type ImportResult struct {
	Applied bool               `doc:"Whether the import was applied; false for a dry run." json:"applied"`
	RunID   domain.ImportRunID `doc:"The recorded run; absent for a dry run."               json:"runId,omitzero"`
	Counts  ImportCounts       `json:"counts"`
	Rows    []ImportRow        `doc:"Every data row, in file order." json:"rows" nullable:"false"`
}

// ImportResultOutput is the response of runImport.
type ImportResultOutput struct {
	Body ImportResult
}

// ImportRowError is a row a run could not take, with its first problem.
type ImportRowError struct {
	Line  int    `doc:"The row's line in the file." json:"line"`
	Code  string `doc:"The problem, as in ImportIssue." json:"code"`
	Field string `doc:"The field it is about."       json:"field,omitempty"`
}

// ImportRunEntry is an applied import.
type ImportRunEntry struct {
	ID         domain.ImportRunID `doc:"Run ID."                           json:"id"`
	FileName   string             `doc:"The file's name."                  json:"fileName"`
	By         *AuditAccount      `doc:"Who ran it; absent for the command line." json:"by,omitempty"`
	StartedAt  time.Time          `doc:"When it started."                  json:"startedAt"`
	FinishedAt time.Time          `doc:"When it finished."                 json:"finishedAt"`
	Counts     ImportCounts       `json:"counts"`
	Errors     []ImportRowError   `doc:"The rows it could not take (at most 200)." json:"errors" nullable:"false"`
}

// ImportRunList is the response body of listImportRuns.
type ImportRunList struct {
	Items []ImportRunEntry `doc:"The latest runs, newest first." json:"items" nullable:"false"`
}

// ImportRunListOutput is the response of listImportRuns.
type ImportRunListOutput struct {
	Body ImportRunList
}

func (h *handlers) inspectImport(ctx context.Context, in *ImportInspectInput) (*ImportInspectionOutput, error) {
	insp, err := h.deps.Imports.Inspect(service.ImportFile{Name: in.Body.FileName, Data: in.Body.Content})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := ImportInspection{
		Encoding: insp.Encoding, Delimiter: insp.Delimiter, Headers: insp.Headers, Rows: insp.Rows,
		Sample: insp.Sample, Suggested: importMapping(insp.Suggested),
	}
	if out.Sample == nil {
		out.Sample = [][]string{}
	}

	return &ImportInspectionOutput{Body: out}, nil
}

func (h *handlers) runImport(ctx context.Context, in *ImportRunInput) (*ImportResultOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	res, err := h.deps.Imports.Import(ctx, actorAccount(p), service.ImportFile{
		Name: in.Body.FileName, Data: in.Body.Content,
	}, in.Body.Mapping.service(), in.Body.Apply)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := ImportResult{
		Applied: res.Applied, RunID: res.RunID, Counts: importCounts(res.Counts),
		Rows: make([]ImportRow, 0, len(res.Rows)),
	}

	for _, r := range res.Rows {
		row := ImportRow{
			Line: r.Line, MemberNo: r.MemberNo, GivenName: r.GivenName, FamilyName: r.FamilyName, PersonID: r.PersonID,
			Action: string(r.Action), Changes: make([]string, 0, len(r.Changes)),
			Issues: make([]ImportIssue, 0, len(r.Issues)), Duplicates: make([]ImportDuplicate, 0, len(r.Duplicates)),
			Guardians: make([]ImportGuardian, 0, len(r.Guardians)),
		}

		for _, c := range r.Changes {
			row.Changes = append(row.Changes, string(c))
		}

		for _, d := range r.Duplicates {
			row.Duplicates = append(row.Duplicates, ImportDuplicate{
				ID: d.ID, GivenName: d.GivenName, FamilyName: d.FamilyName, Email: d.Email, MemberNo: d.MemberNo,
			})
		}

		for _, i := range r.Issues {
			row.Issues = append(row.Issues, ImportIssue{Code: i.Code, Field: string(i.Field)})
		}

		for _, g := range r.Guardians {
			row.Guardians = append(row.Guardians, ImportGuardian{
				Slot: g.Slot, GivenName: g.GivenName, FamilyName: g.FamilyName, PersonID: g.PersonID,
				Action: string(g.Action), MatchedBy: g.MatchedBy, Linked: g.Linked,
			})
		}

		out.Rows = append(out.Rows, row)
	}

	return &ImportResultOutput{Body: out}, nil
}

func (h *handlers) listImportRuns(ctx context.Context, _ *struct{}) (*ImportRunListOutput, error) {
	runs, err := h.deps.Imports.ImportRuns(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]ImportRunEntry, 0, len(runs))

	for _, e := range runs {
		r := e.Run
		entry := ImportRunEntry{
			ID: r.ID, FileName: r.FileName, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
			Counts: importCounts(r.Counts), Errors: make([]ImportRowError, 0, len(r.Errors)),
		}

		if !r.ActorID.IsZero() {
			e.Actor.Account.ID = r.ActorID
			by := auditAccount(e.Actor)
			entry.By = &by
		}

		for _, re := range r.Errors {
			entry.Errors = append(entry.Errors, ImportRowError{Line: re.Line, Code: re.Code, Field: re.Field})
		}

		items = append(items, entry)
	}

	return &ImportRunListOutput{Body: ImportRunList{Items: items}}, nil
}

// actorAccount is the account that acts for p: the admin while
// impersonating.
func actorAccount(p service.Principal) domain.AccountID {
	if !p.ImpersonatorID.IsZero() {
		return p.ImpersonatorID
	}

	return p.AccountID
}

func importCounts(c domain.ImportCounts) ImportCounts {
	return ImportCounts{
		Rows: c.Rows, Created: c.Created, Updated: c.Updated, Unchanged: c.Unchanged, Failed: c.Failed,
		GuardiansCreated: c.GuardiansCreated, GuardiansUpdated: c.GuardiansUpdated, LinksAdded: c.LinksAdded,
	}
}

// fields pairs the target fields with the fields of c that name their
// columns.
func (c *ImportColumns) fields() map[service.ImportField]*string {
	return map[service.ImportField]*string{
		service.FieldMemberNo: &c.MemberNo, service.FieldGivenName: &c.GivenName, service.FieldFamilyName: &c.FamilyName,
		service.FieldEmail: &c.Email, service.FieldPhone: &c.Phone, service.FieldBirthDate: &c.BirthDate,
		service.FieldNotes: &c.Notes, service.FieldGuardian1GivenName: &c.Guardian1GivenName,
		service.FieldGuardian1FamilyName: &c.Guardian1FamilyName, service.FieldGuardian1Email: &c.Guardian1Email,
		service.FieldGuardian1Phone: &c.Guardian1Phone, service.FieldGuardian2GivenName: &c.Guardian2GivenName,
		service.FieldGuardian2FamilyName: &c.Guardian2FamilyName, service.FieldGuardian2Email: &c.Guardian2Email,
		service.FieldGuardian2Phone: &c.Guardian2Phone,
	}
}

func (m ImportMapping) service() service.ImportMapping {
	out := service.ImportMapping{Columns: map[service.ImportField]string{}, Notes: m.Notes}

	for f, h := range m.Columns.fields() {
		if *h != "" {
			out.Columns[f] = *h
		}
	}

	return out
}

func importMapping(m service.ImportMapping) ImportMapping {
	var out ImportMapping

	fields := out.Columns.fields()
	for f, h := range m.Columns {
		if p, ok := fields[f]; ok {
			*p = h
		}
	}

	out.Notes = m.Notes

	return out
}
