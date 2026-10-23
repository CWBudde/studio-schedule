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
One-off lessons (no class) and kinds other than `regular` (workshops, private lessons) ignore the
window: they are "published early" by rule, without a per-lesson release field. Canceled lessons
stay visible with their status. `members` lessons need a signed-in member and `hidden` lessons
staff (Phase 3). `Settings.DefaultVisibleDays` and the per-account override additionally limit how
far ahead signed-in members see once accounts exist. From Phase 4, booking enforces the same window
on the server.

**Lesson generation.** Lessons are generated from the weekly classes of each term for the dates in
the term's inclusive range (classes have no range of their own). A lesson starts at
`date.At(class.StartTime, schoolZone)`, which is wall-clock time: an 18:00 class is at 18:00 before
and after a DST change. A wall-clock time that happens twice resolves to the earlier instant, one
that does not exist moves forward by the gap. Generation is idempotent on `(class_id, starts_at)`:
it reads the existing lessons of the range and creates only the missing ones, so it never relies on
a failed insert (which would abort a PostgreSQL transaction). The horizon rolls: from 7 days ago to
`max(7·(ReleaseWeeksAhead + 2), DefaultVisibleDays) + 7` days ahead, refreshed by an hourly job.

**Background jobs.** One `internal/jobs` runner with a Go ticker per job serves both targets; Go
timers work in `js/wasm`. The server and the WASM build run every job once at startup (WASM before
it answers the first request), then on their interval.

## Consequences

- A lesson that an admin moves to a different start is re-created by the next generation run.
  Phase 3 lesson administration needs a per-class "generated through" watermark or a link between
  a lesson and its slot.
- There is no "publish early" switch for a single regular lesson; Phase 3 can add one if needed.
- The demo's term has fixed dates, so its schedule is empty once the term has ended.
