package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

// accountsSetupDoc documents the JSON of `accounts setup-link`, an
// accountsResetJSON.
const accountsSetupDoc = "one object: account (" + accountEntryJSON + "), link (the setup link), expiresAt " +
	"(when the link stops working, an RFC 3339 instant), mailed (always false)"

// newAccountsSetupLinkCmd is `accounts setup-link` (ADR 0020, amendment):
// the way back in for a school whose admins cannot sign in, such as a first
// admin whose init link expired. It is an operator command: without an
// admin, --as finds nobody to act as. osUser is the OS user who runs it,
// recorded in the audit event as the command line's principals record it.
func newAccountsSetupLinkCmd(app *cli.App, osUser string) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "setup-link " + accountRef,
		Short: "Print a new setup link for an unconfirmed account while no admin can sign in",
		Long: "Print a new setup link for an account that has not confirmed its email, such as the first " +
			"admin of init whose 72-hour link expired before it was used: the way back in for a school none " +
			"of whose admins can sign in. The link sets the password, and so confirms the account; it is " +
			"valid for 72 hours, works once and replaces the account's pending links. It is printed on " +
			"stdout and nothing is mailed; keep it secret. This is an operator command: it acts as no " +
			"account and takes no --as, since without an admin there is nobody to act as. It works only " +
			"while no admin can sign in (an admin account that is confirmed and not disabled); otherwise it " +
			"is refused (admin-exists, exit status 4) and nothing changes, so it never gets round an admin, " +
			"who issues a link with accounts reset-password instead. A confirmed account is refused " +
			"(confirmed, exit status 4), and so are a disabled account and a registration with a password " +
			"of its own. Recorded in the audit log with the OS user who ran it. " + accountRefHelp,
		Example: "  studio-schedule accounts setup-link --yes admin@example.org\n" +
			"  studio-schedule accounts setup-link --yes -o json first.admin@example.org",
		Args:        cobra.ExactArgs(1),
		Store:       true,
		Destructive: true,
		JSON:        accountsSetupDoc,
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			a, err := env.Services.Auth.AccountByRef(ctx, args[0])
			if err != nil {
				return err
			}

			question := "Issue a setup link for " + a.Account.Email + "? Its pending link stops working."
			if err := env.Confirm(question); err != nil {
				return err
			}

			r, err := env.Services.Auth.IssueSetupLink(ctx, a.Account.ID.String(),
				domain.AuditVia{Channel: domain.ChannelCLI, OSUser: osUser})
			if err != nil {
				return accountGuard(err)
			}

			body := accountsResetJSON{Account: httpapi.NewAccountEntry(r.Account), Link: r.Link, ExpiresAt: r.ExpiresAt}

			return env.Out.Print(body, func(t *cli.Text) {
				t.Line("Open this link before %s to set the password of %s and confirm the account (it works "+
					"once; keep it secret):", t.Time(r.ExpiresAt), r.Account.Account.Email)
				t.Line("")
				t.Line("  %s", r.Link)
			})
		},
	})
}
