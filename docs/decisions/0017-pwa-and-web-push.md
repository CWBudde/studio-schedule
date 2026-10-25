# 0017 — PWA: installable, offline schedule, web push

- Status: accepted
- Date: 2026-10-26

## Context

Members check the schedule and their lessons on the phone, often on the way to the studio with a bad
connection, and a waiting-list invite (ADR 0010) holds a place for hours only: a mail is easily
missed. Phase 9 makes the app a progressive web app: installable, usable offline for what was last
seen, and able to notify a device. The API answers `no-store` (ASVS 8.2.1, `securityHeaders`), the
web demo runs the same app in the browser (ADR 0001), and nothing may be demo-only except the
transport and the store, mailer and PDF renderer implementations.

## Decision

**Installable.** `vite-plugin-pwa` with `injectManifest` builds our own service worker
(`web/src/sw.ts`) with Workbox's precaching. It precaches the app shell (the hashed assets,
`index.html`, `theme.js`, the icons) but never the demo's `*.wasm` and `wasm_exec.js` (large, and
loaded once), answers navigations with `index.html` except `/api`, `/ical` and the manifest, and
caches no API answer. A new version waits; a toast ("New version", "Reload") activates it, so a page
never runs half old, half new code. The server answers `GET /manifest.webmanifest` from the public
settings (the school's name and language, its accent as the theme color; `application/manifest+json`,
five minutes' cache), and the web build links it; the Pages demo ships a static manifest named
"Studio Schedule (demo)". The icons (192, 512, maskable 512, apple-touch 180) are rendered once from
`favicon.svg` and committed. `sw.js` is served `no-cache`; the CSP names `worker-src 'self'` and
`manifest-src 'self'`.

**Offline: the last schedule and My lessons, read-only.** The page keeps an IndexedDB copy of the
query cache (`@tanstack/react-query-persist-client`, `idb-keyval`) with an allow-list: the public
settings, the schedule, the lessons opened from it, My lessons' coming lessons and `me`. The copy is
used for seven days and dropped when the API's schema changes (its hash is the buster). It is
deleted on every change of identity, so on sign-out and on any 401 (the global handler of ADR 0004).
The service worker caches no API answer, so `no-store` stays true. Offline, queries pause and show
the copy, a banner says so, submit buttons and the booking actions are disabled with a tooltip, and
the transport refuses every write (`OfflineError`), also in the demo, whose backend runs in the
browser: nothing is queued to be sent later. In the demo the backend may not load offline at all;
the pages show the copy then.

**Web push, for waiting-list invites only, besides the mail.** A device subscribes on the account
page ("Notifications on this device"): the browser's permission, `PushManager.subscribe` with the
server's VAPID key, and `POST /api/v1/my/push/subscriptions` (signed in; `DELETE` with the endpoint
in the body turns it off). An endpoint belongs to one subscription (`push_subscriptions`, migration
00024): saved again by the same account it is refreshed, saved by another account (a shared device)
it moves to that account and its queued messages go. Sign-out deletes the device's subscription and
unsubscribes the browser; an admin signed in as an account cannot subscribe (403). A deleted
account's subscriptions go with it.

`waitlister.invite` queues, in the transaction of the invite, one message per subscription of every
account its mail goes to (`enqueuePush` beside `enqueueMail`, `push_outbox`). The notification is
rendered in the account's language from the `push.txt` part of the `waitlist-invite` template: the
lesson and its time and the reservation, never a name, as it shows on lock screens; the payload adds
the accept link and expires with the invite. The `deliver-push` job sends every minute and when a
request queued something (`Wake`). A 404 or 410 deletes the subscription; a refusal (400, 401, 403,
413), a retry after the invite's expiry or the fifth attempt gives the message up; other errors back
off (1, 5, 15, 60 minutes).

**Our own sender.** `internal/webpush` implements RFC 8291 (aes128gcm, one record) and RFC 8292
(an ES256 JWT, valid 12 hours) with the standard library only, tested against the RFCs' examples.
The keys come from `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY` and `VAPID_SUBJECT` (all three or none;
`studio-schedule vapid-keys` prints a pair). Without them push is off: nothing is queued, the API
says so and the card is hidden. Endpoints come from browsers, so the sender connects to public
addresses only (checked on the resolved address), follows no redirects and uses no proxy. The
package is server only (`check-wasm-deps`).

**The demo** shows a real notification: its push sender (`wasmbridge.PushSender`, wired in
`cmd/wasm` like the mailer) hands the payload to the worker, the worker to the page, and the page
calls `registration.showNotification`, so it looks and clicks like a real push. The demo's
"subscription" has a real P-256 key but an endpoint nobody serves.

**Retention.** The `purge-personal-data` job (ADR 0016) deletes subscriptions unused for six months
(a device refreshes its subscription once a day while its account is signed in) and push messages of
a week ago. The data export lists an account's devices by label, without endpoint and keys.

## Consequences

- The schedule and My lessons survive a reload offline; everything else needs the network, and no
  write is ever queued.
- Notifications reach a device only while the browser keeps its subscription; the mail stays the
  channel that always arrives.
- Push endpoints belong to the browser vendors' push services (Google, Mozilla, Apple, Microsoft):
  the server sends them encrypted payloads they cannot read, and a VAPID token naming the
  `VAPID_SUBJECT` contact.
- A new VAPID key pair invalidates every subscription; members turn notifications on again.
- iOS shows notifications only for an app added to the home screen.
