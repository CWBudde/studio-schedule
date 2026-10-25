# 0014 — Announcements and the mail log

- Status: accepted
- Date: 2026-10-25

## Context

Phase 7 adds the school's communication: announcements ("Mitteilungen") to everyone or to a part of
the school, and a log of the mails the outbox sends (ADR 0004). `domain.Message` existed since
Phase 1 with an audience of everyone, members or staff, without a table; the plan asks for a role, a
course type or a lesson. The previous system fanned announcements out into one row per account and
mailed them after the commit, so a failure lost mails and a later member never saw what was
current; it had no mail log. The outbox kept every mail forever, bodies included, and a failed mail
could not be sent again. The audit log (ADR 0005) only knew account → account.

## Decision

**Announcements.** The code calls them `domain.Announcement` (glossary: "Announcement",
"Mitteilung"); tables `announcements`, `announcement_dismissals`, `announcement_mailings`
(migration 00019). The office and admins write them (`AccessOffice`): a one-line title (up to 120
characters), plain text with line breaks (up to 4,000 characters, the `noticeText` rules of ADR 0009:
normalized line breaks, no control or invisible format characters but the zero-width joiners), an
audience, `publishAt` (default now, may lie in the future) and an optional `expiresAt`, which must
not have passed (a 422 on `expiresAt`: such an announcement would never show, and must not take a
place in the daily limit). One text, in the language it was written. They stay editable until they
expire; they can always be deleted. The author is the acting account (the admin while
impersonating).

**Audience.** One of:

- `everyone`: every account;
- `role`: the accounts of one role (exactly that role);
- `class`: the class's members and the persons with an active booking of one of its lessons that is
  not over;
- `course-type`: the same for every class of the course type whose term has not ended, and every
  lesson of it that is not over;
- `lesson`: the persons with an active booking of the lesson.

A class or course type an announcement is for cannot be deleted (409 `in-use`, ADR 0007); a lesson's
announcement goes with the lesson, because timetable edits delete lessons (ADR 0007) and must not be
blocked by it.

**Resolved when read, and when mailed.** Nothing is fanned out. An account sees an announcement that
is shown (published, not expired) and for everyone, its role, or persons among which are the
account's person or one of its dependents (ADR 0008): parents see their children's class and lesson
announcements. The reader's side costs a few queries for any number of announcements: the person
and dependents, their memberships, their bookings of lessons that are not over, and, for lesson
audiences, their bookings of those lessons. A new member sees what is current.

**Dismissed per account.** The banner under the header shows the newest announcement the account has
not dismissed, and all of them on request; dismissing stores an `announcement_dismissals` row
(idempotent). The home page lists every shown announcement, dismissed ones included. While an admin
impersonates the account, a dismissal is not stored (the banner hides it for the visit): the admin
must not change what the account sees.

**Email, once, by the job.** With `email`, an announcement is mailed to its audience once, and only
by the `announcements` job (`AnnouncementService.SendDue`): writing it saves it and takes its place
in the daily limit, nothing more, so the request stays cheap (the demo runs on one thread) and there
is one mailing path. The job runs every minute, also in the demo's Worker, and an announcement that
publishes now wakes it (`jobs.Waker`, `Deps.Wake`), so it goes out right after the response; the
office reads "being sent" until it is done. A run queues at most 100 mails: a larger audience is
mailed over several runs, its recipients ordered by their lowercased address and `MailCursor` (the
last one mailed) telling the next run where to go on, so nobody gets it twice; someone who joins the
audience meanwhile gets it if their address comes later. `emailedAt` marks it once the last batch is
queued, and `emailRecipients` counts the addresses (so far, while it is being sent). Edits never mail
again: one whose mailing has begun keeps `email` on and goes on with the edited text for the rest;
one not mailed yet is mailed like a new one. Each mailing and each edit locks the announcement
(after `LockMail`, below), and an edit writes everything but the mail state
(`AnnouncementRepo.Update` versus `SetMailState`), so an edit that races the job can neither
undo its mailing nor send it twice. Recipients:

