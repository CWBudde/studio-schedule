# Themes and layouts: design

Date: 2026-10-30. Status: approved in brainstorming, ready for an implementation plan.

## Goal

A school chooses the look of its studio-schedule: a **theme** that brings its own colours and
fonts, and its own **layout** for the main screens. Next to today's look ("Programme") comes a
second theme, **Werkstatt**, which is a close copy of the previous dance-school app (`../NTW`). People
who used that app should feel at home: same colours, fonts, header, buttons and screen layouts
where it matters. Navigation and features stay studio-schedule's.

The GitHub Pages demo offers both: one site with two fictional demo schools, one per theme.

### Decisions (asked and answered)

| Question                      | Answer                                                                                      |
| ----------------------------- | ------------------------------------------------------------------------------------------- |
| Purpose of the second look    | Familiar look for the old app's users (not a pixel replica, not only a showcase)            |
| Where the theme is chosen     | A school setting (Settings → Branding), stored on the server, the same for every visitor    |
| How two demos are offered     | One site, two demo schools; the visitor picks one; each keeps its own saved data            |
| Background photo              | A new Branding setting: the school uploads a background image; the demo seeds a CC0 photo   |
| Depth                         | A full layout system: themes replace whole building blocks, not only CSS                    |
| Screens with their own layout | App shell, schedule, home/dashboard, profile, lesson participants                           |
| Theme name                    | **Werkstatt** (id `werkstatt`); today's look is **Programme** (id `programme`)              |
| Architecture                  | A theme registry with typed layout slots (not whole pages per theme, not a second frontend) |

### Constraints

- **No real data.** Nothing of the old app's identity enters the repo: not its name, logo, icons
  (the NTW-Icons font), staff photos, background photo, texts or contact addresses. Werkstatt
  copies the visual language only. The demo school has a fictional name, and its background is a CC0
  photo whose source and licence are recorded next to the file. Fonts: Roboto Condensed (Apache 2.0) via
  `@fontsource/roboto-condensed`; icons come from lucide.
- **One app, two targets.** The theme is a real feature on both targets. The demo-specific parts are
  limited to the seed variant (seed data) and the demo's about page/banner, which already exist.
- **Every user-facing string** in `web/src/locales/{en,de}`; Werkstatt adds keys only where its
  wording differs (e.g. "Mein Stundenplan" on the home page).
