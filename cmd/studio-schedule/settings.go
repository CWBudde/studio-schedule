package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// settingsVersion is the version of the document of `settings export`;
// `settings import` reads this version only.
const settingsVersion = 1

// What the commands print with --output json.
const (
	settingsGetJSON = "with an area, the response body of its get operation: getSchoolSettings (school), " +
		"getBrandingSettings (branding), getRegionalSettings (regional), getBookingRules (booking-rules), " +
		"getReceiptSettings (receipts) or getPassSettings (passes); without one, an object with every area's: " +
		"school, branding, regional, bookingRules, receipts, passes"
	settingsSetJSON = "the response body of the area's update operation: updateSchoolSettings, " +
		"updateBrandingSettings, updateRegionalSettings, updateBookingRules, updateReceiptSettings or " +
		"updatePassSettings"
	settingsExportJSON = "the settings document, always JSON: version (number, 1), then every area's request " +
		"body by area: school (updateSchoolSettings), branding (updateBrandingSettings), regional " +
		"(updateRegionalSettings), bookingRules (updateBookingRules), receipts (updateReceiptSettings), passes " +
		"(updatePassSettings); with --file it goes into the file, and stdout stays empty"
	settingsImportJSON = "one object: dryRun (bool), changes (array of {area, key, from, to}: the area as the " +
		"commands name it, the key of its request body, and the values before and after in the request body's " +
		"form, null for none), generatedLessons (number: lessons a wider release window generated at once)"
)

// newSettingsCmd is the group `settings` (ADR 0013, 0020).
func newSettingsCmd(app *cli.App) *cobra.Command {
	return cli.Group("settings", "Read and change the school settings",
		newSettingsGetCmd(app), newSettingsSetCmd(app), newSettingsExportCmd(app), newSettingsImportCmd(app))
}

func newSettingsGetCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "get [area]",
		Short: "Print the settings of an area, or of all areas",
		Long: "Print the settings of an area (" + areaNames() + "), or of every area without one, as the settings " +
			"page shows them, acting as the office account of --as: one line per setting with the area, the key " +
			"and the value. The keys are the fields of the area's API body, which settings set takes; the " +
			"fields only shown (such as timezoneLocked or the booking rules' window) are read-only. Durations " +
			"show in days, hours and minutes, amounts in the school's currency and instants in the school's " +
			"time zone; --output json prints the API's form.",
		Example: "  studio-schedule settings get --as office@example.org\n" +
			"  studio-schedule settings get booking-rules --as office@example.org --output json",
		Args:   cobra.MaximumNArgs(1),
		Access: httpapi.AccessOffice,
		JSON:   settingsGetJSON,
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			currency, err := schoolCurrency(ctx, env)
			if err != nil {
				return err
			}

			areas := settingsAreas

			if len(args) == 1 {
				a, err := areaNamed(args[0])
				if err != nil {
					return err
				}

				areas = []settingsArea{a}
			}

			bodies := make([]any, len(areas))

			for i, a := range areas {
				body, err := a.get(ctx, env.Services)
				if err != nil {
					return err
				}

				bodies[i] = body
			}

			doc := bodies[0]
			if len(args) == 0 {
				doc = byArea(areas, bodies)
			}

			return printSettings(env, currency, doc, areas, bodies)
		},
	})
}

func newSettingsSetCmd(app *cli.App) *cobra.Command {
	// fields name a field error of a key as its argument, key=value
	// (filled in by Run).
	fields := map[string]string{}

	return app.Command(cli.Spec{
		Use:   "set <area> key=value...",
		Short: "Change settings of an area",
		Long: "Change the named settings of an area (" + areaNames() + ") and keep the others, as the area's " +
			"card on the settings page does, acting as the admin account of --as: the area is read, the keys " +
			"are changed and the area is written back, with the same rules as the API. The keys are the fields " +
			"of the area's API body (settings get lists them): a number is a whole number, a date YYYY-MM-DD " +
			"(empty: none), a time HH:MM, and the cutoffs count minutes. The time zone cannot change once " +
			"lessons exist, the currency once a price or a receipt exists (exit status 4). A wider release " +
			"window generates its lessons at once. It prints the area after the change.",
		Example: "  studio-schedule settings set school schoolName='Studio Example' --as admin@example.org\n" +
			"  studio-schedule settings set booking-rules releaseWeeksAhead=3 cancellationCutoffMinutes=180 " +
			"--as admin@example.org",
		Args:   cobra.MinimumNArgs(2),
		Access: httpapi.AccessAdmin,
		JSON:   settingsSetJSON,
		Fields: fields,
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			a, err := areaNamed(args[0])
			if err != nil {
				return err
			}

			values, err := settingValues(a, args[1:])
			if err != nil {
				return err
			}

			currency, err := schoolCurrency(ctx, env)
			if err != nil {
				return err
			}

			for _, arg := range args[1:] {
				key, _, _ := strings.Cut(arg, "=")
				fields[key] = arg
			}

			body, err := a.set(ctx, env, values)
			if err != nil {
				return fieldOfSetting(err)
			}

			return printSettings(env, currency, body, []settingsArea{a}, []any{body})
		},
	})
}

