package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// newAccountsCmd is the group `accounts` (ADR 0020): the admin's account
// administration (ADR 0016) on the shell, and the way back in when the
// last admin has forgotten their password.
func newAccountsCmd(app *cli.App) *cobra.Command {
	return cli.Group("accounts", "Administer the accounts: roles, passwords, sessions",
		newAccountsListCmd(app), newAccountsShowCmd(app), newAccountsCreateCmd(app), newAccountsSetRoleCmd(app),
		newAccountsDisableCmd(app, true), newAccountsDisableCmd(app, false), newAccountsResetPasswordCmd(app),
		cli.Group("sessions", "End the sessions of accounts", newAccountsRevokeCmd(app)),
		newAccountsDeleteCmd(app))
}

// accountRef is how a command names an account: its argument.
const accountRef = "<email|id>"

// accountRefHelp explains accountRef.
const accountRefHelp = "The account is named by its email address (in any case) or its ID."

// accountEntryJSON documents the JSON of a command that prints one account.
const accountEntryJSON = "an item of the response body of the operation listAccounts (AccountEntry)"

func newAccountsListCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List every account with its role and status",
		Long: "List every account, ordered by email, with its role, its status (active; pending: the email " +
			"is not confirmed yet; disabled) and the name of its person, as the admin's account page does.",
		Example: "  studio-schedule accounts list --as admin@example.org",
		Access:  httpapi.AccessAdmin,
		JSON:    "the response body of the operation listAccounts",
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			accounts, err := env.Services.Auth.Accounts(ctx)
			if err != nil {
				return err
			}

			return env.Out.Print(httpapi.NewAccountList(accounts), func(t *cli.Text) {
				t.Table("Email", "Role", "Status", "Name")

				for _, a := range accounts {
					t.Row(a.Account.Email, string(a.Account.Role), accountStatus(a.Account), accountHolder(a.Person))
				}
			})
		},
	})
}

func newAccountsShowCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:     "show " + accountRef,
		Short:   "Show one account",
		Long:    "Show an account: its ID, email, role, status and person. " + accountRefHelp,
		Example: "  studio-schedule accounts show --as admin@example.org member@example.org",
		Args:    cobra.ExactArgs(1),
		Access:  httpapi.AccessAdmin,
		JSON:    accountEntryJSON,
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			a, err := env.Services.Auth.AccountByRef(ctx, args[0])
			if err != nil {
				return err
			}

			return env.Out.Print(httpapi.NewAccountEntry(a), func(t *cli.Text) {
				t.Table("Field", "Value")
				t.Row("ID", a.Account.ID.String())
				t.Row("Email", a.Account.Email)
				t.Row("Role", string(a.Account.Role))
				t.Row("Status", accountStatus(a.Account))
				t.Row("Name", accountHolder(a.Person))
				t.Row("Person", a.Person.ID.String())
			})
		},
	})
}

func newAccountsCreateCmd(app *cli.App) *cobra.Command {
	var person, role string

	return app.Command(cli.Spec{
		Use:   "create <email> --person <id> --role <role>",
		Short: "Create an account for a person and mail it an invitation",
		Long: "Create an account for an existing person, as the office's person page does: unconfirmed and " +
			"without a password. It mails the address an invitation, whose link (valid for 72 hours) sets " +
			"the password and confirms the account; the deliver-mail job sends it. Only an admin creates an " +
			"admin account. The person is named by its ID (see the office's person page); a person with an " +
			"account and an email another account has are refused with exit status 4.",
		Example: "  studio-schedule accounts create --as office@example.org --person " +
			"0190a5d2-ac96-774b-bcce-b302099a8057 --role instructor dance.teacher@example.org",
		Args:        cobra.ExactArgs(1),
		Access:      httpapi.AccessOffice,
		Destructive: true,
		JSON:        "the response body of the operation createPersonAccount",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&person, "person", "", "ID of the person the account is for (required)")
			cmd.Flags().StringVar(&role, "role", "", "role of the account: member, instructor, office or admin "+
				"(required)")
			_ = cmd.MarkFlagRequired("person")
			_ = cmd.MarkFlagRequired("role")
		},
		Fields: map[string]string{"email": "<email>", "role": "--role"},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			id, err := domain.ParseID[domain.PersonID](person)
			if err != nil {
				return cli.Usagef("--person: %q is not an ID", person)
			}

			question := fmt.Sprintf("Create the %s account %s and mail it an invitation?", role, args[0])
			if err := env.Confirm(question); err != nil {
				return err
			}

			acc, err := env.Services.Auth.CreateAccount(ctx, env.Principal, id, service.NewAccount{
				Email: args[0], Role: domain.Role(role),
			})
			if err != nil {
				return err
			}

			return env.Out.Print(httpapi.NewPersonAccount(acc), func(t *cli.Text) {
				t.Line("Created the %s account %s; its invitation is mailed, and its link is valid for %d hours.",
					acc.Role, acc.Email, int(service.InviteTokenTTL/time.Hour))
			})
		},
	})
}

func newAccountsSetRoleCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "set-role " + accountRef + " <role>",
		Short: "Change the role of an account",
		Long: "Give an account another role (member, instructor, office or admin); it takes effect with its " +
			"next request. Recorded in the audit log. " + accountRefHelp + " " + accountGuardsHelp,
		Example: "  studio-schedule accounts set-role --as admin@example.org office@example.org admin",
		Args:    cobra.ExactArgs(2),
		Access:  httpapi.AccessAdmin,
		JSON:    "the response body of the operation setAccountRole",
		Fields:  map[string]string{"role": "<role>"},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			a, err := env.Services.Auth.AccountByRef(ctx, args[0])
			if err != nil {
				return err
			}

			a, err = env.Services.Auth.SetAccountRole(ctx, env.Principal, a.Account.ID, domain.Role(args[1]))
			if err != nil {
				return accountGuard(err)
			}

			return env.Out.Print(httpapi.NewAccountEntry(a), func(t *cli.Text) {
				t.Line("%s is %s now.", a.Account.Email, a.Account.Role)
			})
		},
	})
}

// newAccountsDisableCmd is `accounts disable` (disable) or `accounts
// enable`.
func newAccountsDisableCmd(app *cli.App, disable bool) *cobra.Command {
	spec := cli.Spec{
		Use:   "enable " + accountRef,
		Short: "Enable a disabled account again",
		Long: "Enable a disabled account again: it may sign in. Recorded in the audit log. " + accountRefHelp +
			" The --as account's own account is refused (own-account, exit status 4).",
		Example: "  studio-schedule accounts enable --as admin@example.org member@example.org",
		JSON:    "the response body of the operation enableAccount",
	}

	verb := "Enabled"

	if disable {
		spec = cli.Spec{
			Use:   "disable " + accountRef,
			Short: "Disable an account: it is signed out and may not sign in",
			Long: "Disable an account: it may no longer sign in, and its sessions and the impersonations it " +
				"started end. Recorded in the audit log. " + accountRefHelp + " " + accountGuardsHelp,
			Example:     "  studio-schedule accounts disable --as admin@example.org --yes member@example.org",
			Destructive: true,
			JSON:        "the response body of the operation disableAccount",
		}
		verb = "Disabled"
	}

	spec.Args, spec.Access = cobra.ExactArgs(1), httpapi.AccessAdmin
	spec.Run = func(ctx context.Context, env *cli.Env, args []string) error {
		a, err := env.Services.Auth.AccountByRef(ctx, args[0])
		if err != nil {
			return err
		}

		if disable {
			if err := env.Confirm(fmt.Sprintf("Disable %s and end its sessions?", a.Account.Email)); err != nil {
				return err
			}
		}

		a, err = env.Services.Auth.SetAccountDisabled(ctx, env.Principal, a.Account.ID, disable)
		if err != nil {
			return accountGuard(err)
		}

		return env.Out.Print(httpapi.NewAccountEntry(a), func(t *cli.Text) {
			t.Line("%s %s.", verb, a.Account.Email)
		})
	}

	return app.Command(spec)
}