- **Accessibility stays:** landmarks, accessible names, focus rings, contrast ≥ 4.5:1 for text
  (Werkstatt's day bands use dark text where white text would fail, e.g. on yellow).

## 1. Backend: theme setting and background image

### Theme

- `domain.Theme`, a string enum with `Valid()`: `ThemeProgramme = "programme"`,
  `ThemeWerkstatt = "werkstatt"`. The zero value means Programme (ADR 0002: zero = absent).
- `domain.Settings.Theme`, validated in `Settings.Validate` (unknown value → field error on
  `theme`).
- Branding card: `getBrandingSettings` / `updateBrandingSettings` carry `theme` (admin writes, office
  reads, as today). `getPublicSettings` adds `theme` (always set; Programme when unset) and
  `hasBackground`.
- PostgreSQL: a migration adds the column to the settings table, with a CHECK on the known values;
  sqlc queries updated. The memory store carries it in its settings row and its snapshot (the
  snapshot test `TestSnapshotCoversEveryTable` must keep passing).

### Background image

Stored apart from the settings, like the receipt logo (`SettingsRepo.Logo`, `PutLogo`, `LogoInfo`):

- `domain.Background{ContentType, Data}` with `Validate()` (JPEG, PNG or WebP; at most
  `MaxBackgroundBytes` = 1 MiB; field errors on `contentType` / `data`), `Digest()`, and
  `domain.BackgroundInfo{ContentType, Digest}`.
- `SettingsRepo.Background(ctx)`, `PutBackground(ctx, b)` (the zero value removes it),
  `BackgroundInfo(ctx)`; implemented in memory and PostgreSQL (migration: columns `background`,
  `background_type`, `background_digest` on `settings`, like the logo's in migrations 00018 and
  earlier), with storetest contract cases.
- `SettingsService`: `Background(ctx, has)` returning a cached image like `Logo`, and
  `PutBackground` (admin). Not audited, like the logo change (`ReceiptService`, receipts.go).
- HTTP:
  - `getSchoolBackground`: `GET /api/v1/settings/background`. Public, ETag from the digest,
    `Cache-Control: public, no-cache`, 404 without one.
  - `updateBrandingBackground`: `PUT /api/v1/admin/settings/branding/background` (admin; raw image
    body with its content type, as the receipt logo upload does).
  - `deleteBrandingBackground`: `DELETE /api/v1/admin/settings/branding/background` (admin, 204).
  - Each is pinned in `internal/app/access_test.go`; then `just gen-api`.

### Seed variant

- `seed.LoadDemoAt(ctx, st, anchor)` keeps its behaviour and becomes
  `LoadDemoSchoolAt(ctx, st, anchor, seed.SchoolProgramme)`. The new `seed.School` type (`SchoolProgramme`,
  `SchoolWerkstatt`) selects the demo school: `programme` (today's data, unchanged) or `werkstatt`.
- `werkstatt` seeds the same people, classes, rooms and history, with a different fictional school
  name (German, not resembling the old app's name), `Theme = werkstatt`, accent `#9c215b` (it passes
  the 4.5:1 check), German as the default language, and the background photo.
- The photo is a CC0 dance-studio picture, compressed (≤ 300 KB JPEG), embedded in the seed package
  with a `LICENSE`/source note next to it.
- `studio-schedule seed` gets `--school programme|werkstatt` (default programme), so the variant is
  not WASM-only.

### ADR

`docs/decisions/0018-themes-and-layouts.md`: what a theme is, the theme setting, the background,
the slot registry, the fallback rule, and why there are no per-visitor themes.

## 2. Demo: two schools on one site

- The demo school is `programme` or `werkstatt`. It is chosen:
  - by `?demo=werkstatt` (or `?demo=programme`) in the page URL, outside the hash, e.g.
    `https://cwbudde.github.io/studio-schedule/?demo=werkstatt`. The param is read at start-up,
    stored, and removed from the address with `history.replaceState`;
  - by the about page's second button, "Open in the Werkstatt look", which stores the choice and
    enters the demo.
- It is stored in localStorage (`studio-schedule-demo-school`, guarded like `entered.ts`); the
  default is `programme`.
- The transport passes the school to the Worker at boot. `cmd/wasm` seeds that variant on a fresh
  start.
- Persistence keeps one IndexedDB record per school (today's record `current` becomes the
  programme record, so existing visitors keep their data; werkstatt gets its own key). The snapshot
  header names its school; a mismatch starts fresh, as a version mismatch does.
- "Reset demo" resets only the current school.
- The tab lock stays one per origin (one Worker at a time). Switching schools stores the new choice
  and reloads the page.
- The demo banner gets a link "Switch to the Werkstatt look" / "Switch to the Programme look".
- The about page shows both demos side by side (name, one line each, a small preview built from
  the theme's colours and fonts, no screenshots to maintain).
- README "Live demo" links both URLs.
- docs/demo-mode.md describes the two schools, the param, the per-school records.

## 3. Frontend: registry and slots

### Registry

- `web/src/themes/registry.ts`: `ThemeId = 'programme' | 'werkstatt'` (from the generated API
  types), and `loadTheme(id): Promise<Theme>`, which uses a dynamic `import()` per theme so each
  theme is its own chunk. Programme is the eager default.
- `web/src/themes/slots.ts`: the typed slot contracts (props interfaces) and the `Theme` type:
  `{ id, slots: Partial<Slots> }`. A theme's CSS is imported by its module (side effect), scoped
  under `[data-theme="<id>"]`.
- The slots: `Shell`, `ScheduleList`, `DayHeading`, `LessonCard`, `Home`, `Profile`,
  `Participants`. Each slot's props are a view model plus callbacks: the page or container computes
  the data and the actions (book, cancel, waitlist, filters, navigation, form state) once, and the
  slot renders it. No slot fetches or mutates on its own.
- Fallback: a slot a theme does not provide is Programme's. Programme provides all.
- `useSlot(name)` returns the active theme's component for a slot.

### Programme behind the slots

- Today's components move behind the slot contracts: `RootLayout`'s chrome becomes the Programme
  `Shell`, and `ScheduleList`, `DayHeading`, `LessonCard`, `HomePage`'s body, the account/profile
  page body and the participant list become Programme slots. Their logic moves into the
  pages/containers.
- No visual or behavioural change: the existing Vitest and Playwright suites pass unchanged (only
  imports may move). Screenshots of the main screens before and after (desktop and phone, light and
  dark) are compared.

### ThemeProvider and loading

- `ThemeProvider` (inside the app, above `RootShell`) reads the theme from the public settings,
  loads it, sets `data-theme` on `<html>`, and provides it to `useSlot`. Until the settings arrive it
  uses the cached theme.
- Flash avoidance: the last theme is kept in localStorage (`studio-schedule-theme`) and applied to
  `<html>` by the inline script in `web/index.html` that already sets the dark class. While a
  non-default theme's chunk loads, the shell shows the existing loading state, not Programme's chrome.
- The background: with `hasBackground` and a theme that shows one (Werkstatt), the image is loaded
  like the logo (`useDataUrl`) and set as a CSS variable (`--school-background`) on `<html>`.
- The school's accent still overrides `--primary` (`accentStyleSheet`), in both themes.
- The demo's about page is outside the shell and keeps its own look.

### Werkstatt

A lazily loaded chunk `web/src/themes/werkstatt/`:

- **Tokens** (`werkstatt.css`, under `[data-theme="werkstatt"]` and `.dark[data-theme="werkstatt"]`):
  - fonts: Roboto Condensed, weights 300/400/700;
  - colours: primary `#9c215b` (`#871d4f` dark variant); action colours mint `#74b391` (book),
    red `#dc5047` / `#c32f25` (cancel), brown `#b39474` (waiting list); neutral grey `#abb0b2`;
    text `#354052`, muted `#7f8fa4`, lines `#dfe2e5` / `#e6eaee`;
  - day colours: today `#f8398a`, tomorrow `#fbec19` with dark pink text, later `#68b3c9`
    (adjusted where text contrast requires it);
  - radius 3px;
  - the translucent panel (`rgb(255 255 255 / 50%)`) over the fixed, cover-sized background.
  - Dark: primary `#f8398a`, mint `#3a7053`, grey `#383838`, near-black panels.
- **Shell:**
  - The header is a translucent white bar with the school logo/name on the left and a solid primary
    "» Anmelden"/"» Abmelden" button (small caps) on the right. The account menu, language switcher,
    demo mailbox and reset are kept, styled to fit.
  - A tab row below, with uppercase tabs with lucide icons, the active tab in primary with a 2px
    underline.
  - Under 600px a primary hamburger square opens a white drop panel, and the page behind it
    fades and blurs.
  - The page is one panel, 1000px wide (1200px from 1280px).
  - Dialogs get a warm brown scrim with blur, a white panel, a light title and a thin ×.
- **ScheduleList and DayHeading:**
  - Full-width day bands (bold uppercase, letter-spaced) with ‹ › to jump between days.
  - On desktop each day has a grey room header row, and lessons sit in their room's column, in time
    order.
  - On phones there is one column of cards.
  - The filter box sits above the list.
- **LessonCard:**
  - A white card at 80% with a 1px border.
  - Layout: title top-left, level top-right; time in primary; description muted; participant count
    green, or red when full; instructor in primary with the accompanist in brackets.
  - A square action button at the side: mint with a door icon to book, red to cancel, brown for
    the waiting list, diagonal stripes when full.
  - A booked lesson gets a primary frame and a yellow star.
  - A faint tint per course type, from the course type's id hashed onto a fixed palette.
- **Home:** two columns on desktop, stacked on phones. Left: "Infos", the school's welcome text and
  the avatar placeholder. Right: "Mein Stundenplan", the member's own lessons by day, in Werkstatt
  day bands and cards. A visitor who is not signed in gets the schedule teaser instead.
- **Profile:** the account page's person data, password and email sections in the old app's layout
  (light labels above full-width inputs, mint primary buttons).
- **Participants:** a dense table: present checkbox, name, email, phone, pass, trial-lesson marker,
  remove. Headers are small bold uppercase in primary. The add-a-person form sits below. The same
  actions as today.

Pages without their own Werkstatt slot (classes, my lessons, teach, all office and admin pages)
render Programme's structure with Werkstatt's tokens and fonts, inside Werkstatt's shell.

### Settings UI

The Branding card gets a theme select (Programme / Werkstatt, with one line each) and a background
image upload/remove (preview, size/type errors as field errors), following the receipt-logo upload.

## 4. Testing and delivery

- **Go:**
  - domain tests (Theme, Background);
  - service tests (theme in branding, background put/get/delete, access);
  - storetest cases for the theme column and the background, on memory and PostgreSQL;
  - a seed test that the werkstatt variant loads and sets theme, accent and background;
  - the snapshot round trip.
- **Vitest:**
  - every Werkstatt slot gets behaviour tests: book, cancel, booked marker, room columns, empty
    rooms, the phone menu, the participants table actions, the home sections;
  - a test renders each slotted page under both themes;
  - ThemeProvider: cached theme, fallback slots, `data-theme`;
  - the branding form's theme select and background upload.
- **Playwright:**
  - the existing suite is unchanged (Programme);
  - a new `web/e2e/werkstatt.spec.ts` opens `?demo=werkstatt` and checks: the theme is applied, book
    and cancel on the schedule, the home page's own lessons, the profile, the participants of a
    lesson as staff, the phone menu, and that switching the look keeps each school's data;
  - `about.spec.ts` covers the second button.
- **Delivery** by subagents, in phases (each phase ends green before the next starts):
  1. The backend: theme setting, background image, ADR, `gen-api`.
  2. Programme behind the slots: the registry, slots and ThemeProvider, no visual change, checked
     with before/after screenshots.
  3. Werkstatt, as three parallel agents: (a) tokens + Shell; (b) ScheduleList, DayHeading,
     LessonCard; (c) Home, Profile, Participants. (a) lands first or provides the tokens the others
     use.
  4. The demo schools, seed variant, persistence per school, about page and banner, README,
     docs/demo-mode.md, CLAUDE.md code map, the Branding UI.

  Then: `/code-review`, fixes, `just ci`, commit on main, push.

## Out of scope

- Per-visitor themes, and more than two themes.
- Werkstatt layouts for pages other than the five above.
- The old app's features that studio-schedule lacks (video, quizzes, sensor data, seasonal headers,
  the update countdown text).
- A handwriting font and seasonal decorations.
