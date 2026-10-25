# 0011 — Calendar feeds

- Status: accepted
- Date: 2026-10-25

## Context

Phase 5 lets people see their lessons in the calendar app they already use, which subscribes to an
iCalendar URL and polls it. `domain.CalendarFeed` has existed since Phase 1 with its scopes
(`bookings`, `teaching`, `schedule`) and a token hash, but without a table, a repository or a
service. The previous system served its feeds without any secret: the URL named the person (by name
or email address), so anyone who knew or guessed it read someone's lessons, and nothing could be
revoked. Tokens follow ADR 0004 (32 random bytes, only their SHA-256 stored), visibility ADR 0003,
families ADR 0008, bookings ADR 0009, and the access levels ADR 0005.

## Decision

**Subjects.** A feed shows one subject:

- `bookings`: the lessons the account's family (its person and dependents, managed ones included;
  ADR 0008) has active bookings for, whatever the lessons' visibility (as in My lessons). One event
  per lesson; its summary names who is booked. Waiting-list entries are not shown.
- `teaching`: the lessons that the active staff records linked to the account's person teach or
  accompany (as in the instructor view), canceled and hidden ones included; the summary adds the
  part. Only staff with such a record may create one; the office uses schedule feeds.
- `schedule`: the schedule as the account sees it, the whole school or one location
  (`locationId`).

**Every feed belongs to an account.** Visitors sign up to subscribe. An account has one active link
per subject (scope and location; `calendar_feeds_one_active_key`): creating a link revokes the old
one, which answers 404 from then on. The token is shown once, in the response that creates it, with
the path `/ical/{token}.ics` and the absolute URL (from `BASE_URL`; none in a demo installation,
which no calendar can reach); only its hash is stored. The
owner lists their feeds (scope, location, created, last used; never a token) and revokes them. An
admin signed in as an account may list and revoke its feeds but not create one: the link would
outlast the impersonation. Creating a link locks the account's person first, so two concurrent
creates of a subject do not collide: the later revokes the earlier.

**The owner's rights at the time of the fetch.** A feed is built from what its account may see when
a calendar fetches it, not when the link was made: its current role, early access, family and staff
records. A disabled account's feeds answer 404; a demoted one's show what it now sees (a teaching
feed of an account that is no staff any more is empty). A feed goes with its account and its
location (`ON DELETE CASCADE`).

**Window.** From midnight 28 days before today (school time) on. Ahead: bookings as far as they go;
the schedule and teaching up to the generation horizon (ADR 0003), within which a member sees
regular lessons up to their early access and the published-early kinds up to the horizon, and staff
see everything.

**The document** (`internal/ical`, standard library only): RFC 5545 with CRLF, lines folded at 75
octets without splitting a UTF-8 sequence, escaped TEXT. `METHOD:PUBLISH`, `NAME` and
`X-WR-CALNAME` "School – subject" in the owner's language (their mail language, else the school's;
English and German), `X-WR-TIMEZONE`, `REFRESH-INTERVAL` and `X-PUBLISHED-TTL` of an hour. Times
are local to the school's zone (`DTSTART;TZID=…`) with a `VTIMEZONE` that lists the zone's
transitions in the years the events cover, computed from Go's zone data (no `RRULE`, so it holds for
any zone and any past rule change); a local time that occurs twice is written in UTC unless it is
the first. Each lesson is a `VEVENT`: `UID` `<lessonId>-<scope>[-<locationId>]@<host of BASE_URL>` (stable
across fetches and per subject, so a lesson in two subscribed feeds, the school's and a location's
schedule say, stays two events), `DTSTAMP` the generation
time, `SUMMARY` "Course type – instructor" (bookings: "– the booked names", teaching: "(part)"),
`LOCATION` "location, room, address", `DESCRIPTION` with the cancel note, the kind, who is booked,
instructor, accompanist and room, `URL` to the lesson in the app, `SEQUENCE:0`. A lesson the school
canceled stays, with `STATUS:CANCELLED`, `TRANSP:TRANSPARENT` and "Canceled:" before its summary
(calendars that ignore the status still show it); a booking the family canceled drops out.

**HTTP.** `getCalendarFeed` (`GET /ical/{token}.ics`, access `public`, outside `/api` so the URL
stays short; the Vite dev proxy forwards it, and unknown `/ical/…` paths are a 404 problem, never
the app's page) answers `text/calendar; charset=utf-8` with `Cache-Control: private, max-age=900`
and an `ETag` over the content without the `DTSTAMP`s (hashed in the same pass that writes the
document; the zone's transitions are cached per zone and years), so `If-None-Match` gets a 304 until something
changes. `Content-Disposition` is `inline`, or an attachment with a file name such as
`studio-example-schedule.ics` with `download=true` (the web demo's download). An unknown or revoked
token is a 404. Fetches are rate-limited per client IP, generously (2000 an hour,
`RateLimits.CalendarFeeds`), because calendar services and NATs poll many feeds from few addresses,
and per feed (30 an hour by the token's hash, `RateLimits.CalendarFeedTokens`), so a leaked link
cannot be hammered; under both limits every fetch is answered as usual. The token never appears in
logs or error details: the access log, the panic log and the 404 of an unknown route write
`/ical/<redacted>.ics`. The first fetch of a school day (the date in the school's zone) records
`last_used_at`. Management lives under `/api/v1/my/calendar-feeds`
(`listCalendarFeeds`, `createCalendarFeed` 201, `revokeCalendarFeed` 204; access `signedIn`).

**The web demo** has no server that a calendar could reach. It creates real feeds with the same
code and offers the document as a download through the bridge.

## Consequences

- A leaked link shows its subject until it is revoked or replaced; the owner sees on which day it
  was last used. The token's entropy and the rate limits make guessing pointless.
- Calendar apps refresh at their own pace (Google every few hours), whatever `REFRESH-INTERVAL`
  asks for; changes reach them with that delay.
- Personal data: feeds and their usage dates belong to the account. The export and deletion of
  Phase 8 include them; deleting an account deletes its feeds.
