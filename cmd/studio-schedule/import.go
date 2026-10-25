package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func newImportCmd(env config.LookupFunc) *cobra.Command {
	var (
		db    databaseFlag
		maps  []string
		notes []string
		apply bool
	)

	cmd := &cobra.Command{
		Use:   "import <file.csv>",
		Short: "Import members from a CSV file (a dry run unless --apply)",
		Long: "Import the members of a CSV file into the database from " + config.EnvDatabaseURL + " or " +
			"--database-url, as the office's import page does (ADR 0015): a member is found by its member " +
			"number, a guardian by its email or name; updates write only the filled fields; guardian links " +
			"are added, never removed; no accounts are created. The columns are matched by their English or " +
			"German headers, or mapped with --map field=Header (fields: " + fieldNames() + "). Without " +
			"--apply it is a dry run that writes nothing. Rows with problems are reported and left out; " +
			"the other rows are imported, and the command exits with status 1.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}

			databaseURL, err := db.load(cmd, env)
			if err != nil {
				return err
			}

			mapping, err := importMapping(data, maps, notes)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			st, err := openPostgres(ctx, databaseURL)
			if err != nil {
				return err
			}

			defer st.Close()

			file := service.ImportFile{Name: filepath.Base(args[0]), Data: data}

			return runImport(ctx, st, cmd.OutOrStdout(), file, mapping, apply)
		},
	}

	db.register(cmd.Flags())
	cmd.Flags().StringArrayVar(&maps, "map", nil, "map a field to a column, e.g. --map memberNo=Nr (repeatable; "+
		"replaces the header matching)")
	cmd.Flags().StringArrayVar(&notes, "notes", nil, "a further column whose values go into the notes (repeatable)")
	cmd.Flags().BoolVar(&apply, "apply", false, "import; without it, a dry run that writes nothing")

	return cmd
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
				return m, fmt.Errorf("invalid --map %q: want field=Header with a field of %s", kv, fieldNames())
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

// runImport imports file into st as the command line (no account) and
// reports every row and the counts. Rows that failed are errRowsFailed,
// after the report.
func runImport(
	ctx context.Context, st store.Store, out io.Writer, file service.ImportFile, m service.ImportMapping, apply bool,
) error {
	res, err := service.NewImportService(st, clock.System{}, ids.V7{}).Import(ctx, domain.AccountID{}, file, m, apply)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	for _, r := range res.Rows {
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

		_, _ = fmt.Fprintf(w, "line %d\t%s\t%s\t%s %s\t%s\n", r.Line, r.Action, r.MemberNo, r.GivenName, r.FamilyName,
			strings.TrimPrefix(detail, "; "))
	}

	if err := w.Flush(); err != nil {
		return err
	}

	c := res.Counts
	verb := "would import"

	if apply {
		verb = "imported"
	}

	_, err = fmt.Fprintf(out, "%s %d rows: %d created, %d updated, %d unchanged, %d failed; guardians: %d created, "+
		"%d updated, %d links added\n", verb, c.Rows, c.Created, c.Updated, c.Unchanged, c.Failed, c.GuardiansCreated,
		c.GuardiansUpdated, c.LinksAdded)
	if err == nil && !apply {
		_, err = fmt.Fprintln(out, "dry run: nothing was written; run again with --apply to import")
	}

	if err == nil && c.Failed > 0 {
		err = fmt.Errorf("%w: %d of %d (see the lines marked error)", errRowsFailed, c.Failed, c.Rows)
	}

	return err
}