- everyone or a role: the confirmed accounts that are not disabled, each in its language;
- persons: the mail chain of each person (ADR 0007/0009: a disabled account gets nothing, else the
  confirmed account, else the person's own address, else the confirmed guardian accounts), each
  address once ignoring case, with the persons it gets the mail for named in it.

Guests without an account are not reached. The mails go through the outbox in the job's
transaction (`TemplateAnnouncement`, en and de). One that expired before the job came, or while it
was being sent, is marked emailed without (more) mails. The audience costs a few queries for any
number of lessons: the lessons not over are those of the class or course type that started less
than 25 hours ago or later (a lesson ends on its school day) and end after now, and their bookings
come in one query.

**A daily limit counted in the database.** A school day (in the school's zone) has at most 3 emailed
announcements, sent ones (`announcement_mailings`) and scheduled ones (`email` and not yet mailed,
publishing that day) together; beyond that the write is a 409 `mail-limit`, at once, though the job
sends it. An announcement being sent counts as scheduled; its mailing row is written with its last
batch, or when it is deleted while being sent (with the mails queued so far). A mailing row outlives
its announcement, so deleting one does not free its place. Every check and every mailing runs under
`AnnouncementRepo.LockMail` (a transaction-scoped advisory lock in PostgreSQL), so two concurrent
writes cannot both take the last place. The form asks before emailing and shows how many addresses
the audience reaches now (`countAnnouncementRecipients`). Throughput stays the outbox's: about 20
mails a minute.

**The mail log.** Admins only (`AccessAdmin`), like the audit log: bodies hold personal data. The
log lists every mail's metadata (time, recipient, template, subject, status, attempts, next attempt,
sent time, last error, attachment name), newest first, filtered by status, template (an enum of
`mail.TemplateNames` in the API, so the client's types know every template) and a part of the
recipient (ignoring ASCII case), 50 to a page with an opaque cursor (creation time and ID). The
list never carries bodies. The detail shows them, except for the five templates with a secret,
single-use link: confirm-email, change-email, reset-password, invite and waitlist-invite
(`mail.SecretLink`). Their bodies never leave the server: reading them would let an admin take over
an account or a reserved place without the audit log knowing.

**Queuing again.** A failed mail can be queued again: its next attempt is now and its attempts start
over, so it gets every retry again (the same row; no copy). Pending and sent mails are a 409
`not-failed`. Mails with a secret link are a 409 `secret-link`: their link may be used or stale, and
the flows that send a new link (reset password, a new invite) do it properly. Each requeue is
recorded in the audit log.

**Retention.** The daily `purge-mail` job removes the bodies of mails sent more than 30 days ago
(`body_text` and `body_html` NULL; a sent mail may have no body) and deletes mails created more than
180 days ago, whatever their state. A mail with a receipt only refers to the immutable receipt
(ADR 0012); deleting the mail leaves the receipt. In the demo nothing is delivered, so every mail
stays pending; the log says so, and the demo mailbox stays.

**The audit log, wider.** `audit_events` (migration 00020) gains `target_kind` (`account`, `person`
or none), `target_person_id` and a `details` JSON object (`domain.AuditDetails`, typed, every field
optional). Actions are rows of `audit_actions` referenced by a foreign key instead of a CHECK list,
so a new action is one `INSERT` (the merge of ADR 0015 adds `person-merged`). `mail-requeued` has no
target and names the mail, its recipient and template in its details. `/admin/audit` shows what an
event concerns: the account, the person or the mail.

## Consequences

- An announcement's reach changes with memberships and bookings; nobody can tell later who saw one,
  only who was mailed (the count) and who dismissed it.
- The audience of a course type depends on terms that have not ended and lessons not over; a
  course type without current classes reaches nobody but those booked for its upcoming lessons.
- 3 emailed announcements a day is a fixed constant (`domain.AnnouncementMailsPerDay`), not a
  setting.
- After 30 days a sent mail's text is gone from the log; after 180 days the mail is. Phase 8's data
  export and erasure build on this.
- Admins cannot read password-reset or invitation mails, even in the log.
