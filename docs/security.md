# Security

How Studio Schedule meets [OWASP ASVS 4.0.3](https://owasp.org/www-project-application-security-verification-standard/)
Level 1, checked against the code on 2026-10-25. Each requirement that applies to the app is
listed with its status and where it is handled; the open items at the end say what is missing and
propose how to close it.

- ✅ met
- ⚠️ partly met or not met: see [Open items](#open-items)
- n/a does not apply to this app (the reason is given)

Paths are relative to the repository root. The operator's part (TLS, backups, updates, access to
the server) is in [operations.md](operations.md); personal data and its retention in
[privacy.md](privacy.md).

## V1 Architecture, design and threat modeling

ASVS 4.0.3 has no Level 1 requirements in V1. The design decisions with their security reasoning
are the ADRs in [docs/decisions](decisions), the layering and the trust boundaries are in
[architecture.md](architecture.md).

## V2 Authentication

Accounts, sessions and mail links: [ADR 0004](decisions/0004-accounts-sessions-and-mail.md);
password reset, email change and rate limits: [ADR 0006](decisions/0006-credentials-rate-limits-and-headers.md).

| ID     | Requirement                                                 | Status | Where                                                                                                                                                                                                                                                                                 |
| ------ | ----------------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2.1.1  | Passwords of at least 12 characters                         | ⚠️     | The minimum is 8 (`MinPasswordLength`, `internal/service/auth.go`; `minLength` in `internal/httpapi/auth.go`, `credentials.go`; `web/src/features/auth/form.ts`). See [O1](#o1-password-length).                                                                                      |
| 2.1.2  | Passwords of 64 characters and more are allowed             | ✅     | Up to 256 bytes (`MaxPasswordLength`); the bound limits the hashing work per request.                                                                                                                                                                                                 |
| 2.1.3  | No truncation of passwords                                  | ✅     | The password is hashed as entered: no trimming, no truncation (`internal/auth/password`).                                                                                                                                                                                             |
| 2.1.4  | Any printable Unicode character allowed                     | ✅     | No character rules; `internal/auth/password/password_test.go` hashes Unicode and emoji.                                                                                                                                                                                               |
| 2.1.5  | Users can change their password                             | ✅     | `changePassword` (`internal/httpapi/credentials.go`, `AuthService.ChangePassword` in `internal/service/credentials.go`), on the account page.                                                                                                                                         |
| 2.1.6  | Changing the password requires the current and the new one  | ✅     | `checkCurrentPassword`; wrong ones count against the per-email limit.                                                                                                                                                                                                                 |
| 2.1.7  | Passwords are checked against breached or common passwords  | ⚠️     | No check. See [O2](#o2-common-passwords).                                                                                                                                                                                                                                             |
| 2.1.8  | A password strength meter                                   | ⚠️     | None. See [O2](#o2-common-passwords).                                                                                                                                                                                                                                                 |
| 2.1.9  | No composition rules                                        | ✅     | Only the length is checked (`checkPassword`).                                                                                                                                                                                                                                         |
| 2.1.10 | No periodic rotation or password history                    | ✅     | None.                                                                                                                                                                                                                                                                                 |
| 2.1.11 | Pasting, browser helpers and password managers work         | ✅     | Plain inputs without paste blocking; `autoComplete="current-password"` / `"new-password"` on every password field.                                                                                                                                                                    |
| 2.1.12 | The password can be shown while typing                      | ⚠️     | No show/hide toggle. See [O3](#o3-show-password).                                                                                                                                                                                                                                     |
| 2.2.1  | Anti-automation against credential stuffing and brute force | ✅     | `DefaultRateLimits` (`internal/httpapi/ratelimit.go`): 5 wrong passwords per email per 15 min, 20 logins per IP per 15 min, 5 mails on request per IP per hour; 429 with `Retry-After`. Unknown emails verify a dummy hash.                                                           |
| 2.2.2  | Weak authenticators (email) only for recovery               | ✅     | Email links only confirm an address, reset a password or change the email; sign-in always needs the password.                                                                                                                                                                         |
| 2.2.3  | Notification after changes of authentication details        | ⚠️     | An email change notifies the old address (`notifyEmailChanged`), a deleted account gets a confirmation; a password change or reset sends nothing. See [O4](#o4-password-changed-mail).                                                                                                |
| 2.3.1  | Initial activation secrets are random and expire            | ✅     | Invitations and the `init` link: 32 random bytes, 72 h, single use, only the SHA-256 stored (`internal/service/invite.go`, `setup.go`).                                                                                                                                               |
| 2.5.1  | No initial or recovery secret sent in clear text            | ✅     | The app never sends a password; links expire (1 h for a reset) and work once.                                                                                                                                                                                                         |
| 2.5.2  | No password hints or secret questions                       | ✅     | None.                                                                                                                                                                                                                                                                                 |
| 2.5.3  | Recovery does not reveal the current password               | ✅     | Reset sets a new password; the hash is never shown.                                                                                                                                                                                                                                   |
| 2.5.4  | No shared or default accounts                               | ✅     | A real installation starts empty (`init` creates the first admin). The demo data (`seed`) has accounts with a published password; `seed` warns, and [operations.md](operations.md) forbids it for real use. The demo endpoints are 404 outside the demo (`internal/httpapi/demo.go`). |
| 2.5.5  | Notification when an authentication factor changes          | ⚠️     | As 2.2.3.                                                                                                                                                                                                                                                                             |
| 2.5.6  | Forgotten password through a secure recovery mechanism      | ✅     | A single-use, 1-hour link to the account's address; setting the password ends every session and does not sign in (`internal/service/credentials.go`). The request answers 202 for any address.                                                                                        |
| 2.7.x  | Out-of-band verifiers                                       | n/a    | No SMS or push authenticators.                                                                                                                                                                                                                                                        |
| 2.8.1  | Time-based one-time passwords                               | n/a    | No OTP authenticators (see [O6](#o6-multi-factor-authentication-for-staff)).                                                                                                                                                                                                          |

Passwords are hashed with argon2id at the OWASP parameters (19 MiB, t=2, p=1;
`internal/auth/password/params.go`), with a per-hash salt; outdated hashes are upgraded at the next
sign-in, and at most `GOMAXPROCS` hashes run at once.

## V3 Session management

| ID    | Requirement                                            | Status | Where                                                                                                                                                                                                                |
| ----- | ------------------------------------------------------ | ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 3.1.1 | No session token in URLs                               | ✅     | Only in the `studio_session` cookie; `/api/v1/auth/me` never returns it.                                                                                                                                             |
| 3.2.1 | A new session token at authentication                  | ✅     | Sign-in, confirmation and impersonation create a new session and end the previous one (`internal/service/auth.go` `signIn`, `internal/httpapi/auth.go`).                                                             |
| 3.2.2 | Session tokens with at least 64 bits of entropy        | ✅     | 32 bytes from `crypto/rand` (`newToken`); the database stores only their SHA-256 (`domain.HashToken`).                                                                                                               |
| 3.2.3 | Session tokens stored securely in the browser          | ✅     | An HttpOnly cookie; nothing in local or session storage.                                                                                                                                                             |
| 3.3.1 | Logout and expiry invalidate the session on the server | ✅     | `Logout` deletes the session row; expired sessions fail `Authenticate` and are deleted hourly (`purge-sessions`).                                                                                                    |
| 3.3.2 | Re-authentication at least every 30 days               | ⚠️     | Sessions end after 14 idle days and at most 90 days after sign-in (`SessionIdleTimeout`, `SessionMaxAge`). See [O5](#o5-session-lifetime).                                                                           |
| 3.4.1 | Cookies with the `Secure` attribute                    | ✅     | When `BASE_URL` is `https://` (`setTransportSecurity`, `cmd/studio-schedule/serve.go`); plain HTTP is for local development only, and `serve` warns.                                                                 |
| 3.4.2 | Cookies with the `HttpOnly` attribute                  | ✅     | `sessionCookie` (`internal/httpapi/auth.go`).                                                                                                                                                                        |
| 3.4.3 | Cookies with `SameSite`                                | ✅     | `SameSite=Lax`.                                                                                                                                                                                                      |
| 3.4.4 | Cookies with the `__Host-` prefix                      | ⚠️     | The cookie is `studio_session`. See [O7](#o7-__host--cookie-prefix).                                                                                                                                                 |
| 3.4.5 | Cookie path when the domain is shared                  | ✅     | `Path=/`, no `Domain`: the app runs on a host of its own (`BASE_URL` has no path).                                                                                                                                   |
| 3.7.1 | Re-authentication before sensitive account changes     | ✅     | Changing the password or email and deleting one's own account require the password; credential changes are refused while an admin impersonates the account (`internal/service/credentials.go`, `accounts_admin.go`). |

## V4 Access control

Roles and impersonation: [ADR 0005](decisions/0005-rbac-and-impersonation.md); families:
[ADR 0008](decisions/0008-families-and-dependents.md).

| ID    | Requirement                                                 | Status | Where                                                                                                                                                                                                                                                   |
| ----- | ----------------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 4.1.1 | Access control enforced on the server                       | ✅     | Every operation is registered with an access level (`register`, `internal/httpapi/access.go`); the access middleware answers 401/403 before the handler. The web app's checks are only for display.                                                     |
| 4.1.2 | Users cannot change the attributes access control relies on | ✅     | Roles and family links come from the database for every request, never from the request body.                                                                                                                                                           |
| 4.1.3 | Least privilege                                             | ✅     | Roles `member` < `instructor` < `office` < `admin`; staff see only the lessons they teach (`manages`), guardians only their dependents (`dependentOf`).                                                                                                 |
| 4.1.5 | Access controls fail securely                               | ✅     | An unknown access level panics at registration; an operation without one answers 500 (`accessMiddleware`). `TestAccessLevelsPinned` and `TestAccessMatrix` (`internal/app/access_test.go`) pin every operation's level.                                 |
| 4.2.1 | Protection against insecure direct object references        | ✅     | Services check ownership and answer 404 for anything outside the caller's reach: `dependentOf` (`internal/service/family.go`), `manages` (`participants.go`), receipts by payer (`receipts.go`).                                                        |
| 4.2.2 | Protection against cross-site request forgery               | ✅     | `http.CrossOriginProtection` rejects every cross-origin POST, PUT, PATCH and DELETE with 403 (`crossOriginProtection`, `internal/app/app.go`), plus `SameSite=Lax` and JSON bodies. Pinned by the server smoke tests (`web/e2e-server/system.spec.ts`). |
| 4.3.1 | Administrative interfaces use multi-factor authentication   | ⚠️     | Admin and office sign in with a password only. See [O6](#o6-multi-factor-authentication-for-staff).                                                                                                                                                     |
| 4.3.2 | No directory browsing, no metadata files                    | ✅     | The embedded UI serves only the built files (`internal/webui`), no directory listings, no source maps; unknown `/api` paths are 404 problems.                                                                                                           |

## V5 Validation, sanitization and encoding

| ID     | Requirement                                       | Status | Where                                                                                                                                                                                                                                             |
| ------ | ------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 5.1.1  | Defense against HTTP parameter pollution          | ✅     | Huma binds each parameter once to a typed field.                                                                                                                                                                                                  |
| 5.1.2  | Protection against mass assignment                | ✅     | Typed input structs per operation; the schemas forbid unknown properties (`additionalProperties: false` in `api/openapi.json`).                                                                                                                   |
| 5.1.3  | Input validated by allow lists                    | ✅     | Huma validates types, enums, lengths and formats; domain types validate the rest (`Validate()` in `internal/domain`, ADR 0002).                                                                                                                   |
| 5.1.4  | Structured data strongly typed and validated      | ✅     | Typed IDs, `domain.Date`, `domain.TimeOfDay`, `domain.Money`.                                                                                                                                                                                     |
| 5.1.5  | Redirects only to allowed destinations            | ✅     | No redirect or return URL parameter; after sign-in the app always goes to `/`.                                                                                                                                                                    |
| 5.2.1  | Untrusted HTML sanitized                          | ✅     | User content is never rendered as HTML or Markdown: announcements, notices and the legal notice are plain text.                                                                                                                                   |
| 5.2.2  | Unstructured data sanitized                       | ✅     | As 5.2.1; text is length-limited and rendered escaped.                                                                                                                                                                                            |
| 5.2.3  | Protection against mail injection                 | ✅     | `mail.Message.Validate` (`internal/mail/mail.go`) rejects line breaks in subjects and accepts single bare addresses (`net/mail`); the SMTP sender encodes headers (`internal/mail/smtp/sender.go`).                                               |
| 5.2.4  | No `eval` or dynamic code execution               | ✅     | None; the CSP has no `unsafe-eval`.                                                                                                                                                                                                               |
| 5.2.5  | Protection against template injection             | ✅     | Fixed, embedded mail templates (`html/template`, `missingkey=error`, `internal/mail`); the receipt template reads its data as JSON (`tools/receipt-pdf/receipt.typ`).                                                                             |
| 5.2.6  | Protection against SSRF                           | ✅     | The server makes no outbound request from user input; SMTP goes to the configured host.                                                                                                                                                           |
| 5.2.7  | User-supplied SVG is sanitized                    | ✅     | The only image upload is the logo (`updateReceiptLogo`), which receipts print and the header shows as the school's logo: PNG and JPEG only, checked by their magic bytes, at most 512 KiB (`domain.Logo.Validate`, `internal/domain/receipt.go`). |
| 5.3.1  | Output encoding for the context                   | ✅     | React escapes all rendered text; `html/template` escapes the mails' HTML; JSON through `encoding/json`.                                                                                                                                           |
| 5.3.3  | Context-aware escaping against XSS                | ✅     | As 5.3.1. The one `dangerouslySetInnerHTML` (shadcn's `components/ui/chart.tsx`) gets constant CSS only; the CSP allows the app's own scripts only.                                                                                               |
| 5.3.4  | Parameterized queries                             | ✅     | All SQL through sqlc with parameters (`internal/store/postgres/queries`); no SQL is built from strings.                                                                                                                                           |
| 5.3.5  | No unsafe database or ORM features                | ✅     | As 5.3.4.                                                                                                                                                                                                                                         |
| 5.3.6  | Protection against JSON injection                 | ✅     | JSON is only produced by encoders, never by string concatenation.                                                                                                                                                                                 |
| 5.3.7  | LDAP injection                                    | n/a    | No LDAP.                                                                                                                                                                                                                                          |
| 5.3.8  | OS command injection                              | ✅     | No `os/exec`: the receipt renderer runs as WebAssembly in-process (wazero).                                                                                                                                                                       |
| 5.3.9  | Local or remote file inclusion                    | ✅     | No file paths from user input; uploads are stored in the database.                                                                                                                                                                                |
| 5.3.10 | XPath or XML injection                            | n/a    | No XML.                                                                                                                                                                                                                                           |
| 5.5.2  | XML parsers restricted (XXE)                      | n/a    | No XML parsing.                                                                                                                                                                                                                                   |
| 5.5.3  | No deserialization of untrusted data into objects | ✅     | JSON into typed structs only; the member import parses CSV into strings (`internal/csvimport`).                                                                                                                                                   |
| 5.5.4  | `JSON.parse` in the browser                       | ✅     | The web app parses JSON with `JSON.parse` / `fetch().json()`, never `eval`.                                                                                                                                                                       |

## V6 Stored cryptography

| ID    | Requirement                         | Status | Where                                                                                                                               |
| ----- | ----------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| 6.2.1 | Cryptographic modules fail securely | ✅     | Go's standard library and `golang.org/x/crypto/argon2`; a failing `crypto/rand` fails the operation (`newToken` returns the error). |

Secrets at rest are hashes: argon2id for passwords, SHA-256 for 256-bit tokens (sessions, mail
links, invitations, calendar feeds, waiting-list invitations). The database itself is not
encrypted by the app; disk and backup encryption are the operator's.

## V7 Error handling and logging

| ID    | Requirement                                   | Status | Where                                                                                                                                                                                                                                                                              |
| ----- | --------------------------------------------- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 7.1.1 | No credentials or session tokens in logs      | ✅     | The access log records method, path, status and duration, without query string or body; secret path segments (feed and invitation tokens) are `<redacted>` (`internal/app/redact.go`, `TestNoSecretPathInLogs`). The configuration logs with passwords masked (`internal/config`). |
| 7.1.2 | No other sensitive data in logs               | ✅     | No log line carries an email, a name or a password; errors are logged by their cause, not the request's data.                                                                                                                                                                      |
| 7.4.1 | Generic error messages with an ID for support | ✅     | Every error is problem+json with `requestId`; a 500 says "internal server error" only and the cause is logged (`maskServerErrors`, `internal/app/app.go`; `internal/app/security_test.go`).                                                                                        |

## V8 Data protection

| ID    | Requirement                                                    | Status | Where                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ----- | -------------------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 8.2.1 | Anti-caching headers for sensitive data                        | ✅     | Every API answer carries `Cache-Control: no-store` (`securityHeaders`, `internal/app/app.go`; `TestAPIAnswersAreNotCached`), except the files that set their own: receipt PDFs (and the demo mailbox's attachments) `private, no-store`; the logo `public, no-cache` at `getSchoolLogo` (public, no personal data) and `no-cache` at `getReceiptLogo`. Calendar feeds (`/ical/`, outside the API) send `private, max-age=900`. |
| 8.2.2 | No sensitive data in browser storage                           | ✅     | localStorage keeps the UI language and the import's column mapping (header names); IndexedDB only in the demo, with fictional data.                                                                                                                                                                                                                                                                                            |
| 8.2.3 | Client-side data cleared after the session ends                | ✅     | Signing out or deleting the account resets the query cache (`changeIdentity`, `web/src/api/queries.ts`), as does a 401 on a change. A 401 on a plain read does not; see [O9](#o9-minor-items).                                                                                                                                                                                                                                 |
| 8.3.1 | Sensitive data in the body or headers, not in the query string | ⚠️     | No secrets in API query strings (mail-link tokens go in POST bodies). The office's search terms (names, email parts) are query parameters of GET requests, which reverse-proxy logs record. See [O8](#o8-search-terms-in-query-strings).                                                                                                                                                                                       |
| 8.3.2 | Users can export and delete their data                         | ✅     | Export for oneself and one's dependents, self-deletion of the account, anonymization by the office (`internal/httpapi/privacy.go`, [ADR 0016](decisions/0016-privacy-and-account-administration.md), [privacy.md](privacy.md)).                                                                                                                                                                                                |
| 8.3.3 | Users are told how their data is used                          | n/a    | The school's privacy policy, an operator duty ([privacy.md](privacy.md#what-the-operator-still-has-to-do)).                                                                                                                                                                                                                                                                                                                    |
| 8.3.4 | Sensitive data is identified and handled by a policy           | ✅     | [privacy.md](privacy.md): what is stored, why and for how long, with automatic retention jobs.                                                                                                                                                                                                                                                                                                                                 |

## V9 Communication

| ID    | Requirement                    | Status | Where                                                                                                                                                                                                                                                         |
| ----- | ------------------------------ | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 9.1.1 | TLS for all client connections | ✅     | Terminated by the operator's reverse proxy ([operations.md](operations.md#reverse-proxy-and-tls)); with an `https://` `BASE_URL` the app sends HSTS (`max-age=31536000`, with `includeSubDomains` unless `HSTS_INCLUDE_SUBDOMAINS=false`) and Secure cookies. |
| 9.1.2 | Only strong cipher suites      | n/a    | Configured at the reverse proxy (operator).                                                                                                                                                                                                                   |
| 9.1.3 | Only current TLS versions      | n/a    | Configured at the reverse proxy (operator). The app's own outbound TLS (SMTP, PostgreSQL) uses Go's defaults, TLS 1.2 and later.                                                                                                                              |

## V10 Malicious code

| ID     | Requirement                                          | Status | Where                                                                                                                                                                                                                            |
| ------ | ---------------------------------------------------- | ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 10.3.1 | Auto-updates over secure channels with signatures    | n/a    | The app does not update itself; images come from ghcr.io over TLS.                                                                                                                                                               |
| 10.3.2 | Integrity protection, no code from untrusted sources | ✅     | The web app bundles all its code and fonts. The API docs page (`/api/v1/docs`) loads its renderer from unpkg.com with Subresource Integrity, under its own strict CSP. Dependencies are pinned by `go.sum` and `pnpm-lock.yaml`. |
| 10.3.3 | Protection against subdomain takeover                | n/a    | DNS is the operator's.                                                                                                                                                                                                           |

## V11 Business logic

| ID     | Requirement                             | Status | Where                                                                                                                       |
| ------ | --------------------------------------- | ------ | --------------------------------------------------------------------------------------------------------------------------- |
| 11.1.1 | Business flows in sequential order      | ✅     | State transitions are checked in the services under locks (bookings, waiting list, passes, receipts; ADR 0009, 0010, 0012). |
| 11.1.2 | Business limits per user                | ✅     | Capacity under `Lessons().Lock`, one active booking per person and lesson, booking and cancellation cutoffs, pass visits.   |
| 11.1.3 | Business limits and validation          | ✅     | Domain rules return field errors (422) or conflicts (409); receipts are numbered without gaps and immutable (triggers).     |
| 11.1.4 | Anti-automation against excessive calls | ✅     | The rate limits of 2.2.1, plus waiting-list links (20 per IP per 15 min) and calendar feeds (per IP and per token).         |
| 11.1.5 | Business logic threats considered       | ✅     | The ADRs; PLAN.md's "Lessons from the previous attempt" lists the races and abuses this design closes.                      |

## V12 Files and resources

| ID     | Requirement                                                 | Status | Where                                                                                                                                                                                                                         |
| ------ | ----------------------------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 12.1.1 | No large files that fill storage or cause denial of service | ✅     | Huma's 1 MiB body limit; the member import about 1.4 MiB and 5000 rows (`internal/httpapi/imports.go`, `internal/csvimport`); the logo 512 KiB; read timeouts in `serve`.                                                     |
| 12.3.1 | File names from users not used in the file system           | ✅     | Uploads are stored in the database; nothing is written to disk.                                                                                                                                                               |
| 12.3.2 | File metadata validated against path disclosure             | ✅     | As 12.3.1.                                                                                                                                                                                                                    |
| 12.3.3 | No remote file inclusion or SSRF through file names         | ✅     | As 12.3.1.                                                                                                                                                                                                                    |
| 12.3.4 | No reflected file download                                  | ✅     | Download names are built by the server (receipt numbers, slugs); every API answer carries `Content-Disposition: attachment` unless it sets its own (receipt PDFs: an attachment with the receipt number; the logo: `inline`). |
| 12.3.5 | No OS commands with file metadata                           | ✅     | No `os/exec`.                                                                                                                                                                                                                 |
| 12.4.1 | Untrusted files stored outside the web root                 | ✅     | In the database.                                                                                                                                                                                                              |
| 12.4.2 | Untrusted files scanned for malware                         | n/a    | The app never passes uploaded files on: the CSV import is parsed and discarded, the logo is checked as PNG or JPEG and embedded in receipt PDFs.                                                                              |
| 12.5.1 | Only files of expected types served                         | ✅     | The embedded UI serves its build only (`internal/webui`).                                                                                                                                                                     |
| 12.5.2 | Uploaded files never served as HTML or scripts              | ✅     | The logo is served `inline` with its PNG or JPEG type, `nosniff` and the CSP.                                                                                                                                                 |
| 12.6.1 | Allow list for server requests to resources                 | n/a    | The server requests no URLs from user input.                                                                                                                                                                                  |

## V13 API and web services

| ID     | Requirement                                      | Status | Where                                                                                                                                                                                                                                                                                                                         |
| ------ | ------------------------------------------------ | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 13.1.1 | The same encodings and parsers across components | ✅     | One API (Huma, OpenAPI 3.1) for both targets; the web app's types are generated from it.                                                                                                                                                                                                                                      |
| 13.1.3 | API URLs expose no sensitive information         | ✅     | No session tokens or keys in URLs. Calendar feeds (`/ical/{token}.ics`) and waiting-list invitations carry a capability token by design (calendar apps cannot send headers): 256-bit, stored hashed, revocable, rate-limited, redacted in logs, and kept from other sites by `Referrer-Policy: same-origin` (ADR 0010, 0011). |
| 13.2.1 | Only the HTTP methods valid for each route       | ✅     | Other methods answer 405 problem (`internal/app/app.go`); the UI answers only GET and HEAD.                                                                                                                                                                                                                                   |
| 13.2.2 | JSON schema validation before processing         | ✅     | Huma validates every body against its schema.                                                                                                                                                                                                                                                                                 |
| 13.2.3 | CSRF protection for cookie-based APIs            | ✅     | As 4.2.2.                                                                                                                                                                                                                                                                                                                     |
| 13.3.1 | XSD validation                                   | n/a    | No XML or SOAP.                                                                                                                                                                                                                                                                                                               |

## V14 Configuration

| ID     | Requirement                                          | Status | Where                                                                                                                                                                                                                                                                                                                                                                             |
| ------ | ---------------------------------------------------- | ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 14.2.1 | Components up to date, dependency checks             | ✅     | Renovate (`renovate.json`, weekly); `govulncheck` and `pnpm audit --prod --audit-level high` block CI (`just vuln`, `.github/workflows/test-vuln.yaml`, which also runs weekly on its own); Trivy scans every image.                                                                                                                                                              |
| 14.2.2 | Unneeded features, documentation and samples removed | ✅     | The demo endpoints (mailbox, logins) answer only in the demo; no debug or profiling routes; metrics only on a separate, opt-in listener.                                                                                                                                                                                                                                          |
| 14.2.3 | Subresource Integrity for externally hosted assets   | ✅     | As 10.3.2.                                                                                                                                                                                                                                                                                                                                                                        |
| 14.3.2 | Debug modes disabled in production                   | ✅     | There is no debug mode; `LOG_LEVEL=debug` adds log lines only.                                                                                                                                                                                                                                                                                                                    |
| 14.3.3 | No version information of components in responses    | ⚠️     | No `Server` or `X-Powered-By` header. The public `GET /api/v1/health` returns the app's build version, which the web app shows in its footer. See [O9](#o9-minor-items).                                                                                                                                                                                                          |
| 14.4.1 | A Content-Type with a charset on every response      | ✅     | `text/html; charset=utf-8`, `text/calendar; charset=utf-8`; JSON and problem+json are UTF-8 by definition.                                                                                                                                                                                                                                                                        |
| 14.4.2 | API answers with `Content-Disposition: attachment`   | ✅     | `securityHeaders` (`internal/app/app.go`) on every API answer (`/api` and below) except the API's documentation (`/api/v1/docs`, `/api/v1/openapi*`); file answers set their own: receipt PDFs an attachment with a file name, the logo (`getSchoolLogo`, `getReceiptLogo`) `inline`. Calendar feeds (`/ical/`) are not API answers: `inline`, or an attachment with `?download`. |
| 14.4.3 | A Content Security Policy                            | ✅     | `default-src 'self'`, no inline or eval scripts, `object-src 'none'`, `frame-ancestors 'none'`, `base-uri`, `form-action` (`contentSecurityPolicy`; `TestContentSecurityPolicy`). Styles allow `'unsafe-inline'` for two UI libraries (ADR 0006).                                                                                                                                 |
| 14.4.4 | `X-Content-Type-Options: nosniff`                    | ✅     | `securityHeaders`.                                                                                                                                                                                                                                                                                                                                                                |
| 14.4.5 | `Strict-Transport-Security`                          | ✅     | `max-age=31536000; includeSubDomains` with an `https://` `BASE_URL`; without `includeSubDomains` when `HSTS_INCLUDE_SUBDOMAINS=false`, for a domain whose other subdomains still serve plain HTTP ([operations.md](operations.md#reverse-proxy-and-tls)).                                                                                                                         |
| 14.4.6 | A suitable `Referrer-Policy`                         | ✅     | `same-origin`: tokens in URLs never leave the site.                                                                                                                                                                                                                                                                                                                               |
| 14.4.7 | Content not embeddable in other sites                | ✅     | `frame-ancestors 'none'` and `X-Frame-Options: DENY`.                                                                                                                                                                                                                                                                                                                             |
| 14.5.1 | Only the expected HTTP methods                       | ✅     | As 13.2.1.                                                                                                                                                                                                                                                                                                                                                                        |
| 14.5.2 | The Origin header is not used for authentication     | ✅     | `Origin` and `Sec-Fetch-Site` only feed the CSRF check; authentication is the session cookie.                                                                                                                                                                                                                                                                                     |
| 14.5.3 | CORS with a strict allow list                        | ✅     | No `Access-Control-Allow-*` headers at all: the API is same-origin only.                                                                                                                                                                                                                                                                                                          |

## Open items

Gaps against Level 1, each with a proposal. Items O1 and O5 would change an accepted decision
(ADR 0004), the others add features.

### O1 Password length

ASVS 2.1.1 asks for at least 12 characters; the app allows 8 (ADR 0004, after NIST SP 800-63B).
**Proposal:** raise `MinPasswordLength`, the API's `minLength` and the form's check to 12 for new
passwords (registration, reset, change, invitation). Existing passwords keep working; the demo
password `demo-password` has 13 characters.

### O2 Common passwords

ASVS 2.1.7 asks for a check against breached or common passwords, 2.1.8 for a strength meter.
**Proposal:** embed a list of the 10,000 most common passwords that fit the length rule and refuse
them with a field error when a password is set, offline (no call to an outside service, so it
works in the WebAssembly demo and leaks nothing). The form shows the length and the result of the
same check as a simple meter.

### O3 Show password

ASVS 2.1.12. **Proposal:** a `PasswordInput` with a show/hide button (shadcn input group), used
for every password field.

### O4 Password-changed mail

ASVS 2.2.3 and 2.5.5. **Proposal:** a mail `password-changed` (en, de) to the account's address
after a password change and a reset, in the same transaction, without a link ("if this was not
you, reset your password and contact the school").

### O5 Session lifetime

ASVS 3.3.2 asks for re-authentication at least every 30 days; sessions last up to 90 days (14 days
idle). **Proposal:** `SessionMaxAge` of 30 days. Members then sign in once a month.

### O6 Multi-factor authentication for staff

ASVS 4.3.1 asks for multi-factor authentication on administrative interfaces; office and admin
accounts see and change the personal data of every member. **Proposal:** TOTP (or passkeys) for
`office` and `admin` accounts, required by a school setting, with recovery codes. A larger
feature, for its own ADR.

### O7 `__Host-` cookie prefix

ASVS 3.4.4. The prefix requires `Secure`, which local development over plain HTTP lacks, and the
cookie's name is part of three operations' signatures. **Proposal:** name the cookie
`__Host-studio_session` when `BASE_URL` is https and keep `studio_session` otherwise, reading the
cookie in the session middleware instead of the operations' input structs; existing sessions end
once with the rename.

### O8 Search terms in query strings

ASVS 8.3.1. The office's searches (persons, participants, receipts, the mail log) send names or
email parts as query parameters. The app logs no query strings, but a reverse proxy does.
**Proposal:** accept, and document it (done in [operations.md](operations.md) and
[privacy.md](privacy.md)): keep the proxy's access log short or drop query strings from it.
Moving the searches into POST bodies would cost the REST semantics and caching of these reads.

### O9 Minor items

- **Version disclosure (14.3.3):** the public health endpoint names the build version. It is an
  open-source app whose releases are public; showing the version only to staff would make
  support harder. **Proposal:** accept.
- **Cache after a 401 on a read (8.2.3):** a session that expires while a page is open keeps the
  page's data in memory until the next change or reload. **Proposal:** reset the query cache on any
  401, as on a change.
- **Password length counted in bytes (2.1.2):** the API counts characters, the service bytes, so a
  password of more than 85 three-byte characters passes the form and fails with a 422 that names
  no field. **Proposal:** a field error on `password` for it.
