package storetest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testJobRuns(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	latest := func(e *env) []domain.JobRun {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.JobRun, error) {
			return tx.JobRuns().Latest(ctx)
		})
	}

	create := func(e *env, r domain.JobRun) error {
		return e.write(func(ctx context.Context, tx store.Tx) error { return tx.JobRuns().Create(ctx, r) })
	}

	// run builds a run of job with the ID id, started at at and lasting a
	// second; it is not written.
	run := func(e *env, id domain.JobRunID, job string, at time.Time, result domain.JobRunResult) domain.JobRun {
		e.t.Helper()

		r := domain.JobRun{ID: id, Job: job, StartedAt: at, EndedAt: at.Add(time.Second), Result: result}
		if result == domain.JobFailed {
			r.Error = "smtp: connection refused"
		}

		valid(e.t, r)

		return r
	}

	mustCreate := func(e *env, runs ...domain.JobRun) {
		e.t.Helper()

		for _, r := range runs {
			if err := create(e, r); err != nil {
				e.t.Fatalf("create job run: %v", err)
			}
		}
	}

	cases(t, newStore, map[string]func(e *env){
		"Empty": func(e *env) { wantSlice(e.t, latest(e), nil) },
		"RoundTrip": func(e *env) {
			r := domain.JobRun{
				ID: newID[domain.JobRunID](e), Job: "deliver-mail",
				// A fraction of a microsecond is cut off like in PostgreSQL.
				StartedAt: t0.Add(500 * time.Nanosecond), EndedAt: t0.Add(1500 * time.Millisecond),
				Result: domain.JobFailed, Error: domain.JobRunError(strings.Repeat("ä", domain.MaxJobRunErrorLen)),
			}
			valid(e.t, r)
			mustCreate(e, r)

			r.StartedAt = t0
			wantSlice(e.t, latest(e), []domain.JobRun{r})
		},
		"LatestPerJob": func(e *env) {
			// Two runs with the same start: the greater ID wins, whatever
			// the order they were written in.
			lower, greater := newID[domain.JobRunID](e), newID[domain.JobRunID](e)

			mailOld := run(e, newID[domain.JobRunID](e), "deliver-mail", t0, domain.JobFailed)
			mailNew := run(e, newID[domain.JobRunID](e), "deliver-mail", t0.Add(time.Minute), domain.JobOK)
			genTieWinner := run(e, greater, "generate-lessons", t0, domain.JobSkipped)
			genTieLoser := run(e, lower, "generate-lessons", t0, domain.JobOK)
			// Byte-wise order of names: upper case sorts first.
			upper := run(e, newID[domain.JobRunID](e), "Zeta", t0.Add(-time.Hour), domain.JobOK)

			mustCreate(e, mailNew, genTieWinner, mailOld, genTieLoser, upper)

			got := latest(e)
			wantSlice(e.t, got, []domain.JobRun{upper, mailNew, genTieWinner})

			for _, r := range got {
				if r.Error != "" {
					e.t.Errorf("run %s (%s) has the error %q", r.Job, r.Result, r.Error)
				}
			}
		},
		"Conflicts": func(e *env) {
			r := run(e, newID[domain.JobRunID](e), "deliver-mail", t0, domain.JobOK)
			mustCreate(e, r)

			other := run(e, r.ID, "generate-lessons", t0.Add(time.Hour), domain.JobOK)
			wantErr(e.t, create(e, other), domain.ErrConflict)
			wantSlice(e.t, latest(e), []domain.JobRun{r})
		},
		"ReadOnly": func(e *env) {
			r := run(e, newID[domain.JobRunID](e), "deliver-mail", t0, domain.JobOK)

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.JobRuns().Create(ctx, r) })
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.JobRuns().DeleteStartedBefore(ctx, t0)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"DeleteStartedBefore": func(e *env) {
			old := run(e, newID[domain.JobRunID](e), "deliver-mail", t0.Add(-time.Hour), domain.JobOK)
			older := run(e, newID[domain.JobRunID](e), "generate-lessons", t0.Add(-2*time.Hour), domain.JobOK)
			atCut := run(e, newID[domain.JobRunID](e), "generate-lessons", t0, domain.JobFailed)
			after := run(e, newID[domain.JobRunID](e), "deliver-mail", t0.Add(time.Microsecond), domain.JobSkipped)
			mustCreate(e, old, older, atCut, after)

			var n int

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				n, err = tx.JobRuns().DeleteStartedBefore(ctx, t0)

				return err
			})
			wantEqual(e.t, n, 2)
			wantSlice(e.t, latest(e), []domain.JobRun{after, atCut})

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				n, err = tx.JobRuns().DeleteStartedBefore(ctx, t0)

				return err
			})
			wantEqual(e.t, n, 0)
		},
	})
}

func testJobLocks(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	// lock takes the lock of name and fails the test unless it got it.
	lock := func(e *env, name string) func() {
		e.t.Helper()

		release, ok, err := e.st.TryLockJob(e.t.Context(), name)
		if err != nil || !ok {
			e.t.Fatalf("TryLockJob(%q) = %t, %v; want the lock", name, ok, err)
		}

		return release
	}

	// wantTaken fails the test unless name's lock is held by another caller.
	wantTaken := func(e *env, name string) {
		e.t.Helper()

		release, ok, err := e.st.TryLockJob(e.t.Context(), name)
		if err != nil || ok || release != nil {
			e.t.Fatalf("TryLockJob(%q) = %t, %v; want it taken", name, ok, err)
		}
	}

	cases(t, newStore, map[string]func(e *env){
		"HeldIsRefused": func(e *env) {
			release := lock(e, "deliver-mail")
			wantTaken(e, "deliver-mail")

			other := lock(e, "generate-lessons")
			wantTaken(e, "generate-lessons")
			wantTaken(e, "deliver-mail")

			other()
			release()
		},
		"ReleaseFreesIt": func(e *env) {
			release := lock(e, "deliver-mail")
			release()

			again := lock(e, "deliver-mail")

			// A second call of the first release does not free the lock
			// taken again.
			release()
			wantTaken(e, "deliver-mail")

			again()
			again()
			lock(e, "deliver-mail")()
		},
		"HolderRunsTransactions": func(e *env) {
			release := lock(e, "deliver-mail")
			defer release()

			p := e.person("Hilde", "Holder")
			wantEqual(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
				return tx.Persons().Get(ctx, p.ID)
			}), p)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.JobRuns().Create(ctx, domain.JobRun{
					ID: newID[domain.JobRunID](e), Job: "deliver-mail", StartedAt: t0, EndedAt: t0, Result: domain.JobOK,
				})
			})
		},
		"InsideTransaction": func(e *env) {
			outside := lock(e, "generate-lessons")
			defer outside()

			e.mustWrite(func(ctx context.Context, _ store.Tx) error {
				release, ok, err := e.st.TryLockJob(ctx, "deliver-mail")
				if err != nil || !ok {
					e.t.Errorf("TryLockJob inside a transaction = %t, %v; want the lock", ok, err)

					return nil
				}

				defer release()

				release2, ok, err := e.st.TryLockJob(ctx, "generate-lessons")
				if err != nil || ok || release2 != nil {
					e.t.Errorf("TryLockJob of a held lock inside a transaction = %t, %v; want it taken", ok, err)
				}

				return nil
			})

			lock(e, "deliver-mail")()
		},
	})
}
