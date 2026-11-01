# Werkstatt catalogue: implementation plan

> **For agentic workers:** this plan is run by `ccbatch run plan`, one headless session per
> checkbox below, phase by phase (`--prefix 1.`, then `2.`, …). A later phase starts only after the
> earlier phase's pull request is merged into `main`. Each phase has one item, because each one
> builds on the one before and most of them change the API (a parallel item would conflict in
> `api/openapi.json`). Your item's detailed steps are in the section "Task N.M" further down. Read
> that section, the spec, `CLAUDE.md` and the files it names before you change anything. Use
> subagents for independent chunks (e.g. Go stores vs. web UI vs. tests). Keep the steps'
> test-first order: write the failing test, see it fail, implement, see it pass, commit.

**Goal:** the Werkstatt demo school becomes comparable with the previous app it is modelled on.
It gets its rooms in the house's order (Oben, Unten, Online, Trainingsraum), levels the school
manages itself, classes that state a level range, a stage and an age, and its own German people,
catalogue and weekly plan.

**Architecture:**

- Rooms get a position within their location.
- Levels become records of the school (name, optional code, position), maintained by the office
  like course types.
- A class refers to two levels (a span) or is for all levels, and also states a stage and an age
  range in months.
- The seed builds each demo school from its own tables; the programme school's data stays
  exactly as it is.

**Tech stack:** Go (Huma v2, chi, PostgreSQL + sqlc + goose, memory store), React 19 +
TypeScript, Vite, Tailwind v4, shadcn/ui, TanStack Router/Query, react-hook-form + zod,
react-i18next, Vitest, Playwright.