func newSettingsExportCmd(app *cli.App) *cobra.Command {
	var file outputFile

	return app.Command(cli.Spec{
		Use:   "export",
		Short: "Write every area's settings as one JSON document",
		Long: "Write the settings of every area as one JSON document, acting as the office account of --as: the " +
			"document settings import reads, to set up a second school the same way. Each area is its API " +
			"request body, so the read-only fields are left out, and so are the logo and the background image: " +
			"set them on the settings page. The receipt settings name the school as the issuer; edit them for " +
			"another school. The document goes to stdout, with nothing else, in either output format; with " +
			"--file it goes into that file, created with mode 0600, and an existing file is refused unless " +
			"--force.",
		Example: "  studio-schedule settings export --as office@example.org --file settings.json\n" +
			"  studio-schedule settings export --as office@example.org > settings.json",
		Access: httpapi.AccessOffice,
		JSON:   settingsExportJSON,
		Flags:  func(cmd *cobra.Command) { file.bind(cmd, "the settings", " instead of stdout") },
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			if err := file.check(); err != nil {
				return err
			}

			s, err := env.Services.Settings.Export(ctx)
			if err != nil {
				return err
			}

			data := make([]any, len(settingsAreas))
			for i, a := range settingsAreas {
				data[i] = a.data(s)
			}

			doc := settingsDocument{areas: byArea(settingsAreas, data)}

			if file.name == "" {
				return env.Out.Print(doc, func(t *cli.Text) { t.Line("%s", bytes.TrimSuffix(encodeJSON(doc), []byte("\n"))) })
			}

			if err := file.writeBytes(encodeJSON(doc)); err != nil {
				return err
			}

			if env.Out.JSON() {
				return nil
			}

			return env.Out.Print(nil, func(t *cli.Text) {
				t.Line("exported the settings of %d areas to %s", len(settingsAreas), file.name)
			})
		},
	})
}

func newSettingsImportCmd(app *cli.App) *cobra.Command {
	var dryRun bool

	return app.Command(cli.Spec{
		Use:   "import <file>",
		Short: "Write the settings of a settings export (overwrites them)",
		Long: "Write the areas of a document of settings export, acting as the admin account of --as: all of them " +
			"in one transaction, or none if one is refused, each with the rules of its card. An area in the " +
			"document replaces all of its settings; an area left out stays as it is. The time zone and the " +
			"currency stay locked once used: a different locked value is a conflict (exit status 4). It " +
			"overwrites settings, so it asks for confirmation on a terminal and needs --yes otherwise; " +
			"--dry-run checks everything and writes nothing. It prints every setting that changes " +
			"(or would change), with its value before and after.",
		Example: "  studio-schedule settings import --as admin@example.org --dry-run settings.json\n" +
			"  studio-schedule settings import --as admin@example.org --yes settings.json",
		Args:        cobra.ExactArgs(1),
		Access:      httpapi.AccessAdmin,
		Destructive: true,
		JSON:        settingsImportJSON,
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check the document and print the changes; write nothing")
		},
		Fields: importFields(),
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return cli.Usage(err)
			}

			in, err := settingsImport(args[0], data)
			if err != nil {
				return err
			}

			if !dryRun {
				if err := env.Confirm(fmt.Sprintf("Overwrite the settings with those of %s?", args[0])); err != nil {
					return err
				}
			}

			res, err := env.Services.Settings.Import(ctx, in, dryRun)
			if err != nil {
				return fieldOfSetting(err)
			}

			warnGenerate(env, res.GenerateErr)

			return printImported(env, res, dryRun)
		},
	})
}

// importFields name the keys of an import's field errors by their place in
// the document, e.g. bookingRules.releaseTime; the keys of all areas are
// distinct.
func importFields() map[string]string {
	fields := map[string]string{}

	for _, a := range settingsAreas {
		for _, k := range a.keys() {
			fields[k.name] = a.field() + "." + k.name
		}
	}

	return fields
}

// settingsImport reads the document data of file into the import of its
// areas. Invalid JSON, another version, an unknown area or key, a missing
// key or an empty document are usage errors.
func settingsImport(file string, data []byte) (service.SettingsImport, error) {
	var in service.SettingsImport

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return in, cli.Usagef("%s: %w", file, err)
	}

	var version int
	if err := json.Unmarshal(doc["version"], &version); err != nil || version != settingsVersion {
		return in, cli.Usagef("%s: not a settings export of version %d (settings export writes one)", file,
			settingsVersion)
	}

	delete(doc, "version")

	if len(doc) == 0 {
		return in, cli.Usagef("%s: no settings area", file)
	}

	for _, a := range settingsAreas {
		if raw, ok := doc[a.field()]; ok {
			if err := a.put(raw, &in); err != nil {
				return in, err
			}

			delete(doc, a.field())
		}
	}

	for name := range doc {
		return in, cli.Usagef("%s: unknown settings area %q", file, name)
	}

	return in, nil
}

