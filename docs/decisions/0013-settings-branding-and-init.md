# 0013 — Settings, branding and `init`

- Status: accepted
- Date: 2026-10-25
- Amends: [0003](0003-release-window-and-visibility.md) (the release rule and the time zone become
  editable)

## Context

Phase 7 adds the settings UI. `domain.Settings` has held the school's name, time zone, language,
currency, release rule, cutoffs, early access and legal notice since Phase 1, but only the seed wrote
them: no endpoint changed them, and a fresh database had no settings and no admin at all. Phase 6
added two cards to `/admin/settings` (passes, receipts; ADR 0012). The header and `<title>` said
"Studio Schedule" and the theme's primary color was fixed in `web/src/index.css`. The school decided
(2026-10-25): admins edit, the office reads, one form card per area; one accent color, checked for
contrast, with a derived dark variant; the receipt logo also in the header; the school name in the
header and `<title>`; the time zone and the currency only at the first setup; and a CLI `init`.

## Decision

**Cards.** Each area of the settings page has its own pair of operations, like the pass and receipt
cards: `GET /api/v1/admin/settings/<card>` for the office, `PUT` for an admin (access pinned in
`internal/app/access_test.go`). A `PUT` replaces only its card's fields, in one transaction that reads,
changes, validates (`Settings.Validate`) and stores the whole record; field errors are 422s on the
request's fields (the booking rules map the domain's duration fields to their `…Minutes` names).
Each card reads only what it shows besides the settings: the branding the logo's type, the time and
language what locks them (`Usage`), the booking rules the release window.

- `school`: `schoolName` (1–200 characters), `legalNotice` (up to 2000 characters, plain text with
  line breaks); the domain counts characters, as the API's `maxLength` and the form do.
- `branding`: `accentColor` (`#rrggbb` or absent); the answer adds `accentColorDark`, `minContrast`
  and `hasLogo`.
- `regional` (time and language): `timezone`, `defaultLocale` (`en` or `de`, now checked by
  `Settings.Validate`; a tag such as `de-AT` is stored as its base language), `currency`; the answer
  adds `timezoneLocked` and `currencyLocked`. Before, the default language could be any non-empty
  tag. Migration 00018 rewrites a stored one to its base language when that is `en` or `de`, else to
  `en`, the app's fallback; the services read the settings the same way (`domain.SchoolLocale`), so a
  legacy value can never keep the other cards from saving.
- `booking-rules`: the release weekday, time and weeks ahead, the booking and cancellation cutoffs
  and the waiting-list invite hold in minutes, and the default early access in days; the answer adds
  the release `window` now (`now`, `lastReleaseAt`, `nextReleaseAt`, `releasedUntil`) and
  `generatedLessons`. `GET …/booking-rules/preview?releaseWeekday&releaseTime&releaseWeeksAhead`
  computes the window of a rule being typed with `ComputeReleaseWindow`, so the client still never
  computes the rule itself (ADR 0003). It checks the rule alone (`domain.ValidateReleaseRule`, errors
  on `query.<field>`), not the stored settings.

**Time zone and currency are chosen at the first setup.** Every lesson instant was computed in the
school's zone, and every amount is in minor units of the currency. So the time zone may change only
while no lesson exists, and the currency only while no pass type has a price above 0, no pass was
paid and no receipt was issued (`SettingsRepo.Usage`, one query of `EXISTS`). A change after that is
a 409 `timezone-locked` or `currency-locked`; sending the current value is no change, so the card
always saves. The language may always change. This amends ADR 0003, under which the zone was fixed by
the seed.

**Booking rules change from now on.** A narrower window holds back the lessons it no longer reaches;
released lessons keep their bookings. A wider one (more weeks ahead, or more days of default early
access) generates the lessons it reaches right away, with `LessonGenerator.GenerateAhead` in its own
transaction after the settings are stored (transactions never nest), instead of waiting for the
hourly job; the answer counts them. If that generation fails, the change stays and the job catches
up: the `PUT` still answers 200 with the stored rules and `generatedLessons` 0, and the server logs
the failure. An error would tell the client that nothing was saved, although the rules were.

