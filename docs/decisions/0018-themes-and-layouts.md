# 0018 — Themes and layouts

- Status: accepted
- Date: 2026-10-30
- Amends: [0013](0013-settings-branding-and-init.md) (the branding card gains the theme and the
  background image)

## Context

Since ADR 0013 a school brands the app with a name, a logo and one accent color; everything else of
the look is the app's own. Many members and staff of the schools this app is written for used the
previous dance-school app for years. Its look (warm colors, a condensed font, a translucent page over
a photo, the schedule as day bands with room columns, square action buttons) is what they know, and
the school decided (2026-10-30) to offer it as a second look next to today's, not as a pixel replica
and not as a separate frontend. Both looks must work on both targets, the server and the web demo
(ADR 0001), and nothing of the old app's identity (name, logo, icons, photos, texts) may enter the
repository. The design is [the themes and layouts spec](../superpowers/specs/2026-10-30-themes-and-layouts-design.md).

## Decision

**A theme is a school setting.** `domain.Theme` is a string enum: `programme` (today's look) and
`werkstatt` (the old app's visual language). The zero Theme is Programme (ADR 0002: zero = absent),
so settings stored before this ADR need no migration of their values; `Theme.OrDefault` names it.
`Settings.Validate` refuses an unknown theme with a field error on `theme`. It lives on the branding
card: `getBrandingSettings` answers `theme`, `updateBrandingSettings` takes it (admins), and the
public settings carry `theme` (always set) so every visitor gets the school's look before signing in.
PostgreSQL keeps it in `settings.theme` (migration 00025), NULL for the zero Theme, with a CHECK on
the known values.

**No per-visitor themes.** The look is the school's, the same for every visitor, as the accent is.
A visitor's own choice would double what each screen must be tested in, and the point of Werkstatt
is that a school's members find the app they know; the light/dark switch stays the visitor's.

**The background image is stored like the logo.** Werkstatt shows a photo behind its page panel. A
school uploads it on the branding card: a JPEG, PNG or WebP of at most 1 MiB
(`domain.Background.Validate`: the type, the size and the type's magic bytes, field errors on
`contentType` and `data`). It is kept apart from the settings record, which every request reads:
`SettingsRepo.Background`, `PutBackground` (the zero value removes it), `BackgroundInfo` (type and
digest without the bytes), in PostgreSQL the columns `background`, `background_type` and
`background_digest` of `settings`, all three NULL or none. The digest is `Logo.Digest`'s scheme
(SHA-256 of the type, a line feed and the bytes), written with the image. Endpoints:

- `GET /api/v1/settings/background` (`getSchoolBackground`, public): the image, `ETag` from the
  digest, `Cache-Control: public, no-cache`, a 304 on a matching `If-None-Match`, a 404 without one;
  the public settings say `hasBackground`. Like the logo, the web reads it through the transport as
  data, so the content security policy and the web demo need nothing new.
- `PUT` and `DELETE /api/v1/admin/settings/branding/background` (`updateBrandingBackground`,
  `deleteBrandingBackground`, admins, 204). The upload is a JSON body with `contentType` and the
  base64 `data`, as the receipt logo's (ADR 0012). Like the logo change it is not audited.

The background is a setting of its own, not of a theme: a school may keep it while it tries the
other theme. Programme does not show it.

**Layouts are slots with a Programme fallback.** The web app has a theme registry
(`web/src/themes`) with typed layout slots for the screens that differ: the shell, the schedule list,
its day heading and lesson card, home, profile and the lesson participants. A page computes its view
model and its actions (book, cancel, filters, forms) once and renders the active theme's slot; a slot
neither fetches nor mutates. Programme provides every slot; another theme overrides some, and a slot
it does not provide is Programme's, so every other page renders Programme's structure in the theme's
colors and fonts. Each theme but Programme is a lazily loaded chunk with its CSS scoped under
`[data-theme="<id>"]`; the last theme is cached in the browser and set before the first paint, so a
returning visitor sees no flash of the other look. The school's accent still overrides the primary
color in both themes.

**The demo offers one school per theme.** `seed.LoadDemoSchoolAt` seeds `programme` (today's demo,
unchanged; `LoadDemoAt` is this school) or `werkstatt`: at first the same people, classes and
history in a house of its own (its own data since the amendment below), with another fictional
name, the Werkstatt theme, its accent, German as the default language and a CC0 background photo
embedded in the seed package (`internal/seed/assets`, source and licence noted next to it).
`studio-schedule seed --school` takes the variant, so it is not demo-only. The web demo picks the
school and keeps its data per school (docs/demo-mode.md).

## Consequences

- Every slotted screen exists twice; each Werkstatt slot gets its own behaviour tests, and the
  Programme suites (Vitest, Playwright) guard that moving today's components behind the slots
  changed nothing.
- A theme changes for every visitor at once; an open page follows on its next read of the public
  settings.
- The settings row grows by up to 1 MiB with a background; `Get` does not read it, and the image is
  read only for a client without the current one.
- A third theme is a registry entry, an enum value and a CHECK change; it costs a migration.
- Snapshots of the web demo written before this change lack the background and start fresh, as any
  snapshot of another build does.

## Amendment (2026-11-02): the demo schools have their own data

The Werkstatt school had the programme school's people and classes in German settings, so a visitor
saw English names and an English timetable in the old app's look. Each school is now built from
tables of its own (`schoolSpec` in `internal/seed`: `programme()` and `werkstatt()`) by the same
builder; the design is §3 of
[the Werkstatt catalogue spec](../superpowers/specs/2026-11-01-werkstatt-catalogue-design.md).

- **Programme stays as it was.** Its tables are the ones from before, and
  `TestProgrammeDatasetUnchanged` pins its dataset for `FixedDemoAnchor` as the SHA-256 of its
  JSON: same IDs, same records. The two schools' IDs no longer line up.
- **Werkstatt is a German school of its own.** One house "Tanzhaus am Lindenhof" with the rooms
  Oben, Unten, Online and Trainingsraum (in that order), the levels Einsteiger (E) to Profi (P),
  70 weekly classes from Monday to Saturday, seven instructors with a field each (and a few
  classes beside it) and a former one, two accompanists, German passes (Einzelstunde, 10er-Karte, Monatskarte) and
  announcements, and terms named "Schuljahr …" (`schoolYear`). Its 23 households mirror the
  programme's record by record (roles, families and guardians), so the same scenarios exist: a
  member with several children, a managed teenager, single adults, a walk-in guest, a former
  instructor, an accompanist with a login, two duplicate pairs, classes behind with the attendance.
- **Shaped like the previous app, moved and renamed.** The Werkstatt look is for members who know
  the previous app, so its timetable should read like one: the catalogue and the weekly plan take
  the previous app's course kinds, level codes, class lengths and the rhythm of its week, but on
  other days, in other rooms and at other times, under other names. Nothing else is taken from it:
  every person, description, price and address is invented, as for the programme school.
- **Workshops take Sunday.** The plan leaves Sunday free, so each Werkstatt workshop, the full one
  included, lies on the Sunday on or after its day offset (the programme's offsets;
  `workshopsOn`), and the rental on the Wednesday on or after its offset, in the evening after
  Unten's last class (`rentalOn`). Since several offsets may fall on one Sunday, each workshop has
  a room and time of its own, so none meet whichever Sunday they fall on.
- **Workshops need course types of their own.** A lesson has no title; a workshop is named by its
  course type. The spec's German workshop titles (Floor Barre, Pas de deux, Yoga für Tänzerinnen
  und Tänzer) are therefore three course types beyond the plan's 23, used by no class.
