package memory

import (
	"cmp"
	"context"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// ---------------------------------------------------------------- credentials

type credentialRepo struct{ tx *tx }

func (r credentialRepo) Get(_ context.Context, account domain.AccountID) (domain.Credential, error) {
	c, ok := r.tx.state.credentials.get(account)
	if !ok {
		return domain.Credential{}, notFound("credential")
	}

	return c, nil
}

func (r credentialRepo) Put(_ context.Context, c domain.Credential) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !s.accounts.has(c.AccountID) {
		return conflict("credential references a missing account")
	}

	c.ChangedAt = instant(c.ChangedAt)
	s.credentials.put(r.tx.gen, c.AccountID, c)

	return nil
}

// ---------------------------------------------------------------- sessions

type sessionRepo struct{ tx *tx }

func (r sessionRepo) GetByToken(_ context.Context, hash [32]byte) (domain.Session, error) {
	found := r.tx.state.sessions.filter(func(s domain.Session) bool { return s.TokenHash == hash })
	if len(found) == 0 {
		return domain.Session{}, notFound("session")
	}

	return found[0], nil
}

func (r sessionRepo) Create(_ context.Context, s domain.Session) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.sessions.has(s.ID) {
		return conflict("session %s exists", s.ID)
	}

	return r.put(s)
}

func (r sessionRepo) Update(_ context.Context, s domain.Session) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.sessions.has(s.ID) {
		return notFound("session")
	}

	return r.put(s)
}

func (r sessionRepo) put(sess domain.Session) error {
	s := &r.tx.state

	switch {
	case !s.accounts.has(sess.AccountID):
		return conflict("session references a missing account")
	case !sess.ImpersonatorID.IsZero() && !s.accounts.has(sess.ImpersonatorID):
		return conflict("session references a missing impersonator")
	case s.sessions.exists(func(o domain.Session) bool { return o.ID != sess.ID && o.TokenHash == sess.TokenHash }):
		return conflict("session token is taken")
	}

	sess.CreatedAt = instant(sess.CreatedAt)
	sess.LastSeenAt = instant(sess.LastSeenAt)
	sess.ExpiresAt = instant(sess.ExpiresAt)
	s.sessions.put(r.tx.gen, sess.ID, sess)

	return nil
}

func (r sessionRepo) Delete(_ context.Context, id domain.SessionID) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.sessions.has(id) {
		return notFound("session")
	}

	r.tx.state.sessions.delete(r.tx.gen, id)

	return nil
}

func (r sessionRepo) DeleteByAccount(_ context.Context, account domain.AccountID, except domain.SessionID) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	actsFor := r.tx.state.sessions.filter(func(s domain.Session) bool {
		return (s.AccountID == account || s.ImpersonatorID == account) && s.ID != except
	})
	for _, s := range actsFor {
		r.tx.state.sessions.delete(r.tx.gen, s.ID)
	}

	return len(actsFor), nil
}

func (r sessionRepo) DeleteExpired(_ context.Context, now time.Time) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	expired := r.tx.state.sessions.filter(func(s domain.Session) bool { return !s.ExpiresAt.After(now) })
	for _, s := range expired {
		r.tx.state.sessions.delete(r.tx.gen, s.ID)
	}

	return len(expired), nil
}

// ---------------------------------------------------------------- account tokens

type accountTokenRepo struct{ tx *tx }

func (r accountTokenRepo) Get(_ context.Context, hash [32]byte) (domain.AccountToken, error) {
	t, ok := r.tx.state.accountTokens.get(hash)
	if !ok {
		return domain.AccountToken{}, notFound("account token")
	}

	return t, nil
}

func (r accountTokenRepo) Create(_ context.Context, t domain.AccountToken) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case s.accountTokens.has(t.Hash):
		return conflict("account token exists")
	case !s.accounts.has(t.AccountID):
		return conflict("account token references a missing account")
	}

	t.CreatedAt = instant(t.CreatedAt)
	t.ExpiresAt = instant(t.ExpiresAt)
	s.accountTokens.put(r.tx.gen, t.Hash, t)

	return nil
}

func (r accountTokenRepo) Delete(_ context.Context, hash [32]byte) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.accountTokens.has(hash) {
		return notFound("account token")
	}

	r.tx.state.accountTokens.delete(r.tx.gen, hash)

	return nil
}

func (r accountTokenRepo) DeleteByAccount(_ context.Context, account domain.AccountID, purpose domain.TokenPurpose) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	for _, t := range r.tx.state.accountTokens.filter(func(t domain.AccountToken) bool {
		return t.AccountID == account && t.Purpose == purpose
	}) {
		r.tx.state.accountTokens.delete(r.tx.gen, t.Hash)
	}

	return nil
}

func (r accountTokenRepo) DeleteExpired(_ context.Context, now time.Time) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	expired := r.tx.state.accountTokens.filter(func(t domain.AccountToken) bool { return !t.ExpiresAt.After(now) })
	for _, t := range expired {
		r.tx.state.accountTokens.delete(r.tx.gen, t.Hash)
	}

	return len(expired), nil
}

// ---------------------------------------------------------------- outbox

type outboxRepo struct{ tx *tx }

