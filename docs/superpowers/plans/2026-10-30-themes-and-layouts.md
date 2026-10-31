# Themes and layouts: implementation plan

> **For agentic workers:** this plan is run by `ccbatch run plan`, one headless session per
> checkbox below, phase by phase (`--prefix 1.`, then `2.`, …). A later phase starts only after the
> earlier phase's pull requests are merged into `main`. Your item's detailed steps are in the
> section "Task N.M" further down; read that section, the spec, `CLAUDE.md`, and the files it names
> before you change anything. Use subagents for independent chunks (e.g. Go stores vs. web UI vs.
> tests) and keep the steps' test-first order: write the failing test, see it fail, implement,
> see it pass, commit.

**Goal:** a school picks a theme (Programme, today's look, or Werkstatt, a close copy of the old
dance-school app's look) that brings its own colours, fonts and layouts for the shell, schedule,
home, profile and lesson participants; the GitHub Pages demo offers one fictional school per theme.

**Architecture:** the theme is a school setting (`domain.Theme` on the Branding card) plus a
school-uploaded background image stored like the receipt logo. The web app has a theme registry
(`web/src/themes`) with typed layout slots; pages compute view models and actions once and render
the active theme's slot; Programme provides every slot, Werkstatt (a lazy chunk) overrides seven.
The WASM demo seeds one of two school variants and keeps one saved state per school.

**Tech stack:** Go (Huma v2, chi, PostgreSQL + sqlc, memory store), React 19 + TypeScript, Vite,
Tailwind v4, shadcn/ui, TanStack Router/Query, react-i18next, Vitest, Playwright.

**Spec:** [docs/superpowers/specs/2026-10-30-themes-and-layouts-design.md](../specs/2026-10-30-themes-and-layouts-design.md)
(read it in full; this plan does not repeat its reasoning).

## Global constraints

- **No real data.** Nothing of the old app's identity enters the repo: not its name, its logo, the
  NTW-Icons font, its photos, texts or addresses. Don't copy files from `../NTW`. Werkstatt copies
  the visual language (colours, fonts, layout) only. Icons: lucide. Font: `@fontsource/roboto-condensed`.
- **One app, two targets.** The theme and the background are real features on server and demo.
  Demo-specific code is limited to the seed variant, the transport/worker/persistence, the about
  page and the demo banner.
- **Every user-facing string** goes in `web/src/locales/en` and `web/src/locales/de` (both).
- **API changes:** Huma operations with explicit camelCase `OperationID`, then `just gen-api`;
  CI fails on a stale `api/openapi.json` or `web/src/api/schema.d.ts` (`just check-generated`).
- **Both stores:** every repo method gets a `internal/store/storetest` case that passes on memory and
  PostgreSQL (`just test-pg`, Docker).
- **Text contrast ≥ 4.5:1** in both themes, light and dark; focus rings and accessible names stay.
- **Programme does not change visually** (phase 2 moves code, it does not restyle).
- **Conventional Commits** in English, ending with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **Lint locally** with golangci-lint v2.14.0 (CI's version): `PATH=$HOME/go/bin:$PATH just lint`.
- **Parallel sessions:** several sessions run in sibling worktrees at once, and Playwright reuses a
  server already on its port. Run e2e on a port of your own worktree:
  `E2E_PORT=$((4200 + $(pwd | cksum | cut -d' ' -f1) % 700)) just e2e`.

## Review focus

1. **A school without a background but with theme Werkstatt** must look finished (a plain
   warm-white page), not show a broken image or a dark gap. Owned by 3.1 (Shell/background test).
2. **A visitor whose cached theme differs from the school's** (theme changed by an admin, or the
   other demo school) must end on the school's theme without a reload and without a crash while
   the chunk loads. Owned by 2.1 (ThemeProvider test).
3. **A member of a family with several people** pressing Werkstatt's square "book" button must not
   book the wrong person: with more than one bookable person the button opens the lesson sheet.
   Owned by 2.1 (`useLessonQuickAction` test) and 4.1 (card test).
4. **A lesson in a room that no other lesson of the day uses, or a day with one room only**, must
   not produce empty or squeezed room columns in Werkstatt's desktop schedule. Owned by 4.1.
5. **A returning demo visitor** who had data in today's (Programme) demo must keep it after the
   per-school records arrive (`current` becomes the Programme record). Owned by 2.3
   (persistence test).

---

## Checklist (one ccbatch session each)

- [x] 1.1 `backend` Theme setting, background image, demo school seed variant, `seed --school`, ADR 0018 — see "Task 1.1" below.
  - Done: the interfaces as listed; 15 new Go test functions plus the storetest cases
    `Settings/Theme` and `Settings/Background` (memory and PostgreSQL), and the new fields and the
    three operations in `internal/app` (`TestSettingsCards`, `TestSchoolBackground`, an exactly 1 MiB
    upload included). `just test`, `just lint`, `just test-pg`, `just check-generated`, `just check-sqlc`,
    `just check-wasm-deps` and `just web-check` (1028 web unit tests) green.
  - Deviations: `settings.theme` is nullable (NULL = the zero theme) instead of `NOT NULL DEFAULT ''`,
    following 00001's "optional values are NULL"; the background constraints are named like the logo's
    (`settings_background_type`, `settings_background_digest`). The upload is a JSON body
    `{contentType, data}` with base64 data, because that is what `updateReceiptLogo` really takes (the
    plan's "raw body" was wrong about it); it sets `MaxBodyBytes` (~1.4 MiB), since Huma's 1 MiB default
    refuses a base64 1 MiB image with a 413. `Background.Validate` also checks the magic bytes, like the
    logo's.
  - For 2.2: `updateBrandingSettings` without `theme` keeps the stored theme (`service.Branding.Theme`
    is a pointer); `""` is a 422. `getBrandingSettings` always answers `theme` (`OrDefault`).
  - For 2.3: `seed.School("")` is invalid (the caller maps an empty choice to programme); `cmd/wasm`
    still seeds programme. The werkstatt school keeps the programme school's legal notice, receipt
    issuer ("Studio Demo Dance School") and English receipts, because only the listed settings change;
    the image adds ~130 KB to `app.wasm`. `docs/demo-mode.md` already said `seed` uses `LoadDemo`
    (it uses `LoadDemoAt`); left for 2.3.
  - `misspell` (US) flags "programme": added to its `ignore-rules` in `.golangci.yml`.

- [x] 2.1 `slots` Theme registry, typed slots, ThemeProvider, theme cache script, Programme moved behind the slots without visual change, `useLessonQuickAction` — see "Task 2.1" below.
  - Done: `web/src/themes` (`slots.ts`, `registry.ts`, `useSlot.ts` with `slotOf`, `themeContext.ts`,
    `themeCache.ts`, `ThemeProvider.tsx`, `programme/` with all seven slots); containers in
    `RootLayout` (+ `useShellHeader` in `AppHeader.tsx`), `ScheduleList`, `HomePage`, `AccountPage`,
    `ParticipantsSection`; `TeachList` uses the `DayHeading` slot too (`list/DayHeading.tsx` is gone);
    booking rules shared in `booking/bookingRules.ts`. 33 new Vitest cases (1061 in total),
    `just web-check` and `just e2e` (196) green. Before/after screenshots (home, schedule, lesson sheet,
    account, office participants; 1280×900 and 390×844, light and dark; clock frozen in page and
    worker): 20 of 20 pixel-identical.
  - Deviations: `useLessonQuickAction(lesson, timeZone, now)` returns `{ action, dialog }` (a hook
    cannot render the cancel confirmation; the list's server clock instead of one timer per card).
    `HomeProps.renderNextLessons(className)`, `ProfileProps`/`ParticipantsProps` are discriminated
    by `state`; `ShellProps` has `announcements` and `header.language` (today's DOM order). The
    ThemeProvider sits in `RootShell`'s non-bare layout, not around the whole router; bare routes
    drop `data-theme`, and theme.js skips `#/about`. A failed theme load shows Programme
    (`console.warn`) and is not cached. `AppHeader.test.tsx`/`branding.test.tsx` got a 3-line
    test-local `AppHeader` (header hook + Programme's `Header`); their cases are unchanged.
  - Discovery: `QuickAction` `full` is never produced: a full lesson open for booking always has a
    waiting list (`bookable` in `internal/service/booking.go`, no setting turns it off); the member
    stays for the slot contract. The sheet offers "cancel" on a canceled, not yet started lesson; the
    quick action answers `none` there.
  - Residual: signed-in schedule visitors now load `/family` once (shared by the cards), also in
    Programme. A Programme school with a background still downloads it. Rendering a slot needs
    `eslint-disable-next-line react-hooks/static-components` (one comment per container).
  - For 2.2: background upload/remove must invalidate `schoolBackgroundQuery` and the public
    settings. A switch to a non-Programme theme shows the loading state and remounts the page.
  - For 3.1: replace registry's inline empty `werkstatt` theme with `import('./werkstatt')`.
  - For 5.1: with a cached non-Programme theme, "Starting demo backend…" appears only after the
    theme loads ("Loading the school's look…" first), so `gotoDemo`'s `toBeHidden` may pass early.

- [x] 2.2 `branding-ui` Branding card: theme select and background image upload/remove — see "Task 2.2" below.
  - Done: the theme is a select in the Branding form, with the selected theme's line as its hint (the
    tax mode's pattern). The office sees it as a fact. The background section shows the preview from
    `getSchoolBackground` (fetched only when `hasBackground`), and for an admin upload/replace/remove
    like the receipt logo (JPEG/PNG/WebP, ≤ 1 MiB checked before sending; a server 422 on
    `body.contentType`/`body.data` shows the same message; the error and hint are the file input's
    description). New in `queries.ts`: `schoolBackgroundQuery` (`['settings', 'background']`,
    public, for 2.1's `--school-background` too), `updateBrandingBackgroundMutation`, and
    `deleteBrandingBackgroundMutation`, which invalidate the branding, public-settings and background
    queries. 15 tests in `BrandingSettingsCard.test.tsx`, one e2e case in `settings.spec.ts` that
    uploads the seed's real JPEG through the WASM bridge. `just web-check` green (1043 web unit tests).
  - An unchanged theme is left out of the PUT body, so an accent-only save never overwrites a theme
    another admin changed meanwhile; the existing Branding tests' bodies stay `{ accentColor }`.
  - The `branding` fixture in `SettingsCards.test.tsx` lacked `theme`/`hasBackground` (stale since
    1.1); with the theme in the form's schema it must carry them, so it does now.
  - de: the select is "Design", not "Erscheinungsbild", because the card already has that title and
    the form, region and combobox would all share one name. The Werkstatt line describes the look
    3.1/4.1 build; until then choosing it changes nothing visible.
  - `maxBackgroundBytes` is a constant in the card: `BrandingSettings` has no `…MaxBytes` field like
    the receipt settings' `logoMaxBytes`.

- [x] 2.3 `demo-schools` Two demo schools: `?demo=` param and stored choice, worker seeds the variant, one saved state per school, about page second button, banner switch link, README and docs/demo-mode.md — see "Task 2.3" below.
  - Done: the interfaces as listed, plus `seed.ParseSchool` (empty/unknown → programme, `ok` false;
    `cmd/wasm` warns for an unknown value only), `wasmbridge.SnapshotBuild(version, school)` (the
    snapshot's `build` header is `"<version> (<school>)"`, so the memory store refuses another
    school's snapshot as `ErrSnapshotMismatch` without a store change), `persistence.snapshotKey`,
    an `init` worker message (`WasmBridge` posts it first; the worker waits for it before it claims),
    and `transport.switchSchool`, which flushes the worker before `switchDemoSchool` reloads (the
    250 ms save debounce would otherwise lose a change made just before a switch). Tests: 4 new Go
    tests (`TestWerkstattOwnIdentity`, `TestParseSchool`, `TestSnapshotBuild`,
    `TestSnapshotOfAnotherSchool`), 2 updated; web unit tests 1058 (school 11, persistence +5,
    bridge +2, transport 4, AboutPage +4, DemoBanner 4); e2e 202 (about.spec +3 per project).
    `just test`, `just lint`, `just check-wasm-deps`, `just wasm-smoke`, `just check-formatted`,
    `pnpm -C web check`/`test` and `just e2e` green.
  - Seed: the werkstatt school has its own imprint, issuer "Tanzhaus am Lindenhof" (Lindenhofweg 12,
    10999 Beispielstadt), German footer and prefix `TL`; `demoAt` now takes the school's settings,
    so its seeded receipts are German and its own (programme output unchanged). Residual: the pass
    type names and the cancellation reason in those receipts stay English (shared data).
  - Discoveries: transport reads the school once per page load (`pageSchool`), so a reset in a tab
    whose stored choice another tab changed still clears the school that tab runs. The offline
    query cache's buster carries the school in the demo, so a switch never shows the other school's
    cached answers. The e2e fixture's `demoSchool` init script sets the key only if absent (init
    scripts rerun on every reload and would undo a switch). The e2e names in the Werkstatt school
    accept English or German (its language is `de`); 5.1 asserts `data-theme`.
  - Programme snapshots saved by an earlier build carry the plain version and are refused once; every
    release changes the version anyway.

- [x] 3.1 `werkstatt-shell` Werkstatt theme chunk: tokens light/dark, Roboto Condensed, school background, Shell (header, tabs, phone drop menu, page panel), dialog styling — see "Task 3.1" below.
  - Done: `web/src/themes/werkstatt/` (`index.ts`, `werkstatt.css`, `Shell.tsx`), registered as
    `import('./werkstatt')`; the build guard in `vite.chunks.ts` also fails when the entry's chunks
    hold Werkstatt modules or load CSS with Roboto Condensed (checked by breaking it both ways). The
    chunk is 4.6 kB JS + 5.7 kB CSS, plus six woff2 files. Tests: `tokens.test.ts` (75: every text
    pair ≥ 4.5:1 in light and dark, translucent surfaces composited; the dark block redefines every
    light token; selectors and `--school-background` without fallback pinned), `Shell.test.tsx`
    (12), registry +1, `AppHeader.test.tsx` +2 (session); web unit tests 1198. New
    `web/e2e/werkstatt.spec.ts` (7 cases, 12 runs on the two projects: font and background, a school without a
    background is plain `#fbfaf8` (review focus 1), header sign-in/out, account-menu sign-out,
    dialog scrim, desktop tabs, phone menu); `just e2e` 218 passed, 2 skipped (the desktop-only and
    phone-only cases). `pnpm -C web check`, `just check-formatted` green.
  - Deviations: `ShellProps.header.session` (`unknown | signedOut | signedIn` with `signOut`) for
    the "» Anmelden/Abmelden" button, since slots may not call mutations; `useSignOut`
    (`components/layout/useSignOut.ts`) is shared with the account menu. Signed out, Werkstatt
    shows its button instead of `header.account`; signed in, the account menu and "» Abmelden".
    Colours changed for contrast: muted text `#5d6b7f` (`#66758a` is 4.49:1 on `#fbfaf8`), today
    `#d81b6f` with white (4.9:1; `#f8398a` is 3.5:1, and My lessons, which has no Werkstatt slot,
    puts 18px medium text on it), dark destructive `#f26f67`, dark text on mint/brown
    (`--werkstatt-*-foreground`). The fonts are own `@font-face` rules (latin, latin-ext; woff2).
    The ui components needed no change (their `data-slot`s exist). The header is not sticky.
  - Discoveries: `@theme inline` writes the font families into `font-sans`/`font-heading`, so
    overriding `--font-sans` changes nothing; werkstatt.css sets them on the page and the two
    utilities. fontsource's per-subset files (`latin-300.css`) have no `unicode-range`, so importing
    two lets the later hide the earlier. The token blocks are 0,1,0 (`[data-theme='werkstatt']`,
    `:where(.dark)[…]`) and win over index.css by load order while the accent's `html:root`
    (0,1,1) still wins; the day bands are 0,2,0+ so the accent does not repaint today. Under
    Vitest's `css: false`, `import x from './a.css?raw'` is `""` (tokens.test reads the file).
  - Residual: over a very dark photo, muted text on the bare panel is about 3:1 (the panel and
    wash opacities are the spec's); iOS Safari ignores `background-attachment: fixed`.
  - For 4.1: cancel text goes on `--werkstatt-red-dark` (white on `--werkstatt-red` is 4.0:1);
    mint and brown carry `--werkstatt-mint-foreground`/`-brown-foreground` (dark in light mode).
  - For 5.1: `web/e2e/werkstatt.spec.ts` exists; extend it. It sets the visitor's language to
    English (`studio-schedule.lang`) so the shared helpers' names apply.

- [x] 4.1 `werkstatt-schedule` Werkstatt ScheduleList, DayHeading and LessonCard (day bands, room columns, square action button) — see "Task 4.1" below.
  - Done: `web/src/themes/werkstatt/` `ScheduleList.tsx`, `roomColumns.ts`, `DayHeading.tsx`,
    `LessonCard.tsx`, `courseTint.ts`, registered in `index.ts`; strings under `schedule:werkstatt.*`
    (en, de). Tests: ScheduleList 12 (room columns, one room, a room of another day, order by start,
    location then room, "Room · Location" with two locations, phone single list, each lesson rendered
    once, prevId/nextId; `roomColumns` 3), DayHeading 6, LessonCard 23 (each quick action, review
    focus 3: `open` → "Details" calls `onOpen`, never `run`; star and frame; German), courseTint 3,
    tokens +36 (`--werkstatt-green` pairs; card text on all six tints), registry pins the four own
    slots; web unit tests 1278. `werkstatt.spec.ts` +3 cases (desktop room columns, each with lessons
    and ≥ 13rem wide; phone one list; a guardian's square button is "Details" and opens the sheet).
    `just web-check` green; `just e2e` 220 passed, 4 skipped, 2 failed — `pwa.spec.ts` offline case
    only, a time-of-day flake (see below) that fails the same way on the base commit. Chunk
    11.8 kB JS + 6.6 kB CSS.
  - Deviations: the card's muted line is "room · location": the API has no course description
    (`CourseTypeRef` is id and name). "Participants: taken / capacity" uses `capacity - freePlaces`, so
    a place an invite holds counts as taken. The card is a `group` named like its body button, so
    "Book" can be found per card. The frame is `border-primary` + `ring-1` (2px, no layout jump). With
    many rooms a day scrolls sideways (columns ≥ 13rem) instead of squeezing (review focus 4).
  - Discoveries: the app uses a hash router in the demo, so a plain `href="#id"` would change the
    route; the day arrows keep the href but scroll to and focus the heading (`tabIndex=-1`). The demo
    **member is a guardian**: every Werkstatt card shows "Details", never "Book" (review focus 3 as
    intended). `pwa.spec.ts`'s offline case picks a card with "N free"; Programme shows free places on
    a lesson that has started but not ended, whose sheet says "has started" without a Book button, so
    it fails whenever the first such card is a running lesson (Saturday 10:00–10:45, then
    10:00–11:30 in the seed: failing at 10:39 and 10:49); unrelated to this task.
  - For 5.1: "book with the card's Eintragen button" needs an account that books for itself only (the
    demo member has dependents), or book in the sheet; `werkstatt.spec.ts` has the schedule cases.
  - For 4.2: Home can reuse `DayHeading`/`LessonCard` through `useSlot`; the card needs a
    `quickAction` (`useLessonQuickAction`) from its container.

- [x] 4.2 `werkstatt-pages` Werkstatt Home, Profile and Participants slots — see "Task 4.2" below.
  - Done: `web/src/themes/werkstatt/{Home,Profile,Participants}.tsx`, registered in `index.ts`;
    `HomeProps.myLessons` (`HomeMyLessons`: `unknown | signedOut | loading | error | ready` with
    `days: ScheduleDay[]` and `renderLesson`), filled by `features/home/useMyLessons.tsx`: the lessons
    of the coming 7 days the visitor or a dependent is booked for (`bookedFor`) that have not ended,
    canceled ones included, on the server's clock; the card is the schedule's `ScheduleLessonCard`
    (now exported, LessonCard slot + quick action) and opens the lesson in the schedule. Same query
    key as the next lessons' teaser, so Programme asks no more than before; Programme ignores the
    field. Tests: Home 7 + `HomePage.test` +3 (container, with Werkstatt's Home in `ThemeContext`;
    signed out it asks for the schedule once and never for `/family`), Profile 9 (3 through the real
    `AccountPage`: the PUT body, a refused empty name), Participants 18, registry +1; 1236 web unit
    tests. `pnpm -C web check`, `pnpm -C web test`, `just check-formatted` green; `just e2e` 216
    passed, 2 skipped, 2 failed: `pwa.spec.ts` on both projects, a time-of-day flake (see below).
    The chunk is now 20.0 kB JS + 9.0 kB CSS.
  - Home: h1 the school's name; "Infos" (h2, light) with `home.intro` by a round `UserRound`
    avatar, the announcements and a short system status (backend, version for the office, time
    zone, retry on failure — the Programme status card's facts); "My schedule" with the DayHeading
    slot per day and a "Full schedule" link. While the account is unknown the right column is a
    skeleton without a heading, so a signed-out visitor never sees "My schedule" flash.
  - Profile: the connected cards unchanged, inside `.werkstatt-profile`; werkstatt.css restyles them
    (flat cards, 300-weight titles and labels, the person-data and password grids stacked, inputs
    and the password input group with the 1px `--input` border, focus and error border colours
    restored, mint/red buttons with an inset frame on hover and a 2px ring on focus). The h1 is
    `account:werkstatt.title` ("My details"/"Meine Daten"; `myData.title` is the export card).
    Residual: what the cards open in a portal (dependent sheet, delete confirmation) sits outside
    the wrapper and keeps the general Werkstatt dialog look, so the confirmation's delete button is
    Programme's tinted destructive.
  - Participants: one table (Present/Status, Name, Email, Phone, Pass, Trial lesson, actions);
    the checkbox marks attended/booked, a no-show and a reset button keep Programme's three states;
    history (office) and remove as icon buttons; the add button (mint) and "All attended" below;
    the canceled bookings as a second table, the waiting list as a list. In the lesson sheet the
    table scrolls sideways (only Present, Name and Email fit at 1280px).
  - Discovery: `Button` renders `data-variant="default"` for the default variant (the parameter
    default), so `[data-variant='default']` selects it. `pwa.spec.ts` "offline, the last schedule
    …" picks the first card with "N free" and fails while that lesson has started but not ended
    (Programme school: at 10:29 Creative Dance 10:00–10:45, rerun at 10:48 Classical Ballet
    10:00–11:30, both "This lesson has started", no Book button); not touched here.
  - For 4.1: Home renders the DayHeading slot and `renderLesson` (the LessonCard slot), so 4.1's
    look applies there once merged; nothing else to wire.
  - For 5.1: the Home spec can sign in as the demo member and expect "My schedule" with the
    booked lessons (the seed's member has several in the coming week); Profile's h1 is "My details".

- [x] 5.1 `werkstatt-e2e` Werkstatt end-to-end spec, CLAUDE.md code map, architecture docs, final visual comparison — see "Task 5.1" below.
  - Done: `werkstatt.spec.ts` +5 cases (day bands in their tones and the room row; booking with
    the card's "Book" and canceling with "Cancel" through the confirmation; home's "My schedule"
    with a booked lesson that opens in the schedule; the account page keeps a phone number; the
    office marks a participant present in the table, which fits the sheet on desktop), 15 cases,
    26 runs on the two projects (4 skipped: desktop-/phone-only), 3× repeated without a flake.
    Docs: CLAUDE.md "Themes" bullet, architecture.md "Themes", demo-mode.md names the spec.
    `just ci` green: e2e 232 passed, 4 skipped; `e2e-server` 7; 1320 web unit tests.
  - Deviation: the demo member is a guardian (always "Details"), so the booking case signs in by
    email as Irene Inkwell, a seed member without children.
  - Visual pass (no reference screenshots provided; 48 captures, desktop/phone, light/dark, against
    the old app's stylesheet, read only). Fixed: the 28rem lesson sheet hid the participants
    table's phone, pass, trial and remove columns (a sheet holding it is up to 48rem from 40rem,
    `:has(.werkstatt-participants)`; phones scroll); dark `--input` `#383838` was 1.5:1, the
    unchecked present boxes vanished (`#737073`, 3.5:1, pinned ≥ 3:1 in `tokens.test.ts`); the
    phone header's tools wrapped to the left under the menu (`ml-auto`). Closer to the old type:
    day band text centred, 18/24px; card titles light and larger, the time 18px regular; the room
    row taller. The chunk is now 27.5 kB JS + 10.0 kB CSS.
  - Discovery: a first visit without a cached theme renders Programme until the settings arrive,
    then the ThemeProvider remounts the page in Werkstatt and what was typed is gone (the phone
    project lost the login email); the spec waits for `data-theme` before typing. Seeding the
    theme cache from the demo's school choice would avoid it in the demo; not done here.
  - Residual (old look, not cheap or taste): the "» Anmelden" button's size and small caps, tab
    size and centring, a sticky room row, course tints in dark mode, framed Home cards, uppercase
    course titles on phones; light `--input` keeps the spec's `#abb0b2` (2.2:1).

---

## Task 1.1: backend

**Files:**

- Create: `internal/domain/theme.go`, `internal/domain/background.go` (+ tests next to the existing
  domain tests)
- Modify: `internal/domain/settings.go` (field `Theme`, validation)
- Create: `internal/store/postgres/migrations/00025_settings_theme_background.sql`
- Modify: the settings queries in `internal/store/postgres/queries` (find them with
  `grep -rn "logo_digest" internal/store/postgres/queries`), then `just sqlc`
- Modify: `internal/store/postgres/people.go` (settingsRepo), `internal/store/memory/memory.go`
  (settingsRepo), `internal/store/memory/snapshot.go`, `internal/store/store.go` (SettingsRepo)
- Modify: `internal/store/storetest/settings.go` (contract cases)
- Modify: `internal/service/settings.go` (+ `settings_test.go`)
- Modify: `internal/httpapi/settings.go` (+ tests), `internal/app/access_test.go`
- Modify: `internal/seed/seed.go`, `internal/seed/demo.go`, `internal/seed/seed_test.go`; embed
  `internal/seed/assets/werkstatt-background.jpg` (already committed) via `//go:embed`
- Modify: `cmd/studio-schedule/` (the `seed` command: `--school` flag)
- Create: `docs/decisions/0018-themes-and-layouts.md`
- Regenerate: `api/openapi.json`, `web/src/api/schema.d.ts` (`just gen-api`)

**Interfaces produced (later tasks rely on these exact names):**

```go
// internal/domain/theme.go
type Theme string

const (
	ThemeProgramme Theme = "programme"
	ThemeWerkstatt Theme = "werkstatt"
)

// Valid reports whether t is a known theme; the zero Theme (Programme) is valid.
func (t Theme) Valid() bool
// OrDefault returns t, or ThemeProgramme for the zero Theme.
func (t Theme) OrDefault() Theme

// internal/domain/background.go
type Background struct {
	ContentType string // BackgroundJPEG, BackgroundPNG or BackgroundWebP
	Data        []byte
}
const MaxBackgroundBytes = 1 << 20
const (
	BackgroundJPEG = "image/jpeg"
	BackgroundPNG  = "image/png"
	BackgroundWebP = "image/webp"
)
func (b Background) IsZero() bool
func (b Background) Digest() string   // same scheme as Logo.Digest
func (b Background) Validate() error  // field errors on "contentType" and "data"
type BackgroundInfo struct{ ContentType, Digest string }
func (b BackgroundInfo) IsZero() bool

// internal/store/store.go, SettingsRepo
Background(ctx context.Context) (domain.Background, error)
PutBackground(ctx context.Context, b domain.Background) error // zero removes it
BackgroundInfo(ctx context.Context) (domain.BackgroundInfo, error)

// internal/seed
type School string
const (
	SchoolProgramme School = "programme"
	SchoolWerkstatt School = "werkstatt"
)
func (s School) Valid() bool
func LoadDemoSchoolAt(ctx context.Context, st store.Store, anchor domain.Date, school School) error
// LoadDemoAt(ctx, st, anchor) == LoadDemoSchoolAt(ctx, st, anchor, SchoolProgramme)
// LoadDemo(ctx, st) unchanged.
```

HTTP (all camelCase, then `just gen-api`):

- `getPublicSettings` gains `theme` (string enum `programme|werkstatt`, always set, `OrDefault`)
  and `hasBackground` (bool).
- `getBrandingSettings` gains `theme` and `hasBackground`; `updateBrandingSettings`' body gains
  optional `theme` (an unknown value → 422 on `body.theme`).
- `getSchoolBackground`: `GET /api/v1/settings/background`, `public`, answers the image with its
  content type, `ETag` = digest, `Cache-Control: public, no-cache`, 304 on `If-None-Match`, 404
  without one. Model it exactly on `getSchoolLogo` in `internal/httpapi/settings.go`.
- `updateBrandingBackground`: `PUT /api/v1/admin/settings/branding/background`, `admin`, JSON body
  `{contentType, data}` with base64 `data` (as built in 1.1, like `updateReceiptLogo`), 204; errors
  as 422 field errors (`body.contentType`, `body.data`).
- `deleteBrandingBackground`: `DELETE /api/v1/admin/settings/branding/background`, `admin`, 204.

**Steps:**

1. Domain tests first: `Theme.Valid` (programme, werkstatt, "" valid; "neon" invalid),
   `OrDefault`; `Settings.Validate` rejects `Theme("neon")` with a field error on `theme`;
   `Background.Validate` accepts JPEG/PNG/WebP up to 1 MiB, rejects GIF (`contentType`), empty and
   1 MiB + 1 byte (`data`); `Digest` equals the Logo scheme for the same bytes/type. Run
   `go test ./internal/domain`, see them fail; implement; see them pass; commit.
2. Migration `00025_settings_theme_background.sql` (goose, with Down):

   ```sql
   -- +goose Up
   ALTER TABLE settings
       ADD COLUMN theme text NOT NULL DEFAULT '' CHECK (theme IN ('', 'programme', 'werkstatt')),
       ADD COLUMN background bytea,
       ADD COLUMN background_type text CHECK (background_type IN ('image/jpeg', 'image/png', 'image/webp')),
       ADD COLUMN background_digest text CHECK (background_digest ~ '^[0-9a-f]{64}$'),
       ADD CONSTRAINT settings_background_complete CHECK (
           (background IS NULL AND background_type IS NULL AND background_digest IS NULL)
           OR (background IS NOT NULL AND background_type IS NOT NULL AND background_digest IS NOT NULL));
   -- +goose Down
   ALTER TABLE settings
       DROP CONSTRAINT settings_background_complete,
       DROP COLUMN background_digest, DROP COLUMN background_type,
       DROP COLUMN background, DROP COLUMN theme;
   ```

   Check the naming of the logo columns in the earlier migrations and follow it exactly; update the
   settings queries (`theme` in read/write; new `GetBackground`, `GetBackgroundInfo`,
   `PutBackground`), `just sqlc`.

3. storetest cases in `internal/store/storetest/settings.go`, next to the logo cases: theme
   round-trips (zero, programme, werkstatt); background put/get/info/remove; info without reading
   the bytes; `PutBackground` of the zero value on none is fine. Implement memory (+ snapshot field;
   `TestSnapshotCoversEveryTable` and `TestSnapshotRoundTrip` must pass) and PostgreSQL.
   `go test ./internal/store/...` and `just test-pg`.
4. Service: `SettingsService.Background(ctx, has func(digest string) bool)` mirroring `Logo`
   (returns a cached value type like `CachedLogo`), `PutBackground(ctx, b)` validating first; theme
   flows through the branding update. Tests in `internal/service/settings_test.go`: put, get,
   not-modified, remove, invalid type → field error, theme update persists and is public.
5. HTTP: the three operations and the new fields; pin each in `internal/app/access_test.go`
   (`getSchoolBackground` public, the other two admin). httpapi tests: public settings show
   `theme: "programme"` on a fresh store and `hasBackground`; upload/download round trip with ETag
   and 304; 422s; 403 for office on PUT/DELETE. `just gen-api`.
6. Seed variant: `School` type; `LoadDemoSchoolAt`. For `SchoolWerkstatt` change only the settings:
   `SchoolName: "Tanzhaus am Lindenhof"` (fictional), `Theme: domain.ThemeWerkstatt`,
   `AccentColor: "#9c215b"` and its derived dark variant (use the same derivation as the Branding
   update does), `DefaultLocale: "de"`, and `PutBackground` with the embedded
   `internal/seed/assets/werkstatt-background.jpg`. People, classes, rooms and history stay
   identical. Tests: the werkstatt variant loads into a memory store; settings show the theme,
   accent, locale and `hasBackground`; the programme variant is today's data (existing tests
   unchanged); `School("x").Valid()` is false and `LoadDemoSchoolAt` refuses it.
7. `studio-schedule seed --school programme|werkstatt` (default programme), refusing anything else;
   a command test if the package has them.
8. ADR `docs/decisions/0018-themes-and-layouts.md` in the style of 0013/0017: context (two looks,
   one app), decision (school setting, background stored like the logo, slot registry with
   Programme fallback, no per-visitor themes, demo school variant), consequences.
9. `just test`, `PATH=$HOME/go/bin:$PATH just lint`, `just check-generated`, `just check-sqlc`,
   `just test-pg`. Commit in logical commits (`feat(settings): …`, `feat(seed): …`, `docs: …`).

The background photo `internal/seed/assets/werkstatt-background.jpg` and its licence note
`internal/seed/assets/README.md` are committed with this plan; don't replace them.

## Task 2.1: slots

**Files:**

- Create: `web/src/themes/slots.ts`, `web/src/themes/registry.ts`, `web/src/themes/ThemeProvider.tsx`,
  `web/src/themes/useSlot.ts`, `web/src/themes/themeCache.ts`, tests next to them
- Create: `web/src/themes/programme/index.ts` and move the Programme slot components into
  `web/src/themes/programme/` (`Shell.tsx`, `ScheduleList.tsx`, `DayHeading.tsx`, `LessonCard.tsx`,
  `Home.tsx`, `Profile.tsx`, `Participants.tsx`)
- Create: `web/src/features/schedule/booking/quickAction.ts` (+ test)
- Modify: `web/src/components/layout/RootLayout.tsx`, `web/src/features/schedule/list/ScheduleList.tsx`,
  `web/src/features/home/HomePage.tsx`, `web/src/features/account/AccountPage.tsx`,
  `web/src/features/schedule/participants/ParticipantsSection.tsx`, `web/src/main.tsx`
- Modify: `web/public/theme.js` (apply the cached theme before first paint)
- Modify: `web/src/components/layout/branding.ts` (background CSS variable)

**Interfaces produced:**

```ts
// web/src/themes/slots.ts
import type { ComponentType, ReactNode } from "react";
export type ThemeId = "programme" | "werkstatt"; // must equal the generated API enum
export interface Slots {
  Shell: ComponentType<ShellProps>;
  ScheduleList: ComponentType<ScheduleListProps>;
  DayHeading: ComponentType<DayHeadingProps>;
  LessonCard: ComponentType<LessonCardProps>;
  Home: ComponentType<HomeProps>;
  Profile: ComponentType<ProfileProps>;
  Participants: ComponentType<ParticipantsProps>;
}
export interface Theme {
  id: ThemeId;
  slots: Partial<Slots>;
}
```

Define each `…Props` as the view model plus callbacks the current component needs, computed by
its page/container. Rules: no slot calls `useQuery`/`useMutation` or the API; slots may use
`useTranslation`, router `Link`, shadcn components and pure helpers. Minimum contents (extend as
the moved code requires, keep the names):

- `ShellProps`: `{ children: ReactNode; banners: ReactNode; header: { schoolName?: string;
logoUrl?: string; nav: NavItem[]; tools: ReactNode; account: ReactNode }; footer: ReactNode }`,
  `NavItem = { to: string; label: string; icon: LucideIcon }`.
- `ScheduleListProps`: `{ days: ScheduleDay[]; timeZone: string; now: number; empty?: ReactNode;
onOpen(lessonId: string): void }`, `ScheduleDay = { date: SchoolDate; tone: DayTone;
lessons: ScheduleLesson[] }`.
- `DayHeadingProps`: `{ id: string; date: SchoolDate; tone: DayTone; prevId?: string; nextId?: string }`.
- `LessonCardProps`: `{ lesson: ScheduleLesson; timeZone: string; now: number; past: boolean;
quickAction: QuickAction; onOpen(): void }`.
- `HomeProps`, `ProfileProps`, `ParticipantsProps`: the view models of today's components.

```ts
// web/src/features/schedule/booking/quickAction.ts
export type QuickAction =
  | { kind: "none" } // signed out, past, canceled, closed
  | { kind: "open" } // more than one bookable person: open the sheet
  | {
      kind: "book" | "cancel" | "waitlist";
      personId: string;
      run(): void;
      pending: boolean;
    }
  | { kind: "full" };
export function useLessonQuickAction(lesson: ScheduleLesson): QuickAction;
```

`useLessonQuickAction` reuses the existing booking mutations and rules of `BookingSection` (extract
shared pieces rather than duplicate them). `cancel` goes through the existing
`CancelBookingDialog` confirmation (late-cancel notice included), so `run()` opens that dialog.

```ts
// web/src/themes/registry.ts
export const themeIds: readonly ThemeId[];
export function loadTheme(id: ThemeId): Promise<Theme>; // programme eager, others import()
// web/src/themes/useSlot.ts
export function useSlot<K extends keyof Slots>(name: K): Slots[K]; // falls back to Programme
// web/src/themes/themeCache.ts
export const THEME_CACHE_KEY = "studio-schedule-theme";
export function cachedTheme(): ThemeId | undefined; // try/catch around storage
export function cacheTheme(id: ThemeId): void;
```

`ThemeProvider` wraps the router's root (inside `QueryClientProvider`): it takes the theme from the
public settings when the backend is ready, else `cachedTheme()`, else `programme`; loads it; sets
`document.documentElement.dataset.theme`; caches it; renders children with the loaded theme in
context. While a non-Programme chunk loads it renders the existing loading state (the
`BackendGate` spinner style), never Programme's chrome. `web/public/theme.js` additionally sets
`data-theme` from `localStorage['studio-schedule-theme']` (guarded by try/catch) before first
paint.

The background: `useApplyBranding` (or a sibling hook) loads `getSchoolBackground` like the logo
(`useDataUrl`) when `hasBackground`, and sets `--school-background: url(…)` on
`document.documentElement`; removes it otherwise. Programme ignores the variable.

**Steps:**

1. Before touching code, take reference screenshots of Programme: build the demo
   (`just demo-build`), and with Playwright capture `#/`, `#/schedule`, a lesson sheet,
   `#/account` and a lesson's participants as office, at 1280×900 and 390×844, light and dark, into
   a `programme-before/` folder in your session's scratch dir (not the repo).
2. Tests first for `themeCache`, `registry` (`loadTheme('programme')` resolves; ids match the API
   enum), `useSlot` fallback (a fake theme with only `DayHeading` gets Programme's `LessonCard`),
   `ThemeProvider` (cached theme used before settings; school's theme wins after; `data-theme`
   updated; switching from a cached `werkstatt` to school `programme` without a reload — review
   focus 2; until Werkstatt exists, register a test theme through a test-only registry override).
3. `useLessonQuickAction` tests (Vitest with `mockApi`): signed out → none; past/canceled/closed →
   none; one person, not booked, free places → book (run books that person); booked → cancel (run
   opens the dialog); full with waitlist → waitlist; full without → full; two bookable people →
   open (review focus 3).
4. Move Programme behind the slots one slot at a time (Shell, then ScheduleList/DayHeading/
   LessonCard, Home, Profile, Participants). After each, run the affected Vitest files; existing
   tests must pass unchanged except for import paths.
5. Retake the screenshots into `programme-after/` and compare pixel-wise (e.g. `pixelmatch` in a
   scratch script); differences must be zero or explained (animations, clocks). Report the
   comparison in the PR body.
6. `just web-check`, `just e2e` (whole suite unchanged and green). Commit.

## Task 2.2: branding-ui

**Files:**

- Modify: `web/src/features/admin/BrandingSettingsCard.tsx` (+ test), `web/src/api/queries.ts`
- Modify: `web/src/locales/{en,de}/settings.json`

**Steps:**

1. Tests first (`BrandingSettingsCard.test.tsx` with `mockApi`): the card shows a theme select with
   "Programme" and "Werkstatt" and one descriptive line each; saving sends `theme`; office sees it
   read-only (as the rest of the card); background upload sends `PUT …/branding/background` with
   the JSON body `{contentType, data}` (base64, exactly as the receipt logo upload sends it — see
   1.1's notes); omitting `theme` keeps the stored one; a 422 shows as a field error; "Remove" sends `DELETE`; the preview
   shows the current background (`getSchoolBackground`) when `hasBackground`; after save/remove the
   public settings query is invalidated.
2. Implement following the receipt logo upload in `web/src/features/admin/ReceiptSettingsCard.tsx`
   (file input, type/size checks before upload: JPEG/PNG/WebP, ≤ 1 MiB, same message style).
   Strings en/de (de: "Erscheinungsbild", "Programm", "Werkstatt", "Hintergrundbild").
3. `just web-check`. Commit.

## Task 2.3: demo-schools

**Files:**

- Create: `web/src/features/demo/school.ts` (+ test)
- Modify: `web/src/main.tsx` (read `?demo=` before the router starts), `web/src/api/transport.ts`,
  `web/src/api/wasm/worker.ts` (`go.env.DEMO_SCHOOL`), `web/src/api/wasm/persistence.ts`
  (record per school), `cmd/wasm/main.go` (seed the variant)
- Modify: `web/src/features/demo/AboutPage.tsx` (+ test), `web/src/components/layout/DemoBanner.tsx`,
  `web/src/locales/{en,de}/about.json`, `web/src/locales/{en,de}/common.json`
- Modify: `web/e2e/about.spec.ts`, `web/e2e/fixtures.ts` (option `demoSchool`, default programme)
- Modify: `README.md`, `docs/demo-mode.md`

**Interfaces produced:**

```ts
// web/src/features/demo/school.ts
export type DemoSchool = "programme" | "werkstatt";
export const DEMO_SCHOOL_KEY = "studio-schedule-demo-school";
export function demoSchool(): DemoSchool; // stored choice, default programme
export function chooseDemoSchool(s: DemoSchool): void; // stores it (try/catch; in-memory fallback)
export function takeDemoParam(url: URL): DemoSchool | undefined; // reads and strips ?demo=
export function switchDemoSchool(s: DemoSchool): void; // stores, marks entered, reloads to #/
```

Go side: `cmd/wasm/main.go` reads `os.Getenv("DEMO_SCHOOL")`, maps it to `seed.School` (unknown or
empty → programme, with a log line), and calls `seed.LoadDemoSchoolAt`. The snapshot header gets the
school; restoring a snapshot of another school is refused like a version mismatch.

Persistence: the IndexedDB record key becomes per school: `current` stays the Programme record
(existing visitors keep their data — review focus 5), `current-werkstatt` is Werkstatt's. Reset
clears only the current school's record. The tab lock name stays `studio-schedule-demo`.

**Steps:**

1. Tests first: `school.test.ts` (default, stored, invalid stored value → default, param read and
   stripped with `history.replaceState`, storage that throws); persistence tests (programme uses
   `current`, werkstatt uses `current-werkstatt`, reset clears only the current one, an existing
   `current` record restores into programme); the env → `seed.School` mapping as a small pure Go
   function with a unit test.
2. Implement. In `main.tsx`, `takeDemoParam(new URL(location.href))` runs before
   `hasEnteredDemo()`; a `?demo=` link counts as an explicit choice, stores the school, and enters
   the demo (skips the about page) — the README links are meant to show that school directly.
3. About page: the hero shows two "open" buttons ("Open the demo" → programme; "Open in the
   Werkstatt look" → werkstatt) and, in "How the demo works", one line on the two schools; the
   Werkstatt button has a small preview chip in its colours (plum `#9c215b`, mint `#74b391`; plain
   CSS, no images). Banner: a link "Switch to the Werkstatt look" / "Switch to the Programme look"
   that calls `switchDemoSchool`. Strings en/de.
4. e2e: `fixtures.ts` gets `demoSchool: ['programme', { option: true }]` setting
   `studio-schedule-demo-school` in the init script; `about.spec.ts` gains: the Werkstatt button
   opens a school named "Tanzhaus am Lindenhof" (header; `data-theme` comes from 2.1, which runs in
   parallel, so don't assert it here — 5.1 does); the banner switch goes back to Programme; a booking made in Programme is still there after switching
   to Werkstatt and back (separate records).
5. Seed: the werkstatt school must not keep the programme school's identity. In
   `internal/seed` give it its own fictional legal notice / imprint and receipt issuer ("Tanzhaus
   am Lindenhof", a fictional address) and German receipts (`de`), with a seed test asserting that
   no programme school name ("Studio Demo") appears in the werkstatt school's settings. Also fix
   docs/demo-mode.md's note that `seed` uses `LoadDemo` (it uses `LoadDemoAt`).
6. README "Live demo": both links (`…/studio-schedule/` and `…/studio-schedule/?demo=werkstatt`), one
   line each. docs/demo-mode.md: a section "Two demo schools" (param, stored choice, records, reset,
   tab lock).
7. `just test`, `just web-check`, `just wasm-smoke`, `just e2e`. Commit.

## Task 3.1: werkstatt-shell

**Files:**

- Create: `web/src/themes/werkstatt/index.ts`, `web/src/themes/werkstatt/werkstatt.css`,
  `web/src/themes/werkstatt/Shell.tsx` (+ test), `web/src/themes/werkstatt/tokens.test.ts`
- Modify: `web/src/themes/registry.ts` (register `werkstatt` as `import('./werkstatt')`),
  `web/package.json` (`@fontsource/roboto-condensed`), `web/vite.chunks.ts` if the chunk guard needs
  to know the new lazy chunk
- Modify: `web/src/components/ui/dialog.tsx` / `sheet.tsx` / `alert-dialog.tsx` only by adding
  `data-slot` hooks if missing (no visual change for Programme)

**Design (values from the spec; adjust only for contrast):**

```css
/* web/src/themes/werkstatt/werkstatt.css, imported only by the werkstatt chunk */
@import "@fontsource/roboto-condensed/300.css";
@import "@fontsource/roboto-condensed/400.css";
@import "@fontsource/roboto-condensed/700.css";

:root[data-theme="werkstatt"] {
  --font-sans: "Roboto Condensed", sans-serif;
  --font-heading: "Roboto Condensed", sans-serif;
  --radius: 0.1875rem; /* 3px */
  --background: #fbfaf8;
  --foreground: #354052;
  --card: rgb(255 255 255 / 80%);
  --primary: #9c215b;
  --primary-foreground: #ffffff;
  --muted-foreground: #66758a; /* #7f8fa4 darkened to reach 4.5:1 on white */
  --border: #dfe2e5;
  --input: #abb0b2;
  --ring: #9c215b;
  --destructive: #c32f25;
  --day-today: #f8398a; /* verify contrast of its text */
  --day-today-foreground: #ffffff;
  --day-tomorrow: #fbec19;
  --day-tomorrow-foreground: #9c215b;
  --day-later: #68b3c9;
  --day-later-foreground: #10333d;
  --werkstatt-mint: #74b391; /* book */
  --werkstatt-red: #dc5047; /* cancel */
  --werkstatt-red-dark: #c32f25;
  --werkstatt-brown: #b39474; /* waiting list */
  --werkstatt-panel: rgb(255 255 255 / 50%);
  --werkstatt-scrim: rgb(50 32 14 / 40%);
}
:root.dark[data-theme="werkstatt"] {
  --background: #141214;
  --foreground: #ececec;
  --card: rgb(28 26 28 / 85%);
  --primary: #f8398a;
  --primary-foreground: #1a0a11;
  --border: #383838;
  --input: #383838;
  --werkstatt-mint: #3a7053;
  --werkstatt-panel: rgb(20 18 20 / 70%);
}
```

The school's accent (`accentStyleSheet` in `branding.ts`, `html:root`/`html.dark`) must still win
over these values; check the selector specificity and adjust the Werkstatt selectors if needed.
Where white text on `--day-today` or the mint/red buttons fails 4.5:1, use the darker shade
(`#871d4f`, `#c32f25`) or bold ≥ 18.66px text (WCAG large text, 3:1), and note the decision in a
CSS comment. `tokens.test.ts` computes the contrast of every text/background pair above with a WCAG
helper (reuse `web/src/lib/color.ts` if it has one) and fails below the threshold.

**Shell** (`ShellProps` from Task 2.1):

- `body` background: `var(--school-background)` fixed, `center / cover`, under a white wash
  (`linear-gradient(rgb(255 255 255 / 55%), rgb(255 255 255 / 55%))`; dark: a black wash); without
  a background a plain `--background` (review focus 1).
- Header: translucent white bar (`rgb(255 255 255 / 87%)`, backdrop blur), school logo or name
  left (no Programme ornament), right a solid primary button in small caps: "» Anmelden" (signed
  out, links to /login) or "» Abmelden" (signed in, uses the existing sign-out action), plus the
  tools (demo mailbox, reset, language, account menu) as quiet icon buttons.
- Tab row below the header (≥ 600px): `header.nav` as uppercase tabs with their lucide icons,
  `#344450` text, active tab primary with a 2px primary underline (router `activeProps`).
- Under 600px: a primary hamburger square (accessible name from locales) toggles a white drop
  panel listing the tabs with icons; the main content behind gets `opacity: .2; filter: blur(2px)`
  while open; Escape and a click outside close it; focus moves into the panel and back.
- Main: one panel `--werkstatt-panel`, radius 3px, `max-width: 1000px` (1200px from 1280px wide),
  padding 1.5rem; edge to edge on phones.
- Dialogs/sheets in Werkstatt (via `[data-theme='werkstatt'] [data-slot=…]` selectors): scrim
  `--werkstatt-scrim` with `backdrop-filter: blur(2px)`, white panel, light (300) title, thin ×.

**Steps:**

1. Tests first: `Shell.test.tsx` renders the nav items as links with names, the sign-in button when
   signed out and sign-out when signed in, the phone menu (button with accessible name, opens,
   Escape closes, focus returns); no `--school-background` → no background image (review focus 1).
2. Implement CSS + Shell; register the chunk; make sure Programme's entry chunk does not import
   Werkstatt code (extend the build guard in `web/vite.chunks.ts`, or a build test that the entry
   has no `roboto-condensed`).
3. Manual check: `just demo-serve`, open `?demo=werkstatt` — header, tabs, phone menu, dialogs,
   dark mode; screenshots in the PR body.
4. `just web-check`, `just e2e`. Commit.

## Task 4.1: werkstatt-schedule

**Files:**

- Create: `web/src/themes/werkstatt/ScheduleList.tsx`, `DayHeading.tsx`, `LessonCard.tsx`,
  `courseTint.ts` (+ tests)
- Modify: `web/src/themes/werkstatt/index.ts` (add the three slots),
  `web/src/themes/werkstatt/werkstatt.css`, locales for new strings

**Design:**

- DayHeading: full-width band in the day tone colours, bold uppercase, `letter-spacing: 1.5px`,
  text like "Heute, Montag, 2. November" / "Morgen, …" (reuse the existing day label helpers), a ‹
  and › link to the previous/next day's heading (`prevId`/`nextId`, accessible names "Previous day"
  / "Next day"), hidden when there is none.
- ScheduleList (≥ 768px): per day, a grey (`#e6eaee`) header row naming the rooms used that day
  (tiny bold primary uppercase), then one column per room; each lesson sits in its room's column,
  ordered by start time; rooms ordered by location then room name. A day with one room is one full
  width column; never more columns than rooms used that day (review focus 4). Under 768px: one
  column of cards, no room header. The filter UI stays above (from the page), unchanged.
- LessonCard: card `--card`, 1px border, radius 3–5px, a tint by course type
  (`courseTint(courseTypeId)` hashes onto `['#ffffe0', '#fff8f0', '#fff7fe', '#f3fff4', '#f7f3ff',
'#f0f8ff']`, light mode only). Top row: course type (uppercase on desktop) left, level right. Then
  the time in primary, the description muted, "Teilnehmende: 7 / 8" (green, red when full), the
  instructor in primary with the accompanist in brackets. Right: a square button (min 4.5rem) for
  `quickAction`: book = mint with a door-open icon and "Eintragen", cancel = red with door-closed
  and "Austragen", waitlist = brown "Warteliste", full = diagonal red/mint stripes "Voll"
  (disabled), open = mint "Details" opening the sheet, none = no button. Booked
  (`lesson.bookedFor` non-empty) → 2px primary frame and a yellow star (accessible name "Booked").
  The card body (not the button) opens the sheet (`onOpen`). Past lessons dimmed; canceled with a
  subtle checker pattern and the existing canceled label.
- Strings via locales (`schedule` namespace): de "Eintragen", "Austragen", "Warteliste", "Voll",
  "Details", "Teilnehmende"; en "Book", "Cancel", "Waiting list", "Full", "Details", "Participants".

**Steps:**

1. Tests first: room columns (two rooms → two columns with headers; one room → one column; a room
   used on another day only is not shown); order by time inside a column; phone width → single
   column (mock `matchMedia` as other tests do); each quick action kind renders the right button
   name and calls `run`/`onOpen`; booked frame + star; the day heading arrows link to the
   neighbouring days; `courseTint` is stable for an id.
2. Implement; register the slots. Compare by eye with the reference screenshots of the old app if
   the orchestrator gave you their path; never copy assets from it.
3. `just web-check`, `just e2e`. Commit.

## Task 4.2: werkstatt-pages

**Files:**

- Create: `web/src/themes/werkstatt/Home.tsx`, `Profile.tsx`, `Participants.tsx` (+ tests)
- Modify: `web/src/themes/werkstatt/index.ts`, `werkstatt.css`, locales

**Design:**

- Home (≥ 768px two columns, else stacked): left "Infos" (h2, weight 300) with the school's welcome
  text from the existing home view model and a round avatar placeholder (lucide `UserRound` in a
  grey circle); right "Mein Stundenplan": the member's own upcoming lessons grouped by day with the
  `DayHeading` and `LessonCard` slots (through `useSlot`, so 4.1's look applies once merged).
  Signed out: the right column shows the existing schedule teaser/next lessons.
- Profile ("Meine Daten"): the account page's sections (person data, email, password, language,
  dependents, privacy) in one column, light labels above full-width inputs (1px `#abb0b2`, 3px
  radius, weight 300), section titles weight 300 in large type, primary actions as mint uppercase
  buttons, destructive ones red. Same forms and validation as today (the view model passes the form
  components or their props; do not fork the form logic).
- Participants: a dense table (headers 0.78em bold uppercase primary): present checkbox, name,
  email, phone, pass, trial-lesson marker, remove button; the add-a-person form below; the same
  actions and dialogs as today (`AddParticipantDialog`, `TrialDialog`, `BookingHistoryDialog`).

**Steps:**

1. Tests first: Home shows both columns' headings and the member's lessons by day; the signed-out
   variant; Profile renders every section and submits through the passed handlers; Participants
   renders rows and fires the present/remove/add callbacks.
2. Implement; register the slots.
3. `just web-check`, `just e2e`. Commit.

## Task 5.1: werkstatt-e2e

**Files:**

- Create: `web/e2e/werkstatt.spec.ts`
- Modify: `CLAUDE.md` (code map: themes), `docs/demo-mode.md`, `docs/architecture.md` (a short
  "Themes" section)

**Steps:**

1. `werkstatt.spec.ts` with `test.use({ demoSchool: 'werkstatt' })`, desktop and phone projects:
   `html[data-theme=werkstatt]`; the schedule shows day bands and (desktop) room column headers;
   sign in as the demo member, book a lesson with the card's "Eintragen" button, see the star,
   cancel with "Austragen" through the confirmation; home shows "Mein Stundenplan" with a booked
   lesson; the account page saves a phone number; as instructor or office open a lesson's
   participants and mark one present; on phone, the hamburger menu opens, navigates, closes.
2. Run the whole e2e suite (`just e2e`) and `just e2e-server`.
3. Visual pass: capture the Werkstatt schedule, home, account and participants at desktop and
   phone, light and dark, and compare side by side with the reference screenshots of the old app
   (if the orchestrator provided them); list remaining differences that matter for familiarity in
   the PR body; fix the cheap ones.
4. Docs: CLAUDE.md code map bullet "Themes" (`web/src/themes`, slots, fallback, ADR 0018);
   architecture.md section; demo-mode.md mentions the Werkstatt e2e spec.
5. `just ci`. Commit.
