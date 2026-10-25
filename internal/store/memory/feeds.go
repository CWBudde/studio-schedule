package memory

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ---------------------------------------------------------------- calendar feeds

type calendarFeedRepo struct{ tx *tx }

func (r calendarFeedRepo) Get(_ context.Context, id domain.CalendarFeedID) (domain.CalendarFeed, error) {
	f, ok := r.tx.state.calendarFeeds.get(id)
	if !ok {
		return domain.CalendarFeed{}, notFound("calendar feed")
	}

	return f, nil
}

func (r calendarFeedRepo) GetByTokenHash(_ context.Context, hash [32]byte) (domain.CalendarFeed, error) {
	found := r.tx.state.calendarFeeds.filter(func(f domain.CalendarFeed) bool { return f.TokenHash == hash })
	if len(found) == 0 {
		return domain.CalendarFeed{}, notFound("calendar feed")
	}

	return found[0], nil
}

func (r calendarFeedRepo) Create(_ context.Context, f domain.CalendarFeed) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	s := &r.tx.state

	switch {
	case s.calendarFeeds.has(f.ID):
		return conflict("calendar feed %s exists", f.ID)
	case s.calendarFeeds.exists(func(o domain.CalendarFeed) bool { return o.TokenHash == f.TokenHash }):
		return conflict("calendar feed token hash is taken")
	case !s.accounts.has(f.AccountID) || (!f.LocationID.IsZero() && !s.locations.has(f.LocationID)):
		return conflict("calendar feed references a missing record")
	case f.Active() && s.calendarFeeds.exists(func(o domain.CalendarFeed) bool {
		return o.Active() && sameSubject(o, f.AccountID, f.Scope, f.LocationID)
	}):
		return fmt.Errorf("the account has an active calendar feed of this subject: %w", domain.ErrDuplicate)
	}

	f.CreatedAt, f.LastUsedAt, f.RevokedAt = instant(f.CreatedAt), instant(f.LastUsedAt), instant(f.RevokedAt)
	s.calendarFeeds.put(r.tx.gen, f.ID, f)

	return nil
}

func (r calendarFeedRepo) ListByAccount(_ context.Context, account domain.AccountID) ([]domain.CalendarFeed, error) {
	feeds := r.tx.state.calendarFeeds.filter(func(f domain.CalendarFeed) bool {
		return f.AccountID == account && f.Active()
	})

	return sorted(feeds, func(a, b domain.CalendarFeed) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), compareID(a.ID, b.ID))
	}), nil
}

func (r calendarFeedRepo) Revoke(_ context.Context, id domain.CalendarFeedID, at time.Time) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	f, ok := r.tx.state.calendarFeeds.get(id)
	if !ok || !f.Active() {
		return notFound("active calendar feed")
	}

	f.RevokedAt = instant(at)
	r.tx.state.calendarFeeds.put(r.tx.gen, id, f)

	return nil
}

func (r calendarFeedRepo) RevokeActive(
	_ context.Context, account domain.AccountID, scope domain.FeedScope, location domain.LocationID, at time.Time,
) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	feeds := r.tx.state.calendarFeeds.filter(func(f domain.CalendarFeed) bool {
		return f.Active() && sameSubject(f, account, scope, location)
	})

	for _, f := range feeds {
		f.RevokedAt = instant(at)
		r.tx.state.calendarFeeds.put(r.tx.gen, f.ID, f)
	}

	return len(feeds), nil
}

func (r calendarFeedRepo) TouchLastUsed(_ context.Context, id domain.CalendarFeedID, at time.Time) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	f, ok := r.tx.state.calendarFeeds.get(id)
	if !ok {
		return notFound("calendar feed")
	}

	f.LastUsedAt = instant(at)
	r.tx.state.calendarFeeds.put(r.tx.gen, id, f)

	return nil
}

// sameSubject reports whether f is the account's feed of the scope and the
// location.
func sameSubject(f domain.CalendarFeed, account domain.AccountID, scope domain.FeedScope, location domain.LocationID) bool {
	return f.AccountID == account && f.Scope == scope && f.LocationID == location
}
