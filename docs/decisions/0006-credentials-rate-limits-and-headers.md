# 0006 — Password reset, email change, rate limits and security headers

- Status: accepted
- Date: 2026-10-23

## Context

ADR 0004 introduced accounts, sessions and single-use email tokens for the confirmation link.
Members also need to recover a forgotten password, change their password and move their account to
another address. These flows send mails and accept guesses, so they need limits against password
guessing and mail flooding. The app also serves its own UI, so it should set the usual browser
security headers. The previous system had none of this; password resets were done by hand by the
office.

## Decision

**Tokens.** All three email links use `account_tokens` (only the SHA-256 hash of a 256-bit random
token is stored; using a token deletes it; a new token deletes the older ones of the same purpose):

| Purpose          | Sent to         | Valid | Also valid only while                     |
| ---------------- | --------------- | ----- | ----------------------------------------- |
| `confirm-email`  | the address     | 24 h  | the account still has that address        |
| `reset-password` | the address     | 1 h   | the account still has that address        |
| `change-email`   | the new address | 24 h  | the account may sign in; the address free |

**Password reset.** `POST /auth/password-reset` always answers 202, whether or not the address
belongs to an account, so it reveals nothing. Only a confirmed, enabled account gets a mail.
Setting the new password with the link (`POST /auth/password-reset/confirm`) ends _every_ session
of the account, including impersonations started by it, and does not sign in. Any new password
also invalidates every open link of the account (reset, email change): whoever requested one may
have known the old password.

**Change password.** `POST /auth/password` needs the current password and ends every other session
of the account (and impersonations it started); the current session stays.

**Change email.** `POST /auth/email` needs the password and mails a link to the new address. The
account keeps its old address until the link is used (`POST /auth/email/confirm`, public, so it can
be opened in another browser). Then the address is checked to be still free, switched, and a notice
goes to the _old_ address, so a hijacked session cannot move an account away unnoticed. A request
for an address that already has an account answers like any other and sends nothing.

**Impersonation.** While an admin is signed in as someone else (ADR 0005), changing that account's
password or email is refused (403): an admin who acts as someone never changes their credentials.

**Rate limits.** In memory, per process, in fixed windows on the injected clock
(`internal/ratelimit`):

| What                                             | Key       | Limit         |
| ------------------------------------------------ | --------- | ------------- |
| failed logins                                    | the email | 5 per 15 min  |
| login attempts                                   | client IP | 20 per 15 min |
| mails on request (register, reset, email change) | client IP | 5 per hour    |

A blocked request answers 429 problem+json with `Retry-After`. A successful login clears the email's
failures. An email change counts against the mail limit only when it passes the password check. The
per-email limit lets anyone block an account's login for 15 minutes by sending wrong passwords; that
is accepted in exchange for a hard bound on guesses per account. The client IP is the connection's
address; behind a reverse proxy, the proxies are listed in `TRUSTED_PROXIES` and `X-Forwarded-For`
is read from the right, skipping them (`internal/clientip`). A request with `X-Forwarded-For` and no
`TRUSTED_PROXIES` is logged once as a warning, since all clients behind such a proxy share one
limit. Limits are not shared between processes: the app runs as a single binary (Phase 8). Several
instances would need a shared store for them.

**Security headers.** Every response carries a Content Security Policy that allows only the app's
own scripts, without `unsafe-inline` or `unsafe-eval` (the theme script moved from inline to
`theme.js` for this), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and
`Referrer-Policy: same-origin` (reset and confirmation tokens are in URLs). Styles allow
`unsafe-inline`: sonner and Radix's scroll lock inject `<style>` elements whose content varies, and
neither takes a nonce. `Strict-Transport-Security` is sent when `BASE_URL` is https. The GitHub
Pages demo cannot set headers; it has no server and no real data.

**Errors.** Every error, including unknown routes, wrong methods, panics and rejected cross-origin
requests, is problem+json. A 500 says "internal server error" and nothing else; the cause is only
logged. `internal/app/security_test.go` pins this for every error status.

## Consequences

- Login, registration and the mail-sending endpoints can answer 429; the web app shows when to try
  again.
- Changing the email requires a working new address; an admin will be able to change it directly
  in the admin area later.
- A password reset logs out every device, on purpose.
