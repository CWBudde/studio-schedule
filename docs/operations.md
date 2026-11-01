# Operations

How to configure, deploy, back up, update and release Studio Schedule. The app runs as **one
instance** (see [One instance](#one-instance)).

- [Configuration reference](#configuration-reference)
- [Deployment guide](#deployment-guide)
- [Container image](#container-image)
- [Backups and restore](#backups-and-restore)
- [Upgrades](#upgrades)
- [Upgrading PostgreSQL to a new major version](#upgrading-postgresql-to-a-new-major-version)
- [Releases](#releases)
- [Privacy](#privacy)

## Configuration reference

The server binary `studio-schedule` reads its configuration from environment variables and
command-line flags. A flag given on the command line wins over the environment; an empty
environment variable counts as unset. `serve` checks every value at startup and names each bad one
in a single error, without printing secrets. The other commands check the same values, except the
log settings, which only `serve` uses. `.env.example` lists the variables with examples; the `just`
recipes load a local `.env`.

Prefer the environment for secrets: flags are visible in the process list.

### Environment variables

| Variable                  | Flag             | Default                       | Meaning                                                                                                                                                                                                                                                                                                                             |
| ------------------------- | ---------------- | ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DATABASE_URL`            | `--database-url` | empty                         | PostgreSQL connection (below); the flag is the global `--database-url` ([Global flags](#global-flags)). Empty: `serve` refuses to start unless `DEMO_MODE=true`. `migrate`, `seed`, `init`, `import`, `doctor` and the commands that act as an account need it.                                                                     |
| `DEMO_MODE`               | –                | `false`                       | `true` or `false`. Explicit opt-in to run `serve` without a database, on an in-memory store with the fictional demo data and its published demo logins; changes are lost on exit. Never set it in production: a missing `DATABASE_URL` must fail loudly instead of silently becoming the public demo.                               |
| `HTTP_ADDR`               | `--addr`         | `:8080`                       | Listen address `host:port` of the app (API, web UI, `/healthz`, `/readyz`). `healthcheck` requests this port on the local host.                                                                                                                                                                                                     |
| `BASE_URL`                | `--base-url`     | `http://localhost:<port>`     | The public origin `scheme://host[:port]`, without path. Links in mails, calendar feed URLs and the link `init` prints point to it. With `https://` the session cookie is `Secure` and every answer carries `Strict-Transport-Security`; with `http://` or unset, `serve` warns at startup.                                          |
| `TRUSTED_PROXIES`         | –                | empty                         | Reverse proxies in front of the app, as addresses or CIDR prefixes, comma-separated (`10.0.0.0/8,192.0.2.10`). Only their `X-Forwarded-For` and `X-Request-Id` headers are believed. Empty: the connection's address is the client.                                                                                                 |
| `HSTS_INCLUDE_SUBDOMAINS` | –                | `true`                        | `true` or `false`. With an `https://` `BASE_URL`, `Strict-Transport-Security` covers every subdomain of the host too (`includeSubDomains`); `false` limits it to the app's own host. Turn it off when the app runs on a domain whose other subdomains still serve plain HTTP (see [Reverse proxy and TLS](#reverse-proxy-and-tls)). |
| `METRICS_ADDR`            | `--metrics-addr` | empty                         | Listen address `host:port` of a separate server for Prometheus metrics at `/metrics`. Empty: no metrics. Must not be the port of `HTTP_ADDR`. Never expose it to the internet.                                                                                                                                                      |
| `LOG_FORMAT`              | `--log-format`   | `text` (`json` in the image)  | `text` or `json` (one JSON object per line).                                                                                                                                                                                                                                                                                        |
| `LOG_LEVEL`               | `--log-level`    | `info`                        | `debug`, `info`, `warn` or `error`.                                                                                                                                                                                                                                                                                                 |
| `SMTP_HOST`               | –                | empty                         | Mail server. Empty: mails stay in the outbox and are never sent, and `serve` warns at startup (with a database).                                                                                                                                                                                                                    |
| `SMTP_PORT`               | –                | by `SMTP_TLS`: 587, 465 or 25 | Port of the mail server.                                                                                                                                                                                                                                                                                                            |
| `SMTP_TLS`                | –                | `starttls`                    | `starttls` (the server must offer STARTTLS), `tls` (implicit TLS, SMTPS) or `none` (plain text, only for a local mail catcher such as Mailpit; authentication then needs a localhost host).                                                                                                                                         |
| `SMTP_USERNAME`           | –                | empty                         | User name for SMTP authentication; empty: no authentication.                                                                                                                                                                                                                                                                        |
| `SMTP_PASSWORD`           | –                | empty                         | Password for SMTP authentication. Masked in every log line.                                                                                                                                                                                                                                                                         |
| `SMTP_FROM`               | –                | empty                         | Sender address, optionally with a name: `Studio Example <schedule@example.test>`. Required with `SMTP_HOST`.                                                                                                                                                                                                                        |
| `VAPID_PUBLIC_KEY`        | –                | empty                         | Web push (ADR 0017): the public key of the app's VAPID key pair, which `studio-schedule vapid-keys` prints. With all three `VAPID_*` set, members can turn on notifications on a device for waiting-list invites (besides the mail); without them push is off and the switch hidden. All three or none.                             |
| `VAPID_PRIVATE_KEY`       | –                | empty                         | The private key of that pair. Masked in every log line. Keep it: a new pair invalidates every device's notifications (members turn them on again).                                                                                                                                                                                  |
| `VAPID_SUBJECT`           | –                | empty                         | The contact the browser vendors' push services see, a `mailto:` or `https:` URL: `mailto:office@example.test`.                                                                                                                                                                                                                      |
| `XDG_CACHE_HOME`          | –                | the system's cache directory  | Not a setting of the app: the cache directory of the user (on Linux `~/.cache`). The compiled receipt renderer is kept in `<cache>/studio-schedule/wazero`; it is disposable. The image sets `/var/cache/studio-schedule`.                                                                                                          |
| `STUDIO_AS`               | `--as`           | empty                         | The email of the account a command of the command line acts as ([The command line](#the-command-line)); its role must reach the command's access level. `serve` ignores it.                                                                                                                                                         |
| `NO_COLOR`                | –                | empty                         | Set and not empty: the command line's text output has no colors ([no-color.org](https://no-color.org)).                                                                                                                                                                                                                             |

**`DATABASE_URL`** takes either form libpq knows, with any of its parameters:

```sh
# URL; percent-encode special characters of the password (@ → %40, / → %2F, …)
DATABASE_URL='postgres://studio:change-me@db.example.test:5432/studio?sslmode=verify-full'
# key=value DSN; quote values with spaces in single quotes
DATABASE_URL="host=db.example.test port=5432 user=studio password=change-me dbname=studio sslmode=require"
```

Use `sslmode=verify-full` (or at least `require`) for a database on another host; the image has the
CA certificates to verify a managed database's certificate. `sslmode=disable` is fine only on a
private network such as the one of Docker Compose.

### Global flags

These go before or after the command, as `studio-schedule --output json doctor` or
`studio-schedule doctor -o json`; [cli.md](cli.md#global-flags) has their details.

| Flag             | Variable       | Meaning                                                                                                  |
| ---------------- | -------------- | -------------------------------------------------------------------------------------------------------- |
| `--database-url` | `DATABASE_URL` | as `DATABASE_URL`: the database of every command that needs one                                          |
| `--output`, `-o` | –              | `text` (default) or `json`: one JSON document on stdout; a command that prints text only refuses `json`  |
| `--yes`, `-y`    | –              | confirm a destructive command without a prompt; needed when stdin is not a terminal                      |
| `--as`           | `STUDIO_AS`    | as `STUDIO_AS`: the email of the account a command acts as; a command that acts as no account refuses it |

### Commands and flags

| Command        | Flag                  | Meaning                                                                                                                                                                                                                 |
| -------------- | --------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `serve`        | `--addr`              | as `HTTP_ADDR`                                                                                                                                                                                                          |
| `serve`        | `--base-url`          | as `BASE_URL`                                                                                                                                                                                                           |
| `serve`        | `--metrics-addr`      | as `METRICS_ADDR`                                                                                                                                                                                                       |
| `serve`        | `--log-format`        | as `LOG_FORMAT`                                                                                                                                                                                                         |
| `serve`        | `--log-level`         | as `LOG_LEVEL`                                                                                                                                                                                                          |
| `seed`         | `--anchor`            | the date the demo term and workshops lie around: `YYYY-MM-DD`, or `today` in the demo school's time zone (Europe/Berlin); default `2026-09-22`, the fixed date of the tests. Use `today` for a demo that stays current. |
| `seed`         | `--school`            | the demo school: `programme` (default, the app's own look) or `werkstatt` (the same data as a German school in the Werkstatt look, with a background image).                                                            |
| `init`         | `--base-url`          | as `BASE_URL`: the origin of the printed setup link                                                                                                                                                                     |
| `init`         | `--school-name`       | the school's name                                                                                                                                                                                                       |
| `init`         | `--timezone`          | the school's IANA time zone, e.g. `Europe/Berlin`                                                                                                                                                                       |
| `init`         | `--locale`            | the default language, `en` (default) or `de`                                                                                                                                                                            |
| `init`         | `--currency`          | ISO 4217 currency, default `EUR`                                                                                                                                                                                        |
| `init`         | `--admin-email`       | email address of the first admin                                                                                                                                                                                        |
| `init`         | `--admin-given-name`  | given name of the first admin                                                                                                                                                                                           |
| `init`         | `--admin-family-name` | family name of the first admin                                                                                                                                                                                          |
| `import`       | `--map`               | `field=Header`: the column of a field (repeatable), instead of matching the headers by name                                                                                                                             |
| `import`       | `--notes`             | a further column whose values go into the notes (repeatable)                                                                                                                                                            |
| `import`       | `--apply`             | import; without it, a dry run that writes nothing                                                                                                                                                                       |
| `healthcheck`  | `--addr`              | as `HTTP_ADDR`: the port to request `/healthz` at                                                                                                                                                                       |
| `config check` | `--addr`              | as for `serve`: the value to check instead of `HTTP_ADDR`                                                                                                                                                               |
| `config check` | `--base-url`          | as for `serve`, instead of `BASE_URL`                                                                                                                                                                                   |
| `config check` | `--metrics-addr`      | as for `serve`, instead of `METRICS_ADDR`                                                                                                                                                                               |
| `config check` | `--log-format`        | as for `serve`, instead of `LOG_FORMAT`                                                                                                                                                                                 |
| `config check` | `--log-level`         | as for `serve`, instead of `LOG_LEVEL`                                                                                                                                                                                  |
| `doctor`       | `--addr`              | as for `serve`: the configuration to check, instead of `HTTP_ADDR`                                                                                                                                                      |
| `doctor`       | `--base-url`          | as for `serve`, instead of `BASE_URL`                                                                                                                                                                                   |
| `doctor`       | `--metrics-addr`      | as for `serve`, instead of `METRICS_ADDR`                                                                                                                                                                               |
| `doctor`       | `--log-format`        | as for `serve`, instead of `LOG_FORMAT`                                                                                                                                                                                 |
| `doctor`       | `--log-level`         | as for `serve`, instead of `LOG_LEVEL`                                                                                                                                                                                  |

### Commands and flags: completion

| Command                               | Flag                | Meaning                                                 |
| ------------------------------------- | ------------------- | ------------------------------------------------------- |
| `completion bash/zsh/fish/powershell` | `--no-descriptions` | leave the commands' descriptions out of the completions |

### Commands and flags: lessons, classes, stats

`terms list` takes no flag of its own; these commands act as the office account of `--as`.

| Command            | Flag                     | Meaning                                                                        |
| ------------------ | ------------------------ | ------------------------------------------------------------------------------ |
| `lessons list`     | `--date`                 | the school date to list (`YYYY-MM-DD`); default today                          |
| `lessons list`     | `--from`                 | the first school date of a range, with `--to`                                  |
| `lessons list`     | `--to`                   | the last school date of a range (inclusive; at most 42 days), with `--from`    |
| `lessons generate` | `--until`                | the last school date to generate the weekly classes' lessons for (required)    |
| `lessons cancel`   | `--note`                 | a note for those booked, e.g. the reason, in their mail                        |
| `lessons restore`  | `--confirm-room-overlap` | restore even if another lesson uses the room at an overlapping time            |
| `classes list`     | `--term`                 | the term's ID (from `terms list`); default the term that includes today        |
| `stats`            | `--range`                | `last4Weeks` (default), `last12Weeks`, `term` or `custom`                      |
| `stats`            | `--from`                 | the first school date of a custom range, with `--to`                           |
| `stats`            | `--to`                   | the last school date of a custom range (inclusive; at most 366 days)           |
| `stats`            | `--participants`         | list the top participants in the text, with their names (JSON always has them) |

### Commands and flags: jobs and mail

`jobs list` and `jobs run <name>` take no flags of their own.

| Command        | Flag         | Meaning                                                                      |
| -------------- | ------------ | ---------------------------------------------------------------------------- |
| `mail test`    | `--to`       | the address the test mail goes to (required)                                 |
| `mail test`    | `--locale`   | the language of the test mail: `en` (default) or `de`                        |
| `mail list`    | `--status`   | only mails in this state: `pending`, `sent` or `failed`                      |
| `mail list`    | `--template` | only mails of this template, e.g. `receipt`                                  |
| `mail list`    | `--cursor`   | where the page starts: the cursor of the previous page's hint (`nextCursor`) |
| `mail requeue` | `--failed`   | queue every failed mail again instead of one                                 |

### Commands and flags: accounts

| Command                    | Flag       | Meaning                                                                                     |
| -------------------------- | ---------- | ------------------------------------------------------------------------------------------- |
| `accounts create`          | `--person` | the ID of the person the new account is for (required)                                      |
| `accounts create`          | `--role`   | the new account's role: `member`, `instructor`, `office` or `admin` (required)              |
| `accounts reset-password`  | `--mail`   | mail the reset link to the account (the `deliver-mail` job sends it) instead of printing it |
| `accounts sessions revoke` | `--all`    | end the sessions of every account instead of one                                            |

`migrate up` applies every pending migration, `migrate down` rolls back the latest one and
`migrate status` lists them all; `seed` loads the fictional demo data into an empty, migrated
database. `openapi` prints the OpenAPI document, `version` the build version and `vapid-keys` a new
VAPID key pair for web push; none of these takes a flag of its own. To turn web push on
([ADR 0017](decisions/0017-pwa-and-web-push.md)), run `studio-schedule vapid-keys` once, put both
lines and a `VAPID_SUBJECT` into the environment and restart; keep the keys like a password. The
server then sends notifications to the browser vendors' push services over HTTPS (outbound port
443 to public addresses).
`init` asks for every value no flag gives; without a terminal, give them all as flags.
`config check` prints the effective configuration with the secrets redacted, and `doctor` checks a
deployment (configuration, database, schema, settings, mail server, push keys, `BASE_URL`,
receipts). Every command explains itself with `--help`, and [cli.md](cli.md) is the reference of
all of them. A test (`cmd/studio-schedule/docs_test.go`) fails when a variable or flag of the code,
global or a command's, is missing from these tables, or a table names one the code doesn't have.

### Commands and flags: db

| Command    | Flag     | Meaning                                                                                                       |
| ---------- | -------- | ------------------------------------------------------------------------------------------------------------- |
| `db check` | `--only` | run only these checks (repeatable or comma-separated): `passes`, `capacity`, `tombstones`, `receipt-counters` |

`db check` looks for what the services keep consistent and the database cannot enforce: a visit
pass's used visits against the bookings it paid for, each lesson's active bookings against its
places, merged and anonymized persons against what still refers to them, and each year's receipt
counter against the receipt numbers issued. It only reads, in one read-only transaction, so it is
safe on a live database. Run it after a restore, before and after an upgrade, and from cron: it
exits with status 1 on a finding (and prints the findings by record ID, never by personal data),
with 0 when the data is consistent.

### The command line

Every command runs locally on the server's shell, with the database's credentials; there is no
remote mode ([ADR 0020](decisions/0020-command-line.md)). [cli.md](cli.md) lists every command with
its flags, output and exit codes; it is generated from the commands (`just cli-docs`). With Docker
Compose, run a command as `docker compose run --rm app <command>`.

- **Acting as an account:** a command that does what the office or an admin does in the app (such
  as `import`) acts as the account of `--as <email>`, or of `STUDIO_AS`: a confirmed, enabled
  account whose role reaches the command's access level. No password is asked, since whoever can
  run the binary with the database's credentials can change the database anyway. The audit log
  records the change as made via the command line, with the OS user. Operator commands (`serve`,
  `migrate`, `seed`, `init`, `config check`, `doctor`, …) act as no account and refuse `--as`.
- **Exit codes:** `0` ok; `1` failed, including a check with findings and an import with failed
  rows; `2` usage (an unknown flag, a missing argument, invalid input before anything ran, a refused
  confirmation); `3` not found, also an unknown `--as`; `4` conflict, also a role too low for the
  command.
- **JSON:** `--output json` prints one JSON document on stdout: the response body of the API
  operation the command mirrors, as in `api/openapi.json`, or the schema that cli.md gives. Fields
  are only added; a rename or a removal waits for a major release. An error goes to stderr as one
  line, `error: …`, and with `--output json` as a problem document (`title`, `status`, `detail`,
  `errors`).
- **Confirmation:** a command that deletes, anonymizes, mails, cancels or merges asks `[y/N]` when
  stdin is a terminal. In a script or a cron job, give `--yes`: without it and without a terminal,
  the command changes nothing and exits with `2`. Secrets and search terms come from stdin, never
  from arguments.
- **Completion:** `studio-schedule completion bash` (or `zsh`, `fish`, `powershell`) prints a
  completion script; `studio-schedule completion bash --help` explains where to put it.

```sh
source <(studio-schedule completion bash)   # completions in the current shell
studio-schedule completion bash > /etc/bash_completion.d/studio-schedule  # in every new shell
```

## Deployment guide

### Quick start with Docker Compose

`docker-compose.yml` runs the app with PostgreSQL 18 and Mailpit; [Docker Compose](#docker-compose)
below has the details.

```sh
cp .env.example .env              # set POSTGRES_PASSWORD, BASE_URL, SMTP_* (see below)
docker compose up -d              # the app restarts until the schema is migrated
docker compose run --rm app migrate up
docker compose run --rm app init  # school name, time zone, language, currency, first admin
```

`init` prints a one-time link, valid for 72 hours, that sets the first admin's password. Open it,
set the password, and sign in. Everything else (locations, rooms, course types, terms, classes,
prices, receipts, branding) is set in the app under Administration. `init` refuses to run on a
database that has a school already. Never run `seed` on an installation that real people use: its
demo accounts, the admin included, share the published password `demo-password`.

### Reverse proxy and TLS

The app speaks plain HTTP. Put a reverse proxy in front of it that terminates TLS, and set
`BASE_URL` to the public `https://` origin: the session cookie is then `Secure` and browsers get
`Strict-Transport-Security` (one year, subdomains included). The app must be served from the root
of its host (`BASE_URL` has no path).

**HSTS and subdomains.** A browser that has seen the header refuses plain HTTP for a year, not only
for the app's host but, by default, for every subdomain of it. Serving the app on an apex domain
(`https://example.test`) or a host with subdomains of its own while another subdomain, such as an
intranet page or a printer's web interface, still serves only `http://`, makes that subdomain
unreachable in every browser that opened the app. Set `HSTS_INCLUDE_SUBDOMAINS=false` there, before
the first browser sees the header: a browser keeps the policy it got until it expires or the
browser opens the app again and gets the new one, so turning the setting off later frees the
subdomains only for browsers that come back.

[Caddy](https://caddyserver.com) gets and renews a certificate by itself:

```caddyfile
schedule.example.test {
	reverse_proxy 127.0.0.1:8080 {
		# A request ID for the app's log lines (with TRUSTED_PROXIES).
		header_up X-Request-Id {http.request.uuid}
	}
}
```

nginx, with a certificate from elsewhere:

```nginx
server {
    listen 443 ssl;
    server_name schedule.example.test;
    ssl_certificate     /etc/ssl/schedule.example.test/fullchain.pem;
    ssl_certificate_key /etc/ssl/schedule.example.test/privkey.pem;

    client_max_body_size 2m;   # the largest request is a member import of about 1.4 MB

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id $request_id;
    }
}
```

Keep the `Host` header: the cross-origin protection compares the browser's `Origin` with it, and
refuses a state-changing request (POST, PUT, DELETE) from any other origin with 403. Don't add
`Access-Control-Allow-*` headers at the proxy; the app needs none.

**`TRUSTED_PROXIES`** lists the proxy's address as the app sees it (`127.0.0.1` for a proxy on the
same host, the Docker network's prefix such as `172.16.0.0/12` for a proxy container). The app then
takes the client's address from `X-Forwarded-For`, read from the right and skipping trusted
addresses, for its rate limits (sign-in, password reset, registration). Without it, every client
behind the proxy shares one limit, and the app logs a warning at the first forwarded request. Never
list addresses that clients can reach the app from directly: they could then choose their address.

**Request IDs.** Every answer carries an `X-Request-Id` header, every log line of a request names
it as `requestId`, and every problem document (`application/problem+json`) has it in `requestId`.
A request ID from a trusted proxy is kept when it is 1 to 128 characters of letters, digits and
`-._~:+/=@` (so the proxy's and the app's logs match); otherwise, and from anyone else, the app
generates one. When a user reports an error, the request ID from the error leads to the log lines.

### Mail (SMTP)

The app sends mail through any SMTP server, e.g. that of the school's mail provider or a sending
service. A typical configuration with STARTTLS on port 587:

```sh
SMTP_HOST=smtp.example.test
SMTP_USERNAME=schedule@example.test
SMTP_PASSWORD=change-me
SMTP_FROM=Studio Example <schedule@example.test>
# SMTP_PORT=465 and SMTP_TLS=tls for a provider that offers only SMTPS
```

- Send from an address of a domain with SPF and DKIM records for the provider, or mails land in
  spam. Many providers require the sender to be the authenticated account or a verified address.
- Mails are written to an outbox in the same transaction as the change that causes them. The
  `deliver-mail` job sends them once a minute, at most 20 per run, and retries a failed send after
  5 min, 15 min, 1 h and 3 h; a permanent error (an SMTP 5xx reply) ends it at once. The office
  sees every mail and its state under Administration → Mails.
- Without `SMTP_HOST` nothing is sent. Docker Compose sends to its Mailpit instead, which shows
  each mail at http://localhost:8025 and delivers nothing.

### One instance

Run exactly one instance of `serve` per database. The rate limits are counted in the process's
memory, and the background jobs (lesson generation, mail delivery, waiting-list invitations, data
retention) run in the process on their own timers. A second instance would double every limit and
run every job twice. The mail delivery is safe against that (`FOR UPDATE SKIP LOCKED`), the others
are not designed for it. A restart resets the rate limits. For updates, stop the old instance
before the new one starts (Compose's `docker compose up -d` does this).

### Health and readiness

| Path       | Answers                                                                                                                   | Use for                                   |
| ---------- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------- |
| `/healthz` | 200 `{"status":"ok"}` while the process serves requests                                                                   | liveness: restart the container otherwise |
| `/readyz`  | 200 when the database answers and its schema matches the binary; 503 problem otherwise (always 200 with `DEMO_MODE=true`) | readiness: send traffic only when 200     |

Both are public, answer GET and HEAD, are never cached, and are logged at Debug (a failing probe at
Info). `/readyz` checks at
most once every 2 seconds and shares the result between the probes in that time (a flood of
probes costs one database query per 2 s); a hanging database fails the check after 3 seconds. The
log says when the app turns unready (Warn, with the cause) and ready again (Info). The image's
`HEALTHCHECK` runs `studio-schedule healthcheck` against `/healthz`. `GET /api/v1/health` stays
for the web app; it names no version (`studio-schedule version` does, and the office and admins
see it in the web app's footer).

### Metrics

With `METRICS_ADDR` set (e.g. `127.0.0.1:9464`, or `:9464` inside a container on a private
network), a second server answers `GET /metrics` in the Prometheus format, and nothing else. It is
never on the public port. Expose it to your monitoring only: the request counts by route reveal
the activity of the school.

| Metric                                               | Type      | Labels                      | Meaning                                                                          |
| ---------------------------------------------------- | --------- | --------------------------- | -------------------------------------------------------------------------------- |
| `studio_schedule_http_requests_total`                | counter   | `method`, `route`, `status` | answered requests; `route` is the route pattern, `other` for the web app's files |
| `studio_schedule_http_request_duration_seconds`      | histogram | `method`, `route`           | time to answer                                                                   |
| `studio_schedule_job_runs_total`                     | counter   | `job`, `result`             | background job runs, `success` or `failure`                                      |
| `studio_schedule_job_duration_seconds`               | histogram | `job`                       | duration of the job runs                                                         |
| `studio_schedule_job_last_success_timestamp_seconds` | gauge     | `job`                       | Unix time of a job's last successful run                                         |
| `studio_schedule_outbox_mails`                       | gauge     | `status`                    | unsent mails: `pending` (another attempt is due) or `failed` (given up)          |
| `studio_schedule_build_info`                         | gauge     | `version`                   | always 1                                                                         |

Go's runtime and process metrics (`go_*`, `process_*`) come along. The jobs are `generate-lessons`,
`purge-sessions` (hourly), `waitlist-expiry`, `announcements`, `deliver-mail` (every minute),
`purge-mail` and `purge-personal-data` (daily). Worth an alert: `outbox_mails{status="failed"} > 0`
(mails were given up: check the SMTP settings), a growing `outbox_mails{status="pending"}`,
`job_runs_total{result="failure"}`, and `time() - job_last_success_timestamp_seconds` above twice
a job's interval.

A Prometheus scrape configuration:

```yaml
scrape_configs:
  - job_name: studio-schedule
    static_configs:
      - targets: ["studio-schedule.internal:9464"]
```

### Logs

`serve` logs to stderr: in the image as JSON (`LOG_FORMAT=json`), one object per line with `time`,
`level`, `msg` and the fields of the message; elsewhere as text by default. At `info` there is a
line per API request (`msg="http request"` with `method`, `path`, `status`, `duration` and
`requestId`), server errors, the jobs' work (e.g. `mails sent`) and the startup with the redacted
configuration. `debug` adds the requests for the web app's files and the health probes. Paths
appear without their query string, and secret path segments (calendar feed and invitation tokens)
are replaced by `<redacted>`; passwords, session tokens and the secrets of `DATABASE_URL` and
`SMTP_PASSWORD` are never logged. Collect the log with the container runtime (`docker logs`) or a
log shipper; the app writes no files.

## Container image

The image `ghcr.io/cwbudde/studio-schedule` holds the single binary with the web UI embedded, on
Alpine Linux, for `linux/amd64` and `linux/arm64`. The workflow `.github/workflows/image.yaml`
publishes it:

| Tag      | From                                                   |
| -------- | ------------------------------------------------------ |
| `edge`   | a push to `main`, once every job of `test.yaml` passed |
| `vX.Y.Z` | a published release (also `X.Y` and `latest`)          |

`test.yaml` calls `image.yaml` for a push to `main` after its tests succeeded, so a failing commit
never becomes `edge`. A push that changes only `docs/` and Markdown files since the last successful
run on `main` builds no image.

What the image sets up:

- It runs as the non-root user `app` (UID and GID 10001) and listens on port 8080.
- The entrypoint is the `studio-schedule` binary and the default command is `serve`, so
  `docker run … ghcr.io/cwbudde/studio-schedule:edge migrate up` runs another subcommand instead.
- `LOG_FORMAT=json` is the default (override with `LOG_FORMAT=text`); `LOG_LEVEL` defaults to
  `info`.
- `XDG_CACHE_HOME=/var/cache/studio-schedule` holds the compiled receipt renderer (wazero's
  compilation cache, about 60 MB), so a restart doesn't compile it again. The image declares no
  volume, which would leave an anonymous volume behind for every container. Mount a named volume
  there to keep the cache when the container is replaced, e.g.
  `docker run -v studio-schedule-cache:/var/cache/studio-schedule …` (Compose does this). It is a
  cache: deleting it is safe.
- The health check runs `studio-schedule healthcheck`, which requests `/healthz` at the port of
  `HTTP_ADDR` and fails unless the answer is 200 within three seconds. Set the listen address with
  `HTTP_ADDR`, not with `serve --addr`, so that the health check finds the server.
- `ca-certificates` (SMTP over TLS, PostgreSQL with `sslmode=verify-full`) and `tzdata` are
  installed.

Trivy scans every pushed image, the `linux/amd64` and `linux/arm64` variants separately (code
scanning categories `trivy-image-amd64` and `trivy-image-arm64`). Its findings show up as code
scanning alerts on GitHub and never fail the build.

### Building the image

The receipt renderer `internal/pdf/typstwasm/receipt.wasm` is a Git LFS file. Pull it before you
build, otherwise the build stops with a message that says so:

```sh
git lfs pull
docker build --build-arg VERSION="$(git describe --tags --always --dirty)" -t studio-schedule:local .
```

For both architectures: `docker buildx build --platform linux/amd64,linux/arm64 .`. The web and Go
stages run on the build machine and cross-compile, so only the small runtime stage is emulated.

### Docker Compose

`docker-compose.yml` runs the app with PostgreSQL 18 (data in the volume `postgres`) and
[Mailpit](https://mailpit.axllent.org), which catches every mail the app sends and shows it at
http://localhost:8025. It suits a local trial and is a starting point for a single-server
deployment.

```sh
cp .env.example .env                    # set POSTGRES_PASSWORD (and BASE_URL)
docker compose up -d --build            # the app restarts until the schema is migrated
docker compose run --rm app migrate up
docker compose run --rm app init        # set up the school and the first admin
# or: docker compose run --rm app seed  # the fictional demo data instead
```

Migrations never run on start. `serve` refuses to start while the schema doesn't match the binary,
and the container restarts until `migrate up` has run. Run `migrate up` after every image update
as well.

Compose builds `DATABASE_URL` from `POSTGRES_USER`, `POSTGRES_PASSWORD` and `POSTGRES_DB`.
`APP_PORT` and `MAILPIT_PORT` change the host ports; `BASE_URL` defaults to
`http://localhost:<APP_PORT>`. The compiled receipt renderer lives in the volume `cache`.

While `SMTP_HOST` is empty, the app sends every mail to Mailpit (port 1025, no TLS, from
`schedule@example.test` unless `SMTP_FROM` is set). For a real mail server set `SMTP_HOST` and
`SMTP_FROM` in `.env`. The `SMTP_*` values then apply as given: an empty `SMTP_TLS` or `SMTP_PORT`
means the app's default, STARTTLS on port 587.

Web push stays off until `.env` holds `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY` and
`VAPID_SUBJECT` (`docker compose run --rm app vapid-keys` prints a pair).

Compose builds the image from the checkout. To run a published image instead, set
`STUDIO_SCHEDULE_IMAGE=ghcr.io/cwbudde/studio-schedule:edge` (or a release tag) in `.env`, then:

```sh
docker compose pull app
docker compose up -d --no-build
```

For a production server, put a reverse proxy with TLS in front (above), bind the app's port to
the local host only (`127.0.0.1:${APP_PORT}:8080` in `docker-compose.yml`), set `BASE_URL` to the
public `https://` origin and `TRUSTED_PROXIES` to the proxy, configure a real mail server, and
remove the `mailpit` service or keep its port private.

## Backups and restore

Back up the **PostgreSQL database**, and nothing else: it holds every record, the settings, the
logo, the stored receipt PDFs and the mail outbox. The app keeps no other state. The wazero cache
(`XDG_CACHE_HOME`, the volume `cache`) is rebuilt on start and needs no backup; the configuration
(`.env`, the secrets) belongs in your own secret store.

A backup holds the personal data of members and their children. Encrypt it, keep it off the
server, restrict who can read it, and delete old ones by your retention policy (below).

### Taking a backup

`pg_dump` in its custom format (`--format=custom`) writes a compressed, consistent snapshot while
the app keeps running. Use a `pg_dump` of the server's major version or newer. With Compose, run
the one in the PostgreSQL container:

```sh
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' \
  > "studio-$(date +%F).dump"
```

With a managed or separate PostgreSQL server, from any host with the client tools:

```sh
pg_dump --format=custom --file="studio-$(date +%F).dump" "$DATABASE_URL"
```

Run it daily (cron, a systemd timer, or the scheduler of the host) and copy the file to storage
outside the server. Managed PostgreSQL services offer automatic backups and point-in-time recovery
as well; use them, and still take a `pg_dump` now and then: it restores into any PostgreSQL server,
also of a later major version or another provider.

### Restoring

Restore into an **empty** database, with the app stopped, then let the binary check the schema:

```sh
docker compose stop app
docker compose exec -T postgres sh -c 'dropdb -U "$POSTGRES_USER" --force "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --exit-on-error --single-transaction' \
  < studio-2026-10-25.dump
docker compose run --rm app migrate status   # every migration applied, none pending
docker compose up -d
```

Elsewhere: `createdb` a new database, `pg_restore --exit-on-error --single-transaction
--dbname="$DATABASE_URL" studio.dump`, and point `DATABASE_URL` at it. A dump restored as another
database user than the one that wrote it needs `--no-owner` (the objects then belong to the
restoring user, which must be the app's user). If the dump comes from an older version of the app,
run `migrate up` with the current binary after the restore; `serve` refuses to start until the
schema matches. A dump from a newer version needs that newer binary.

### Restore drill

A backup counts only once it has been restored. `just restore-drill` (`scripts/restore-drill.sh`)
rehearses it with Docker, and CI runs it on every push (`test-restore.yaml`):

1. migrate and seed a fresh database;
2. back it up with `pg_dump --format=custom`;
3. restore it into a second, empty database with `pg_restore --exit-on-error --single-transaction`;
4. compare the copy with the original: the row counts of every table and the triggers (the
   receipts' immutability is a trigger);
5. run `migrate status` on the copy: every migration applied, none pending;
6. start `serve` on the copy, wait for `/readyz` to answer 200, and sign in with a seeded account
   through the API.

Do the same with your real backups now and then (quarterly, and after major upgrades): restore the
latest dump into a scratch database on another machine, run `migrate status`, start the app on it
(`serve` without SMTP, so no mail goes out, and on a port nobody else reaches), and check that you
can sign in and see recent bookings. Delete the scratch database afterwards.

### Retention of backups

Backups keep data that the app has deleted or anonymized meanwhile (anonymized persons, deleted
accounts, purged guest data and mail bodies, see [docs/privacy.md](privacy.md)). Keep them only as
long as you need them to recover, e.g. daily backups for 14 days and monthly ones for 6 months, and
write the period into your privacy policy. A restore brings erased data back: anonymize those
persons again after restoring an older backup.

## Upgrades

To update the app:

1. Read the release notes in `CHANGELOG.md` and on the GitHub release (release-please writes them
   from the commits); breaking changes are listed first.
2. Take a backup (above).
3. Pull the new image (or build the binary) and apply its migrations:

   ```sh
   docker compose pull app                  # with STUDIO_SCHEDULE_IMAGE set to the new tag
   docker compose run --rm app migrate up
   docker compose up -d                     # restarts the app on the new image
   ```

4. Check `/readyz` and the log.

`serve` refuses to start while the database lacks migrations of its binary ("the database schema
is not up to date: run `studio-schedule migrate up`") or has migrations the binary doesn't know
("the database schema is newer than this binary"), and `/readyz` answers 503 while either is true.
So the order is: migrate, then start the new binary. The old binary stops serving at the next
restart once the schema is newer, which is why the update replaces it right after `migrate up`.

**Signing in again after the session-cookie upgrade.** From the release that adds the
`__Host-` prefix, an installation whose `BASE_URL` is `https://` names its session cookie
`__Host-studio_session` instead of `studio_session` ([ADR 0004](decisions/0004-accounts-sessions-and-mail.md),
amendment). The old cookie is ignored, so everyone signs in again once after that upgrade, and the
browser is told to delete it on its next request; nothing else changes. The same release ends sessions 30 days after the sign-in (was 90).

**Migration policy.** In production, migrations only go forward. Each release's migrations are
written so that `migrate up` works on the data of the release before. `migrate down` rolls back
the latest migration and exists for development: it can drop columns and tables with their data.
To go back to an older version, restore the backup taken before the update, together with the old
image. Don't skip `migrate up` across several releases: it applies all pending migrations in order
in one go.

## Upgrading PostgreSQL to a new major version

Renovate updates the PostgreSQL image within its major version only. A new major version can't
read the old one's data files, so the data moves with a dump and a restore. The PostgreSQL images
from version 18 on keep each major version's data in its own directory of the volume
(`/var/lib/postgresql/<major>/docker`): the new version starts with an empty cluster, and the old
data stays untouched until you remove it. With Compose, from 18 to 19 for example:

```sh
docker compose stop app
# A dump of the app's database (it holds personal data: keep it safe, delete it afterwards).
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' > studio.dump
# In docker-compose.yml: image: postgres:19-alpine
docker compose up -d postgres
docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --exit-on-error' < studio.dump
docker compose run --rm app migrate status   # every migration applied, none pending
docker compose up -d
```

If something fails, set the old image tag again: the old version still finds its data. Once the
app runs on the new version, remove the old data with
`docker compose exec postgres rm -rf /var/lib/postgresql/18` and delete `studio.dump`.

## Releases

[release-please](https://github.com/googleapis/release-please) maintains a release PR from the
Conventional Commits on `main`. Merging that PR tags `vX.Y.Z` and publishes the GitHub release.
The release then triggers `image.yaml` (image tags `vX.Y.Z`, `X.Y` and `latest`) and `pages.yaml`
(the demo).

GitHub doesn't start workflows for events caused by the default `GITHUB_TOKEN`. With that token, the
release PR gets no CI run and the published release builds no image. release-please therefore uses
the repository secret `RELEASE_PLEASE_TOKEN` when it exists, a fine-grained personal access token:

1. GitHub → Settings → Developer settings → Personal access tokens → Fine-grained tokens →
   **Generate new token**.
2. **Resource owner:** the owner of the repository. **Repository access:** _Only select
   repositories_ → `studio-schedule`.
3. **Repository permissions:** _Contents_ read and write, and _Pull requests_ read and write
   (_Metadata_ read is added automatically). _Workflows_ isn't needed: release PRs only change
   `CHANGELOG.md`, `version.txt` and `.release-please-manifest.json`.
4. Choose an expiry (at most one year) and note when to renew it. Once it expires, release-please
   falls back to `GITHUB_TOKEN` and releases build no image again.
5. Repository → Settings → Secrets and variables → Actions → **New repository secret**, named
   `RELEASE_PLEASE_TOKEN`, with the token as its value.

The first push creates the package on ghcr.io. Its visibility starts as private: to let others
pull it without signing in, set it to public under the package's settings.

## Privacy

The school operating the app is the controller of the personal data in it. What the app stores,
how long it keeps it, and how the office exports and erases a person's data is in
[docs/privacy.md](privacy.md), with the list of what an operator still has to do (privacy policy,
processor agreements with the hosting and mail providers, backup retention). The security
checklist is [docs/security.md](security.md).
