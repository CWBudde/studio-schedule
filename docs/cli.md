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
- [`studio-schedule vapid-keys`](#studio-schedule-vapid-keys): Print a new VAPID key pair for web push
- [`studio-schedule version`](#studio-schedule-version): Print the version

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
