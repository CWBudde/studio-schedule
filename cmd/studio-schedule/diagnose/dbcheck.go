package diagnose

import (
	"context"

	"github.com/cwbudde/studio-schedule/internal/service"
)

// DBCheckJSON documents the JSON of a DBCheckReport.
const DBCheckJSON = `one object: ok (bool, false when a check found something), checks (an array of the ` +
	`names of the checks that ran, in their order) and findings (an array, empty without any, of {check, ` +
	`entity, id, message}: the check that found it; the entity pass, booking, lesson, person or ` +
	`receipt-year; the record's ID, or the year of a receipt counter; and what is inconsistent)`

// DBCheckReport is the report of db check.
type DBCheckReport struct {
	OK       bool             `json:"ok"`
	Checks   []string         `json:"checks"`
	Findings []DBCheckFinding `json:"findings"`
}

// DBCheckFinding is one finding of a DBCheckReport.
type DBCheckFinding struct {
	Check   string `json:"check"`
	Entity  string `json:"entity"`
	ID      string `json:"id"`
	Message string `json:"message"`
}

// DBCheck runs the integrity checks (service.SelectIntegrityChecks) and
// returns their report.
func DBCheck(ctx context.Context, integrity *service.IntegrityService, checks []service.IntegrityCheck) (DBCheckReport, error) {
	findings, err := integrity.Check(ctx, checks...)
	if err != nil {
		return DBCheckReport{}, err
	}

	r := DBCheckReport{OK: len(findings) == 0, Checks: make([]string, len(checks)), Findings: []DBCheckFinding{}}
	for i, c := range checks {
		r.Checks[i] = string(c)
	}

	for _, f := range findings {
		r.Findings = append(r.Findings, DBCheckFinding{
			Check: string(f.Check), Entity: string(f.Entity), ID: f.ID, Message: f.Message,
		})
	}

	return r, nil
}