// accountsResetJSON is what `accounts reset-password --output json`
// prints.
type accountsResetJSON struct {
	Account   httpapi.AccountEntry `json:"account"`
	Link      string               `json:"link,omitempty"`
	ExpiresAt time.Time            `json:"expiresAt"`
	Mailed    bool                 `json:"mailed"`
}

// accountsResetDoc documents accountsResetJSON.
const accountsResetDoc = "one object: account (" + accountEntryJSON + "), link (the reset link; absent with " +
	"--mail), expiresAt (when the link stops working, an RFC 3339 instant), mailed (bool)"

func newAccountsResetPasswordCmd(app *cli.App) *cobra.Command {
	var mailIt bool

	return app.Command(cli.Spec{
		Use:   "reset-password " + accountRef,
		Short: "Print a password-reset link for an account, or mail it with --mail",
		Long: "Issue a password-reset link for an account, valid for one hour; it replaces the account's " +
			"older links. Without --mail the link is printed on stdout and nothing is mailed: this is the " +
			"way back in for an admin who has forgotten their password (run it --as their own email). With " +
			"--mail the link is mailed to the account, as \"forgot password\" does, and not printed; the " +
			"deliver-mail job sends it. The link sets the password once; keep it secret. It works for an " +
			"account that may sign in and for an invited one; a disabled account or an unconfirmed " +
			"registration is refused with exit status 4. " + accountRefHelp,
		Example: "  studio-schedule accounts reset-password --as admin@example.org admin@example.org\n" +
			"  studio-schedule accounts reset-password --as admin@example.org --mail --yes member@example.org",
		Args:        cobra.ExactArgs(1),
		Access:      httpapi.AccessAdmin,
		Destructive: true,
		JSON:        accountsResetDoc,
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&mailIt, "mail", false, "mail the link to the account instead of printing it")
		},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			a, err := env.Services.Auth.AccountByRef(ctx, args[0])
			if err != nil {
				return err
			}

			question := "Issue a password-reset link for " + a.Account.Email + "? Its older links stop working."
			if mailIt {
				question = "Mail a password-reset link to " + a.Account.Email + "? Its older links stop working."
			}

			if err := env.Confirm(question); err != nil {
				return err
			}

			r, err := env.Services.Auth.IssuePasswordReset(ctx, a.Account.ID, mailIt)
			if err != nil {
				return err
			}

			body := accountsResetJSON{
				Account: httpapi.NewAccountEntry(r.Account), Link: r.Link, ExpiresAt: r.ExpiresAt, Mailed: r.Mailed,
			}

			return env.Out.Print(body, func(t *cli.Text) {
				if r.Mailed {
					t.Line("Mailed a password-reset link to %s, valid until %s; the deliver-mail job sends it.",
						r.Account.Account.Email, t.Time(r.ExpiresAt))

					return
				}

				t.Line("Open this link before %s to set the password of %s (it works once; keep it secret):",
					t.Time(r.ExpiresAt), r.Account.Account.Email)
				t.Line("")
				t.Line("  %s", r.Link)
			})
		},
	})
}

// accountsRevokeJSON is what `accounts sessions revoke --output json`
// prints.
type accountsRevokeJSON struct {
	SessionsEnded int `json:"sessionsEnded"`
	Accounts      int `json:"accounts"`
}

