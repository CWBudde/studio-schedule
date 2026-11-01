package memory

import (
	"cmp"
	"context"
	"sync"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// TryLockJob implements store.Store with a mutex per job name. The store
// serves one process, so another caller in it is the only other holder.
func (s *Store) TryLockJob(_ context.Context, name string) (release func(), ok bool, err error) {
	s.jobLocksMu.Lock()

	if s.jobLocks == nil {
		s.jobLocks = map[string]*sync.Mutex{}
	}

	m, found := s.jobLocks[name]
	if !found {
		m = &sync.Mutex{}
		s.jobLocks[name] = m
	}

	s.jobLocksMu.Unlock()

	if !m.TryLock() {
		return nil, false, nil
	}

	return sync.OnceFunc(m.Unlock), true, nil
}

// ---------------------------------------------------------------- job runs

type jobRunRepo struct{ tx *tx }

func (r jobRunRepo) Create(_ context.Context, run domain.JobRun) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.jobRuns.has(run.ID) {
		return conflict("job run %s exists", run.ID)
	}

	run.StartedAt, run.EndedAt = instant(run.StartedAt), instant(run.EndedAt)
	r.tx.state.jobRuns.put(r.tx.gen, run.ID, run)

	return nil
}

func (r jobRunRepo) Latest(context.Context) ([]domain.JobRun, error) {
	latest := map[string]domain.JobRun{}

	for _, run := range r.tx.state.jobRuns.rows {
		if l, ok := latest[run.Job]; !ok || cmp.Or(run.StartedAt.Compare(l.StartedAt), compareID(run.ID, l.ID)) > 0 {
			latest[run.Job] = run
		}
	}

	runs := make([]domain.JobRun, 0, len(latest))
	for _, run := range latest {
		runs = append(runs, run)
	}

	return sorted(runs, func(a, b domain.JobRun) int { return cmp.Compare(a.Job, b.Job) }), nil
}

func (r jobRunRepo) DeleteStartedBefore(_ context.Context, before time.Time) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	before = instant(before)
	runs := r.tx.state.jobRuns.filter(func(run domain.JobRun) bool { return run.StartedAt.Before(before) })

	for _, run := range runs {
		r.tx.state.jobRuns.delete(r.tx.gen, run.ID)
	}

	return len(runs), nil
}