// areaValues are values of areas in order, a JSON object by their fields.
type areaValues []areaValue

type areaValue struct {
	field string
	value any
}

func byArea(areas []settingsArea, values []any) areaValues {
	out := make(areaValues, len(areas))
	for i, a := range areas {
		out[i] = areaValue{field: a.field(), value: values[i]}
	}

	return out
}

// MarshalJSON writes the object with the areas in their order. The values
// are the API's bodies, plain data that always encodes.
func (v areaValues) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer

	b.WriteByte('{')

	for i, a := range v {
		if i > 0 {
			b.WriteByte(',')
		}

		b.Write(plainJSON(a.field))
		b.WriteByte(':')
		b.Write(plainJSON(a.value))
	}

	b.WriteByte('}')

	return b.Bytes(), nil
}

// plainJSON encodes v (plain data) as the API does: "&" and "<" kept, not
// escaped as for HTML.
func plainJSON(v any) []byte {
	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)

	_ = enc.Encode(v)

	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// settingsDocument is the document of settings export: the version, then
// the areas.
type settingsDocument struct{ areas areaValues }

// MarshalJSON writes the version first.
func (d settingsDocument) MarshalJSON() ([]byte, error) {
	areas, _ := d.areas.MarshalJSON()

	return append(fmt.Appendf(nil, `{"version":%d,`, settingsVersion), areas[1:]...), nil
}

// encodeJSON encodes doc as the presenter does with --output json:
// indented, "<" kept.
func encodeJSON(doc settingsDocument) []byte {
	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)

	_ = enc.Encode(doc) // its MarshalJSON does not fail

	return b.Bytes()
}

// printSettings presents doc (JSON), or the settings of the bodies of areas
// as text: one line per setting with the area, the key and the value, the
// amounts in currency.
func printSettings(env *cli.Env, currency string, doc any, areas []settingsArea, bodies []any) error {
	return env.Out.Print(doc, func(t *cli.Text) {
		t.Table("Area", "Key", "Value")

		for i, a := range areas {
			for _, s := range areaRows(a, bodies[i]) {
				t.Row(a.name(), s.key, settingText(t, currency, s.key, s.value))
			}
		}
	})
}

// schoolCurrency is the school's currency, which the text output shows
// amounts in; it is not read for JSON. A database without settings is
// service.ErrNoSchool.
func schoolCurrency(ctx context.Context, env *cli.Env) (string, error) {
	if env.Out.JSON() {
		return "", nil
	}

	s, err := env.Services.Settings.Public(ctx)

	return s.Currency, err
}

// settingChange is a setting an import changes, in the API's form; null is
// none.
type settingChange struct {
	Area string          `json:"area"`
	Key  string          `json:"key"`
	From json.RawMessage `json:"from"`
	To   json.RawMessage `json:"to"`
}

// settingsImported is what settings import prints with --output json.
type settingsImported struct {
	DryRun           bool            `json:"dryRun"`
	Changes          []settingChange `json:"changes"`
	GeneratedLessons int             `json:"generatedLessons"`
}

// printImported presents the settings an import changed, or would change.
func printImported(env *cli.Env, res service.SettingsImported, dryRun bool) error {
	out := settingsImported{DryRun: dryRun, Changes: []settingChange{}, GeneratedLessons: res.Generated}

	for _, a := range settingsAreas {
		before, after := settingRows(a.data(res.Before)), settingRows(a.data(res.After))

		for _, k := range a.keys() {
			from, to := valueOf(before, k.name), valueOf(after, k.name)
			if !bytes.Equal(from, to) {
				out.Changes = append(out.Changes, settingChange{Area: a.name(), Key: k.name, From: from, To: to})
			}
		}
	}

	return env.Out.Print(out, func(t *cli.Text) {
		t.Table("Area", "Key", "From", "To")

		for _, c := range out.Changes {
			t.Row(c.Area, c.Key, settingText(t, res.After.Currency, c.Key, c.From),
				settingText(t, res.After.Currency, c.Key, c.To))
		}

		if dryRun {
			t.Line("dry run: %d settings would change; nothing was written; run again without --dry-run to import",
				len(out.Changes))
		} else {
			t.Line("imported: %d settings changed", len(out.Changes))
		}

		if out.GeneratedLessons > 0 {
			t.Line("generated %d lessons for the wider release window", out.GeneratedLessons)
		}
	})
}
