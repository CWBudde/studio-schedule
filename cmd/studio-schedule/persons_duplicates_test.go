package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The demo persons of the duplicates and merge tests (seed.LoadDemo): the
// strong pair Lena Quillfeather, the weak pair Jonas Brightwater, and
// family members with bookings, class memberships and accounts.
const (
	pdLenaA    = "00000000-0000-7000-8000-000000000326"
	pdLenaB    = "00000000-0000-7000-8000-000000000327"
	pdJonasA   = "00000000-0000-7000-8000-000000000328"
	pdJonasB   = "00000000-0000-7000-8000-000000000329"
	pdClara    = "00000000-0000-7000-8000-000000000001" // the member account demoMember
	pdTom      = "00000000-0000-7000-8000-000000000003" // a member account too
	pdElla     = "00000000-0000-7000-8000-000000000005" // Clara's dependent
	pdEmil     = "00000000-0000-7000-8000-000000000015" // in a class of Ella's, booked into its next lesson
	pdUnknown  = "00000000-0000-7000-8000-000000000999"
	pdEllaJazz = "00000000-0000-7000-8000-000000000061" // the class Ella and Emil are members of
	pdLesson   = "00000000-0000-7000-8000-0000000000c9" // its next lesson, both booked
	pdBallet   = "00000000-0000-7000-8000-00000000005c" // a class of Ella's only
)

// pdWith is r with stdin, a terminal or not, and env added to r's.
func pdWith(r cliRun, terminal bool, stdin string, env ...string) cliRun {
	out := r
	out.terminal, out.stdin = terminal, stdin
	out.env = map[string]string{}

	for k, v := range r.env {
		out.env[k] = v
	}

	for i := 0; i+1 < len(env); i += 2 {
		out.env[env[i]] = env[i+1]
	}

	return out
}

// pdParse parses the ID s, failing t if it is invalid.
func pdParse[I domain.AnyID](t *testing.T, s string) I {
	t.Helper()

	id, err := domain.ParseID[I](s)
	if err != nil {
		t.Fatal(err)
	}

	return id
}

// pdRead runs fn in a read-only transaction of st.
func pdRead(t *testing.T, st store.Store, fn func(ctx context.Context, tx store.Tx) error) {
	t.Helper()

	if err := st.InTx(t.Context(), store.ReadOnly, fn); err != nil {
		t.Fatal(err)
	}
}

// pdPerson returns the person id of st, merged and anonymized ones too.
func pdPerson(t *testing.T, st store.Store, id string) domain.Person {
	t.Helper()

	var p domain.Person

	pdRead(t, st, func(ctx context.Context, tx store.Tx) error {
		var err error
		p, err = tx.Persons().Get(ctx, pdParse[domain.PersonID](t, id))

		return err
	})

	return p
}

// pdLive fails t unless every person of ids is in st and not merged.
func pdLive(t *testing.T, st store.Store, ids ...string) {
	t.Helper()

	for _, id := range ids {
		if p := pdPerson(t, st, id); p.Merged() {
			t.Errorf("%s %s was merged into %s", p.GivenName, id, p.MergedInto)
		}
	}
}

// pdDismissals returns the duplicate dismissals of st.
func pdDismissals(t *testing.T, st store.Store) []domain.DuplicateDismissal {
	t.Helper()

	var ds []domain.DuplicateDismissal

	pdRead(t, st, func(ctx context.Context, tx store.Tx) error {
		var err error
		ds, err = tx.DuplicateDismissals().List(ctx)

		return err
	})

	return ds
}

// pdAudit returns the newest audit events of st.
func pdAudit(t *testing.T, st store.Store) []domain.AuditEvent {
	t.Helper()

	var events []domain.AuditEvent

	pdRead(t, st, func(ctx context.Context, tx store.Tx) error {
		var err error
		events, err = tx.Audit().List(ctx, 10)

		return err
	})

	return events
}

// pdMerges counts the person-merged events of st.
func pdMerges(t *testing.T, st store.Store) int {
	t.Helper()

	n := 0

	for _, e := range pdAudit(t, st) {
		if e.Action == domain.AuditPersonMerged {
			n++
		}
	}

	return n
}