func newAccountsRevokeCmd(app *cli.App) *cobra.Command {
	var all bool

	return app.Command(cli.Spec{
		Use:   "revoke " + accountRef + "|--all",
		Short: "End the sessions of an account, or of every account",
		Long: "Sign an account out everywhere: end its sessions and the impersonations it started, as " +
			"disabling it does, and remove its devices' push subscriptions. It may sign in again. With " +
			"--all, every account is signed out (after a leak, say). It prints how many sessions ended. " +
			accountRefHelp,
		Example: "  studio-schedule accounts sessions revoke --as admin@example.org --yes member@example.org\n" +
			"  studio-schedule accounts sessions revoke --as admin@example.org --all",
		Args:        cobra.MaximumNArgs(1),
		Access:      httpapi.AccessAdmin,
		Destructive: true,
		JSON: "one object: sessionsEnded (how many sessions ended that had not ended yet), accounts (of how " +
			"many accounts)",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&all, "all", false, "end the sessions of every account")
		},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			if all == (len(args) == 1) {
				return cli.Usagef("name one account (%s) or give --all", accountRef)
			}

			if all {
				if err := env.Confirm("End every session of every account?"); err != nil {
					return err
				}

				n, accounts, err := env.Services.Auth.RevokeAllSessions(ctx)
				if err != nil {
					return err
				}

				return env.Out.Print(accountsRevokeJSON{SessionsEnded: n, Accounts: accounts}, func(t *cli.Text) {
					t.Line("Ended %s of %d accounts.", accountSessions(n), accounts)
				})
			}

			a, err := env.Services.Auth.AccountByRef(ctx, args[0])
			if err != nil {
				return err
			}

			if err := env.Confirm("End every session of " + a.Account.Email + "?"); err != nil {
				return err
			}

			n, err := env.Services.Auth.RevokeSessions(ctx, a.Account.ID)
			if err != nil {
				return err
			}

			return env.Out.Print(accountsRevokeJSON{SessionsEnded: n, Accounts: 1}, func(t *cli.Text) {
				t.Line("Ended %s of %s.", accountSessions(n), a.Account.Email)
			})
		},
	})
}

func newAccountsDeleteCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "delete " + accountRef,
		Short: "Delete an account; its person stays",
		Long: "Delete an account with its password, sessions, links, calendar feeds and announcement " +
			"dismissals, as the admin's account page does; its person stays. An account that receipts, the " +
			"audit log, announcements or duplicate dismissals name is refused (in-use, exit status 4): " +
			"disable it instead. Recorded in the audit log. " + accountRefHelp + " " + accountGuardsHelp,
		Example:     "  studio-schedule accounts delete --as admin@example.org --yes member@example.org",
		Args:        cobra.ExactArgs(1),
		Access:      httpapi.AccessAdmin,
		Destructive: true,
		JSON: "the account as it was, " + accountEntryJSON + "; the operation deleteAccount answers " +
			"without a body",
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			a, err := env.Services.Auth.AccountByRef(ctx, args[0])
			if err != nil {
				return err
			}

			question := fmt.Sprintf("Delete the account %s? Its person %s stays.", a.Account.Email,
				accountHolder(a.Person))
			if err := env.Confirm(question); err != nil {
				return err
			}

			if err := env.Services.Auth.DeleteAccount(ctx, env.Principal, a.Account.ID); err != nil {
				return accountGuard(err)
			}

			return env.Out.Print(httpapi.NewAccountEntry(a), func(t *cli.Text) {
				t.Line("Deleted the account %s; its person %s stays.", a.Account.Email, accountHolder(a.Person))
			})
		},
	})
}

// accountGuardsHelp names the guards of ADR 0016.
const accountGuardsHelp = "Refused with exit status 4: the --as account's own account (own-account) and the " +
	"last admin who can sign in (last-admin)."

// accountGuards are the refusals of the account administration by the
// name the API gives them (the reason of its 409).
var accountGuards = []struct {
	err  error
	name string
}{
	{service.ErrOwnAccount, "own-account"},
	{service.ErrLastAdmin, "last-admin"},
	{service.ErrAccountKept, "in-use"},
}

// accountGuard names the guard that refused err, keeping its exit status.
func accountGuard(err error) error {
	for _, g := range accountGuards {
		if errors.Is(err, g.err) {
			return fmt.Errorf("refused (%s): %w", g.name, err)
		}
	}

	return err
}

// accountStatus is the status of acc as createPersonAccount names it.
func accountStatus(acc domain.Account) string {
	return httpapi.NewPersonAccount(acc).Status
}

// accountHolder is the name of an account's person as the web shows it
// (web/src/lib/personName.ts). An anonymized person's stored name is the
// fixed "Deleted person" (domain.AnonymizedGivenName), and anonymizing a
// person deletes its account, so no account names one otherwise.
func accountHolder(p domain.Person) string { return p.GivenName + " " + p.FamilyName }

// accountSessions is "1 session" or "n sessions".
func accountSessions(n int) string {
	if n == 1 {
		return "1 session"
	}

	return fmt.Sprintf("%d sessions", n)
}
