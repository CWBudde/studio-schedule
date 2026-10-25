# 0005 — Access levels and impersonation

- Status: accepted
- Date: 2026-10-23

## Context

From Phase 3 on, the API has operations that only some roles may call (account administration,
later timetable and lesson administration). The previous system checked permissions inside
handlers, inconsistently, with role numbers that drifted between two backends. Whoever supports the
members needs to see the app as a member sees it, to answer questions; the previous system had no
way to do that except asking for passwords. Seeing another account's bookings and personal data is
a strong right, so it is reserved to admins; the office can be admitted later if the school wants
that.

## Decision

**Access levels.** Every Huma operation declares who may call it when it is registered
(`httpapi.register`, or `httpapi.Declare` for an operation registered directly):

| Level      | Admits                     |
| ---------- | -------------------------- |
| `public`   | everyone, signed in or not |
| `signedIn` | every signed-in account    |
| `staff`    | instructor, office, admin  |
| `office`   | office, admin              |
| `admin`    | admin                      |

Roles are ordered as in `domain.Roles()` (`Role.AtLeast`).

- One middleware, after the session middleware, enforces the level before the request input is
  parsed. Without a session it answers 401, and with a role below the level 403.
- An operation without a level answers 500 (fail closed). Nothing is open by accident.
- The level is published as the `x-access` extension of the operation in `api/openapi.json`.
- `internal/app/access_test.go` pins the level of every operation. It calls every operation as a
  guest and as every role, and expects 401/403 exactly below the level. A new operation fails the
  test until it is added there.

Access levels decide _whether_ an operation may be called. What a caller then sees is business
logic in the services, e.g. the schedule's visibility rules (ADR 0003) and, later, "only your own
bookings".

**Impersonation ("login as").** An admin can sign in as another account to see what it sees:

- Admins only, and never as another admin, as oneself, or from within an impersonation. The
  target must be confirmed and not disabled.
- Starting it deletes the admin's session and creates a session of the target that records the
  admin (`sessions.impersonator_id`). The cookie is replaced. The session lasts at most one hour and
  is never extended.
- While it lasts, every request runs with the target's role. Admin operations are therefore
  refused until the admin returns. The session ends at once if the admin is disabled or no longer an
  admin.
- `getMe` names the impersonator, and the web app shows a banner on every page with "Return to my
  account". Returning deletes the impersonation session and signs the admin in with a new session:
  only the hash of the old token was stored, so the old cookie cannot be restored.
- The start, the return, and signing out of an impersonation are written to `audit_events` (actor,
  action, target, time). An impersonation that simply expires has no end event; the start event and
  the one-hour limit bound it. (Since ADR 0016, an impersonation ended because its account, or the
  admin's, was disabled or deleted or got a new password is recorded as ended too, by the admin.)

## Consequences

- Adding an endpoint now also means choosing its access level and adding it to the pinned table.
- Services may still check roles where the rule is business logic (e.g. `Impersonate` checks for an
  admin itself), so that they stay safe when called from elsewhere.
- The admin area lists the latest audit events at `/admin/audit`.

## Amendment (2026-10-25): account administration

[ADR 0016](0016-privacy-and-account-administration.md) adds the admin's account administration
(disable, enable, change the role, delete; `admin`), each recorded in the audit log, and lets a
member delete their own account. An admin never changes their own account there (409
`own-account`), and the last admin who can sign in is never disabled, demoted or deleted (409
`last-admin`). An impersonation also ends once its account becomes an admin. An account that acted
in the audit log is kept (the audit actor refuses its deletion), so an admin cannot erase their
trail.
