# Parity leftovers: implementation plan

> **For agentic workers:** this plan is run by `ccbatch run plan`, one headless session per
> checkbox below (`--prefix 1.`). The four items are independent: each starts from `main` and
> opens its own pull request. Your item's details are in the section under its checkbox. Read it,
> `CLAUDE.md`, the PLAN.md lines it names and the files it names before you change anything. Keep
> the test-first order: write the failing test, see it fail, implement, see it pass, commit.

**Goal:** close the open PLAN.md items of Phase 15 ("Parity with the live system") that need no
decision: the review leftovers of the rental and statistics work, one school-date check for the
web forms, and the feedback of every action that sends mail.

**Tech stack:** Go (Huma v2, chi, PostgreSQL + sqlc + goose, memory store), React 19 +
TypeScript, Vite, Tailwind v4, shadcn/ui, TanStack Router/Query, react-hook-form + zod,
react-i18next, Vitest, Playwright.

## Global constraints

- **Don't edit PLAN.md.** The four pull requests would conflict there (the items are adjacent
  lines). Say in the PR body which PLAN.md lines your item closes; they are ticked after the merge.
- **No real data.** Fictional names and example domains only.
- **Every user-facing string** goes in `web/src/locales/en` and `web/src/locales/de` (both).
- **Both stores:** a changed or removed repo method changes its `internal/store/storetest` case
  on memory and PostgreSQL (`just test-pg`, Docker). After a query change, `just sqlc`. After a
  repository interface change, regenerate `internal/service/fault_gen_test.go`.
- **API changes** (none are expected): `just gen-api`; CI fails on a stale `api/openapi.json`.
- **Coverage gate:** 95 % total, 80 % per package (`just test-cover`, `just coverage`). A branch
  no input can reach is deleted, never excluded.
