package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// errAlreadySetUp is returned by init for a database with a school.
var errAlreadySetUp = errors.New("the school is set up already: init only runs on a new database " +
	"(change the settings in the app, as an admin)")

// initField is a value init asks for: its flag, the prompt, the default
// offered (none if empty) and where it goes.
type initField struct {
	flag, prompt, fallback string
	// field is the name a validation error uses (domain.FieldError).
	field string
	value *string
}

type initOptions struct {
	db      databaseFlag
	baseURL string
	data    service.SetupData
}

func (o *initOptions) fields() []initField {
	return []initField{
		{"school-name", "School name", "", "schoolName", &o.data.SchoolName},
		{"timezone", "Time zone (IANA, e.g. Europe/Berlin)", "", "timezone", &o.data.Timezone},
		{"locale", "Default language (en or de)", "en", "defaultLocale", &o.data.Locale},
		{"currency", "Currency (ISO 4217)", "EUR", "currency", &o.data.Currency},
		{"admin-email", "Email address of the first admin", "", "email", &o.data.AdminEmail},
		{"admin-given-name", "Given name of the first admin", "", "givenName", &o.data.AdminGivenName},
		{"admin-family-name", "Family name of the first admin", "", "familyName", &o.data.AdminFamilyName},
	}
}

// storeOpener opens the store of a database URL; release releases it.
type storeOpener func(ctx context.Context, databaseURL string) (st store.Store, release func(), err error)

func newInitCmd(env config.LookupFunc) *cobra.Command {
	return newInitCmdWith(env, openDatabase)
}

// newInitCmdWith is the init command with the store from open (tests use
// the memory store).
func newInitCmdWith(env config.LookupFunc, open storeOpener) *cobra.Command {
	var o initOptions

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up a new school: its settings and the first admin",
		Long: "Set up a new school in the empty, migrated database from " + config.EnvDatabaseURL + " or " +
			"--database-url: the school's name, time zone, language and currency (the other settings start " +
			"with defaults that admins change in the app), and the first admin account. Values not given as " +
			"flags are asked for. The time zone and the currency cannot change once lessons or prices exist. " +
			"No mail is sent: init prints a one-time link, valid for 72 hours, that sets the admin's " +
			"password. It points to " + config.EnvBaseURL + " (or --base-url). init refuses to run when the " +
			"school is set up already.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := config.Flags{DatabaseURL: o.db.explicit(cmd)}

			if cmd.Flags().Changed(config.FlagBaseURL) {
				flags.BaseURL = &o.baseURL
			}

			// init writes no log: the log settings are not checked.
			cfg, err := config.LoadWithoutLog(env, flags)
			if err != nil {
				return err
			}

			if cfg.DatabaseURL == "" {
				return errNoDatabaseURL
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			st, closeStore, err := open(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}

			defer closeStore()

			return runInit(ctx, cmd, st, appURL(cfg), &o)
		},
	}

	cmd.Flags().StringVar(&o.baseURL, config.FlagBaseURL, "",
		"public origin of the app that the setup link points to (env "+config.EnvBaseURL+")")

	for _, f := range o.fields() {
		cmd.Flags().StringVar(f.value, f.flag, "", f.prompt)
	}

	return cmd
}

// runInit asks for the values that no flag gave, sets the school up in st
// and prints the setup link.
func runInit(ctx context.Context, cmd *cobra.Command, st store.Store, app string, o *initOptions) error {
	in := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()

	for _, f := range o.fields() {
		if cmd.Flags().Changed(f.flag) {
			continue
		}

		v, err := ask(in, cmd.ErrOrStderr(), f)
		if err != nil {
			return err
		}

		*f.value = v
	}

	res, err := service.NewSetupService(st, clock.System{}, ids.V7{}, app).Init(ctx, o.data)

	var fe *domain.FieldError

	switch {
	case errors.Is(err, service.ErrAlreadySetUp):
		return errAlreadySetUp
	case errors.As(err, &fe):
		for _, f := range o.fields() {
			if f.field == fe.Field {
				return fmt.Errorf("invalid --%s: %s", f.flag, fe.Msg)
			}
		}

		return err
	case err != nil:
		return err
	}

	_, err = fmt.Fprintf(out, "The school %q is set up, with %s as its admin.\n"+
		"Open this link before %s to set the admin's password (it works once; keep it secret):\n\n  %s\n\n"+
		"After that, \"forgot password\" on the sign-in page sends a new link, once mail is configured.\n",
		o.data.SchoolName, res.Account.Email, res.ExpiresAt.UTC().Format("2006-01-02 15:04 MST"), res.Link)

	return err
}

// ask prompts for the value of f on prompt and reads a line from in; an
// empty line takes the default. Input that ends first is an error naming
// the flag, so init without a terminal needs every flag without a default.
func ask(in *bufio.Reader, prompt io.Writer, f initField) (string, error) {
	label := f.prompt
	if f.fallback != "" {
		label += " [" + f.fallback + "]"
	}

	if _, err := fmt.Fprint(prompt, label+": "); err != nil {
		return "", err
	}

	line, err := in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		if f.fallback != "" && errors.Is(err, io.EOF) {
			return f.fallback, nil
		}

		return "", fmt.Errorf("missing --%s", f.flag)
	}

	if v := strings.TrimSpace(line); v != "" {
		return v, nil
	}

	if f.fallback == "" {
		return "", fmt.Errorf("missing --%s", f.flag)
	}

	return f.fallback, nil
}