**Spec:** [docs/superpowers/specs/2026-11-01-werkstatt-catalogue-design.md](../specs/2026-11-01-werkstatt-catalogue-design.md)
(read it in full; this plan does not repeat its reasoning, and §3's tables are the data for 5.1).

## Global constraints

- **No real data.** The Werkstatt catalogue in the spec was derived by structure only. Take
  nothing else from the previous app: don't open `../NTW` or any database dump, and don't add
  people, prices, descriptions or addresses from anywhere. Every name, email and description is
  invented. Emails use reserved example domains only (`*.example`).
- **One app, two targets.** Room order, levels and the class audience are real features on server
  and demo. Demo-specific code stays in `internal/seed`, the transport and the demo pages.
- **Every user-facing string** goes in `web/src/locales/en` and `web/src/locales/de` (both).
  Level names are data, not locale strings.
- **API changes:** Huma operations with explicit camelCase `OperationID`, catalogue writes on the
  collection path like `createRoom` (`POST /api/v1/rooms`), office reads under `/api/v1/admin/…`.
  Pin every new operation in `internal/app/access_test.go`. Then `just gen-api`; CI fails on a
  stale `api/openapi.json` or `web/src/api/schema.d.ts` (`just check-generated`).
- **Both stores:** every repo method gets a `internal/store/storetest` case that passes on memory and
  PostgreSQL (`just test-pg`, Docker). After a query change, `just sqlc` (`just check-sqlc`).
- **Migrations** are numbered in the order of this plan: `00033_room_position.sql` (1.1),
  `00034_levels.sql` (2.1) and `00035_class_audience.sql` (3.1). If `main` has taken a number in
  the meantime, use the next free one and say so in the PR.
- **Coverage gate:** 95 % total, 80 % per package (`just test-cover`, `just coverage`). A branch
  no input can reach is deleted, never excluded.
- **Command line:** `just cli-docs` after a command's help changes; CLI goldens are updated with
  `go test ./cmd/studio-schedule -update`. Review the golden diff; only the intended columns and
  IDs may change.
- **Conventional Commits** in English, ending with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **Lint locally** with golangci-lint v2.14.0 (CI's version): `PATH=$HOME/go/bin:$PATH just lint`.
- **Parallel sessions:** Playwright reuses a server already on its port. Run e2e on a port of your
  own worktree: `E2E_PORT=$((4200 + $(pwd | cksum | cut -d' ' -f1) % 700)) just e2e`.

## Review focus

1. **An office that reorders while another office user adds a room or a level:** the reorder names
   the old set, the server answers 422 on `body.roomIds` / `body.levelIds`, and the page refetches
   the list and shows an error instead of a stale order. Owned by 1.1 (service + LocationsPage
   test) and 2.1 (LevelsPage test).
2. **Levels reordered after classes use them:** every class's span stays valid. It is shown lower
   level first, and a level moved between the two ends now belongs to the span. Owned by 3.1
   (`Audience` test after `SetOrder`).
3. **A level filter on a level strictly between a class's two ends:** the class is in the result,
   an all-levels class is too, and a class without a level is not. Owned by 3.1 (schedule service
   test).
4. **The admin class sheet on a school without levels:** no empty selects, a link to the Levels
   page, and saving a class without a level works. Owned by 3.1 (ClassSheet test).
5. **Switching the web demo to Werkstatt:** the login page shows the Werkstatt accounts, not the
   programme school's, and signing in with them works. Owned by 4.1 (`SchoolLogins` test) and
   5.1 (e2e).

---

## Checklist (one ccbatch session each)

- [x] 1.1 `room-order` Rooms get a position: migration 00033, `RoomRepo.SetOrder`, `setRoomOrder`, `position` in the API, move up/down on the locations page, Werkstatt room columns by position, ADR 0007 amendment — see "Task 1.1" below.

  Done: migration 00033 (backfill tested by `TestRoomPositionBackfill`), `RoomRepo.SetOrder` with
  four contract cases on both stores, `SetRoomOrder` (stale-order test for review focus 1, fault
  sweep), `setRoomOrder` pinned as office, seed positions (`TestDemoRoomPositions`), move buttons
  in an "Order" column of the locations page (shown only while the table is in API order, not
  sorted by another column or searched), `roomColumns` by position, `roomOptions` no longer sorts
  by name, two e2e tests. Coverage 98.9 % total; every new function at 100 %. Notes for later
  tasks: adding a migration means extending the migration lists in `TestUniqueNamesMigration`,
  `TestLessonSlotsMigration`, `TestSettingsMigration` and `migrateFailNewest`;
  `internal/service/fault_gen_test.go` had fallen behind the events repos and nothing checks it
  (regenerated here). A refused order shows a translated toast (stale, gone, failed), not the
  server's text, like the delete dialogs.

- [ ] 2.1 `levels` Levels as records of the school: migration 00034, `LevelRepo`, `listLevels` and the four office writes, the admin Levels page, the programme seed's levels, ADR 0021 (levels part) — see "Task 2.1" below.

- [ ] 3.1 `class-audience` Classes state a level span or all levels, a stage and an age range: migration 00035 (moves `classes.level` onto level rows), service `Audience`, API fields, the level filter by level ID, statistics, CLI, `formatAudience` on every card and table, the class sheet's new fields, ADR 0021 completed — see "Task 3.1" below.

- [ ] 4.1 `seed-split` Each demo school is built from its own tables (`schoolSpec`); the programme dataset stays byte-identical (digest test); per-school validity, overlap and login tests replace `TestDemoSchoolSameData`; `SchoolLogins` wired into the WASM demo — see "Task 4.1" below.

- [ ] 5.1 `werkstatt-data` The Werkstatt school's own data from spec §3: German people, staff and accounts, the Online room, five levels, 23 course types, the 70-class plan, Sunday workshops, a rental, German passes and announcements; ADR 0018 amendment, docs/demo-mode.md, e2e — see "Task 5.1" below.

---

## Task 1.1: room-order

**Files:**

- Modify: `internal/domain/catalogue.go` (`Room.Position`, `Validate`) and its tests in
  `internal/domain/entities_test.go`
- Create: `internal/store/postgres/migrations/00033_room_position.sql`
- Modify: `internal/store/postgres/queries/catalog.sql`, then `just sqlc`;
  `internal/store/postgres/catalog.go` (roomRepo)
- Modify: `internal/store/store.go` (`RoomRepo`), `internal/store/memory/repos.go` (roomRepo; and
  `internal/store/memory/snapshot.go` if rooms have their own record there)
- Modify: `internal/store/storetest/catalogue.go` (contract cases)
- Modify: `internal/service/catalog_admin.go` (+ its tests)
- Modify: `internal/httpapi/catalog.go` (`Room`), `internal/httpapi/schedule.go` (`RoomRef`),
  `internal/httpapi/catalog_admin.go` (`setRoomOrder`), `internal/app/access_test.go`
- Modify: `internal/seed/demo.go` (`builder.location`), `internal/seed/seed_test.go`
- Create: `web/src/lib/reorder.ts` (+ `reorder.test.ts`), `web/src/features/admin/MoveButtons.tsx`
- Modify: `web/src/api/queries.ts`, `web/src/features/admin/LocationsPage.tsx` (+ test),
  `web/src/themes/werkstatt/roomColumns.ts` (+ its test), `web/src/test/mockApi.ts`,
  `web/src/locales/{en,de}/admin.json`
- Modify: `web/e2e/admin.spec.ts`, `web/e2e/werkstatt.spec.ts`
- Modify: `docs/decisions/0007-timetable-administration.md` (amendment "Room order")
- Regenerate: `api/openapi.json`, `web/src/api/schema.d.ts`

**Interfaces produced (later tasks rely on these exact names):**

```go
// internal/domain/catalogue.go
type Room struct {
	ID         RoomID
	LocationID LocationID
	Name       string
	Capacity   int
	Position   int // 1-based within the location; orders its rooms
}
// Validate adds: on("position", r.Position > 0, "room position must be positive")

// internal/store/store.go, RoomRepo
// List returns the rooms of a location, or all rooms if location is zero,
// ordered by location name and ID, then position, name and ID.
List(ctx context.Context, location domain.LocationID) ([]domain.Room, error)
// SetOrder sets the positions of the location's rooms to 1..n in the order
// given. It fails with domain.ErrValidation unless order holds every room of
// the location exactly once and no other room.
SetOrder(ctx context.Context, location domain.LocationID, order []domain.RoomID) error

// internal/service/catalog_admin.go
// SetRoomOrder: an unknown location is domain.ErrNotFound (404); an order that
// is not exactly the location's rooms is a *domain.FieldError on "roomIds".
func (s *CatalogService) SetRoomOrder(ctx context.Context, location domain.LocationID, order []domain.RoomID) error
```

HTTP (camelCase, then `just gen-api`):

- `Room` (listRooms, the room writes) and `RoomRef` (the schedule's `room`) gain `position`
  (integer, minimum 1, always set).
- `setRoomOrder`: `PUT /api/v1/locations/{locationId}/room-order`, `office`, body
  `{"roomIds": [uuid, ...]}` (required, `nullable:"false"`), 204. Register it `withConflict` like
  the other catalogue writes.

```ts
// web/src/lib/reorder.ts
/** `items` with the item at `index` moved one place up (-1) or down (+1); unchanged at the ends. */
export function moveItem<T>(items: readonly T[], index: number, delta: -1 | 1): T[];

// web/src/features/admin/MoveButtons.tsx
/** "Move <name> up/down" icon buttons; the first item has no up, the last no down. */
export function MoveButtons(props: {
  name: string;
  index: number;
  count: number;
  disabled?: boolean;
  onMove: (delta: -1 | 1) => void;
}): JSX.Element;

// web/src/api/queries.ts
export const setRoomOrderMutation: (queryClient: QueryClient) => /* useMutation options */;
// mutationFn: ({ locationId, roomIds }: { locationId: string; roomIds: string[] }) => …
// invalidates roomsQuery.queryKey, the locations query and the schedule queries (room order shows there)
```

**Steps:**

1. Domain test first: `Room.Validate` rejects `Position` 0 and -1 with a field error on
   `position` and accepts 1. Update every `domain.Room{…}` literal in tests that now needs
   `Position: 1` (`grep -rn "domain.Room{" --include='*_test.go'`). Run `go test ./internal/domain`
   and see it fail. Implement it and see it pass. Commit `feat(domain): rooms have a position`.
2. Migration (goose, with Down). Backfilled in today's list order, so nothing visible changes:

   ```sql
   -- +goose Up
   -- A room's position orders the rooms of its location (ADR 0007, amendment).
   -- Existing rooms keep today's order: by name.
   ALTER TABLE rooms ADD COLUMN position integer;
   UPDATE rooms r SET position = o.n
   FROM (SELECT id, row_number() OVER (PARTITION BY location_id ORDER BY name, id) AS n FROM rooms) o
   WHERE o.id = r.id;
   ALTER TABLE rooms
       ALTER COLUMN position SET NOT NULL,
       ADD CONSTRAINT rooms_position_positive CHECK (position > 0);

   -- +goose Down
   ALTER TABLE rooms DROP COLUMN position;
   ```

   No unique constraint: a reorder rewrites all positions in one transaction, and ties (two
   concurrent creates) fall back to the name. Queries in `catalog.sql`:
   - `CreateRoom` and `UpdateRoom` write `position`.
   - `ListRoomsByLocation` orders by `position, name, id`.
   - `ListRooms` joins `locations` and orders by `locations.name, locations.id, rooms.position,
rooms.name, rooms.id`.
   - New `LockRoomsByLocation :many` (`SELECT … WHERE location_id = $1 ORDER BY id FOR UPDATE`)
     and `SetRoomPosition :execrows` (`UPDATE rooms SET position = $2 WHERE id = $1`).

   Then `just sqlc`.

3. storetest cases in `internal/store/storetest/catalogue.go`, next to the room CRUD:
   - `Rooms/Position` round-trips;
   - `List` orders by position before name, within a location and across locations (the
     location name first);
   - `SetOrder` reverses three rooms;
   - `SetOrder` with a missing room, a repeated room or another location's room is
     `domain.ErrValidation` and changes nothing;
   - `SetOrder` on a location without rooms with an empty order is fine.

   Implement in memory (`repos.go`; sort with the same comparison as the other memory lists) and
   PostgreSQL (`catalog.go`: lock the location's rooms, compare the sets, update each). Run
   `go test ./internal/store/...` and `just test-pg`.

4. Service (`catalog_admin.go`) with tests in its test file:
   - `CreateRoom` appends: position = the location's highest + 1, or 1 for the first room. The
     body doesn't take a position.
   - `UpdateRoom` keeps the position when the location stays, and appends to the new location
     when it changes (test both).
   - `SetRoomOrder` maps the store's `ErrValidation` to
     `&domain.FieldError{Field: "roomIds", Msg: "must list each room of the location exactly once"}`
     and an unknown location to `ErrNotFound`.
   - Review focus 1: a test where a room is created between reading the list and sending an order
     without it gets the field error.

   Add the store failure paths to the `FaultStore` sweep the file's neighbours use
   (`internal/service/fault_test.go`).

5. HTTP: `position` in `Room` and `RoomRef`, and the `setRoomOrder` operation. In
   `internal/app/access_test.go`, pin `setRoomOrder` as office. httpapi/app tests: 204 and the
   new order in `listRooms`; 422 on `body.roomIds`; 404 for an unknown location; 403 for a
   member. Then `just gen-api`.
6. Seed: `builder.location` sets `Position: i + 1` in table order. `TestWerkstattPlaces` (or a new
   `TestDemoRoomPositions`) asserts Werkstatt's order Oben 1, Unten 2, Trainingsraum 3, and that
   every location's positions are 1..n.
7. Web:
   - `reorder.test.ts`: up, down, no-op at both ends.
   - `MoveButtons` built from the shadcn `Button` (`size="icon"`, lucide `ArrowUp`/`ArrowDown`),
     with accessible names from `admin.json` (`reorder.up`: "Move {{name}} up", `reorder.down`;
     de "{{name}} nach oben/unten verschieben").
   - `LocationsPage`: each location's rooms in API order with `MoveButtons`; a move sends the whole
     new order with `setRoomOrderMutation`. While pending, the buttons are disabled; on error
     there is a toast with the problem's message, and the rooms are refetched (review focus 1).
   - `LocationsPage.test.tsx`: the first room has no up button, moving the second up sends
     `{roomIds: [second, first, …]}`, and a 422 shows the error and refetches.
   - `roomColumns.ts` sorts by `location.name`, then `room.position`, then `room.name`, then id;
     update its doc comment and test (Oben, Unten, Trainingsraum, by position, not name).
   - Room pickers (`RoomSheet`, `ClassSheet`, the schedule filter): remove any client-side sort by
     name so that they follow the API order (`grep -rn "localeCompare" web/src/features`).
   - `mockApi.ts` gets the new handler and `position` in its fixtures.
8. e2e:
   - `admin.spec.ts`: the office moves the second room of "Studio North" up and sees the new order
     after a reload.
   - `werkstatt.spec.ts` "the schedule": on a desktop day, the column headers appear in the order
     Oben, Unten, Trainingsraum, as far as the day uses them (compare indexes, so a day without
     one of them still passes).
9. ADR 0007 amendment "Room order" (what the position is, append on create, the set rule of
   `setRoomOrder`, why there is no unique constraint). Mention the operation in the CLAUDE.md
   "Admin writes" bullet only if that bullet lists the room operations (it does not today:
   leave it).
10. `just test`, `PATH=$HOME/go/bin:$PATH just lint`, `just check-generated`, `just check-sqlc`,
    `just test-pg`, `just web-check`, `just e2e` (own port). Commit in logical commits
    (`feat(store): …`, `feat(api): …`, `feat(web): …`, `docs: …`).

## Task 2.1: levels

**Files:**

- Modify: `internal/domain/catalogue.go` (rename the enum, below), every user of it
- Create: `internal/domain/level.go` (+ `level_test.go`); modify `internal/domain/id.go`
  (`LevelID`, following `CourseTypeID`)
- Create: `internal/store/postgres/migrations/00034_levels.sql`,
  `internal/store/postgres/queries/levels.sql`, `internal/store/postgres/levels.go`
- Modify: `internal/store/store.go` (`LevelRepo`, `Tx.Levels()`), `internal/store/memory/repos.go`
  (or a new `levels.go` there), `internal/store/memory/memory.go` (state),
  `internal/store/memory/snapshot.go`, `internal/store/postgres/errors.go` (the two unique indexes
  → `ErrDuplicate`), `internal/store/postgres/postgres.go` (tx accessor)
- Create: `internal/store/storetest/levels.go`; register it in `storetest.Run`
- Modify: `internal/service/catalog.go` (public `Levels`), `internal/service/catalog_admin.go`
  (writes), tests
- Modify: `internal/httpapi/catalog.go` (`listLevels`), `internal/httpapi/catalog_admin.go` (the four
  writes), `internal/app/access_test.go`
- Modify: `internal/seed/demo.go`, `internal/seed/seed.go` (`Dataset.Levels`, loading),
  `internal/seed/seed_test.go`
- Create: `web/src/routes/admin.levels.tsx`, `web/src/features/admin/LevelsPage.tsx` (+ test),
  `web/src/features/admin/LevelSheet.tsx`
- Modify: `web/src/api/queries.ts`, `web/src/features/admin/AdminLayout.tsx` (+ test),
  `web/src/test/mockApi.ts`, `web/src/locales/{en,de}/admin.json`
- Create: `web/e2e/levels.spec.ts`
- Create: `docs/decisions/0021-levels-and-class-audience.md`
- Regenerate: `api/openapi.json`, `web/src/api/schema.d.ts`, CLI goldens if the seed's IDs moved
  them

**Interfaces produced:**

```go
// internal/domain/catalogue.go: the old enum is renamed so that Level is free.
// No other change; 3.1 deletes it.
type SkillLevel string // was Level
const (
	LevelBeginner     SkillLevel = "beginner" // the constants keep their names
	LevelIntermediate SkillLevel = "intermediate"
	LevelAdvanced     SkillLevel = "advanced"
	LevelProfessional SkillLevel = "professional"
	LevelOpen         SkillLevel = "open"
)
func SkillLevels() []SkillLevel // was Levels
func (l SkillLevel) Valid() bool

// internal/domain/level.go
// Level is a step on the school's level scale; Position orders the scale from
// least to most advanced (ADR 0021). Code is an optional short label ("A").
type Level struct {
	ID       LevelID
	Name     string
	Code     string
	Position int
}
const (
	MaxLevelNameLen = 60 // runes
	MaxLevelCodeLen = 8  // runes
)
// Validate: id set; on("name", not blank and ≤ MaxLevelNameLen); on("code", ≤ MaxLevelCodeLen);
// on("position", > 0). The service trims name and code before.
func (l Level) Validate() error

// internal/store/store.go
type LevelRepo interface {
	Get(ctx context.Context, id domain.LevelID) (domain.Level, error)
	// Create and Update fail with domain.ErrDuplicate if another level has the
	// name or the (non-empty) code, ignoring ASCII case.
	Create(ctx context.Context, l domain.Level) error
	Update(ctx context.Context, l domain.Level) error
	// Delete fails with domain.ErrConflict while classes use the level (3.1).
	Delete(ctx context.Context, id domain.LevelID) error
	// List returns all levels ordered by position, name and ID.
	List(ctx context.Context) ([]domain.Level, error)
	// SetOrder sets the positions to 1..n in the order given. It fails with
	// domain.ErrValidation unless order holds every level exactly once.
	SetOrder(ctx context.Context, order []domain.LevelID) error
}
// Tx gains: Levels() LevelRepo

// internal/service
type LevelData struct{ Name, Code string } // Code empty when there is none
func (s *CatalogService) Levels(ctx context.Context) ([]domain.Level, error)
func (s *CatalogService) CreateLevel(ctx context.Context, d LevelData) (domain.Level, error) // appends
func (s *CatalogService) UpdateLevel(ctx context.Context, id domain.LevelID, d LevelData) (domain.Level, error) // keeps the position
func (s *CatalogService) DeleteLevel(ctx context.Context, id domain.LevelID) error // via remove: ErrInUse
func (s *CatalogService) SetLevelOrder(ctx context.Context, order []domain.LevelID) error // FieldError "levelIds"

// internal/seed
type levelSpec struct{ name, code string }
// Dataset gains: Levels []domain.Level (loaded before the classes)
```

HTTP:

- Public `listLevels`: `GET /api/v1/levels`, `{items: Level[]}` ordered by position.
- Office writes, all `withConflict`:
  - `createLevel`: `POST /api/v1/levels` → 201 `Level`;
  - `updateLevel`: `PUT /api/v1/levels/{levelId}` → `Level`;
  - `deleteLevel`: `DELETE /api/v1/levels/{levelId}` → 204;
  - `setLevelOrder`: `PUT /api/v1/level-order`, body `{"levelIds": [uuid, …]}` → 204.
- `Level`: `{id, name, code?, position}`. Request body `LevelData`: `name` (1–60), `code`
  (optional, ≤ 8). A taken name or code is a 409 `duplicate`; an order that isn't exactly the
  levels is a 422 on `body.levelIds`.

```ts
// web/src/api/queries.ts
export type Level = components['schemas']['Level'];
export type LevelData = PostBody<'/api/v1/levels'>;
export const levelsQuery = queryOptions({ queryKey: ['levels'], queryFn: … }); // public
export const createLevelMutation, updateLevelMutation, deleteLevelMutation, setLevelOrderMutation;
// each invalidates levelsQuery.queryKey (3.1 adds the classes and schedule queries)
```

**Steps:**

1. Rename the enum with gopls (`gopls rename` on `Level` and `Levels` in
   `internal/domain/catalogue.go`), so every user follows. Change nothing else. Run `go build ./...`
   and `just test`. Commit `refactor(domain): name the class level enum SkillLevel`.
2. Domain tests first (`level_test.go`): a valid level; a blank name; a 61-rune name (umlauts
   count as one rune); a 9-rune code; position 0 → field errors on `name`, `code`, `position`.
   Implement `LevelID` and `Level`. Commit.
3. Migration:

   ```sql
   -- +goose Up
   -- The school's level scale (ADR 0021): names are the school's own, position
   -- orders the scale, code is an optional short label. Names and codes are
   -- unique ignoring case, like course type names.
   CREATE TABLE levels (
       id uuid PRIMARY KEY,
       name text NOT NULL CHECK (btrim(name) <> ''),
       code text CHECK (btrim(code) <> ''),
       position integer NOT NULL CHECK (position > 0)
   );
   CREATE UNIQUE INDEX levels_name_key ON levels (lower(name));
   CREATE UNIQUE INDEX levels_code_key ON levels (lower(code));

   -- +goose Down
   DROP TABLE levels;
   ```

   The store writes an empty `Code` as NULL and reads NULL as "". Queries in `levels.sql`:
   `GetLevel`, `CreateLevel`, `UpdateLevel`, `DeleteLevel :execrows`,
   `ListLevels` (`ORDER BY position, name, id`), `LockLevels` (`… ORDER BY id FOR UPDATE`) and
   `SetLevelPosition :execrows`. Map both unique indexes to `domain.ErrDuplicate` in `errors.go`,
   as `course_types_name_key` is. Then `just sqlc`.

4. storetest `levels.go`. Reuse the generic CRUD cases of `crud.go` as `catalogue.go` does for
   course types, and add:
   - a duplicate name in other case ("beginner" vs "Beginner") → `ErrDuplicate` on create and on
     update;
   - a duplicate code ("a" vs "A") → `ErrDuplicate`;
   - two levels without a code are fine;
   - `List` order by position, then name;
   - `SetOrder` reverses three levels;
   - `SetOrder` with a missing, repeated or unknown ID → `ErrValidation` with nothing changed;
   - an empty order on no levels is fine.

   Implement memory (state + snapshot: `TestSnapshotCoversEveryTable` and `TestSnapshotRoundTrip`
   must pass) and PostgreSQL. Run `go test ./internal/store/...` and `just test-pg`.

5. Service with tests:
   - `CreateLevel` trims, validates and appends (highest position + 1).
   - `UpdateLevel` returns 404 for an unknown ID before any field check (like `UpdateCourseType`)
     and keeps the position.
   - `DeleteLevel` goes through `remove`.
   - `SetLevelOrder` maps `ErrValidation` to
     `&domain.FieldError{Field: "levelIds", Msg: "must list each level exactly once"}`
     (review focus 1, as in 1.1).
   - Fault sweep cases.
6. HTTP: the five operations; pin `listLevels` public and the writes office in `access_test.go`.
   Tests:
   - create → 201;
   - a duplicate name in other case → 409 `duplicate`;
   - a 61-character name → 422 on `body.name`;
   - reorder → 204, and `listLevels` shows the new order;
   - a wrong order → 422 on `body.levelIds`;
   - delete → 204;
   - a member → 403;
   - signed out, `listLevels` → 200.

   Then `just gen-api`.

7. Seed: `demoLevels` (Beginner B, Intermediate I, Advanced A, Professional P) in
   `demo.go`, built by `builder.level` right after the course types, into `Dataset.Levels`, and
   loaded by `LoadDemo…` before the classes. The new IDs shift every later entity's ID. Update
   the CLI goldens (`go test ./cmd/studio-schedule -update`) and any test that pinned a seeded ID
   (`go test ./...` tells you which), and check the diff only moves IDs. Seed test: the demo has the
   four levels in that order with their codes.
8. Web:
   - Route `admin.levels.tsx` modelled on `admin.course-types.tsx`.
   - `LevelsPage`: the table of levels in order (name, code) with `MoveButtons` from 1.1 and
     edit/delete row actions as on `CourseTypesPage`. The empty state reads "No levels yet. Add
     the levels your classes are for." and has the "Add level" button.
   - `LevelSheet` (react-hook-form + zod: name 1–60 trimmed, code ≤ 8 optional; the server's 422s
     and 409 `duplicate` land on the fields as in `CourseTypeSheet`).
   - Delete confirms and shows the in-use message for a 409 `in-use` (as course types do).
   - The nav link "Levels" / "Stufen und Level" after "Course types" in `AdminLayout`. Pick the de
     word to match the Werkstatt wording; "Niveaus" reads oddly, so prefer "Level".
   - Strings in `admin.json` (en, de).
   - Tests in `LevelsPage.test.tsx`:
     - lists levels in API order;
     - creates one;
     - a 409 on create shows on the name field;
     - moves one down (body `{levelIds}`);
     - a 422 on reorder shows the error and refetches (review focus 1);
     - delete refused as in use.
   - `AdminLayout.test.tsx`: the new link.
9. e2e `levels.spec.ts` (both projects): the office signs in, opens Admin → Levels, and sees
   Beginner first. It adds "Masterclass" with code "MC", which appears last. It moves it up, so it
   comes before "Professional", and reloads to check the order stays. Then it deletes it.
10. ADR 0021 "Levels and class audience" in the style of 0013/0017: context (the previous app's
    free text, a fixed enum here), decision for levels (school records, name/code/position, names
    not translated, unique ignoring case, order by `setLevelOrder`, a fresh school has none), and
    a "Class audience" section marked as following in the next step (3.1 completes it).
11. `just test`, lint, `just check-generated`, `just check-sqlc`, `just test-pg`, `just web-check`,
    `just e2e`. Commit in logical commits.

## Task 3.1: class-audience

**Files:**

- Modify: `internal/domain/timetable.go` (`Class`), delete `SkillLevel` from
  `internal/domain/catalogue.go`, create `internal/domain/audience.go` (+ tests)
- Create: `internal/store/postgres/migrations/00035_class_audience.sql`,
  `internal/store/postgres/migration_audience_test.go`
- Modify: `internal/store/postgres/queries/timetable.sql`, `internal/store/postgres/timetable.go`,
  `internal/store/memory` (classes; `levelRepo.Delete` checks classes), `internal/store/storetest/
timetable.go` and `levels.go`
- Create: `internal/service/audience.go` (+ test); modify `internal/service/class_admin.go`,
  `internal/service/timetable.go`, `internal/service/schedule.go`, `internal/service/statistics.go`,
  `internal/service/servicetest/world.go`, and their tests
- Modify: `internal/httpapi/schedule.go`, `internal/httpapi/timetable.go`,
  `internal/httpapi/timetable_admin.go`, `internal/httpapi/statistics.go`, tests
- Modify: `cmd/studio-schedule/names.go`, `classes.go`, `lessons.go`, `stats.go`, goldens,
  `docs/cli.md`
- Modify: `internal/seed/demo.go` (`classSpec`), `internal/seed/seed_test.go`
- Create: `web/src/lib/audience.ts` (+ `audience.test.ts`)
- Modify: `web/src/api/client.ts`, `web/src/api/queries.ts`, `web/src/features/schedule/search.ts`,
  `filters/filterParams.ts`, `filters/ScheduleFilters.tsx`, `list/cardParts.ts`,
  `detail/LessonSheet.tsx`, `web/src/themes/programme/LessonCard.tsx` (or wherever the programme
  card renders the level), `web/src/themes/werkstatt/LessonCard.tsx`,
  `web/src/features/timetable/WeekList.tsx`, `web/src/features/admin/StatisticsPage.tsx`,
  `web/src/features/admin/classForms.ts`, `web/src/features/admin/ClassSheet.tsx`,
  `web/src/test/mockApi.ts`, `web/src/locales/{en,de}/{schedule,admin}.json`, the tests next to
  each
- Modify: `web/e2e/schedule.spec.ts` (level filter), the e2e that edits a class (find it with
  `grep -rln "Edit class\|Add class" web/e2e`)
- Modify: `docs/decisions/0021-levels-and-class-audience.md`, `docs/glossary.md` (Level row),
  `CLAUDE.md` (code map: a "Levels" mention under "Admin writes")
- Regenerate: `api/openapi.json`, `web/src/api/schema.d.ts`, CLI goldens, `docs/cli.md`

**Interfaces produced:**

```go
// internal/domain/audience.go
// AgeMonths is an age in months; 0 means not stated.
type AgeMonths int
const MaxAgeMonths AgeMonths = 1200
// LevelRange is the span between two levels of the school's scale; the ends
// may be in either order by position. The zero LevelRange states no level.
type LevelRange struct{ From, To LevelID }
func (r LevelRange) IsZero() bool

// internal/domain/timetable.go: Class loses Level and gains
	Levels    LevelRange
	AllLevels bool // for every level; never with Levels
	Stage     int  // 0 or 1–9
	AgeFrom   AgeMonths
	AgeTo     AgeMonths
// Validate rules (field names are the body's):
//   on("levelToId",   r.From.IsZero() == r.To.IsZero(), "a level range needs both ends")
//   on("allLevels",   !(c.AllLevels && !c.Levels.IsZero()), "all levels excludes a level range")
//   on("stage",       c.Stage == 0 || 1 <= c.Stage && c.Stage <= 9, …)
//   on("ageFromMonths", 0 <= AgeFrom <= MaxAgeMonths, …), same for "ageToMonths"
//   on("ageToMonths", AgeTo == 0 || AgeTo >= AgeFrom, "the age range ends before it starts")

// internal/service/audience.go
// Audience is who a class is for, its levels resolved: Low and High are the
// span's ends ordered by position (then ID), zero when the class states no level.
type Audience struct {
	Low, High      domain.Level
	AllLevels      bool
	Stage          int
	AgeFrom, AgeTo domain.AgeMonths
}
// audienceOf resolves c's levels; an unknown one is errBrokenReference.
func audienceOf(c domain.Class, levels map[domain.LevelID]domain.Level) (Audience, error)
// Covers reports whether a class with this audience is for level l: all levels,
// or l's position between Low's and High's (inclusive). No level covers nothing.
func (a Audience) Covers(l domain.Level) bool

// ClassData (class_admin.go) replaces Level with
	Levels    domain.LevelRange
	AllLevels bool
	Stage     int
	AgeFrom, AgeTo domain.AgeMonths
// An unknown level is &domain.FieldError{Field: "levelFromId" / "levelToId", Msg: "level does not exist"}.
// TimetableClass gains Audience Audience; ScheduledLesson replaces Level with Audience Audience;
// ScheduleQuery.Level becomes domain.LevelID (zero = no filter; an unknown ID matches nothing).
// The statistics row's class gains Audience.

// cmd/studio-schedule/names.go
// audienceText is "Beginner–Intermediate · stage 2 · 4–6 yrs", "all levels", or "" for none.
func audienceText(a service.Audience) string
```

HTTP:

- An embedded `Audience` in `Class` (timetable), `LessonSummary` and the statistics row:
  `levelFrom?: LevelRef`, `levelTo?: LevelRef` (lower first), `allLevels?: boolean`,
  `stage?: int`, `ageFromMonths?: int`, `ageToMonths?: int`, each omitted when absent.
  `LevelRef` is `{id, name, code?}`.
- Class request bodies (`createClass`, `updateClass`) replace `level` with optional `levelFromId`,
  `levelToId`, `allLevels`, `stage` (1–9), `ageFromMonths` and `ageToMonths` (1–1200).
- `getSchedule`'s `level` query parameter becomes a level ID (uuid). The enum is gone.

```ts
// web/src/lib/audience.ts
export type AudienceFields = Pick<
  LessonSummary,
  | "levelFrom"
  | "levelTo"
  | "allLevels"
  | "stage"
  | "ageFromMonths"
  | "ageToMonths"
>;
/** The audience in the user's language: 'long' with level names (Programme), 'short' with codes,
 * falling back to the name (Werkstatt). '' when nothing is stated. */
export function formatAudience(
  a: AudienceFields,
  t: TFunction,
  style: "long" | "short",
): string;
/** Years with halves: 42 → "3½", 48 → "4"; months that are not a half year round down to one. */
export function formatAgeYears(months: number): string;
```

**Steps:**

1. Domain tests first (`audience_test.go`, `entities_test.go`): every rule above, with the field
   name it fails on. Cases:
   - both ends set is fine, one end only fails on `levelToId`;
   - all levels plus a range fails on `allLevels`;
   - stage 10 fails;
   - age 1201 fails;
   - ages 48/72 are fine, 72/48 fails on `ageToMonths`;
   - `AgeTo` without `AgeFrom` ("up to 6") is fine.

   Delete `SkillLevel`; let the compiler list its users. Commit.

2. `service/audience_test.go`:
   - `audienceOf` orders the ends by position whichever way they are stored;
   - an unknown level → `errBrokenReference`;
   - `Covers`: an inside, outside or end level; all levels; no level;
   - review focus 2: swap two levels' positions with `SetOrder` so that a third lies between a
     class's ends, and `Covers` now includes it.
3. Migration, then a migration test. Find the old CHECK's name in `00001_core.sql`, and rebuild
   it in Down.

   ```sql
   -- +goose Up
   -- Classes state who they are for (ADR 0021): a span between two of the
   -- school's levels or all levels, a stage and an age range in months. The
   -- old fixed level moves onto level rows: one per value a class uses, in the
   -- old scale's order after the school's own levels; a level the school
   -- already has by that name (ignoring case) is used as it is.
   CREATE TEMPORARY TABLE old_levels (level text PRIMARY KEY, name text, code text, rank int) ON COMMIT DROP;
   INSERT INTO old_levels VALUES
       ('beginner', 'Beginner', 'B', 1), ('intermediate', 'Intermediate', 'I', 2),
       ('advanced', 'Advanced', 'A', 3), ('professional', 'Professional', 'P', 4);

   INSERT INTO levels (id, name, code, position)
   SELECT gen_random_uuid(), o.name,
          CASE WHEN EXISTS (SELECT 1 FROM levels l WHERE lower(l.code) = lower(o.code)) THEN NULL ELSE o.code END,
          (SELECT coalesce(max(position), 0) FROM levels) + row_number() OVER (ORDER BY o.rank)
   FROM old_levels o
   WHERE o.level IN (SELECT level FROM classes)
     AND NOT EXISTS (SELECT 1 FROM levels l WHERE lower(l.name) = lower(o.name));

   ALTER TABLE classes
       ADD COLUMN level_from_id uuid REFERENCES levels ON DELETE RESTRICT,
       ADD COLUMN level_to_id uuid REFERENCES levels ON DELETE RESTRICT,
       ADD COLUMN all_levels boolean NOT NULL DEFAULT false,
       ADD COLUMN stage smallint CHECK (stage BETWEEN 1 AND 9),
       ADD COLUMN age_from_months smallint CHECK (age_from_months BETWEEN 1 AND 1200),
       ADD COLUMN age_to_months smallint CHECK (age_to_months BETWEEN 1 AND 1200);

   UPDATE classes c SET level_from_id = l.id, level_to_id = l.id
   FROM old_levels o JOIN levels l ON lower(l.name) = lower(o.name)
   WHERE c.level = o.level;
   UPDATE classes SET all_levels = true WHERE level = 'open';

   ALTER TABLE classes
       DROP COLUMN level,
       ADD CONSTRAINT classes_level_range CHECK ((level_from_id IS NULL) = (level_to_id IS NULL)),
       ADD CONSTRAINT classes_all_levels CHECK (NOT (all_levels AND level_from_id IS NOT NULL)),
       ADD CONSTRAINT classes_age_range CHECK (age_to_months IS NULL OR age_from_months IS NULL
                                               OR age_to_months >= age_from_months);
   CREATE INDEX classes_level_from_idx ON classes (level_from_id);
   CREATE INDEX classes_level_to_idx ON classes (level_to_id);

   -- +goose Down
   -- Lossy: a span keeps its lower end's old value if its name is one, else open.
   ```

   Write Down to restore `level text NOT NULL` with the original CHECK. Map `level_from_id`'s
   name back through the same four names (else `'open'`), then drop the new columns, indexes and
   constraints. Leave the levels in place (2.1's Down drops the table).

   `migration_audience_test.go` (package `postgres`, runs with `just test-pg`):
   - Take a `newUnmigratedDatabase(t)` and a goose provider (`newProvider`), `UpTo(34)`.
   - Insert one level named "BEGINNER" with code "B", and a location, room, course type, staff
     and term.
   - Insert classes with level `beginner`, `advanced`, `open` and a second `advanced`.
   - `UpTo(35)`.
   - Assert:
     - the `beginner` class points at the existing "BEGINNER" row;
     - one new "Advanced" level got position 2 and code "A", and both advanced classes use it;
     - no "Intermediate" or "Professional" row was created;
     - the open class has `all_levels` and no range.
   - `Down` once, and assert the classes have their old `level` values back.

4. Store: `timetable.sql` and `timetable.go` read and write the new columns. Memory classes the
   same; memory `levelRepo.Delete` refuses a level a class uses as either end
   (`domain.ErrConflict`). storetest:
   - a class round-trips its audience (span both ways round, all levels, stage, ages, none);
   - deleting a level used as `From` → conflict, as `To` → conflict;
   - unused → fine.

   Run `just test-pg`.

5. Service:
   - `ClassData` gets the new fields.
   - `CreateClass`/`UpdateClass` check that both levels exist (field errors) inside the
     transaction.
   - `TimetableService.classes` and the admin list fill `Audience`, reading `Levels().List` once
     per call.
   - `lessonRefs` loads the levels once, and `resolve` fills `ScheduledLesson.Audience` for class
     lessons.
   - `ScheduleQuery.Level` is a `domain.LevelID`. `validate` no longer checks it; `matches` uses
     the filter level from `lessonRefs.levels`, and an unknown ID matches nothing.
   - Statistics rows carry the class's audience.
   - Update `servicetest/world.go`.
   - Tests:
     - review focus 3: classes with spans A–C, A–A and C–C, an all-levels class, a class without a
       level and a workshop, filtered by B → A–C and all levels only; filtered by an unknown ID →
       nothing;
     - an unknown level on create → 422 field error;
     - the fault sweep for the new reads.
6. HTTP:
   - The embedded `Audience` and `LevelRef`; a converter `audience(a service.Audience)` in
     `httpapi`, exported as `NewAudience` if the CLI needs it.
   - The class bodies and the query parameter.
   - Update the doc strings.
   - Tests: create a class with `levelFromId`/`levelToId` reversed → the response lists the lower
     level as `levelFrom`; `allLevels` with a range → 422 on `body.allLevels`; `stage: 10` → 422;
     the schedule filter by level ID.
   - `just gen-api`.
7. Command line: `audienceText` (English, short: `Beginner–Intermediate · stage 2 · 4–6 yrs`,
   `all levels`, ages `from 3½ yrs` / `up to 6 yrs`), with unit tests in `names_test.go` or the
   file's neighbour. Use it in:
   - `classes.go`: the column "Level" becomes "Audience";
   - `lessons.go` line 168: `name (audience)`, or just the name when there is none;
   - `stats.go` line 143, likewise.

   Then `-update` the goldens and run `just cli-docs` if a help text changed.

8. Seed:
   - `classSpec.level` becomes `audience audienceSpec` with
     `type audienceSpec struct{ from, to string; all bool; stage int; ageFrom, ageTo domain.AgeMonths }`
     (level names) and helpers `level(name string) audienceSpec` (`from == to`) and
     `allLevels audienceSpec`.
   - Map each programme class: `LevelOpen` → `allLevels`, any other → `level(<English name>)`.
   - The children's classes state their ages instead of a level, as their course type
     descriptions say: Creative Dance 48–72 months, Pre-Ballet 72–96 months.
   - One Classical Ballet class gets `Beginner`–`Intermediate`, so the demo shows a span.
   - Seed test: every class's levels resolve, and that span exists.
9. Web:
   - `audience.test.ts` first, en and de:
     - "Beginner–Intermediate · Stage 2 · 4–6 yrs" (long) and "A/M · Stufe 2 · 4–6 J." (short,
       de);
     - one level shows only its name or code;
     - "All levels" / "alle Level";
     - ages: "from 3½ yrs" / "ab 3½ J.", "up to 6 yrs" / "bis 6 J.";
     - nothing → "";
     - short form without a code falls back to the name;
     - `formatAgeYears(40)` → "3", `(42)` → "3½", `(48)` → "4".

     Then implement. Keys in `schedule.json`: `audience.allLevels`, `audience.stage`
     ("Stage {{stage}}" / "Stufe {{stage}}"), `audience.ageFrom`, `audience.ageTo`,
     `audience.ageRange`. Remove `level.*`, and `LessonLevel` from `client.ts`.

   - Cards (`cardParts.ts` part `'level'` → `'audience'`): programme card long form, Werkstatt
     card short form; `LessonSheet` long form; `WeekList` badge; the statistics table column.
     Update their tests and `mockApi.ts` fixtures.
   - Filter: `search.ts` `level: z.string().uuid().optional().catch(undefined)` (an old enum value
     in a bookmark is dropped; use the zod API the file uses for `courseType` if it differs); drop
     `levels` and the compile-time check; `ScheduleFilters` level options from `levelsQuery` (by
     name, API order), hidden when the school has no levels; tests.
   - Class sheet and `classForms.ts`:
     - "Level" select: "No level", "All levels", then each level (sets `levelFromId`).
     - "up to" select, shown once a level is chosen: the levels, defaulting to the chosen one.
     - "Stage" number (1–9, empty = none).
     - "Age from"/"Age to" in years, step 0.5 (stored as months).
     - zod checks the same rules (to ≥ from for ages; stage 1–9), and the server's 422s land on
       their fields.
     - With no levels (review focus 4), the selects are replaced by "No levels yet –" plus a link
       to `/admin/levels`, and saving works.

     Tests in the existing ClassSheet tests (`ClassesPage.test.tsx` or its own):
     - create with a span;
     - edit an all-levels class;
     - ages 3.5 → `ageFromMonths: 42`;
     - a 422 on `body.levelToId` shows on "up to";
     - the no-levels case.

   - The level mutations from 2.1 also invalidate the classes and schedule queries (names show
     there).

10. e2e:
    - `schedule.spec.ts`: filter by "Intermediate" shows the Classical Ballet span class and an
      all-levels class and hides a Creative Dance class (find days with the seed's schedule, as
      the file's other cases do).
    - The class-editing e2e: set a level span and see it on the class row.
11. Docs: complete ADR 0021 ("Class audience": span rule, all levels, stage and age in months,
    the filter rule, the migration's mapping and its Down); glossary "Level" row (a school's
    level record; "Audience" row: who a class is for); CLAUDE.md "Admin writes": levels in
    `service/catalog_admin.go` with the room and course type writes.
12. `just test`, lint, `just check-generated`, `just check-sqlc`, `just test-pg`, `just web-check`,
    `just wasm-smoke`, `just e2e`, `just test-cover && just coverage`. Commit in logical commits.

## Task 4.1: seed-split

**Files:**

- Modify: `internal/seed/demo.go`, `internal/seed/seed.go`, `internal/seed/announcements.go`,
  `internal/seed/duplicates.go`, `internal/seed/logins.go`
- Create: `internal/seed/programme.go` (the programme school's tables, moved), and
  `internal/seed/werkstatt.go` (for now the programme tables with Werkstatt's settings and places,
  exactly today's behaviour)
- Modify: `internal/seed/seed_test.go` (split per school; may become `seed_test.go` +
  `schools_test.go`)
- Modify: `cmd/wasm/main.go` (the school's logins)
- Modify: `docs/demo-mode.md` (one sentence: each school is built from its own tables)

**Interfaces produced:**

```go
// internal/seed: everything a demo school is built from.
type schoolSpec struct {
	settings      domain.Settings
	background    domain.Background
	households    []householdSpec
	locations     []locationSpec
	staff         []staffSpec
	levels        []levelSpec
	courseTypes   []domain.CourseType
	classes       []classSpec
	workshops     []workshopSpec
	fullWorkshop  fullWorkshopSpec
	staffBookings staffBookingsSpec
	rental        rentalSpec
	passTypes     []domain.PassType
	passes        []passSpec
	announcements []announcementSpec
	duplicates    []domain.Person
	// attendanceSkipped is the course type whose lessons have no attendance
	// taken yet (today's constant "Classical Ballet").
	attendanceSkipped string
	// termName names the term, e.g. "School Year 2026/27".
	termName func(start, end domain.Date) string
}
func programme() schoolSpec
func werkstatt() schoolSpec // 4.1: programme() with Werkstatt's settings, background and places
// schoolOf replaces schoolData; an unknown school is domain.ErrValidation as today.
func schoolOf(school School) (schoolSpec, error)
func demoAt(anchor domain.Date, s schoolSpec) (Dataset, error)

// SchoolLogins returns the first seeded account of each role of school's
// dataset, like DemoLogins; DemoLogins() is SchoolLogins(SchoolProgramme).
func SchoolLogins(school School) ([]Login, error)
```

Move every package-level table that a school needs (`demoHouseholds`, `demoStaff`,
`demoCourseTypes`, `demoClasses`, `demoWorkshops`, `demoFullWorkshop`, `demoStaffBookings`,
`demoRental`, `demoPassTypes`, `demoPasses`, `demoAnnouncements`, `demoDuplicates`, `demoLevels`,
`demoLocations`, the `attendanceSkipped` constant) into `programme()`. The builder methods read
them from the `schoolSpec` they get, never from a package variable. Keep the `holds` room mapping
and the `placeIDs` ID padding for now; 5.1 removes them.

**Steps:**

1. Before any change, add `TestProgrammeDatasetUnchanged`. It JSON-encodes `DemoAt(FixedDemoAnchor)`
   (every field: `encoding/json` on the `Dataset`) and compares its SHA-256 with a constant. Run it
   once to print the digest, then set the constant. The comment says: a deliberate change to the
   programme data updates the digest, which the test prints on failure. Commit
   `test(seed): pin the programme demo dataset`.
2. Turn the existing per-dataset tests into tests over `Schools()`, each as `t.Run(string(school))`
   on `DemoSchoolAt(FixedDemoAnchor, school)`: `TestDemoValid`, `TestDemoReferences`,
   `TestDemoTimetableConflicts` (classes in one room never overlap), `TestDemoChildren`,
   `TestLoadDemoRoundTrip`. Add:
   - `TestDemoWorkshopsFree`: a workshop or the rental never overlaps a class lesson in its room;
   - `TestDemoInstructorsNeverDoubleBooked`: no instructor or accompanist has two classes at once;
   - `TestSchoolLogins`: per school, one login per role with an account; each signs in through
     `service.AuthService` on a store loaded with that school, the way `TestDemoLogins` checks
     today.

   Tests that check programme-only facts (names, counts, specific scenarios) stay programme-only.
   Delete `TestDemoSchoolSameData`. For now, `TestWerkstattPlaces` stays as it is.

3. Refactor `demo.go` into `schoolSpec`, `programme()` and `werkstatt()`. The builder takes the
   spec; `DemoAt`/`DemoSchoolAt` build `schoolOf(school)`. `go test ./internal/seed` must stay
   green at every commit, `TestProgrammeDatasetUnchanged` included.
4. `SchoolLogins` and `DemoLogins`; `cmd/wasm/main.go` passes `seed.SchoolLogins(school)` for the
   school it seeds (it parses `DEMO_SCHOOL` today). `serve` keeps `DemoLogins()` (it seeds the
   programme school). A wasmbridge or app test that the Werkstatt demo's `/api/v1/demo/logins`
   lists the Werkstatt school's logins (the same people today, so assert they equal
   `SchoolLogins(SchoolWerkstatt)`).
5. `just test`, lint, `just check-wasm-deps`, `just wasm-smoke`, `just e2e`,
   `just test-cover && just coverage`. Commit `refactor(seed): build each demo school from its own
tables`.

## Task 5.1: werkstatt-data

**Files:**

- Modify: `internal/seed/werkstatt.go` (its own tables), `internal/seed/demo.go` (remove the
  `holds` mapping and `placeIDs` padding), `internal/seed/seed_test.go` / `schools_test.go`
- Modify: `web/e2e/werkstatt.spec.ts`, `web/e2e/fixtures.ts` (only if the Werkstatt login names
  need it)
- Modify: `docs/decisions/0018-themes-and-layouts.md` (amendment), `docs/demo-mode.md`,
  `README.md` (only if it says the schools share data), `CLAUDE.md` (code map: themes bullet if it
  says the demo schools share data)

**Interfaces:** none new. `werkstatt()` returns its own `schoolSpec`, and its `termName` gives
"Schuljahr 2026/27".

**Data** (spec §3, transcribe exactly; every name is invented here, none comes from elsewhere):

- **Households and people:** the same number and kinds as the programme school's
  (`programme().households`), with German names:
  - families with children, among them a teenager with an own account (managed dependent);
  - single adults;
  - an account per role for the demo logins.

  Emails are `vorname.nachname@<host>` on `*.example` hosts. Use ASCII in the address
  (ä → ae, ß → ss); keep the umlauts in the names. For each person, the role and the
  family/guardian links mirror the programme record at the same index, so the scenarios
  (managed dependent, guardian with several children, former instructor, accompanist with a
  person record) all exist.

- **Staff:** about seven instructors (one former) and two accompanists, German names. There must
  be enough for the 70 classes, so that `TestDemoInstructorsNeverDoubleBooked` passes. Give
  instructors a profile of course kinds: ballet, contemporary, jazz/modern, hip-hop/step/Irish,
  and health (Qigong, Taiji, Stretch). That keeps the assignment readable.
- **Rooms:** one location "Tanzhaus am Lindenhof", rooms Oben 24 (1), Unten 18 (2), Online 40
  (3), Trainingsraum 8 (4).
- **Levels:** Einsteiger E, Anfänger A, Mittelstufe M, Fortgeschritten F, Profi P.
- **Course types:** the 23 of the spec, each with a short, newly written German description of one
  sentence.
- **Classes:** the spec's weekly plan table row by row:
  - an E/A/M/F code range → `audienceSpec{from, to}` by level name;
  - "alle" → `allLevels`;
  - "St. n" → `stage`;
  - "ab n J." → `ageFrom`, "n–m J." → both ages, in months (`n*12`);
  - 1–4 class members each, assigned so that every room has members and the children's classes
    have children of the right age (by birth date relative to `FixedDemoAnchor`).
- **Workshops** (Sunday): one full with a waiting list, one with guests and a trial its
  instructor added. **Rental:** one in Unten on a weekday evening when the room is free, with a
  fictional renter ("Theatergruppe Kupferblatt"). German titles as the spec lists.
- **Passes:** "10er-Karte" and "Monatskarte" plus the programme school's other kinds in German,
  invented whole-euro prices. The `passSpec`s and drop-ins mirror the programme scenarios.
- **Announcements:** German, the same audiences and timing as the programme ones.
- **Duplicates:** a German pair, like the programme's.
- **attendanceSkipped:** "Ballett Erwachsene".

**Steps:**

1. Tests first, in `schools_test.go`. The 4.1 suite already runs per school; add Werkstatt facts:
   - `TestWerkstattCatalogue`: 4 rooms in order Oben, Unten, Online, Trainingsraum with the
     capacities; 5 levels in order with codes; 23 course types; 70 classes from Monday to
     Saturday, none on Sunday; each class's room capacity ≥ its capacity.
   - `TestWerkstattAudiences`: spot checks of five rows of the spec table (e.g. Monday Oben 18:00
     Contemporary A/M; Tuesday Trainingsraum 17:00 Spitze E, from 144 months; a Taiji row all
     levels).
   - `TestWerkstattOwnPeople`: no person, staff name or email of the Werkstatt dataset appears in
     the programme dataset; every email host ends in `.example`.
   - `TestWerkstattWorkshopsOnSunday`: every workshop is on a Sunday.

   Rewrite `TestWerkstattPlaces` for the new rooms. Run them and see them fail.

2. Write the tables in `werkstatt.go`. Remove `holds` and `placeIDs` from `demo.go` (each school
   has its own rooms now). Run until the whole seed suite passes; `TestProgrammeDatasetUnchanged`
   must still pass.
3. e2e `werkstatt.spec.ts`:
   - the room regex gains Online, and the column order test from 1.1 covers four rooms;
   - a Werkstatt card shows a level code range such as "A/M";
   - the login page in the Werkstatt demo lists German names, and `signIn` works for member and
     office (review focus 5);
   - on the phone project, the four-room day falls back to one list.

   Fix any Werkstatt e2e case that pinned a programme name.

4. Docs:
   - ADR 0018 amendment "The demo schools have their own data" (what each school has, the
     programme digest test, why the Werkstatt catalogue is shaped like the previous app's but
     moved and renamed);
   - `docs/demo-mode.md`: the two schools' data;
   - README/CLAUDE.md only where they say the schools share data
     (`grep -rn "share" README.md CLAUDE.md docs/demo-mode.md`).
5. `just test`, lint, `just wasm-smoke`, `just web-check`, `just e2e`,
   `just test-cover && just coverage`. Look at the Werkstatt demo (`just demo-serve`, `?demo=werkstatt`)
   on desktop and phone width. Put a short list of what still differs from the spec's intent in
   the PR body. Commit `feat(seed): the Werkstatt demo school has its own people and catalogue`.

## Running it

Per phase N (1 … 5), after the previous phase's PR is merged:

```sh
GH_TOKEN=$(gh auth token -u CWBudde) ccbatch run plan --repo . \
  --file docs/superpowers/plans/2026-11-01-werkstatt-catalogue.md --prefix N. \
  --branch 'feat/werkstatt-{{.Slug}}' \
  --verify 'just web-install' --verify 'go build ./...' --verify 'just test' \
  --verify 'PATH=$HOME/go/bin:$PATH just lint' --verify 'just check-generated' \
  --verify 'just check-sqlc' --verify 'just web-check' --verify 'just wasm-smoke' \
  --verify 'E2E_PORT=$((4200 + $(pwd | cksum | cut -d" " -f1) % 700)) just e2e'
```
