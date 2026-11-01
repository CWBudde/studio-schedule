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
- [`studio-schedule db`](#studio-schedule-db): Check the database
- [`studio-schedule doctor`](#studio-schedule-doctor): Check the configuration, the database, the mail server and the receipt module
- [`studio-schedule healthcheck`](#studio-schedule-healthcheck): Check that the local server answers (for container health checks)
- [`studio-schedule import`](#studio-schedule-import): Import members from a CSV file (a dry run unless `--apply`)
- [`studio-schedule init`](#studio-schedule-init): Set up a new school: its settings and the first admin
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

### `studio-schedule db`

Check the database.

```text
studio-schedule db <command> [flags]
```

Commands:

- [`studio-schedule db check`](#studio-schedule-db-check): Check the data for inconsistencies the database cannot rule out

#### `studio-schedule db check`

Check the data for inconsistencies the database cannot rule out.

```text
studio-schedule db check [flags]
```

Run the integrity checks on the database from `DATABASE_URL` or `--database-url` and print one line per finding: passes (a visit pass's used visits equal the bookings it paid for, and no pass paid for a booking that costs no visit: one canceled in time, or one of a lesson the school canceled), capacity (no lesson has more active bookings than places, but for those booked beyond the capacity on purpose), tombstones (nothing still refers to a merged or anonymized person that the merge would have moved or anonymizing ended) and receipt-counters (each year's receipt counter is at least the highest receipt number issued that year). `--only` runs some of them. A finding names records by their IDs, never by personal data. The command only reads, in one read-only transaction, so it is safe on a live database. It exits with status 0 without a finding, 1 with findings or when the database cannot be opened, and 2 for an unknown check or without a database URL. With `--output json` it prints one object: ok (bool, false when a check found something), checks (an array of the names of the checks that ran, in their order) and findings (an array, empty without any, of {check, entity, id, message}: the check that found it; the entity pass, booking, lesson, person or receipt-year; the record's ID, or the year of a receipt counter; and what is inconsistent).

Examples:

```sh
studio-schedule db check
studio-schedule db check --only passes,capacity --output json
```

Flags:

- `--only` (stringSlice): run only these checks (repeatable or comma-separated): passes, capacity, tombstones, receipt-counters

Access: an operator command. It acts as no account and refuses `--as`.

Output with `--output json`: one object: ok (bool, false when a check found something), checks (an array of the names of the checks that ran, in their order) and findings (an array, empty without any, of {check, entity, id, message}: the check that found it; the entity pass, booking, lesson, person or receipt-year; the record's ID, or the year of a receipt counter; and what is inconsistent).

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
