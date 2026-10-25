package postgres

import (
	"bytes"
	"context"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// fromPgHash converts a token_hash column; the CHECK constraint guarantees
// 32 bytes.
func fromPgHash(b []byte) [32]byte { return [32]byte(b) }

// ---------------------------------------------------------------- credentials

type credentialRepo struct{ q *sqlc.Queries }

func (r credentialRepo) Get(ctx context.Context, account domain.AccountID) (domain.Credential, error) {
	row, err := r.q.GetCredential(ctx, pgID(account))
	if err != nil {
		return domain.Credential{}, notFound("credential", err)
	}

	return domain.Credential{
		AccountID:    fromPgID[domain.AccountID](row.AccountID),
		PasswordHash: row.PasswordHash,
		ChangedAt:    fromPgInstant(row.ChangedAt),
	}, nil
}

func (r credentialRepo) Put(ctx context.Context, c domain.Credential) error {
	return mapErr(r.q.PutCredential(ctx, sqlc.PutCredentialParams{
		AccountID:    pgID(c.AccountID),
		PasswordHash: c.PasswordHash,
		ChangedAt:    pgInstant(c.ChangedAt),
	}))
}

// ---------------------------------------------------------------- sessions

type sessionRepo struct{ q *sqlc.Queries }

func toSession(row sqlc.Session) domain.Session {
	return domain.Session{
		ID:         fromPgID[domain.SessionID](row.ID),
		AccountID:  fromPgID[domain.AccountID](row.AccountID),
		TokenHash:  fromPgHash(row.TokenHash),
		CreatedAt:  fromPgInstant(row.CreatedAt),
		LastSeenAt: fromPgInstant(row.LastSeenAt),
		ExpiresAt:  fromPgInstant(row.ExpiresAt),

		ImpersonatorID: fromPgID[domain.AccountID](row.ImpersonatorID),
	}
}

func (r sessionRepo) GetByToken(ctx context.Context, hash [32]byte) (domain.Session, error) {
	row, err := r.q.GetSessionByToken(ctx, hash[:])
	if err != nil {
		return domain.Session{}, notFound("session", err)
	}

	return toSession(row), nil
}

func (r sessionRepo) Create(ctx context.Context, s domain.Session) error {
	return mapErr(r.q.CreateSession(ctx, sqlc.CreateSessionParams{
		ID:             pgID(s.ID),
		AccountID:      pgID(s.AccountID),
		TokenHash:      s.TokenHash[:],
		CreatedAt:      pgInstant(s.CreatedAt),
		LastSeenAt:     pgInstant(s.LastSeenAt),
		ExpiresAt:      pgInstant(s.ExpiresAt),
		ImpersonatorID: pgID(s.ImpersonatorID),
	}))
}

func (r sessionRepo) Update(ctx context.Context, s domain.Session) error {
	n, err := r.q.UpdateSession(ctx, sqlc.UpdateSessionParams{
		ID:             pgID(s.ID),
		AccountID:      pgID(s.AccountID),
		TokenHash:      s.TokenHash[:],
		CreatedAt:      pgInstant(s.CreatedAt),
		LastSeenAt:     pgInstant(s.LastSeenAt),
		ExpiresAt:      pgInstant(s.ExpiresAt),
		ImpersonatorID: pgID(s.ImpersonatorID),
	})

	return affected("session", n, err)
}

func (r sessionRepo) Delete(ctx context.Context, id domain.SessionID) error {
	n, err := r.q.DeleteSession(ctx, pgID(id))

	return affected("session", n, err)
}

func (r sessionRepo) DeleteByAccount(
	ctx context.Context, account domain.AccountID, except domain.SessionID,
) ([]domain.Session, error) {
	rows, err := r.q.DeleteSessionsByAccount(ctx, sqlc.DeleteSessionsByAccountParams{
		AccountID: pgID(account),
		ExceptID:  pgID(except),
	})

	sessions, err := convertRows(rows, err, toSession)
	slices.SortFunc(sessions, func(a, b domain.Session) int { return bytes.Compare(a.ID[:], b.ID[:]) })

	return sessions, err
}

func (r sessionRepo) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	n, err := r.q.DeleteExpiredSessions(ctx, pgInstant(now))

	return int(n), mapErr(err)
}

// ---------------------------------------------------------------- account tokens

type accountTokenRepo struct{ q *sqlc.Queries }

func (r accountTokenRepo) Get(ctx context.Context, hash [32]byte) (domain.AccountToken, error) {
	row, err := r.q.GetAccountToken(ctx, hash[:])
	if err != nil {
		return domain.AccountToken{}, notFound("account token", err)
	}

	return domain.AccountToken{
		Hash:      fromPgHash(row.TokenHash),
		AccountID: fromPgID[domain.AccountID](row.AccountID),
		Purpose:   domain.TokenPurpose(row.Purpose),
		Email:     row.Email,
		CreatedAt: fromPgInstant(row.CreatedAt),
		ExpiresAt: fromPgInstant(row.ExpiresAt),
	}, nil
}

func (r accountTokenRepo) Create(ctx context.Context, t domain.AccountToken) error {
	return mapErr(r.q.CreateAccountToken(ctx, sqlc.CreateAccountTokenParams{
		TokenHash: t.Hash[:],
		AccountID: pgID(t.AccountID),
		Purpose:   string(t.Purpose),
		Email:     t.Email,
		CreatedAt: pgInstant(t.CreatedAt),
		ExpiresAt: pgInstant(t.ExpiresAt),
	}))
}

func (r accountTokenRepo) Delete(ctx context.Context, hash [32]byte) error {
	n, err := r.q.DeleteAccountToken(ctx, hash[:])

	return affected("account token", n, err)
}

func (r accountTokenRepo) DeleteByAccount(ctx context.Context, account domain.AccountID, purpose domain.TokenPurpose) error {
	return mapErr(r.q.DeleteAccountTokensByAccount(ctx, sqlc.DeleteAccountTokensByAccountParams{
		AccountID: pgID(account),
		Purpose:   string(purpose),
	}))
}

func (r accountTokenRepo) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	n, err := r.q.DeleteExpiredAccountTokens(ctx, pgInstant(now))

	return int(n), mapErr(err)
}