func (r outboxRepo) Get(_ context.Context, id domain.OutboxMailID) (domain.OutboxMail, error) {
	m, ok := r.tx.state.outbox.get(id)
	if !ok {
		return domain.OutboxMail{}, notFound("outbox mail")
	}

	return m, nil
}

func (r outboxRepo) Create(_ context.Context, m domain.OutboxMail) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.outbox.has(m.ID) {
		return conflict("outbox mail %s exists", m.ID)
	}

	return r.put(m)
}

func (r outboxRepo) Update(_ context.Context, m domain.OutboxMail) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.outbox.has(m.ID) {
		return notFound("outbox mail")
	}

	return r.put(m)
}

func (r outboxRepo) put(m domain.OutboxMail) error {
	if !m.ReceiptID.IsZero() && !r.tx.state.receipts.has(m.ReceiptID) {
		return conflict("outbox mail references a missing receipt")
	}

	m.CreatedAt = instant(m.CreatedAt)
	m.NextAttemptAt = instant(m.NextAttemptAt)
	m.SentAt = instant(m.SentAt)
	r.tx.state.outbox.put(r.tx.gen, m.ID, m)

	return nil
}

func (r outboxRepo) ListDue(_ context.Context, now time.Time, limit int) ([]domain.OutboxMail, error) {
	// Like the PostgreSQL store, which locks the mails it returns.
	if err := r.tx.checkWritable(); err != nil {
		return nil, err
	}

	due := r.tx.state.outbox.filter(func(m domain.OutboxMail) bool {
		return m.SentAt.IsZero() && !m.NextAttemptAt.IsZero() && !m.NextAttemptAt.After(now)
	})
	due = sorted(due, func(a, b domain.OutboxMail) int {
		return cmp.Or(a.NextAttemptAt.Compare(b.NextAttemptAt), compareID(a.ID, b.ID))
	})

	return due[:min(max(limit, 0), len(due))], nil
}

func (r outboxRepo) List(_ context.Context, f store.OutboxFilter) ([]domain.OutboxMail, error) {
	newer := func(a, b domain.OutboxMail) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), compareID(b.ID, a.ID))
	}
	cursor := domain.OutboxMail{CreatedAt: instant(f.AfterCreatedAt), ID: f.AfterID}
	recipient := foldASCII(f.Recipient)

	mails := sorted(r.tx.state.outbox.filter(func(m domain.OutboxMail) bool {
		return (f.Status == "" || m.Status() == f.Status) &&
			(f.Template == "" || m.Template == f.Template) &&
			strings.Contains(foldASCII(m.To), recipient) &&
			(f.AfterCreatedAt.IsZero() || newer(m, cursor) > 0)
	}), newer)

	return mails[:min(max(f.Limit, 0), len(mails))], nil
}

func (r outboxRepo) PurgeBodies(_ context.Context, sentBefore time.Time) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	sentBefore = instant(sentBefore)
	mails := r.tx.state.outbox.filter(func(m domain.OutboxMail) bool {
		return !m.SentAt.IsZero() && m.SentAt.Before(sentBefore) && !m.Purged()
	})

	for _, m := range mails {
		m.Text, m.HTML = "", ""
		r.tx.state.outbox.put(r.tx.gen, m.ID, m)
	}

	return len(mails), nil
}

func (r outboxRepo) CountUnsent(context.Context) (store.OutboxCounts, error) {
	var c store.OutboxCounts

	for _, m := range r.tx.state.outbox.filter(func(m domain.OutboxMail) bool { return m.SentAt.IsZero() }) {
		switch m.Status() {
		case domain.MailPending:
			c.Pending++
		case domain.MailFailed:
			c.Failed++
		case domain.MailSent:
		}
	}

	return c, nil
}

func (r outboxRepo) DeleteCreatedBefore(_ context.Context, before time.Time) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	before = instant(before)
	mails := r.tx.state.outbox.filter(func(m domain.OutboxMail) bool { return m.CreatedAt.Before(before) })

	for _, m := range mails {
		r.tx.state.outbox.delete(r.tx.gen, m.ID)
	}

	return len(mails), nil
}

// ---------------------------------------------------------------- audit

type auditRepo struct{ tx *tx }

func (r auditRepo) Create(_ context.Context, e domain.AuditEvent) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	s := &r.tx.state

	switch {
	case s.audit.has(e.ID):
		return conflict("audit event %s exists", e.ID)
	case !s.accounts.has(e.ActorID),
		!e.TargetID.IsZero() && !s.accounts.has(e.TargetID),
		!e.TargetPersonID.IsZero() && !s.persons.has(e.TargetPersonID):
		return conflict("audit event references a missing account or person")
	}

	e.At = instant(e.At)
	s.audit.put(r.tx.gen, e.ID, e)

	return nil
}

func (r auditRepo) List(_ context.Context, limit int) ([]domain.AuditEvent, error) {
	events := sorted(r.tx.state.audit.filter(all), func(a, b domain.AuditEvent) int {
		return cmp.Or(b.At.Compare(a.At), compareID(b.ID, a.ID))
	})

	return events[:min(max(limit, 0), len(events))], nil
}