**Branding.** The accent is one color, stored in lower case (`settings.accent_color`, migration
00018), absent for the app's own wine. It must reach a WCAG 2 contrast of 4.5:1 (AA for normal text)
against the light theme's text on the primary color and its page background, where the primary is
also a text color (links, the current page). The dark theme derives its variant: the accent's OKLCh
hue at lightness 0.78, the chroma capped at 0.1 and reduced until the color fits into sRGB, as the
built-in theme pairs its primaries. That fixed lightness keeps it far above 4.5:1 against the dark
text on it for every accent (at least 8.6:1, `TestDarkAccentContrast` over the RGB cube), so the
variant needs no check of its own. Go
(`internal/domain/color.go`) decides; the form runs the same math (`web/src/lib/color.ts`, shared test
vectors) to show the contrast and a light and dark preview while the color is typed. The public
settings carry `accentColor` and the derived `accentColorDark`; the app sets `--primary`, `--ring`,
the sidebar's primary and the "today" day heading from them in a `<style>` element (`html:root`,
`html.dark`), and the browser's `theme-color`. Nothing else of the theme changes.

**The school's name and logo.** The header shows the school's name (and names the home link after
it) and the receipt logo when there is one (ADR 0012), else the app's mark; the document title is
the school's name. The logo is served publicly at `GET /api/v1/settings/logo` with
`Cache-Control: public, no-cache` and a strong `ETag`, the SHA-256 of its type and bytes
(`domain.Logo.Digest`), so browsers revalidate cheaply (304) and see a new logo at once; the public
settings say `hasLogo`. The digest is stored with the logo (`settings.logo_digest`, migration 00018,
which fills it in for a logo stored before), so a revalidation reads neither the image nor hashes
it. The header reads the logo through the transport as data, so the content security policy keeps
allowing `data:` images only, and the web demo, which has no server, works the same way. The legal
notice, in the public settings, shows in the footer of every page; before they load, the title is
the app's name in the UI language.

**The default language is the visitors' language.** The UI shows the language a visitor chose in
the header's switcher (stored in the browser); without a choice it follows the school's
`defaultLocale` from the public settings, and remembers it so the next page load starts in it; only
before the first load of the settings does it guess from the browser. Detected languages are no
longer stored as if chosen, so they never hide the school's language. Signing in to an account
with a language of its own (ADR 0008) switches the UI to it, as before, and that counts as the
visitor's choice; an account without one keeps the school's language.

**`studio-schedule init`.** A new installation runs `migrate up`, then `init` (server only): it asks
for, or takes as flags, the school's name, time zone, language (default `en`) and currency (default
`EUR`), and the first admin's email and name. It refuses when settings exist. In one transaction it
stores the settings, the rest from `service.DefaultSettings` (a Sunday 18:00 release of two weeks,
booking until the start, canceling until 2 hours before, invites held for 12 hours, 14 days of early
access, visits used on attendance, receipt numbers "R-…", no receipt issuer yet), and the admin's
person and account: unconfirmed and without a password, like an invited account (ADR 0008). Instead
of a mail, which may not be configured yet, it prints the one-time link that sets the password and so
confirms the account: an invitation token (`reset-password`, 72 hours) on `BASE_URL`.

## Consequences

- The settings record is one row. Two admins saving different cards at the same moment can lose one
  change (read committed, read–change–write); the page shows the stored state after each save. A
  school has few admins, so there is no lock.
- A time zone change races only with the very first lessons; both happen at the first setup.
- Every page load asks for the public settings once (cached for five minutes) and, with a logo, for
  the logo; an accent change shows on other open pages after that.
- The receipt settings remain the place to upload the logo; the branding card shows it and points
  there.
- Further theme colors (charts, the accent tint) stay the built-in ones; a second brand color would
  need its own contrast rules.