- **Conventional Commits** in English, ending with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **Lint locally** with golangci-lint v2.14.0 (CI's version): `PATH=$HOME/go/bin:$PATH just lint`.
- **Parallel sessions:** run e2e on a port of your own worktree:
  `E2E_PORT=$((4200 + $(pwd | cksum | cut -d' ' -f1) % 700)) just e2e`.

---

## Task 1.1

- [x] 1.1 `stats-reads` Statistics: drop `Bookings().CountActivePersons`; the active members come from `CountsByPersons`, which the top list reads anyway — see "Task 1.1".
  - Done: `compute` reads `CountsByPersons` once; `activePersons` picks the entries with
    `Active > 0`, their number is `ActiveMembers`, and `addTopParticipants` takes the counts and
    those persons (it still reads `Persons().ListByIDs` once). `CountActivePersons` is gone from
    `store.BookingRepo`, both stores (`CountActiveBookingPersons` and its sqlc code), the storetest
    and PostgreSQL error-path cases and `fault_gen_test.go`; the `CountsByPersons` contract case
    already covers what its case did (a person in two lessons, guests, canceled in time, a lesson
    not asked for, no lessons). New test `TestStatisticsActiveMembers` (guest, canceled in time,
    late-canceled only, canceled lesson, lesson outside the range, a person in two lessons, a
    merged person with an active booking: 3) passed on the old code before the change, and fails
    there on its second assertion, the exact sequence of `Bookings.*` reads from the `FaultStore`
    log. One read less per request. `just lint`, `just test-cover` (PostgreSQL included),
    `just coverage` (99.0 %) and `just check-sqlc` green.
  - Discovery: `ActiveMembers` counts merged and anonymized persons who kept an active booking,
    unlike the top list, which leaves them out; kept as it was (the count comes from the counts
    before the persons are read), pinned by the test and written into the field's doc comment.

PLAN.md, Phase 15, "Review leftovers", first sub-item. `StatisticsService`
(`internal/service/statistics.go`) reads `CountActivePersons` for `Totals.ActiveMembers` and
`CountsByPersons` for the top participants. Both cover persons (guests excluded) with bookings in
the same lessons, so the active members are the entries of `CountsByPersons` with `Active > 0`.
Read `CountsByPersons` once, set `ActiveMembers` from it and pass the counts to the top-list
code. Then remove `CountActivePersons` from `store.BookingRepo`, both stores (the PostgreSQL
query and the sqlc output), its storetest case and the fault list. Pin the equivalence with a
service test: a range with a guest booking, a canceled booking, a late-canceled booking only, and
one person booked in two lessons gives the same `ActiveMembers` as before. Update every mention:
`CLAUDE.md` (code map, "Statistics") and `docs/architecture.md` ("Statistics", the list of store
reads).

The second sub-item ("rank and cut in the store") stays open: cutting the list in the store
would take away the full counts this item now derives the active members from. Say so in the PR
body.

---

## Task 1.2

- [ ] 1.2 `lesson-update-checks` Lesson writes: `becomesRental` without a second read, the `kind` error of a course-type-less rental together with the other field errors, and the memory store's `ListByStaff` without its dead guard — see "Task 1.2".

PLAN.md, Phase 15, "Review leftovers", the sub-items on `becomesRental`, `ListByStaff` and
`UpdateLesson`, all in `internal/service/lesson_admin.go` and `internal/store/memory/repos.go`.

1. `UpdateLesson` calls `places()` and then `becomesRental`, which reads the bookings (canceled
   ones included, through `hasBookings`) and the open waiting list a second time. Check before
   `places()` or reuse what it read, whichever reads less; the rule stays as ADR 0007's amendment
   states it (any booking, canceled ones included, or an open waiting-list entry refuses the
   change to `rental`). Keep the existing tests green, and add one that counts the store reads
   if the repo's fault or counting helpers make that cheap.
2. The check "a room rental without a course type cannot become another kind" returns its `kind`
   field error before `apply` runs, so a request with that error and another field error reports
   only the first. Report them together, the way the service reports several field errors
   elsewhere (find how `domain.FieldError`s are joined; don't invent a second mechanism). Test:
   one request with both errors gets both fields in the 422.
3. Memory `lessonRepo.ListByStaff`: drop the `!l.InstructorID.IsZero()` and
   `!l.AccompanistID.IsZero()` guards. No caller asks for a zero staff ID, so they cannot change
   the result; both stores then read alike. If the storetest case has no lesson without an
   accompanist among the results, add one so a regression would show.

---

## Task 1.3

- [ ] 1.3 `school-date` Web: one school-date check in `lib/schoolTime.ts`, used by every form with a date field, so that a date that does not exist (2026-02-30) is refused everywhere — see "Task 1.3".

PLAN.md, Phase 15, "Review leftovers", the web sub-item. Today only the booking-rules card refuses
a date that does not exist, through `isSchoolDate` in `web/src/features/admin/settingsForms.ts`.
Move it to `web/src/lib/schoolTime.ts` (with its tests), and use it in every zod schema of a
date-only field: the clone form (`schedule/admin/cloneForms.ts`), the lesson form
(`schedule/admin/lessonForms.ts`), the term sheet (`admin/TermSheet.tsx` or its form module), and
the search and filter forms with a date (find them with
`grep -rn "type=\"date\"\|type='date'" web/src`, e.g. receipts, mail log, audit, statistics'
custom range, announcements, person birth dates). A date input normally cannot produce
2026-02-30, but typed or pasted values and URL search params can; a test per form feeds one and
expects the field's translated error. Reuse the existing `date` error key where a form has one,
otherwise add the key to both locales.

---

## Task 1.4

- [ ] 1.4 `mail-feedback` Web: every action that sends mail shows that it is pending and then whether it worked — see "Task 1.4".

PLAN.md, Phase 15, "Mail feedback". The previous app shows a spinner and colours its mail
buttons by success or failure. Here, check these actions and bring each one to the same
behaviour: while the request runs, the button is disabled and shows a spinner (find how other
pending buttons look and reuse that); afterwards a success toast that names what was sent, or an
error toast with the translated reason, and on failure the button is usable again.

- an invitation (the office's person administration, `service/invite.go`'s operation),
- a password-reset link (the office's or admin's "send reset link" action),
- an announcement saved with mail (the announcement sheet; the mail goes out through the job, so
  the toast says it is queued, not sent),
- the requeue of a failed mail (`web/src/features/mail-log/MailLogPage.tsx`).

Add a component test per action for the pending state (the mocked request held open) and for both
outcomes. Where an action already behaves like this, leave it and say so in the PR body. Don't
copy the old app's colours; follow this app's design.

---

## Running it

```sh
GH_TOKEN=$(gh auth token -u CWBudde) ccbatch run plan --repo . \
  --file docs/superpowers/plans/2026-11-02-parity-leftovers.md --prefix 1. --jobs 2 \
  --branch 'fix/parity-{{.Slug}}' \
  --verify 'just web-install' --verify 'go build ./...' --verify 'just test' \
  --verify 'PATH=$HOME/go/bin:$PATH just lint' --verify 'just check-generated' \
  --verify 'just check-sqlc' --verify 'just web-check' --verify 'just wasm-smoke' \
  --verify 'E2E_PORT=$((4200 + $(pwd | cksum | cut -d" " -f1) % 700)) just e2e'
```
