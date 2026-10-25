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

**Sessions** are server-side rows. The cookie `studio_session` (`__Host-studio_session` over HTTPS,
see the amendment) is HttpOnly, SameSite=Lax, Path=/,
and Secure unless `BASE_URL` is unset or `http://` (local development). Cross-origin unsafe
requests are already rejected by `http.CrossOriginProtection`.

- Expiry slides: 14 days idle, at most 30 days after the login (90 before the amendment).
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

## Amendment (2026-10-25): ASVS Level 1

The ASVS 4.0.3 Level 1 review ([security.md](../security.md)) changes the following.

**Session lifetime (ASVS 3.3.2).** A session ends at most **30 days** after the login
(`SessionMaxAge`, was 90); the 14 idle days stay. Members sign in again at least once a month.

**`__Host-` cookie (ASVS 3.4.4).** When `BASE_URL` is `https://`, the session cookie is named
`__Host-studio_session`: browsers accept that name only with Secure, Path=/ and no Domain, so
neither a subdomain nor a page over plain HTTP can set or shadow it. Over plain HTTP (local
development), in the web demo and with `serve` without `BASE_URL` it stays `studio_session`,
because the prefix needs Secure. The session middleware reads and writes the cookie by the
configured name (`httpapi.Deps.HostCookie`) and hands its token to sign-in and sign-out through
the request context; no operation names the cookie in its signature any more, so the OpenAPI
document pins no cookie name. A cookie of the other name is ignored: the upgrade that turns the
prefix on signs everyone out once ([operations.md](../operations.md#upgrades)).

**Password rules (ASVS 2.1.1, 2.1.2, 2.1.7).** The minimum stays at **8 characters**, as above
(NIST SP 800-63B; ASVS asks for 12, an accepted deviation: the common-password check below keeps
out the weak passwords of 8 to 11 characters that matter, and a longer minimum mostly produces
predictable padding). New: a new password (registration, reset, change, and so the invitation and
setup links) must not be one of the **10,000 most common passwords**, compared ignoring case.
The list is SecLists' `10k-most-common.txt` (MIT License), embedded in `internal/auth/password`
(`IsCommon`; source, checksum and license in `common-passwords-LICENSE.txt`), so the check needs
no network and works in the web demo; the set is built on first use, so sign-ins never pay for
it. The length is counted in characters, by the API (`maxLength`) and the service alike, at most 256. A broken rule is a 422 on the field that carried the password (`body.password`,
`body.newPassword`). The web app shows a strength meter (length and kinds of characters) under
new passwords and a show/hide button on every password field.

**Password-changed mail (ASVS 2.2.3, 2.5.5).** Changing or resetting the password of an account
that had one mails the account, in its language, that its password was changed, in the same
transaction (`password-changed`, no link; whoever did not change it contacts the school).
Setting the first password with an invitation or the setup link sends nothing.

**Multi-factor authentication (ASVS 4.3.1)** for the office and admins is left for a feature of
its own (TOTP or passkeys, with its own ADR).
