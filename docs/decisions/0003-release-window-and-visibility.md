# 0003 — Release window, visibility and lesson generation

- Status: accepted
- Date: 2026-10-23

## Context

Phase 2 publishes the timetable. Three rules decide what a visitor sees: when lessons are
released, which lessons are visible at all, and how lessons come into existence from the weekly
classes. The previous system had no lesson generator and no running background job. It computed
the release rule in three places that disagreed (server, browser client and a rewrite), in the
browser's time zone, and compared dates instead of instants. Its day-cloning shifted lessons by an
hour across DST changes.

## Decision

**Release window (server only).** Every `Settings.ReleaseWeekday` at `ReleaseTime` (school time)
the schedule is released through the same weekday `ReleaseWeeksAhead` weeks later:

- `LastRelease` is the latest release instant at or before now (the release instant itself counts
  as released); `NextRelease` is the one after it. "One week earlier" is computed on school dates,
  not as 168 hours, so DST changes do not move the release time.
- `ReleasedUntil` is school-local midnight after `LastRelease.date + 7·ReleaseWeeksAhead` days.
  With a Friday 18:00 release and two weeks ahead, the release on Friday the 16th opens everything
  that starts before Saturday the 31st 00:00.
- Lessons are compared by their start instant (`StartsAt < ReleasedUntil`), never by date.
- The client receives `releasedUntil`, `nextReleaseAt` and the server's `now` and never recomputes
  the rule.

**Visible means released.** A guest sees a lesson if its visibility is `public` and it is released.
Workshops and private lessons (kinds other than `regular`) ignore the window, whether they belong
to a class or not: they are "published early" by rule, without a per-lesson release field. A
regular lesson follows the window, a one-off one included (amended 2026-10-24: until then every
one-off lesson was published early; lesson administration lets the office add regular one-offs
and clone lessons as one-offs, which must not bypass the release; ADR 0007). Canceled lessons stay
visible with their status. Signed-in accounts see more:

- **Members** also see `members` lessons, and they get _early access_: every lesson they may see
  that starts before school-local midnight after today + N days, where N is the account's
  `VisibleDays` or else `Settings.DefaultVisibleDays`. Early access only adds to the release window
  and never hides a released lesson. Its end comes back as `visibleUntil` (for guests it equals
  `releasedUntil`).
- **Staff** (instructor, office, admin) see every lesson, `hidden` and unreleased ones included, and
  get no `visibleUntil`.

The lesson generator's rolling horizon covers the longest early access of any account. From
Phase 4, booking enforces the same window on the server.

**Lesson generation.** Lessons are generated from the weekly classes of each term for the dates in
the term's inclusive range (classes have no range of their own). A lesson starts at
`date.At(class.StartTime, schoolZone)`, which is wall-clock time: an 18:00 class is at 18:00 before
and after a DST change. A wall-clock time that happens twice resolves to the earlier instant, one
that does not exist moves forward by the gap. Each lesson remembers its slot: the school date it was
generated for (`lessons.slot_date`, NULL for one-off lessons), which stays when the lesson moves.
Generation is idempotent on `(class_id, slot_date)`: it reads the existing lessons of each class and
creates only the missing ones (a week with a lesson of the class has its lesson, ADR 0007), so it never relies on a failed insert (which would abort a PostgreSQL
transaction), and a moved lesson is not created again. The horizon rolls: from today to
`max(7·(ReleaseWeeksAhead + 2), DefaultVisibleDays) + 7` days ahead, refreshed by an hourly job.
Generation creates only lessons that have not started: a lesson nobody could see before its start
did not take place, and a class created today has no history (amended 2026-10-24; the horizon
reached 7 days back before). The demo seed brings the past week of its classes itself. Retired
classes (ADR 0007) generate nothing.

**Background jobs.** One `internal/jobs` runner with a Go ticker per job serves both targets; Go
timers work in `js/wasm`. The server and the WASM build run every job once at startup (WASM before
it answers the first request), then on their interval.

## Consequences

- A lesson that moves to a different start keeps its slot, so generation does not re-create it
  (the slot link, added with the class administration of ADR 0007, 2026-10-24).
- Only weeks of downtime could skip a lesson: generation reaches weeks ahead, and does not fill
  the past.
- There is no "publish early" switch for a single regular lesson; Phase 3 can add one if needed.
- Two server instances generating at the same moment can both see a lesson as missing; the
  second insert then fails on the unique `(class_id, slot_date)` key and that run rolls back.
  The next run fills the gap. Running several instances needs a retry on conflict or an
  advisory lock around generation.
- The web demo seeds its term and workshops relative to the day of the visit (`seed.LoadDemoAt`),
  so its schedule is never empty. The server's demo seed (`seed.LoadDemo`, used by `serve` without
  a database and by `studio-schedule seed`) keeps the fixed school year 2026/27, so its schedule is
  empty once that term has ended.

## Amendment (2026-10-31): a manual "released through" date

The previous system had an office setting "Up to which day should the schedule be visible?". The
school decided to keep it, but only to **extend** the weekly release, never to hold anything back.

**The setting.** `Settings.ReleasedThrough` is an optional school date (`settings.released_through`,
migration 00027; the zero `domain.Date`, NULL, is none). An admin edits it on the booking-rules
card (ADR 0013), the office reads it.

**The window.** `ComputeReleaseWindow` stays the one place that computes the release: its
`ReleasedUntil` is the later of the rule's end (now also returned as `RuleReleasedUntil`) and
school-local midnight after `ReleasedThrough` (`ReleasedThrough.AddDays(1).At(00:00, zone)`, so a
DST change on that day does not shift it). `LastRelease` and `NextRelease` stay the rule's.
Everything that reads the window picks the date up unchanged: what guests see, members'
`visibleUntil` (early access still only adds; it is the later of the two), booking (ADR 0009),
the calendar feeds (ADR 0011) and the booking-rules card. The client still never computes the
window: the card's answer and its preview carry `releasedUntil` and `ruleReleasedUntil`, so the form
shows whether the date reaches further than the rule.

**No clean-up.** Once the weekly rule passes the date, the date has no effect, and nothing clears
it. A past date is therefore allowed when saved too (it has no effect), so a stored date never keeps
the card from saving after its day has passed. A date may lie at most 52 weeks (364 days) after
today in the school's zone, the reach of the longest weekly release
(`domain.ValidateReleasedThrough`, a field error on `releasedThrough`; the preview checks it the
same way).

**Generation.** The rolling horizon reaches at least 7 days past the date:
`max(today + max(7·(ReleaseWeeksAhead + 2), visible days) + 7, ReleasedThrough + 7)`. Saving a later
date counts as a wider window (ADR 0013): it generates the lessons it reaches at once with
`GenerateAhead`.
