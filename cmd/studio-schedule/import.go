package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func newImportCmd(app *cli.App) *cobra.Command {
	var (
		maps  []string
		notes []string
		apply bool
	)

	return app.Command(cli.Spec{
		Use:   "import <file.csv>",
		Short: "Import members from a CSV file (a dry run unless --apply)",
		Long: "Import the members of a CSV file into the database from " + config.EnvDatabaseURL + " or " +
			"--database-url, as the office's import page does (ADR 0015), acting as the office account of --as: " +
			"a member is found by its member number, a guardian by its email or name; updates write only the " +
			"filled fields; guardian links are added, never removed; no accounts are created. The columns are " +
			"matched by their English or German headers, or mapped with --map field=Header (fields: " +
			fieldNames() + "). Without --apply it is a dry run that writes nothing; with it, the run is " +
			"recorded in the import history under the account of --as. Rows with problems are reported and " +
			"left out; the other rows are imported, and the command exits with status 1.",
		Example: "  studio-schedule import --as office@example.org members.csv\n" +
			"  studio-schedule import --as office@example.org --apply --notes Kurs members.csv",
		Args:   cobra.ExactArgs(1),
		Access: httpapi.AccessOffice,
		JSON:   "the response body of the operation runImport",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringArrayVar(&maps, "map", nil, "map a field to a column, e.g. --map memberNo=Nr "+
				"(repeatable; replaces the header matching)")
			cmd.Flags().StringArrayVar(&notes, "notes", nil,
				"a further column whose values go into the notes (repeatable)")
			cmd.Flags().BoolVar(&apply, "apply", false, "import; without it, a dry run that writes nothing")
		},
		Fields: importFlags(),
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return cli.Usage(err)
			}

			mapping, err := importMapping(data, maps, notes)
			if err != nil {
				return err
			}

			file := service.ImportFile{Name: filepath.Base(args[0]), Data: data}

			return runImport(ctx, env, file, mapping, apply)
		},
	})
}

// importFlags name the flags of the field errors of an import: the file's
// content is the argument, a mapped column --map with its field, a notes
// column --notes.
func importFlags() map[string]string {
	flags := map[string]string{"content": "<file.csv>", "mapping.columns": "--map", "mapping.notes": "--notes"}
	for _, f := range service.ImportFields() {
		flags["mapping.columns."+string(f)] = "--map " + string(f)
	}

	return flags
}

func fieldNames() string {
	names := make([]string, 0, len(service.ImportFields()))
	for _, f := range service.ImportFields() {
		names = append(names, string(f))
	}

	return strings.Join(names, ", ")
}

// importMapping is the mapping of --map, or the headers matched by name
// without it, with the notes columns.
func importMapping(data []byte, maps, notes []string) (service.ImportMapping, error) {
	var m service.ImportMapping

	if len(maps) == 0 {
		insp, err := service.NewImportService(nil, clock.System{}, ids.V7{}).Inspect(service.ImportFile{Data: data})
		if err != nil {
			return m, err
		}

		m = insp.Suggested
	} else {
		m.Columns = map[service.ImportField]string{}

		for _, kv := range maps {
			field, header, ok := strings.Cut(kv, "=")
			if !ok || !service.ImportField(field).Valid() {
				return m, cli.Usagef("invalid --map %q: want field=Header with a field of %s", kv, fieldNames())
			}

			m.Columns[service.ImportField(field)] = header
		}
	}

	m.Notes = notes

	return m, nil
}

// errRowsFailed reports an import, or a dry run, in which some rows failed;
// the other rows were imported (or would be). The command exits with
// status 1, so a script notices.
var errRowsFailed = errors.New("rows failed")

// runImport imports file as the account of --as and reports every row and
// the counts, or prints the result as runImport's response body. Rows that
// failed are errRowsFailed, after the report.
func runImport(ctx context.Context, env *cli.Env, file service.ImportFile, m service.ImportMapping, apply bool) error {
	res, err := env.Services.Imports.Import(ctx, env.Principal.AccountID, file, m, apply)
	if err != nil {
		return err
	}

	err = env.Out.Print(httpapi.NewImportResult(res), func(t *cli.Text) {
		t.Table("Line", "Action", "Member no.", "Name", "Details")

		for _, r := range res.Rows {
			t.Row(fmt.Sprintf("line %d", r.Line), string(r.Action), r.MemberNo, r.GivenName+" "+r.FamilyName,
				importDetail(r))
		}

		c := res.Counts
		verb := "would import"

		if apply {
			verb = "imported"
		}

		t.Line("%s %d rows: %d created, %d updated, %d unchanged, %d failed; guardians: %d created, "+
			"%d updated, %d links added", verb, c.Rows, c.Created, c.Updated, c.Unchanged, c.Failed,
			c.GuardiansCreated, c.GuardiansUpdated, c.LinksAdded)

		if !apply {
			t.Line("dry run: nothing was written; run again with --apply to import")
		}
	})
	if err == nil && res.Counts.Failed > 0 {
		err = cli.Reported(fmt.Errorf("%w: %d of %d (see the lines marked error)", errRowsFailed,
			res.Counts.Failed, res.Counts.Rows))
	}

	return err
}

// importDetail is what the report says about the row r: its problems or
// changes, its guardians and its possible duplicates.
func importDetail(r service.ImportRow) string {
	detail := ""

	switch {
	case len(r.Issues) > 0:
		issues := make([]string, 0, len(r.Issues))
		for _, i := range r.Issues {
			issues = append(issues, string(i.Field)+": "+i.Code)
		}

		detail = strings.Join(issues, ", ")
	case len(r.Changes) > 0:
		changes := make([]string, 0, len(r.Changes))
		for _, c := range r.Changes {
			changes = append(changes, string(c))
		}

		detail = strings.Join(changes, ", ")
	}

	for _, g := range r.Guardians {
		detail += fmt.Sprintf("; guardian %s %s: %s", g.GivenName, g.FamilyName, g.Action)
		if g.Linked {
			detail += ", linked"
		}
	}

	if len(r.Duplicates) > 0 {
		names := make([]string, 0, len(r.Duplicates))
		for _, d := range r.Duplicates {
			names = append(names, d.GivenName+" "+d.FamilyName)
		}

		detail += "; possible duplicate of " + strings.Join(slices.Compact(names), ", ")
	}

	return strings.TrimPrefix(detail, "; ")
}