// pdLines are the lines both demo pairs make in persons duplicates, and the
// weak pair's alone (the columns narrower).
const (
	pdJonasAlone = "weak  name  " + pdJonasA + "  Jonas Brightwater  " + pdJonasB + "  Jonas Brightwater\n"
	pdLenaLine   = "strong  email,name  " + pdLenaA + "  Lena Quillfeather  " + pdLenaB + "  Lena Quillfeather\n"
	pdJonasLine  = "weak    name        " + pdJonasA + "  Jonas Brightwater  " + pdJonasB +
		"  Jonas Brightwater\n"
)

// TestPersonsDuplicates lists the demo's two pairs, the strong one first:
// as text records, with a header line only on a terminal, and as the body
// of listDuplicates.
func TestPersonsDuplicates(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "persons", "duplicates", "--as", demoOffice)
		if got.code != 0 || got.stderr != "" || got.stdout != pdLenaLine+pdJonasLine {
			t.Fatalf("text: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/duplicates.txt", got.stdout)

		got = pdWith(r, true, "", "NO_COLOR", "1").exec(t, "persons", "duplicates", "--as", demoOffice)
		if got.code != 0 || !strings.HasPrefix(got.stdout, "Strength  Reasons     Person A  ") {
			t.Fatalf("terminal: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		golden(t, "persons/duplicates-terminal.txt", got.stdout)

		got = r.exec(t, "persons", "duplicates", "--as", demoAdmin, "--output", "json")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("JSON: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/duplicates.json", got.stdout)

		var body struct {
			Items []struct {
				Strength string   `json:"strength"`
				Reasons  []string `json:"reasons"`
				A        struct {
					ID string `json:"id"`
				} `json:"a"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || len(body.Items) != 2 ||
			body.Items[0].Strength != "strong" || body.Items[0].A.ID != pdLenaA || body.Items[1].Strength != "weak" {
			t.Errorf("JSON body %+v, %v", body, err)
		}
	})
}

// TestPersonsDismiss dismisses the weak pair: it is no longer listed, the
// dismissal is the office account's, and dismissing it again changes
// nothing. JSON mode prints nothing; an unknown person, an invalid ID and
// the same person twice are refused and record nothing.
func TestPersonsDismiss(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)

		got := r.exec(t, "persons", "duplicates", "dismiss", "--as", demoOffice, pdJonasB, pdJonasA)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("dismiss: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/dismiss.txt", got.stdout)

		if got := r.exec(t, "persons", "duplicates", "--as", demoOffice); got.code != 0 || got.stdout != pdLenaLine {
			t.Errorf("after the dismissal: exit %d, stdout %q, want the strong pair alone", got.code, got.stdout)
		}

		ds := pdDismissals(t, st)
		if len(ds) != 1 || ds[0].PersonA.String() != pdJonasA || ds[0].PersonB.String() != pdJonasB ||
			ds[0].DismissedBy != accountID(t, st, demoOffice) || !ds[0].DismissedAt.Equal(testNow) {
			t.Errorf("dismissals = %+v, want the pair, ordered, by the office at testNow", ds)
		}

		// Again, as JSON: nothing on stdout, and still one dismissal.
		got = r.exec(t, "persons", "duplicates", "dismiss", "--as", demoAdmin, "--output", "json", pdJonasA, pdJonasB)
		if got.code != 0 || got.stdout != "" || got.stderr != "" {
			t.Errorf("again: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		if ds := pdDismissals(t, st); len(ds) != 1 || ds[0].DismissedBy != accountID(t, st, demoOffice) {
			t.Errorf("after the second dismissal: %+v, want the first kept", ds)
		}

		for name, tc := range map[string]struct {
			args   []string
			code   int
			stderr string
		}{
			"unknown person": {
				args: []string{pdLenaA, pdUnknown}, code: 3,
				stderr: "error: dismiss duplicate " + pdLenaA + "/" + pdUnknown + ": person: not found\n",
			},
			"invalid ID": {
				args: []string{"lena", pdLenaB}, code: 2, stderr: "error: validation failed: invalid ID \"lena\"\n",
			},
			"invalid second ID": {args: []string{pdLenaA, "nope"}, code: 2},
			"same person": {
				args: []string{pdLenaA, pdLenaA}, code: 2, stderr: "error: <person-b>: the persons must differ\n",
			},
			"one person": {
				args: []string{pdLenaA}, code: 2, stderr: "error: accepts 2 arg(s), received 1\n",
			},
		} {
			args := append([]string{"persons", "duplicates", "dismiss", "--as", demoOffice}, tc.args...)

			got := r.exec(t, args...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", name, got.code, got.stdout,
					got.stderr, tc.code, tc.stderr)
			}
		}

		// The same person twice as JSON: the problem names the argument.
		got = r.exec(t, "persons", "duplicates", "dismiss", "--as", demoOffice, "-o", "json", pdLenaA, pdLenaA)
		var problem struct {
			Status int `json:"status"`
			Errors []struct {
				Message  string `json:"message"`
				Location string `json:"location"`
			} `json:"errors"`
		}
		if err := json.Unmarshal([]byte(got.stderr), &problem); err != nil || got.code != 2 || got.stdout != "" ||
			problem.Status != 422 || len(problem.Errors) != 1 || problem.Errors[0].Location != "<person-b>" ||
			problem.Errors[0].Message != "the persons must differ" {
			t.Errorf("same person, JSON: exit %d, stderr %q, %v", got.code, got.stderr, err)
		}

		if ds := pdDismissals(t, st); len(ds) != 1 {
			t.Errorf("the refusals recorded dismissals: %+v", ds)
		}
	})
}

// TestPersonsMerge merges the strong pair: the dry run (text and JSON)
// writes nothing; --apply asks on a terminal, needs --yes without one, and
// writes nothing unless confirmed; confirmed, the duplicate becomes a
// tombstone, the survivor takes the fields of --take, and the merge is
// audited under the account of --as. A tombstone cannot be merged again.
func TestPersonsMerge(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		merge := []string{"persons", "merge", "--as", demoOffice, "--take", "email,birthDate", pdLenaA, pdLenaB}

		got := r.exec(t, merge...)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("dry run: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/merge-dry-run.txt", got.stdout)

		got = r.exec(t, append(merge, "--output", "json")...)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("dry run, JSON: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/merge-dry-run.json", got.stdout)

		var body struct {
			Applied  bool `json:"applied"`
			Survivor struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"survivor"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || body.Applied || body.Survivor.ID != pdLenaA ||
			body.Survivor.Email != "LENA.Quillfeather@example.com" {
			t.Errorf("dry run JSON body %+v, %v", body, err)
		}

		pdLive(t, st, pdLenaA, pdLenaB)

		if p := pdPerson(t, st, pdLenaA); p.Email != "lena.quillfeather@example.com" {
			t.Errorf("the dry run wrote the email: %q", p.Email)
		}

		apply := append(merge, "--apply")

		// Without a terminal, --apply needs --yes.
		got = r.exec(t, apply...)
		if got.code != 2 || got.stdout != "" || got.stderr != "error: studio-schedule persons merge changes data: "+
			"confirm with --yes when stdin is not a terminal; nothing was changed\n" {
			t.Errorf("no --yes: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		// On a terminal, it asks; anything but y writes nothing.
		const prompt = "Merge Lena Quillfeather (" + pdLenaB + ") into Lena Quillfeather (" + pdLenaA + ")? [y/N] "

		got = pdWith(r, true, "n\n").exec(t, apply...)
		if got.code != 2 || got.stdout != "" || got.stderr != prompt+"error: not confirmed; nothing was changed\n" {
			t.Errorf("answer n: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		pdLive(t, st, pdLenaA, pdLenaB)

		if n := pdMerges(t, st); n != 0 {
			t.Errorf("%d merges audited before one was confirmed", n)
		}

		// --apply --yes merges.
		got = r.exec(t, append(apply, "--yes")...)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("apply: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/merge-apply.txt", got.stdout)

		if p := pdPerson(t, st, pdLenaB); p.MergedInto.String() != pdLenaA {
			t.Errorf("the duplicate after the merge: %+v, want merged into the survivor", p)
		}

		if p := pdPerson(t, st, pdLenaA); p.Merged() || p.Email != "LENA.Quillfeather@example.com" ||
			p.BirthDate.String() != "1994-08-17" || p.Phone != "+49 000 0000201" || p.MemberNo != "M-1042" ||
			p.Notes != "Registered at the front desk." {
			t.Errorf("the survivor after the merge: %+v, want the duplicate's email and birth date, its own "+
				"phone and member number, and the duplicate's notes", p)
		}

		events := pdAudit(t, st)
		if len(events) == 0 || events[0].Action != domain.AuditPersonMerged ||
			events[0].ActorID != accountID(t, st, demoOffice) || events[0].TargetPersonID.String() != pdLenaA {
			t.Errorf("newest audit event %+v, want the office's merge of the survivor", events)
		}

		// The pair is gone from persons duplicates.
		if got := r.exec(t, "persons", "duplicates", "--as", demoOffice); got.code != 0 || got.stdout != pdJonasAlone {
			t.Errorf("after the merge: exit %d, stdout %q, want the weak pair alone", got.code, got.stdout)
		}

		// The tombstone cannot be merged again, as either person.
		for _, args := range [][]string{{pdLenaA, pdLenaB}, {pdLenaB, pdLenaA}} {
			got := r.exec(t, append([]string{"persons", "merge", "--as", demoOffice, "--apply", "--yes"}, args...)...)
			if got.code != 3 || got.stdout != "" || !strings.HasSuffix(got.stderr, ": person "+pdLenaB+" is merged: not found\n") {
				t.Errorf("merge %v again: exit %d, stdout %q, stderr %q", args, got.code, got.stdout, got.stderr)
			}
		}

		// The weak pair, confirmed on a terminal, as JSON.
		got = pdWith(r, true, "y\n").exec(t, "persons", "merge", "--as", demoOffice, "--apply", "-o", "json",
			pdJonasA, pdJonasB)
		if got.code != 0 || got.stderr != "Merge Jonas Brightwater ("+pdJonasB+") into Jonas Brightwater ("+
			pdJonasA+")? [y/N] " {
			t.Fatalf("answer y: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/merge-apply.json", got.stdout)

		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || !body.Applied || body.Survivor.ID != pdJonasA {
			t.Errorf("apply JSON body %+v, %v", body, err)
		}

		if p := pdPerson(t, st, pdJonasB); p.MergedInto.String() != pdJonasA {
			t.Errorf("the weak duplicate after the merge: %+v", p)
		}

		if n := pdMerges(t, st); n != 2 {
			t.Errorf("%d merges audited, want 2", n)
		}
	})
}

// TestPersonsMergeBookings merges Ella into Emil, both members of a class
// and booked into its next lesson: Ella's booking of it is canceled, her
// membership of the class ends, the rest moves. Both tables show.
func TestPersonsMergeBookings(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)

		got := r.exec(t, "persons", "merge", "--as", demoOffice, "--apply", "--yes", pdEmil, pdElla)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("apply: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/merge-bookings.txt", got.stdout)

		if p := pdPerson(t, st, pdElla); p.MergedInto.String() != pdEmil {
			t.Errorf("Ella after the merge: %+v", p)
		}

		emil, ella := pdParse[domain.PersonID](t, pdEmil), pdParse[domain.PersonID](t, pdElla)

		pdRead(t, st, func(ctx context.Context, tx store.Tx) error {
			bookings, err := tx.Bookings().ListByLessonsAndPersons(ctx,
				[]domain.LessonID{pdParse[domain.LessonID](t, pdLesson)}, []domain.PersonID{emil, ella})
			if err != nil {
				return err
			}

			active := 0

			for _, b := range bookings {
				if b.PersonID != emil {
					t.Errorf("a booking of the lesson stayed with Ella: %+v", b)
				}

				if b.Status != domain.BookingCanceled {
					active++
				}
			}

			if len(bookings) != 2 || active != 1 {
				t.Errorf("bookings of the lesson: %+v, want Emil's and Ella's canceled one", bookings)
			}

			ms, err := tx.Classes().ListMemberships(ctx, emil)
			if err != nil {
				return err
			}

			classes := map[string]int{}
			for _, m := range ms {
				classes[m.ClassID.String()]++
			}

			if classes[pdEllaJazz] != 1 || classes[pdBallet] != 1 {
				t.Errorf("Emil's memberships: %+v, want the shared class once and Ella's other one", ms)
			}

			return nil
		})
	})
}

// TestPersonsMergeRefusals: what merge refuses writes nothing, even with
// --apply --yes: unknown persons, invalid IDs, the same person twice, a bad
// --take, two persons with an account, and a merge after which the
// survivor's dependent would be its guardian too.
func TestPersonsMergeRefusals(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)

		// Ella, Clara's dependent, becomes a guardian of Lena B: merged into
		// Clara, Lena B would make Ella Clara's guardian.
		err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			return tx.Persons().AddGuardian(ctx, domain.Guardian{
				GuardianID:  pdParse[domain.PersonID](t, pdElla),
				DependentID: pdParse[domain.PersonID](t, pdLenaB),
			})
		})
		if err != nil {
			t.Fatal(err)
		}

		for name, tc := range map[string]struct {
			args   []string
			code   int
			stderr string
		}{
			"unknown survivor": {
				args: []string{pdUnknown, pdLenaB}, code: 3,
				stderr: "error: merge " + pdLenaB + " into " + pdUnknown + ": person: not found\n",
			},
			"unknown duplicate": {args: []string{pdLenaA, pdUnknown}, code: 3},
			"invalid survivor": {
				args: []string{"lena", pdLenaB}, code: 2, stderr: "error: validation failed: invalid ID \"lena\"\n",
			},
			"invalid duplicate": {args: []string{pdLenaA, "lena"}, code: 2},
			"same person": {
				args: []string{pdLenaA, pdLenaA}, code: 2, stderr: "error: <duplicate>: the persons must differ\n",
			},
			"bad --take": {
				args: []string{"--take", "email,shoeSize", pdLenaA, pdLenaB}, code: 2,
				stderr: "error: --take: \"shoeSize\" is not one of name, email, phone, birthDate, memberNo\n",
			},
			"two accounts": {
				args: []string{pdClara, pdTom}, code: 4,
				stderr: "error: merge " + pdTom + " into " + pdClara + ": conflict: both persons have an account\n",
			},
			"guardian cycle": {
				args: []string{pdClara, pdLenaB}, code: 4,
				stderr: "error: merge " + pdLenaB + " into " + pdClara +
					": conflict: the merge would make the guardian links a cycle\n",
			},
			"one person": {args: []string{pdLenaA}, code: 2, stderr: "error: accepts 2 arg(s), received 1\n"},
		} {
			args := append([]string{"persons", "merge", "--as", demoOffice, "--apply", "--yes"}, tc.args...)

			got := r.exec(t, args...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", name, got.code, got.stdout,
					got.stderr, tc.code, tc.stderr)
			}
		}

		pdLive(t, st, pdLenaA, pdLenaB, pdClara, pdTom)

		if n := pdMerges(t, st); n != 0 {
			t.Errorf("the refusals audited %d merges", n)
		}
	})
}

// TestPersonsDuplicatesAccess: the three commands act as an office
// account: without --as they are a usage error, an unknown --as is not
// found, a member or an instructor is forbidden; none prints anything on
// stdout or changes anything.
func TestPersonsDuplicatesAccess(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)

		for _, cmd := range []struct {
			path []string
			args []string
		}{
			{path: []string{"persons", "duplicates"}},
			{path: []string{"persons", "duplicates", "dismiss"}, args: []string{pdJonasA, pdJonasB}},
			{path: []string{"persons", "merge"}, args: []string{"--apply", "--yes", pdLenaA, pdLenaB}},
		} {
			name := "studio-schedule " + strings.Join(cmd.path, " ")

			for _, tc := range []struct {
				as     []string
				code   int
				stderr string
			}{
				{code: 2, stderr: "error: " + name + " acts as an account: give --as <email> or set STUDIO_AS\n"},
				{
					as: []string{"--as", "nobody@example.org"}, code: 3,
					stderr: "error: --as nobody@example.org: not found: no account has this email\n",
				},
				{
					as: []string{"--as", demoMember}, code: 4,
					stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below office, " +
						"which this command needs\n",
				},
				{
					as: []string{"--as", demoInstructor}, code: 4,
					stderr: "error: --as " + demoInstructor + ": forbidden: the account's role instructor is below " +
						"office, which this command needs\n",
				},
			} {
				args := append(append(append([]string{}, cmd.path...), tc.as...), cmd.args...)

				got := r.exec(t, args...)
				if got.code != tc.code || got.stdout != "" || got.stderr != tc.stderr {
					t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", args, got.code,
						got.stdout, got.stderr, tc.code, tc.stderr)
				}
			}
		}

		pdLive(t, st, pdLenaA, pdLenaB)

		if ds := pdDismissals(t, st); len(ds) != 0 {
			t.Errorf("dismissals %+v", ds)
		}

		if n := pdMerges(t, st); n != 0 {
			t.Errorf("%d merges audited", n)
		}
	})
}

// TestPersonsMergeTake: --take sets the fields of MergeTake by their API
// names, comma-separated or repeated; anything else is a usage error.
func TestPersonsMergeTake(t *testing.T) {
	t.Parallel()

	take, err := mergeTake([]string{"name", "email", "phone", "birthDate", "memberNo"})
	if err != nil || !take.Name || !take.Email || !take.Phone || !take.BirthDate || !take.MemberNo {
		t.Errorf("every field: %+v, %v", take, err)
	}

	if take, err := mergeTake(nil); err != nil || take != (service.MergeTake{}) {
		t.Errorf("none: %+v, %v", take, err)
	}

	if _, err := mergeTake([]string{"email", "Email"}); !errors.Is(err, cli.ErrUsage) ||
		err.Error() != `--take: "Email" is not one of name, email, phone, birthDate, memberNo` {
		t.Errorf("a field spelled otherwise: %v, want a usage error", err)
	}

	// Repeated --take adds up: the dry run takes the duplicate's name and
	// member number, keeping the survivor's phone.
	r := cliRun{st: demoStore(t)}

	got := r.exec(t, "persons", "merge", "--as", demoOffice, "--take", "name", "--take", "memberNo", "-o", "json",
		pdJonasB, pdJonasA)
	if got.code != 0 {
		t.Fatalf("exit %d, stderr %q", got.code, got.stderr)
	}

	var body struct {
		Survivor struct {
			Phone    string `json:"phone"`
			MemberNo string `json:"memberNo"`
		} `json:"survivor"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || body.Survivor.Phone != "+49 000 0000202" ||
		body.Survivor.MemberNo != "M-1077" {
		t.Errorf("survivor %+v, %v: want its own phone and the duplicate's member number", body, err)
	}
}

// pdFaultStore is a store whose listing of the duplicate dismissals fails,
// or whose audit writes fail from the auditOK+1st on.
type pdFaultStore struct {
	store.Store
	dismissals bool
	auditOK    int32
	audits     *atomic.Int32
}

// errPdFault is the failure of a pdFaultStore.
var errPdFault = errors.New("the disk is unreadable")

func (s pdFaultStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error { return fn(ctx, pdFaultTx{tx, s}) })
}

type pdFaultTx struct {
	store.Tx
	s pdFaultStore
}

func (tx pdFaultTx) DuplicateDismissals() store.DuplicateDismissalRepo {
	return pdFaultDismissals{tx.Tx.DuplicateDismissals(), tx.s.dismissals}
}

func (tx pdFaultTx) Audit() store.AuditRepo { return pdFaultAudit{tx.Tx.Audit(), tx.s} }

type pdFaultDismissals struct {
	store.DuplicateDismissalRepo
	fail bool
}

func (r pdFaultDismissals) List(ctx context.Context) ([]domain.DuplicateDismissal, error) {
	if r.fail {
		return nil, errPdFault
	}

	return r.DuplicateDismissalRepo.List(ctx)
}

type pdFaultAudit struct {
	store.AuditRepo
	s pdFaultStore
}

func (r pdFaultAudit) Create(ctx context.Context, e domain.AuditEvent) error {
	if r.s.audits != nil && r.s.audits.Add(1) > r.s.auditOK {
		return errPdFault
	}

	return r.AuditRepo.Create(ctx, e)
}

// TestPersonsDuplicatesStoreFails: a store failure of the listing, and of
// the merge after its preview, is exit status 1 with nothing on stdout and
// nothing merged.
func TestPersonsDuplicatesStoreFails(t *testing.T) {
	t.Parallel()

	st := demoStore(t)

	got := cliRun{st: pdFaultStore{Store: st, dismissals: true}}.exec(t, "persons", "duplicates", "--as", demoOffice)
	if got.code != 1 || got.stdout != "" || !strings.HasSuffix(got.stderr, errPdFault.Error()+"\n") {
		t.Errorf("duplicates: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	// The preview's audit event is rolled back; the merge's fails.
	got = cliRun{st: pdFaultStore{Store: st, auditOK: 1, audits: new(atomic.Int32)}}.exec(t, "persons", "merge",
		"--as", demoOffice, "--apply", "--yes", pdLenaA, pdLenaB)
	if got.code != 1 || got.stdout != "" || got.stderr != "error: merge "+pdLenaB+" into "+pdLenaA+": "+
		errPdFault.Error()+"\n" {
		t.Errorf("merge: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	pdLive(t, st, pdLenaA, pdLenaB)
}
