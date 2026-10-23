# 0004 — Accounts, sessions and mail

- Status: accepted
- Date: 2026-10-23

## Context

Phase 3 lets people register and sign in, and the app starts sending email. The previous system
used guessable invite codes, sent mail outside any transaction, and its email stubs reported
success without sending anything. The demo runs in the browser and cannot send mail, but its
registration flow must still work end to end.

## Decision

**Passwords.** `internal/auth/password` hashes with argon2id and stores PHC strings
(`$argon2id$v=19$m=…,t=…,p=…$salt$key`):

- The parameters depend on the build target. The server uses the OWASP recommendation (19 MiB,
  t=2, p=1). The WASM demo uses cheap parameters (1 MiB, t=1), because its runtime is
  single-threaded.
- `Verify` reads the parameters from the stored hash. On login, a hash with outdated parameters is
  replaced (`NeedsRehash`). This upgrades the demo's pre-hashed seed passwords on a server.
- Passwords have 8 to 256 characters; the upper bound limits the hashing work per request. At
  most `GOMAXPROCS` hashes run at once in the process, so a burst of requests cannot exhaust memory.
  A login with an unknown email verifies against a dummy hash, so its timing matches a wrong
  password.

**Registration and confirmation.** Registration creates a person and an unconfirmed member
account. It writes a confirmation link, valid for 24 hours, to the outbox in the same transaction.

- The answer is the same whether the address is new or taken. An existing account gets no mail
  and is left unchanged. Accounts created by the school (seed, import, later the admin area) are
  never changed by a registration. The migration marks all accounts from before it as confirmed.
- Registering an address again whose account is itself an unconfirmed registration (it has a
  password) replaces the name and password and invalidates the older links: the latest
  registration wins.
- Confirming requires the link **and the password** of that registration, and then signs in.
  Without the password check, someone who registered a victim's address first could take the
  account over once the victim clicked the link (pre-registration hijacking). If the password
  changes between reading the token and using it, the confirmation fails.
- Unconfirmed accounts cannot sign in. A login with the right password returns 403 ("email not
  confirmed"); every other failure returns one generic 401.

**Secret tokens** (session cookies, email links) are 32 random bytes, base64url-encoded. The
database stores only their SHA-256 hash (`domain.HashToken`), so a database leak reveals no usable
token. Email tokens are single use: using one deletes it, and of two concurrent uses only one
succeeds.

**Sessions** are server-side rows. The cookie `studio_session` is HttpOnly, SameSite=Lax, Path=/,
and Secure unless `BASE_URL` is unset or `http://` (local development). Cross-origin unsafe
requests are already rejected by `http.CrossOriginProtection`.

- Expiry slides: 14 days idle, at most 90 days after the login.
- A session is extended (and the cookie re-set) at most once a day, so most requests only read.
- Expired sessions and tokens are deleted by the hourly `purge-sessions` job.

**Mail outbox.** Services render a mail (`internal/mail`: `html/template` plus a text part, `en`
and `de`) and write it to `outbox_mails` in their own transaction. A rolled-back transaction
therefore sends nothing. The `deliver-mail` job runs every minute, but only when a mailer is
configured, and not at startup, so a slow mail server cannot delay startup. It sends up to 20
mails per run, one at a time:

1. Claim the next due mail (`FOR UPDATE SKIP LOCKED`). The claim increments the attempt count and
   moves the next attempt into the future. That delay doubles as a lease and is longer than a send
   may take, so no other worker picks the mail meanwhile.
2. Send it outside any transaction.
3. Record the outcome; a successful send is recorded even when the job is shutting down.

Failed attempts are retried after 5 min, 15 min, 1 h and 3 h; delivery is given up after 5
attempts, or at once on a permanent error (`mail.ErrPermanent`, e.g. an SMTP 5xx reply). A crash
between sending and recording can send a mail twice; that is accepted (at-least-once delivery).

Until a mail is sent, its body (including the link) is stored in plain text. Links expire after 24
hours and are single use; that is accepted.

The SMTP sender (`internal/mail/smtp`) is configured through `SMTP_HOST`, `SMTP_PORT`,
`SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` and `SMTP_TLS` (`starttls` by default, `tls`, or
`none` for local mail catchers). It is server-only, because `cmd/wasm` must not import `net/smtp`.
Without `SMTP_HOST` mails stay in the outbox, and the server warns at startup.

**Demo installations.** `app.Deps.Demo` marks an installation that serves the fictional demo data:
the WASM demo, and `serve` without a database. Such an installation answers two endpoints:

- `GET /api/v1/demo/mailbox` lists the outbox, so the confirmation link can be followed without
  mail.
- `GET /api/v1/demo/logins` lists one seeded account per role with the published demo password.

Elsewhere both endpoints are a 404. This depends on the data set, not on the transport. A real
installation never exposes its outbox, even without SMTP. `studio-schedule seed` loads the same
demo accounts into PostgreSQL and warns that they share the published password: never seed a
public installation.

## Consequences

- Every new mail type is a template in every locale (a parity test enforces this) plus an
  `enqueueMail` call inside the transaction that causes it.
- Password reset, email change and "log out everywhere" reuse the token and session tables (new
  `TokenPurpose` values, a delete by account).
- Rate limiting of login and registration comes with the security-headers item; until then,
  argon2id is the only brake on password guessing.
