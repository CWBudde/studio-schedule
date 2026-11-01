# Command line reference

<!-- Generated from the command tree by TestCLIReference (cmd/studio-schedule/clidocs_test.go). Do not edit: change the commands and run `just cli-docs`. -->

Every command of the `studio-schedule` binary with its flags, its output and its exit codes. A command runs locally on the server's shell: it opens the database of `DATABASE_URL` or `--database-url`, checks its schema as `serve` does and calls the same services as the HTTP API, so a command and its operation share their rules, validation and audit. There is no remote mode ([ADR 0020](decisions/0020-command-line.md)). The server's configuration is in [operations.md](operations.md#configuration-reference). Every command also explains itself with `--help`.

- [Global flags](#global-flags)
- [Environment variables](#environment-variables)
- [Output](#output)
- [Confirmation](#confirmation)
- [Exit codes](#exit-codes)
- [Commands](#commands)

## Global flags

They go before or after the command: `studio-schedule --output json <command>` is `studio-schedule <command> -o json`. A command that acts as no account refuses `--as`, and one that prints text only refuses `--output json`.

- `--as` (string): email of the account a command acts as; its role must reach the command's access level (env `STUDIO_AS`)
- `--database-url` (string): PostgreSQL connection URL or key=value DSN; prefer the env var, flags are visible in the process list (env `DATABASE_URL`)
- `--output`, `-o` (string, default `text`): output format: text or json
- `--yes`, `-y` (bool): confirm a destructive command without a prompt

## Environment variables

- `STUDIO_AS`: as `--as`, which wins over it.
- `DATABASE_URL`: as `--database-url`, which wins over it.
- `NO_COLOR`: when set and not empty, the text output has no colors ([no-color.org](https://no-color.org)).

An empty variable counts as unset. Each command's own flags name the variable they stand for in the same way.

## Output

**Text** is the default: one record per line, with a header line only when stdout is a terminal, and dates and times in the school's time zone, never the machine's. Color only on a terminal and without `NO_COLOR`.

**`--output json`** prints one JSON document on stdout. Where a command mirrors an API operation, the document is the operation's response body, with the field names and types of its schema in [api/openapi.json](../api/openapi.json). A command without an operation documents its schema below, under the command. The schema is stable: fields are only added, and a rename or a removal waits for a major release. A command that prints text only refuses `--output json`.

**Errors** go to stderr as one line, `error: …`. An invalid value names the flag the way the command spells it (`--email: …`, or `key=value` for a setting). With `--output json`, stderr gets the error as a problem document like the API's, with `title`, `status`, `detail` and `errors`.

## Confirmation

A command that deletes, anonymizes, mails, cancels or merges asks for confirmation when stdin is a terminal (`[y/N]`), and needs `--yes` otherwise. Without a terminal and without `--yes`, it refuses with exit code 2 and changes nothing. A secret or a search term is never an argument: the command reads it from stdin, after a prompt on a terminal.

## Exit codes

- `0`: The command did what it was asked.
- `1`: It failed, including a check with findings and an import with failed rows.
- `2`: An unknown flag or command, a missing argument, invalid input before anything ran, or a refused confirmation.
- `3`: What the command names does not exist (`domain.ErrNotFound`), also an unknown `--as`.
- `4`: The change conflicts with the data (`domain.ErrConflict` and what wraps it, a refused guard), or the `--as` account's role is too low for the command (`domain.ErrForbidden`).

## Commands

- [`studio-schedule accounts`](#studio-schedule-accounts): Administer the accounts: roles, passwords, sessions
- [`studio-schedule completion`](#studio-schedule-completion): Generate the autocompletion script for the specified shell
- [`studio-schedule config`](#studio-schedule-config): Inspect the configuration
- [`studio-schedule doctor`](#studio-schedule-doctor): Check the configuration, the database, the mail server and the receipt module
- [`studio-schedule healthcheck`](#studio-schedule-healthcheck): Check that the local server answers (for container health checks)
- [`studio-schedule import`](#studio-schedule-import): Import members from a CSV file (a dry run unless `--apply`)
- [`studio-schedule init`](#studio-schedule-init): Set up a new school: its settings and the first admin
- [`studio-schedule jobs`](#studio-schedule-jobs): List the background jobs and run one now
- [`studio-schedule mail`](#studio-schedule-mail): Test the mail server, list the mail log and queue failed mails again
- [`studio-schedule migrate`](#studio-schedule-migrate): Manage the database schema
- [`studio-schedule openapi`](#studio-schedule-openapi): Print the OpenAPI 3.1 specification as JSON
- [`studio-schedule seed`](#studio-schedule-seed): Load the fictional demo data set into an empty database
- [`studio-schedule serve`](#studio-schedule-serve): Run the HTTP server (API and web UI)
- [`studio-schedule settings`](#studio-schedule-settings): Read and change the school settings
- [`studio-schedule vapid-keys`](#studio-schedule-vapid-keys): Print a new VAPID key pair for web push
- [`studio-schedule version`](#studio-schedule-version): Print the version

### `studio-schedule accounts`

Administer the accounts: roles, passwords, sessions.

```text
studio-schedule accounts <command> [flags]
```

Commands:

- [`studio-schedule accounts create`](#studio-schedule-accounts-create): Create an account for a person and mail it an invitation
- [`studio-schedule accounts delete`](#studio-schedule-accounts-delete): Delete an account; its person stays
- [`studio-schedule accounts disable`](#studio-schedule-accounts-disable): Disable an account: it is signed out and may not sign in
- [`studio-schedule accounts enable`](#studio-schedule-accounts-enable): Enable a disabled account again
- [`studio-schedule accounts list`](#studio-schedule-accounts-list): List every account with its role and status
- [`studio-schedule accounts reset-password`](#studio-schedule-accounts-reset-password): Print a password-reset link for an account, or mail it with `--mail`
- [`studio-schedule accounts sessions`](#studio-schedule-accounts-sessions): End the sessions of accounts
- [`studio-schedule accounts set-role`](#studio-schedule-accounts-set-role): Change the role of an account
- [`studio-schedule accounts show`](#studio-schedule-accounts-show): Show one account

#### `studio-schedule accounts create`

Create an account for a person and mail it an invitation.

```text
studio-schedule accounts create <email> --person <id> --role <role> [flags]
```

Create an account for an existing person, as the office's person page does: unconfirmed and without a password. It mails the address an invitation, whose link (valid for 72 hours) sets the password and confirms the account; the deliver-mail job sends it. Only an admin creates an admin account. The person is named by its ID (see the office's person page); a person with an account and an email another account has are refused with exit status 4.

Examples:

```sh
studio-schedule accounts create --as office@example.org --person 0190a5d2-ac96-774b-bcce-b302099a8057 --role instructor dance.teacher@example.org
```

Flags:

- `--person` (string): ID of the person the account is for (required)
- `--role` (string): role of the account: member, instructor, office or admin (required)

Access: `office`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `office` or above.

Destructive: asks for confirmation, or needs `--yes` (see [Confirmation](#confirmation)).

Output with `--output json`: the response body of the operation createPersonAccount.

#### `studio-schedule accounts delete`

Delete an account; its person stays.

```text
studio-schedule accounts delete <email|id> [flags]
```

Delete an account with its password, sessions, links, calendar feeds and announcement dismissals, as the admin's account page does; its person stays. An account that receipts, the audit log, announcements or duplicate dismissals name is refused (in-use, exit status 4): disable it instead. Recorded in the audit log. The account is named by its email address (in any case) or its ID. Refused with exit status 4: the `--as` account's own account (own-account) and the last admin who can sign in (last-admin).

Examples:

```sh
studio-schedule accounts delete --as admin@example.org --yes member@example.org
```

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Destructive: asks for confirmation, or needs `--yes` (see [Confirmation](#confirmation)).

Output with `--output json`: the account as it was, an item of the response body of the operation listAccounts (AccountEntry); the operation deleteAccount answers without a body.

#### `studio-schedule accounts disable`

Disable an account: it is signed out and may not sign in.

```text
studio-schedule accounts disable <email|id> [flags]
```

Disable an account: it may no longer sign in, and its sessions and the impersonations it started end. Recorded in the audit log. The account is named by its email address (in any case) or its ID. Refused with exit status 4: the `--as` account's own account (own-account) and the last admin who can sign in (last-admin).

Examples:

```sh
studio-schedule accounts disable --as admin@example.org --yes member@example.org
```

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Destructive: asks for confirmation, or needs `--yes` (see [Confirmation](#confirmation)).

Output with `--output json`: the response body of the operation disableAccount.

#### `studio-schedule accounts enable`

Enable a disabled account again.

```text
studio-schedule accounts enable <email|id> [flags]
```

Enable a disabled account again: it may sign in. Recorded in the audit log. The account is named by its email address (in any case) or its ID. The `--as` account's own account is refused (own-account, exit status 4).

Examples:

```sh
studio-schedule accounts enable --as admin@example.org member@example.org
```

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Output with `--output json`: the response body of the operation enableAccount.

#### `studio-schedule accounts list`

List every account with its role and status.

```text
studio-schedule accounts list [flags]
```

List every account, ordered by email, with its role, its status (active; pending: the email is not confirmed yet; disabled) and the name of its person, as the admin's account page does.

Examples:

```sh
studio-schedule accounts list --as admin@example.org
```

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Output with `--output json`: the response body of the operation listAccounts.

#### `studio-schedule accounts reset-password`

Print a password-reset link for an account, or mail it with `--mail`.

```text
studio-schedule accounts reset-password <email|id> [flags]
```

Issue a password-reset link for an account, valid for one hour; it replaces the account's older links. Without `--mail` the link is printed on stdout and nothing is mailed: this is the way back in for an admin who has forgotten their password (run it `--as` their own email). With `--mail` the link is mailed to the account, as "forgot password" does, and not printed; the deliver-mail job sends it. The link sets the password once; keep it secret. It works for an account that may sign in and for an invited one; a disabled account or an unconfirmed registration is refused with exit status 4. The account is named by its email address (in any case) or its ID.

Examples:

```sh
studio-schedule accounts reset-password --as admin@example.org admin@example.org
studio-schedule accounts reset-password --as admin@example.org --mail --yes member@example.org
```

Flags:

- `--mail` (bool): mail the link to the account instead of printing it

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Destructive: asks for confirmation, or needs `--yes` (see [Confirmation](#confirmation)).

Output with `--output json`: one object: account (an item of the response body of the operation listAccounts (AccountEntry)), link (the reset link; absent with `--mail`), expiresAt (when the link stops working, an RFC 3339 instant), mailed (bool).

#### `studio-schedule accounts sessions`

End the sessions of accounts.

```text
studio-schedule accounts sessions <command> [flags]
```

Commands:

- [`studio-schedule accounts sessions revoke`](#studio-schedule-accounts-sessions-revoke): End the sessions of an account, or of every account

##### `studio-schedule accounts sessions revoke`

End the sessions of an account, or of every account.

```text
studio-schedule accounts sessions revoke <email|id>|--all [flags]
```

Sign an account out everywhere: end its sessions and the impersonations it started, as disabling it does, and remove its devices' push subscriptions. It may sign in again. With `--all`, every account is signed out (after a leak, say). It prints how many sessions ended. The account is named by its email address (in any case) or its ID.

Examples:

```sh
studio-schedule accounts sessions revoke --as admin@example.org --yes member@example.org
studio-schedule accounts sessions revoke --as admin@example.org --all
```

Flags:

- `--all` (bool): end the sessions of every account

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Destructive: asks for confirmation, or needs `--yes` (see [Confirmation](#confirmation)).

Output with `--output json`: one object: sessionsEnded (how many sessions ended that had not ended yet), accounts (of how many accounts).

#### `studio-schedule accounts set-role`

Change the role of an account.

```text
studio-schedule accounts set-role <email|id> <role> [flags]
```

Give an account another role (member, instructor, office or admin); it takes effect with its next request. Recorded in the audit log. The account is named by its email address (in any case) or its ID. Refused with exit status 4: the `--as` account's own account (own-account) and the last admin who can sign in (last-admin).

Examples:

```sh
studio-schedule accounts set-role --as admin@example.org office@example.org admin
```

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Output with `--output json`: the response body of the operation setAccountRole.

#### `studio-schedule accounts show`

Show one account.

```text
studio-schedule accounts show <email|id> [flags]
```

Show an account: its ID, email, role, status and person. The account is named by its email address (in any case) or its ID.

Examples:

```sh
studio-schedule accounts show --as admin@example.org member@example.org
```

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Output with `--output json`: an item of the response body of the operation listAccounts (AccountEntry).

### `studio-schedule completion`

Generate the autocompletion script for the specified shell.

```text
studio-schedule completion <command> [flags]
```

Generate the autocompletion script for studio-schedule for the specified shell. See each sub-command's help for details on how to use the generated script.

Commands:

- [`studio-schedule completion bash`](#studio-schedule-completion-bash): Generate the autocompletion script for bash
- [`studio-schedule completion fish`](#studio-schedule-completion-fish): Generate the autocompletion script for fish
- [`studio-schedule completion powershell`](#studio-schedule-completion-powershell): Generate the autocompletion script for powershell
- [`studio-schedule completion zsh`](#studio-schedule-completion-zsh): Generate the autocompletion script for zsh

#### `studio-schedule completion bash`

Generate the autocompletion script for bash.

```text
studio-schedule completion bash
```

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package. If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

```text
source <(studio-schedule completion bash)
```

To load completions for every new session, execute once:

Linux:

```text
studio-schedule completion bash > /etc/bash_completion.d/studio-schedule
```

macOS:

```text
studio-schedule completion bash > $(brew --prefix)/etc/bash_completion.d/studio-schedule
```

You will need to start a new shell for this setup to take effect.

Flags:

- `--no-descriptions` (bool): disable completion descriptions

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

#### `studio-schedule completion fish`

Generate the autocompletion script for fish.

```text
studio-schedule completion fish [flags]
```

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

```text
studio-schedule completion fish | source
```

To load completions for every new session, execute once:

```text
studio-schedule completion fish > ~/.config/fish/completions/studio-schedule.fish
```

You will need to start a new shell for this setup to take effect.

Flags:

- `--no-descriptions` (bool): disable completion descriptions

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

#### `studio-schedule completion powershell`

Generate the autocompletion script for powershell.

```text
studio-schedule completion powershell [flags]
```

Generate the autocompletion script for powershell.

To load completions in your current shell session:

```text
studio-schedule completion powershell | Out-String | Invoke-Expression
```

To load completions for every new session, add the output of the above command to your powershell profile.

Flags:

- `--no-descriptions` (bool): disable completion descriptions

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

#### `studio-schedule completion zsh`

Generate the autocompletion script for zsh.

```text
studio-schedule completion zsh [flags]
```

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need to enable it. You can execute the following once:

```text
echo "autoload -U compinit; compinit" >> ~/.zshrc
```

To load completions in your current shell session:

```text
source <(studio-schedule completion zsh)
```

To load completions for every new session, execute once:

Linux:

```text
studio-schedule completion zsh > "${fpath[1]}/_studio-schedule"
```

macOS:

```text
studio-schedule completion zsh > $(brew --prefix)/share/zsh/site-functions/_studio-schedule
```

You will need to start a new shell for this setup to take effect.

Flags:

- `--no-descriptions` (bool): disable completion descriptions

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

### `studio-schedule config`

Inspect the configuration.

```text
studio-schedule config <command> [flags]
```

Commands:

- [`studio-schedule config check`](#studio-schedule-config-check): Validate the configuration and print it with the secrets redacted

#### `studio-schedule config check`

Validate the configuration and print it with the secrets redacted.

```text
studio-schedule config check [flags]
```

Load the configuration as serve does, from the environment and serve's flags, without starting anything: no database, no mail server. An invalid value exits with status 2 and names the variable or flag. Otherwise it prints every effective setting by its environment variable, with the secrets redacted (the database password, `SMTP_PASSWORD`, `VAPID_PRIVATE_KEY`). `DEMO_MODE` is the effective mode: true only without a database. With `--output json` it prints one object: databaseUrl, httpAddr, baseUrl (strings), demoMode (bool), smtp {host, port (number, 0: the default of the TLS mode), tls (empty: starttls), username, password, from}, trustedProxies (array of strings), hstsIncludeSubdomains (bool), metricsAddr, log {format, level}, push {publicKey, privateKey, subject}, as; an empty string is an unset setting, and the secrets (the database password, smtp.password, push.privateKey) are xxxxx when set.

Examples:

```sh
studio-schedule config check
studio-schedule config check --base-url https://schedule.example.org --output json
```

Flags:

- `--addr` (string, default `:8080`): listen address host:port (env `HTTP_ADDR`)
- `--base-url` (string): public origin of the app, e.g. https://schedule.example.org (env `BASE_URL`)
- `--log-format` (string, default `text`): log format: text or json (env `LOG_FORMAT`)
- `--log-level` (string, default `info`): log level: debug, info, warn or error (env `LOG_LEVEL`)
- `--metrics-addr` (string): listen address host:port of the Prometheus metrics (/metrics); empty: no metrics (env `METRICS_ADDR`)

Access: an operator command. It acts as no account and refuses `--as`.

Output with `--output json`: one object: databaseUrl, httpAddr, baseUrl (strings), demoMode (bool), smtp {host, port (number, 0: the default of the TLS mode), tls (empty: starttls), username, password, from}, trustedProxies (array of strings), hstsIncludeSubdomains (bool), metricsAddr, log {format, level}, push {publicKey, privateKey, subject}, as; an empty string is an unset setting, and the secrets (the database password, smtp.password, push.privateKey) are xxxxx when set.

### `studio-schedule doctor`

Check the configuration, the database, the mail server and the receipt module.

```text
studio-schedule doctor [flags]
```

Run every check of a deployment and print one line per check with its result (ok, fail, warn or skip) and a detail: config (the configuration as serve loads it, from the environment and serve's flags), database (it connects), schema (exactly the binary's migrations are applied), settings (the school settings exist, with a time zone and currency the app accepts), smtp (a connection and login to `SMTP_HOST`, nothing is sent; skipped without it), vapid (the web push key pair; skipped without it), base-url (https; plain http on localhost or no `BASE_URL` is a warning, for local development only, elsewhere a failure) and receipts (a fictional sample receipt renders as a PDF; skipped in a build without the receipt renderer). A check that needs a failed one is skipped. It exits with status 1 when a check fails; warnings and skipped checks do not fail. With `--output json` it prints one object: ok (bool, false when a check failed) and checks, an array of {name, result, detail} in the order config, database, schema, settings, smtp, vapid, base-url, receipts, where result is ok, fail, warn or skip.

Examples:

```sh
studio-schedule doctor
studio-schedule doctor --output json
```

Flags:

- `--addr` (string, default `:8080`): listen address host:port (env `HTTP_ADDR`)
- `--base-url` (string): public origin of the app, e.g. https://schedule.example.org (env `BASE_URL`)
- `--log-format` (string, default `text`): log format: text or json (env `LOG_FORMAT`)
- `--log-level` (string, default `info`): log level: debug, info, warn or error (env `LOG_LEVEL`)
- `--metrics-addr` (string): listen address host:port of the Prometheus metrics (/metrics); empty: no metrics (env `METRICS_ADDR`)

Access: an operator command. It acts as no account and refuses `--as`.

Output with `--output json`: one object: ok (bool, false when a check failed) and checks, an array of {name, result, detail} in the order config, database, schema, settings, smtp, vapid, base-url, receipts, where result is ok, fail, warn or skip.

### `studio-schedule healthcheck`

Check that the local server answers (for container health checks).

```text
studio-schedule healthcheck [flags]
```

Request /healthz from the server on this host, at the port of the listen address (`HTTP_ADDR` or `--addr`, as for serve), and exit with status 0 if it answers 200 OK, 1 otherwise. The container image's HEALTHCHECK runs it.

Flags:

- `--addr` (string, default `:8080`): listen address host:port of the server (env `HTTP_ADDR`)

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

### `studio-schedule import`

Import members from a CSV file (a dry run unless `--apply`).

```text
studio-schedule import <file.csv> [flags]
```

Import the members of a CSV file into the database from `DATABASE_URL` or `--database-url`, as the office's import page does (ADR 0015), acting as the office account of `--as`: a member is found by its member number, a guardian by its email or name; updates write only the filled fields; guardian links are added, never removed; no accounts are created. The columns are matched by their English or German headers, or mapped with `--map field=Header` (fields: memberNo, givenName, familyName, email, phone, birthDate, notes, guardian1GivenName, guardian1FamilyName, guardian1Email, guardian1Phone, guardian2GivenName, guardian2FamilyName, guardian2Email, guardian2Phone). Without `--apply` it is a dry run that writes nothing; with it, the run is recorded in the import history under the account of `--as`. Rows with problems are reported and left out; the other rows are imported, and the command exits with status 1.

Examples:

```sh
studio-schedule import --as office@example.org members.csv
studio-schedule import --as office@example.org --apply --notes Kurs members.csv
```

Flags:

- `--apply` (bool): import; without it, a dry run that writes nothing
- `--map` (stringArray): map a field to a column, e.g. `--map memberNo=Nr` (repeatable; replaces the header matching)
- `--notes` (stringArray): a further column whose values go into the notes (repeatable)

Access: `office`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `office` or above.

Output with `--output json`: the response body of the operation runImport.

### `studio-schedule init`

Set up a new school: its settings and the first admin.

```text
studio-schedule init [flags]
```

Set up a new school in the empty, migrated database from `DATABASE_URL` or `--database-url`: the school's name, time zone, language and currency (the other settings start with defaults that admins change in the app), and the first admin account. Values not given as flags are asked for. The time zone and the currency cannot change once lessons or prices exist. No mail is sent: init prints a one-time link, valid for 72 hours, that sets the admin's password. It points to `BASE_URL` (or `--base-url`). init refuses to run when the school is set up already.

Flags:

- `--admin-email` (string): Email address of the first admin
- `--admin-family-name` (string): Family name of the first admin
- `--admin-given-name` (string): Given name of the first admin
- `--base-url` (string): public origin of the app that the setup link points to (env `BASE_URL`)
- `--currency` (string): Currency (ISO 4217)
- `--locale` (string): Default language (en or de)
- `--school-name` (string): School name
- `--timezone` (string): Time zone (IANA, e.g. Europe/Berlin)

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

### `studio-schedule jobs`

List the background jobs and run one now.

```text
studio-schedule jobs <command> [flags]
```

Commands:

- [`studio-schedule jobs list`](#studio-schedule-jobs-list): List the background jobs with their interval and latest run
- [`studio-schedule jobs run`](#studio-schedule-jobs-run): Run a background job now and wait for it

#### `studio-schedule jobs list`

List the background jobs with their interval and latest run.

```text
studio-schedule jobs list [flags]
```

List the background jobs of the database from `DATABASE_URL` or `--database-url` with their interval and their latest recorded run, of any process (a serve instance or jobs run): its start in the school's time zone, its duration, its result (ok, failed, or skipped because another process ran the job) and the error of a failed run. The jobs are those serve runs with the same configuration: deliver-mail only with `SMTP_HOST` set, deliver-push only with the `VAPID_*` keys. Runs are kept for 30 days.

Examples:

```sh
studio-schedule jobs list
studio-schedule jobs list --output json
```

Access: an operator command. It acts as no account and refuses `--as`.

Output with `--output json`: an array of {name, everySeconds, lastRun} in the order the jobs run at startup, where everySeconds is the interval and lastRun, absent before the first run, is {job, startedAt, endedAt, result, error}, where result is ok, failed or skipped and error is the error message of a failed run (absent otherwise); the instants are RFC 3339 in UTC.

#### `studio-schedule jobs run`

Run a background job now and wait for it.

```text
studio-schedule jobs run <name> [flags]
```

Run one background job now, in this process, under the job's lock, and wait for it: what serve does on the job's interval, recorded the same way (jobs list shows the run). The job does its real work: deliver-mail sends the due mails through the configured mail server, deliver-push sends the due push messages, announcements queues the mails of the announcements that are due, and purge-sessions, purge-mail and purge-personal-data delete what their retention periods let go. The jobs are those serve runs with the same configuration: deliver-mail only with `SMTP_HOST` set, deliver-push only with the `VAPID_*` keys. It prints the run and exits with status 1 when the job failed, 3 for a job this configuration does not have, and 4 when another process (a serve instance, another jobs run) holds the job's lock and runs it.

Examples:

```sh
studio-schedule jobs run generate-lessons
studio-schedule jobs run deliver-mail
```

Access: an operator command. It acts as no account and refuses `--as`.

Output with `--output json`: {job, startedAt, endedAt, result, error}, where result is ok, failed or skipped and error is the error message of a failed run (absent otherwise); the instants are RFC 3339 in UTC.

### `studio-schedule mail`

Test the mail server, list the mail log and queue failed mails again.

```text
studio-schedule mail <command> [flags]
```

Commands:

- [`studio-schedule mail list`](#studio-schedule-mail-list): List the mail log, newest first
- [`studio-schedule mail requeue`](#studio-schedule-mail-requeue): Queue a failed mail, or every failed one, again
- [`studio-schedule mail test`](#studio-schedule-mail-test): Send a test mail through the configured mail server

#### `studio-schedule mail list`

List the mail log, newest first.

```text
studio-schedule mail list [flags]
```

List the mails of the outbox as the admins' mail log does (ADR 0014), newest first, at most 50 per page, acting as the admin account of `--as`: when, the delivery state, the template, the recipient, the subject, the attempts and the last error. Bodies are never printed. `--status` and `--template` filter; when another page follows, a hint on stderr gives its `--cursor` (with `--output json`, the nextCursor of the page). The bodies of mails sent more than 30 days ago are removed, mails older than 180 days deleted.

Examples:

```sh
studio-schedule mail list --as admin@example.org --status failed
studio-schedule mail list --as admin@example.org --template receipt --output json
```

Flags:

- `--cursor` (string): where the page starts: the cursor of the previous page's hint (nextCursor)
- `--status` (string): only mails in this state: pending, sent or failed
- `--template` (string): only mails of this template, e.g. receipt

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Output with `--output json`: the response body of the operation searchMails.

#### `studio-schedule mail requeue`

Queue a failed mail, or every failed one, again.

```text
studio-schedule mail requeue <id> | --failed [flags]
```

Queue a failed mail of the mail log again, or with `--failed` every failed mail, acting as the admin account of `--as` (ADR 0014): its next attempt is now and its attempts start over, so the server's deliver-mail job sends it again. Each mail is recorded in the audit log as mail-requeued. A mail with a secret link (confirm-email, change-email, reset-password, invite, waitlist-invite) is never queued again: with an ID that is a conflict, with `--failed` it stays failed and is counted. A pending or sent mail is a conflict too.

Examples:

```sh
studio-schedule mail requeue --as admin@example.org 0190a7c4-1b2c-7d3e-8f40-5a6b7c8d9e0f
studio-schedule mail requeue --as admin@example.org --failed --yes
```

Flags:

- `--failed` (bool): queue every failed mail again instead of one

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Destructive: asks for confirmation, or needs `--yes` (see [Confirmation](#confirmation)).

Output with `--output json`: with an ID, the response body of the operation requeueMail; with `--failed`, one object: requeued (the number of failed mails queued again) and secretLink (the number of failed mails with a secret link, which stay failed).

#### `studio-schedule mail test`

Send a test mail through the configured mail server.

```text
studio-schedule mail test --to <address> [flags]
```

Render a short test mail and send it to `--to` straight through the mail server of `SMTP_HOST` and the other `SMTP_*` variables, as serve sends mail, but not through the outbox: it sends a real mail. It is an operator check like doctor's smtp line, which only logs in: it prints ok when the server accepted the mail and exits with status 1 and the server's error when it did not. Without `SMTP_HOST` it exits with status 2. The mail is in English, or in the language of `--locale`.

Examples:

```sh
studio-schedule mail test --to office@example.org
studio-schedule mail test --to office@example.org --locale de
```

Flags:

- `--locale` (string, default `en`): the language of the test mail: en or de
- `--to` (string): the address the test mail goes to (required)

Access: an operator command. It acts as no account and refuses `--as`.

Output with `--output json`: one object: to (the recipient), server (the mail server, `SMTP_HOST`), locale (the language of the mail) and subject.

### `studio-schedule migrate`

Manage the database schema.

```text
studio-schedule migrate <command> [flags]
```

Apply, roll back or list the schema migrations embedded in the binary. The database comes from `DATABASE_URL` or `--database-url`.

Commands:

- [`studio-schedule migrate down`](#studio-schedule-migrate-down): Roll back the most recent migration
- [`studio-schedule migrate status`](#studio-schedule-migrate-status): List the migrations and whether they are applied
- [`studio-schedule migrate up`](#studio-schedule-migrate-up): Apply all pending migrations

#### `studio-schedule migrate down`

Roll back the most recent migration.

```text
studio-schedule migrate down [flags]
```

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

#### `studio-schedule migrate status`

List the migrations and whether they are applied.

```text
studio-schedule migrate status [flags]
```

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

#### `studio-schedule migrate up`

Apply all pending migrations.

```text
studio-schedule migrate up [flags]
```

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

### `studio-schedule openapi`

Print the OpenAPI 3.1 specification as JSON.

```text
studio-schedule openapi [flags]
```

Print the OpenAPI 3.1 specification of the HTTP API to stdout. The output is deterministic; it is committed as api/openapi.json and used to generate the TypeScript client.

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

### `studio-schedule seed`

Load the fictional demo data set into an empty database.

```text
studio-schedule seed [flags]
```

Load the fictional demo data set (school settings, catalog, timetable, families and accounts) into the database from `DATABASE_URL` or `--database-url`. The schema must be up to date, and the database must be empty: seed never mixes demo data into a school that is in use. Every demo account, admin included, signs in with the published password demo-password, so never seed a public installation.

The demo term and workshops lie around the anchor date: by default the fixed date 2026-09-22 (the data of the tests), with `--anchor today` the current date in the demo school's time zone (like the web demo), or a date YYYY-MM-DD.

`--school` picks the demo school: programme (the default, in the app's own look) or werkstatt (the same people and timetable as a German school in the Werkstatt look, with its own imprint, German receipts and a background image).

Flags:

- `--anchor` (string, default `2026-09-22`): date the demo term lies around: YYYY-MM-DD, or today (in the demo school's time zone)
- `--school` (string, default `programme`): demo school: programme or werkstatt

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

### `studio-schedule serve`

Run the HTTP server (API and web UI).

```text
studio-schedule serve [flags]
```

Run the HTTP server (API and web UI). Every setting can also be given as an environment variable (see .env.example); a flag on the command line wins over the environment.

Flags:

- `--addr` (string, default `:8080`): listen address host:port (env `HTTP_ADDR`)
- `--base-url` (string): public origin of the app, e.g. https://schedule.example.org (env `BASE_URL`)
- `--log-format` (string, default `text`): log format: text or json (env `LOG_FORMAT`)
- `--log-level` (string, default `info`): log level: debug, info, warn or error (env `LOG_LEVEL`)
- `--metrics-addr` (string): listen address host:port of the Prometheus metrics (/metrics); empty: no metrics (env `METRICS_ADDR`)

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

### `studio-schedule settings`

Read and change the school settings.

```text
studio-schedule settings <command> [flags]
```

Commands:

- [`studio-schedule settings export`](#studio-schedule-settings-export): Write every area's settings as one JSON document
- [`studio-schedule settings get`](#studio-schedule-settings-get): Print the settings of an area, or of all areas
- [`studio-schedule settings import`](#studio-schedule-settings-import): Write the settings of a settings export (overwrites them)
- [`studio-schedule settings set`](#studio-schedule-settings-set): Change settings of an area

#### `studio-schedule settings export`

Write every area's settings as one JSON document.

```text
studio-schedule settings export [file] [flags]
```

Write the settings of every area as one JSON document, to the file or else to stdout, acting as the office account of `--as`: the document settings import reads, to set up a second school the same way. Each area is its API request body, so the read-only fields are left out, and so are the logo and the background image: set them on the settings page. The receipt settings name the school as the issuer; edit them for another school. It prints JSON with either `--output`.

Examples:

```sh
studio-schedule settings export --as office@example.org settings.json
```

Access: `office`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `office` or above.

Output with `--output json`: the settings document, always JSON: version (number, 1), then every area's request body by area: school (updateSchoolSettings), branding (updateBrandingSettings), regional (updateRegionalSettings), bookingRules (updateBookingRules), receipts (updateReceiptSettings), passes (updatePassSettings).

#### `studio-schedule settings get`

Print the settings of an area, or of all areas.

```text
studio-schedule settings get [area] [flags]
```

Print the settings of an area (school, branding, regional, booking-rules, receipts, passes), or of every area without one, as the settings page shows them, acting as the office account of `--as`: one line per setting with the area, the key and the value. The keys are the fields of the area's API body, which settings set takes; the fields only shown (such as timezoneLocked or the booking rules' window) are read-only. Durations show in days, hours and minutes, amounts in the school's currency and instants in the school's time zone; `--output json` prints the API's form.

Examples:

```sh
studio-schedule settings get --as office@example.org
studio-schedule settings get booking-rules --as office@example.org --output json
```

Access: `office`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `office` or above.

Output with `--output json`: with an area, the response body of its get operation: getSchoolSettings (school), getBrandingSettings (branding), getRegionalSettings (regional), getBookingRules (booking-rules), getReceiptSettings (receipts) or getPassSettings (passes); without one, an object with every area's: school, branding, regional, bookingRules, receipts, passes.

#### `studio-schedule settings import`

Write the settings of a settings export (overwrites them).

```text
studio-schedule settings import <file> [flags]
```

Write the areas of a document of settings export, acting as the admin account of `--as`: all of them in one transaction, or none if one is refused, each with the rules of its card. An area in the document replaces all of its settings; an area left out stays as it is. The time zone and the currency stay locked once used: a different locked value is a conflict (exit status 4). It overwrites settings, so it asks for confirmation on a terminal and needs `--yes` otherwise; `--dry-run` checks everything and writes nothing. It prints every setting that changes (or would change), with its value before and after.

Examples:

```sh
studio-schedule settings import --as admin@example.org --dry-run settings.json
studio-schedule settings import --as admin@example.org --yes settings.json
```

Flags:

- `--dry-run` (bool): check the document and print the changes; write nothing

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Destructive: asks for confirmation, or needs `--yes` (see [Confirmation](#confirmation)).

Output with `--output json`: one object: dryRun (bool), changes (array of {area, key, from, to}: the area as the commands name it, the key of its request body, and the values before and after in the request body's form, null for none), generatedLessons (number: lessons a wider release window generated at once).

#### `studio-schedule settings set`

Change settings of an area.

```text
studio-schedule settings set <area> key=value... [flags]
```

Change the named settings of an area (school, branding, regional, booking-rules, receipts, passes) and keep the others, as the area's card on the settings page does, acting as the admin account of `--as`: the area is read, the keys are changed and the area is written back, with the same rules as the API. The keys are the fields of the area's API body (settings get lists them): a number is a whole number, a date YYYY-MM-DD (empty: none), a time HH:MM, and the cutoffs count minutes. The time zone cannot change once lessons exist, the currency once a price or a receipt exists (exit status 4). A wider release window generates its lessons at once. It prints the area after the change.

Examples:

```sh
studio-schedule settings set school schoolName='Studio Example' --as admin@example.org
studio-schedule settings set booking-rules releaseWeeksAhead=3 cancellationCutoffMinutes=180 --as admin@example.org
```

Access: `admin`. Acts as the account of `--as` (or `STUDIO_AS`), which needs the role `admin` or above.

Output with `--output json`: the response body of the area's update operation: updateSchoolSettings, updateBrandingSettings, updateRegionalSettings, updateBookingRules, updateReceiptSettings or updatePassSettings.

### `studio-schedule vapid-keys`

Print a new VAPID key pair for web push.

```text
studio-schedule vapid-keys [flags]
```

Print a new VAPID key pair for web push notifications, as `VAPID_PUBLIC_KEY` and `VAPID_PRIVATE_KEY` lines for the environment. Set them once, together with `VAPID_SUBJECT`: a new pair invalidates every browser's subscription.

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.

### `studio-schedule version`

Print the version.

```text
studio-schedule version [flags]
```

Access: an operator command. It acts as no account and refuses `--as`.

Output: text only; `--output json` is refused.
