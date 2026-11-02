package mcpserver

import (
	"context"
	"strings"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/diagnose"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// checkTools are the operator checks of `doctor` and `db check`. On the
// shell they are operator commands; the server always acts as an account,
// so they are admin tools here (ADR 0022).
func checkTools() []tool {
	names := make([]string, 0, len(service.IntegrityChecks()))
	for _, c := range service.IntegrityChecks() {
		names = append(names, string(c))
	}

	return []tool{{
		name: "doctor",
		description: "Run every check of the deployment, as `doctor` does: config, database, schema, " +
			"settings, smtp (a login, nothing is sent), vapid, base-url and receipts. Returns ok (false " +
			"when a check failed) and each check's result (ok, fail, warn or skip) with a detail.",
		access:      httpapi.AccessAdmin,
		annotations: readOnly("Doctor"),
		add:         handler(doctor),
	}, {
		name: "db_check",
		description: "Check the data for inconsistencies the database cannot rule out, as `db check` does, in " +
			"one read-only transaction: " + strings.Join(names, ", ") + ". Returns ok, the checks that ran " +
			"and the findings, which name records by their IDs, never by personal data.",
		access:      httpapi.AccessAdmin,
		annotations: readOnly("Database check"),
		add:         handler(dbCheck),
	}}
}

// doctor is the doctor tool: a report with a failed check is a result, not
// an error, as the report says so.
func doctor(ctx context.Context, s *Server, _ service.Principal, _ struct{}) (any, error) {
	return diagnose.Doctor(ctx, s.opts.Checks, s.clk, config.Flags{}), nil
}

// dbCheckInput are the arguments of the db_check tool.
type dbCheckInput struct {
	Only []string `json:"only,omitempty" jsonschema:"Run only these checks; default all."`
}

// dbCheck is the db_check tool.
func dbCheck(ctx context.Context, s *Server, _ service.Principal, in dbCheckInput) (any, error) {
	names := make([]service.IntegrityCheck, len(in.Only))
	for i, name := range in.Only {
		names[i] = service.IntegrityCheck(name)
	}

	checks, err := service.SelectIntegrityChecks(names...)
	if err != nil {
		return nil, err
	}

	return diagnose.DBCheck(ctx, s.svc.Integrity, checks)
}
