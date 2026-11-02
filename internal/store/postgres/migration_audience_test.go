package postgres

import (
	"database/sql"
	"testing"
)

// TestClassAudienceMigration upgrades classes with the old fixed level
// (00035): a level the school has by that name, ignoring case, is used as it
// is, a missing one is added after the school's levels (only for the values
// classes use), and open becomes all levels. Down brings the old values back.
func TestClassAudienceMigration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m, err := NewMigrator(newUnmigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	if _, err := m.provider.UpTo(ctx, 34); err != nil {
		t.Fatalf("up to 34: %v", err)
	}

	const (
		loc, room, ct, staff = "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000002",
			"00000000-0000-7000-8000-000000000003", "00000000-0000-7000-8000-000000000004"
		term, existing = "00000000-0000-7000-8000-000000000005", "00000000-0000-7000-8000-000000000006"
	)

	classes := map[string]string{ // ID → old level
		"00000000-0000-7000-8000-000000000011": "beginner",
		"00000000-0000-7000-8000-000000000012": "advanced",
		"00000000-0000-7000-8000-000000000013": "open",
		"00000000-0000-7000-8000-000000000014": "advanced",
	}

	for _, stmt := range []string{
		// The school's own level, by the old name in other case; its code
		// takes the one the old scale would give Beginner.
		`INSERT INTO levels (id, name, code, position) VALUES ('` + existing + `', 'BEGINNER', 'B', 1)`,
		`INSERT INTO locations (id, name) VALUES ('` + loc + `', 'Hall')`,
		`INSERT INTO rooms (id, location_id, name, capacity, position) VALUES ('` + room + `', '` + loc + `', 'Studio', 5, 1)`,
		`INSERT INTO course_types (id, name) VALUES ('` + ct + `', 'Tap')`,
		`INSERT INTO staff (id, display_name, kind, active) VALUES ('` + staff + `', 'Ada', 'instructor', true)`,
		`INSERT INTO terms (id, name, starts_on, ends_on) VALUES ('` + term + `', 'Spring', '2026-03-01', '2026-06-30')`,
	} {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	for id, level := range classes {
		if _, err := m.db.ExecContext(ctx, `INSERT INTO classes (id, term_id, course_type_id, room_id, instructor_id,
			level, weekday, start_time, duration, capacity) VALUES ($1, $2, $3, $4, $5, $6, 1, '17:00', '1 hour', 5)`,
			id, term, ct, room, staff, level); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := m.provider.UpTo(ctx, 35); err != nil {
		t.Fatalf("up to 35: %v", err)
	}

	// The levels: the school's own unchanged, Advanced added after it.
	// Intermediate and Professional, which no class used, are not.
	type level struct {
		name     string
		code     sql.NullString
		position int
	}

	byName := map[string]string{} // name → ID

	rows, err := m.db.QueryContext(ctx, `SELECT id, name, code, position FROM levels ORDER BY position`)
	if err != nil {
		t.Fatal(err)
	}

	var levels []level

	for rows.Next() {
		var (
			id string
			l  level
		)

		if err := rows.Scan(&id, &l.name, &l.code, &l.position); err != nil {
			t.Fatal(err)
		}

		levels = append(levels, l)
		byName[l.name] = id
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := []level{
		{"BEGINNER", sql.NullString{String: "B", Valid: true}, 1},
		{"Advanced", sql.NullString{String: "A", Valid: true}, 2},
	}
	if len(levels) != len(want) || levels[0] != want[0] || levels[1] != want[1] {
		t.Fatalf("levels %+v, want %+v", levels, want)
	}

	if byName["BEGINNER"] != existing {
		t.Errorf("BEGINNER has ID %s, want %s", byName["BEGINNER"], existing)
	}

	wantAudience := map[string]struct {
		from, to string
		all      bool
	}{
		"beginner": {existing, existing, false},
		"advanced": {byName["Advanced"], byName["Advanced"], false},
		"open":     {"", "", true},
	}

	for id, old := range classes {
		var (
			from, to              sql.NullString
			all                   bool
			stage, ageFrom, ageTo sql.NullInt16
		)

		err := m.db.QueryRowContext(ctx, `SELECT level_from_id, level_to_id, all_levels, stage, age_from_months,
			age_to_months FROM classes WHERE id = $1`, id).Scan(&from, &to, &all, &stage, &ageFrom, &ageTo)
		if err != nil {
			t.Fatal(err)
		}

		w := wantAudience[old]
		if from.String != w.from || to.String != w.to || from.Valid != (w.from != "") || all != w.all {
			t.Errorf("class %s (%s): levels %v–%v, all %v; want %q–%q, all %v", id, old, from, to, all, w.from, w.to, w.all)
		}

		if stage.Valid || ageFrom.Valid || ageTo.Valid {
			t.Errorf("class %s states a stage or an age: %v, %v, %v", id, stage, ageFrom, ageTo)
		}
	}

	// Down once: every class has its old level back, and the levels stay.
	if res, err := m.Down(ctx); err != nil || res.Name != "00035_class_audience.sql" {
		t.Fatalf("down = %+v, %v; want 00035 rolled back", res, err)
	}

	for id, want := range classes {
		var level string
		if err := m.db.QueryRowContext(ctx, `SELECT level FROM classes WHERE id = $1`, id).Scan(&level); err != nil {
			t.Fatal(err)
		}

		if level != want {
			t.Errorf("class %s: level %q after down, want %q", id, level, want)
		}
	}

	var n int
	if err := m.db.QueryRowContext(ctx, `SELECT count(*) FROM levels`).Scan(&n); err != nil || n != 2 {
		t.Errorf("%d levels after down, %v; want 2", n, err)
	}

	// The old check is back.
	if _, err := m.db.ExecContext(ctx, `UPDATE classes SET level = 'expert'`); err == nil {
		t.Error("down left classes.level without its check")
	}
}

// TestClassAudienceMigrationDown rolls back spans the old level cannot hold:
// a span keeps its lower end by position if that is one of the old names (in
// any case), anything else becomes open.
func TestClassAudienceMigrationDown(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m, err := NewMigrator(newUnmigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	if _, err := m.provider.UpTo(ctx, 35); err != nil {
		t.Fatalf("up to 35: %v", err)
	}

	const (
		loc, room, ct, staff = "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000002",
			"00000000-0000-7000-8000-000000000003", "00000000-0000-7000-8000-000000000004"
		term               = "00000000-0000-7000-8000-000000000005"
		basics, inter, adv = "00000000-0000-7000-8000-000000000021", "00000000-0000-7000-8000-000000000022", "00000000-0000-7000-8000-000000000023"
	)

	for _, stmt := range []string{
		`INSERT INTO levels (id, name, position) VALUES ('` + basics + `', 'Basics', 1),
			('` + inter + `', 'INTERMEDIATE', 2), ('` + adv + `', 'Advanced', 3)`,
		`INSERT INTO locations (id, name) VALUES ('` + loc + `', 'Hall')`,
		`INSERT INTO rooms (id, location_id, name, capacity, position) VALUES ('` + room + `', '` + loc + `', 'Studio', 5, 1)`,
		`INSERT INTO course_types (id, name) VALUES ('` + ct + `', 'Tap')`,
		`INSERT INTO staff (id, display_name, kind, active) VALUES ('` + staff + `', 'Ada', 'instructor', true)`,
		`INSERT INTO terms (id, name, starts_on, ends_on) VALUES ('` + term + `', 'Spring', '2026-03-01', '2026-06-30')`,
	} {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	classes := map[string]struct {
		from, to string // level IDs, "" for none
		all      bool
		want     string
	}{
		"00000000-0000-7000-8000-000000000011": {adv, inter, false, "intermediate"}, // the lower end, stored as To
		"00000000-0000-7000-8000-000000000012": {inter, adv, false, "intermediate"},
		"00000000-0000-7000-8000-000000000013": {adv, adv, false, "advanced"},
		"00000000-0000-7000-8000-000000000014": {basics, adv, false, "open"}, // not an old name
		"00000000-0000-7000-8000-000000000015": {"", "", true, "open"},
		"00000000-0000-7000-8000-000000000016": {"", "", false, "open"},
	}

	for id, c := range classes {
		if _, err := m.db.ExecContext(ctx, `INSERT INTO classes (id, term_id, course_type_id, room_id, instructor_id,
			level_from_id, level_to_id, all_levels, stage, age_from_months, age_to_months, weekday, start_time,
			duration, capacity) VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, $8, 2, 48, 72,
			1, '17:00', '1 hour', 5)`,
			id, term, ct, room, staff, c.from, c.to, c.all); err != nil {
			t.Fatal(err)
		}
	}

	if res, err := m.Down(ctx); err != nil || res.Name != "00035_class_audience.sql" {
		t.Fatalf("down = %+v, %v; want 00035 rolled back", res, err)
	}

	for id, c := range classes {
		var level string
		if err := m.db.QueryRowContext(ctx, `SELECT level FROM classes WHERE id = $1`, id).Scan(&level); err != nil {
			t.Fatal(err)
		}

		if level != c.want {
			t.Errorf("class %s: level %q after down, want %q", id, level, c.want)
		}
	}

	// And up again: the old names find their levels. Professional, which a
	// class now uses, is added after the school's levels, without the code
	// another level has in other case.
	const pro, proClass = "00000000-0000-7000-8000-000000000024", "00000000-0000-7000-8000-000000000017"

	for _, stmt := range []string{
		`INSERT INTO levels (id, name, code, position) VALUES ('` + pro + `', 'Pro', 'p', 4)`,
		`INSERT INTO classes (id, term_id, course_type_id, room_id, instructor_id, level, weekday, start_time,
			duration, capacity) VALUES ('` + proClass + `', '` + term + `', '` + ct + `', '` + room + `', '` + staff +
			`', 'professional', 1, '17:00', '1 hour', 5)`,
	} {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}

	var (
		n        int
		code     sql.NullString
		position int
		usedBy   int
	)

	if err := m.db.QueryRowContext(ctx, `SELECT count(*) FROM levels`).Scan(&n); err != nil || n != 5 {
		t.Errorf("%d levels after up again, %v; want 5 (Professional added)", n, err)
	}

	err = m.db.QueryRowContext(ctx, `SELECT l.code, l.position, count(c.id) FROM levels l
		LEFT JOIN classes c ON c.level_from_id = l.id AND c.level_to_id = l.id
		WHERE l.name = 'Professional' GROUP BY l.id`).Scan(&code, &position, &usedBy)
	if err != nil || code.Valid || position != 5 || usedBy != 1 {
		t.Errorf("Professional: code %v, position %d, %d classes, %v; want no code, position 5, 1 class",
			code, position, usedBy, err)
	}

	for id, c := range classes {
		var (
			from, to sql.NullString
			all      bool
		)

		if err := m.db.QueryRowContext(ctx, `SELECT level_from_id, level_to_id, all_levels FROM classes WHERE id = $1`,
			id).Scan(&from, &to, &all); err != nil {
			t.Fatal(err)
		}

		wantID := map[string]string{"intermediate": inter, "advanced": adv}[c.want]
		if from.String != wantID || to.String != wantID || all != (c.want == "open") {
			t.Errorf("class %s (%s) after up again: levels %v–%v, all %v", id, c.want, from, to, all)
		}
	}
}
