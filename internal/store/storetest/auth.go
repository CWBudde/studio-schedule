package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testCredentials(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	get := func(e *env, id domain.AccountID) (domain.Credential, error) {
		return query(e, func(ctx context.Context, tx store.Tx) (domain.Credential, error) {
			return tx.Credentials().Get(ctx, id)
		})
	}
	put := func(e *env, c domain.Credential) error {
		return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Credentials().Put(ctx, c) })
	}

	cases(t, newStore, map[string]func(e *env){
		"PutThenGetAndReplace": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")

			_, err := get(e, a.ID)
			wantErr(e.t, err, domain.ErrNotFound)

			c := domain.Credential{AccountID: a.ID, PasswordHash: "hash-1", ChangedAt: t0}
			valid(e.t, c)

			if err := put(e, c); err != nil {
				e.t.Fatalf("Put: %v", err)
			}

			got, err := get(e, a.ID)
			if err != nil {
				e.t.Fatalf("Get: %v", err)
			}

			wantEqual(e.t, got, c)

			c.PasswordHash, c.ChangedAt = "hash-2", t0.Add(time.Hour)
			if err := put(e, c); err != nil {
				e.t.Fatalf("Put (replace): %v", err)
			}

			got, _ = get(e, a.ID)
			wantEqual(e.t, got, c)
		},
		"MissingAccountIsConflict": func(e *env) {
			c := domain.Credential{AccountID: newID[domain.AccountID](e), PasswordHash: "h", ChangedAt: t0}
			wantErr(e.t, put(e, c), domain.ErrConflict)
		},
		"ReadOnlyRejectsWrites": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			c := domain.Credential{AccountID: a.ID, PasswordHash: "h", ChangedAt: t0}

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Credentials().Put(ctx, c) })
			wantErr(e.t, err, store.ErrReadOnly)
		},
	})
}

func (e *env) session(a domain.Account, token string, expires time.Time) domain.Session {
	e.t.Helper()

	s := domain.Session{
		ID: newID[domain.SessionID](e), AccountID: a.ID, TokenHash: domain.HashToken(token),
		CreatedAt: expires.Add(-time.Hour), LastSeenAt: expires.Add(-time.Hour), ExpiresAt: expires,
	}
	valid(e.t, s)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, s) })

	return s
}

func testSessions(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	getByToken := func(e *env, token string) (domain.Session, error) {
		return query(e, func(ctx context.Context, tx store.Tx) (domain.Session, error) {
			return tx.Sessions().GetByToken(ctx, domain.HashToken(token))
		})
	}

	cases(t, newStore, map[string]func(e *env){
		"CreateGetUpdateDelete": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")

			_, err := getByToken(e, "secret")
			wantErr(e.t, err, domain.ErrNotFound)

			s := e.session(a, "secret", t0.Add(time.Hour))

			got, err := getByToken(e, "secret")
			if err != nil {
				e.t.Fatalf("GetByToken: %v", err)
			}

			wantEqual(e.t, got, s)

			s.LastSeenAt, s.ExpiresAt = t0.Add(time.Minute), t0.Add(2*time.Hour)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Update(ctx, s) })

			got, _ = getByToken(e, "secret")
			wantEqual(e.t, got, s)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Delete(ctx, s.ID) })

			_, err = getByToken(e, "secret")
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Delete(ctx, s.ID) })
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Update(ctx, s) })
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"Conflicts": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			s := e.session(a, "secret", t0.Add(time.Hour))

			create := func(s domain.Session) error {
				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, s) })
			}

			wantErr(e.t, create(s), domain.ErrConflict) // same ID

			sameToken := s
			sameToken.ID = newID[domain.SessionID](e)
			wantErr(e.t, create(sameToken), domain.ErrConflict)

			noAccount := s
			noAccount.ID, noAccount.TokenHash, noAccount.AccountID = newID[domain.SessionID](e), domain.HashToken("other"), newID[domain.AccountID](e)
			wantErr(e.t, create(noAccount), domain.ErrConflict)

			noImpersonator := s
			noImpersonator.ID, noImpersonator.TokenHash = newID[domain.SessionID](e), domain.HashToken("third")
			noImpersonator.ImpersonatorID = newID[domain.AccountID](e)
			wantErr(e.t, create(noImpersonator), domain.ErrConflict)
		},
		"Impersonation": func(e *env) {
			admin := e.account(e.person("Ada", "Admin"), "ada@example.org")
			member := e.account(e.person("Ben", "Member"), "ben@example.org")

			s := domain.Session{
				ID: newID[domain.SessionID](e), AccountID: member.ID, TokenHash: domain.HashToken("as-ben"),
				CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0.Add(time.Hour), ImpersonatorID: admin.ID,
			}
			valid(e.t, s)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, s) })

			got, err := getByToken(e, "as-ben")
			if err != nil {
				e.t.Fatalf("GetByToken: %v", err)
			}

			wantEqual(e.t, got, s)

			s.ImpersonatorID = domain.AccountID{}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Update(ctx, s) })

			got, _ = getByToken(e, "as-ben")
			wantEqual(e.t, got, s)
		},
		"DeleteByAccount": func(e *env) {
			ada := e.account(e.person("Ada", "Admin"), "ada@example.org")
			ben := e.account(e.person("Ben", "Member"), "ben@example.org")
			cleo := e.account(e.person("Cleo", "Admin"), "cleo@example.org")

			impersonate := func(impersonator, as domain.Account, token string) {
				s := domain.Session{
					ID: newID[domain.SessionID](e), AccountID: as.ID, TokenHash: domain.HashToken(token),
					CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0.Add(time.Hour), ImpersonatorID: impersonator.ID,
				}
				valid(e.t, s)
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, s) })
			}
			deleteByAccount := func(account domain.AccountID, except domain.SessionID) int {
				var n int

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					n, err = tx.Sessions().DeleteByAccount(ctx, account, except)

					return err
				})

				return n
			}
			wantGone := func(tokens ...string) {
				e.t.Helper()

				for _, token := range tokens {
					_, err := getByToken(e, token)
					wantErr(e.t, err, domain.ErrNotFound)
				}
			}
			wantKept := func(sessions ...domain.Session) {
				e.t.Helper()

				for _, s := range sessions {
					got, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Session, error) {
						return tx.Sessions().GetByToken(ctx, s.TokenHash)
					})
					if err != nil {
						e.t.Fatalf("GetByToken %s: %v", s.ID, err)
					}

					wantEqual(e.t, got, s)
				}
			}

			e.session(ada, "ada-1", t0.Add(time.Hour))
			current := e.session(ada, "ada-2", t0.Add(time.Hour))
			e.session(ada, "ada-expired", t0.Add(-time.Hour))
			impersonate(ada, ben, "ada-as-ben")   // acts for Ada
			impersonate(cleo, ada, "cleo-as-ada") // signed in as Ada
			ben1 := e.session(ben, "ben-1", t0.Add(time.Hour))
			cleo1 := e.session(cleo, "cleo-1", t0.Add(time.Hour))

			// Keeps the current session, deletes every other one acting for Ada.
			wantEqual(e.t, deleteByAccount(ada.ID, current.ID), 4)
			wantGone("ada-1", "ada-expired", "ada-as-ben", "cleo-as-ada")
			wantKept(current, ben1, cleo1)

			// A session of another account is no exception for Ada's.
			wantEqual(e.t, deleteByAccount(ada.ID, ben1.ID), 1)
			wantGone("ada-2")
			wantKept(ben1, cleo1)

			wantEqual(e.t, deleteByAccount(ada.ID, domain.SessionID{}), 0)

			// The zero exception deletes all.
			e.session(ben, "ben-2", t0.Add(time.Hour))
			wantEqual(e.t, deleteByAccount(ben.ID, domain.SessionID{}), 2)
			wantGone("ben-1", "ben-2")
			wantKept(cleo1)
		},
		"DeleteExpired": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			e.session(a, "old", t0.Add(-time.Minute))
			e.session(a, "edge", t0)
			keep := e.session(a, "fresh", t0.Add(time.Minute))

			var n int

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				n, err = tx.Sessions().DeleteExpired(ctx, t0)

				return err
			})
			wantEqual(e.t, n, 2)

			got, err := getByToken(e, "fresh")
			if err != nil {
				e.t.Fatalf("GetByToken: %v", err)
			}

			wantEqual(e.t, got, keep)

			_, err = getByToken(e, "edge")
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"ReadOnlyRejectsWrites": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			s := e.session(a, "secret", t0.Add(time.Hour))

			for name, write := range map[string]func(context.Context, store.Tx) error{
				"Create": func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, s) },
				"Update": func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Update(ctx, s) },
				"Delete": func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Delete(ctx, s.ID) },
				"DeleteByAccount": func(ctx context.Context, tx store.Tx) error {
					_, err := tx.Sessions().DeleteByAccount(ctx, a.ID, domain.SessionID{})

					return err
				},
				"DeleteExpired": func(ctx context.Context, tx store.Tx) error {
					_, err := tx.Sessions().DeleteExpired(ctx, t0.Add(2*time.Hour))

					return err
				},
			} {
				if err := e.read(write); !errors.Is(err, store.ErrReadOnly) {
					e.t.Fatalf("%s: want ErrReadOnly, got %v", name, err)
				}
			}
		},
	})
}

func (e *env) accountToken(a domain.Account, token string, expires time.Time) domain.AccountToken {
	e.t.Helper()

	at := domain.AccountToken{
		Hash: domain.HashToken(token), AccountID: a.ID, Purpose: domain.TokenConfirmEmail,
		Email: a.Email, CreatedAt: t0.Add(-time.Hour), ExpiresAt: expires,
	}
	valid(e.t, at)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.AccountTokens().Create(ctx, at) })

	return at
}

func testAccountTokens(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	get := func(e *env, token string) (domain.AccountToken, error) {
		return query(e, func(ctx context.Context, tx store.Tx) (domain.AccountToken, error) {
			return tx.AccountTokens().Get(ctx, domain.HashToken(token))
		})
	}

	cases(t, newStore, map[string]func(e *env){
		"CreateGetDelete": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			at := e.accountToken(a, "secret", t0.Add(time.Hour))

			got, err := get(e, "secret")
			if err != nil {
				e.t.Fatalf("Get: %v", err)
			}

			wantEqual(e.t, got, at)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.AccountTokens().Delete(ctx, at.Hash) })

			_, err = get(e, "secret")
			wantErr(e.t, err, domain.ErrNotFound)

			// A token can be used once: deleting it again reports not found.
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.AccountTokens().Delete(ctx, at.Hash) })
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"EveryPurpose": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")

			for _, purpose := range domain.TokenPurposes() {
				at := domain.AccountToken{
					Hash: domain.HashToken(string(purpose)), AccountID: a.ID, Purpose: purpose,
					Email: "new-" + a.Email, CreatedAt: t0, ExpiresAt: t0.Add(time.Hour),
				}
				valid(e.t, at)

				if err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.AccountTokens().Create(ctx, at) }); err != nil {
					e.t.Fatalf("Create %s: %v", purpose, err)
				}

				got, err := get(e, string(purpose))
				if err != nil {
					e.t.Fatalf("Get %s: %v", purpose, err)
				}

				wantEqual(e.t, got, at)
			}

			// DeleteByAccount removes only the tokens of the given purpose.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.AccountTokens().DeleteByAccount(ctx, a.ID, domain.TokenResetPassword)
			})

			for _, purpose := range domain.TokenPurposes() {
				_, err := get(e, string(purpose))
				if purpose == domain.TokenResetPassword {
					wantErr(e.t, err, domain.ErrNotFound)
				} else if err != nil {
					e.t.Fatalf("Get %s: %v", purpose, err)
				}
			}
		},
		"Conflicts": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			at := e.accountToken(a, "secret", t0.Add(time.Hour))

			create := func(at domain.AccountToken) error {
				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.AccountTokens().Create(ctx, at) })
			}

			wantErr(e.t, create(at), domain.ErrConflict)

			at.Hash, at.AccountID = domain.HashToken("other"), newID[domain.AccountID](e)
			wantErr(e.t, create(at), domain.ErrConflict)
		},
		"DeleteByAccount": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			b := e.account(e.person("Bea", "Account"), "bea@example.org")
			e.accountToken(a, "a1", t0.Add(time.Hour))
			e.accountToken(a, "a2", t0.Add(time.Hour))
			keep := e.accountToken(b, "b1", t0.Add(time.Hour))

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.AccountTokens().DeleteByAccount(ctx, a.ID, domain.TokenConfirmEmail)
			})

			for _, token := range []string{"a1", "a2"} {
				_, err := get(e, token)
				wantErr(e.t, err, domain.ErrNotFound)
			}

			got, err := get(e, "b1")
			if err != nil {
				e.t.Fatalf("Get: %v", err)
			}

			wantEqual(e.t, got, keep)
		},
		"DeleteExpired": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			e.accountToken(a, "old", t0.Add(-time.Minute))
			e.accountToken(a, "edge", t0)
			e.accountToken(a, "fresh", t0.Add(time.Minute))

			var n int

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				n, err = tx.AccountTokens().DeleteExpired(ctx, t0)

				return err
			})
			wantEqual(e.t, n, 2)

			if _, err := get(e, "fresh"); err != nil {
				e.t.Fatalf("Get: %v", err)
			}
		},
		"ReadOnlyRejectsWrites": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			at := e.accountToken(a, "secret", t0.Add(time.Hour))

			for name, write := range map[string]func(context.Context, store.Tx) error{
				"Create": func(ctx context.Context, tx store.Tx) error { return tx.AccountTokens().Create(ctx, at) },
				"Delete": func(ctx context.Context, tx store.Tx) error { return tx.AccountTokens().Delete(ctx, at.Hash) },
				"DeleteByAccount": func(ctx context.Context, tx store.Tx) error {
					return tx.AccountTokens().DeleteByAccount(ctx, a.ID, domain.TokenConfirmEmail)
				},
				"DeleteExpired": func(ctx context.Context, tx store.Tx) error {
					_, err := tx.AccountTokens().DeleteExpired(ctx, t0.Add(2*time.Hour))

					return err
				},
			} {
				if err := e.read(write); !errors.Is(err, store.ErrReadOnly) {
					e.t.Fatalf("%s: want ErrReadOnly, got %v", name, err)
				}
			}
		},
	})
}

func testAudit(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	list := func(e *env, limit int) []domain.AuditEvent {
		e.t.Helper()

		events, err := query(e, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) {
			return tx.Audit().List(ctx, limit)
		})
		if err != nil {
			e.t.Fatalf("List: %v", err)
		}

		return events
	}

	event := func(e *env, at time.Time, actor, target domain.Account, action domain.AuditAction) domain.AuditEvent {
		ev := domain.AuditEvent{ID: newID[domain.AuditEventID](e), At: at, ActorID: actor.ID, Action: action, TargetID: target.ID}
		valid(e.t, ev)

		return ev
	}

	create := func(e *env, ev domain.AuditEvent) error {
		return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Audit().Create(ctx, ev) })
	}

	cases(t, newStore, map[string]func(e *env){
		"CreateList": func(e *env) {
			admin := e.account(e.person("Ada", "Admin"), "ada@example.org")
			member := e.account(e.person("Ben", "Member"), "ben@example.org")

			wantEqual(e.t, len(list(e, 10)), 0)

			started := event(e, t0, admin, member, domain.AuditImpersonationStarted)
			ended := event(e, t0.Add(time.Minute), admin, member, domain.AuditImpersonationEnded)
			sameTime := event(e, t0.Add(time.Minute), admin, member, domain.AuditImpersonationStarted)

			for _, ev := range []domain.AuditEvent{started, ended, sameTime} {
				if err := create(e, ev); err != nil {
					e.t.Fatalf("Create: %v", err)
				}
			}

			// Newest first; equal times by ID descending.
			wantSlice(e.t, list(e, 10), []domain.AuditEvent{sameTime, ended, started})
			wantSlice(e.t, list(e, 1), []domain.AuditEvent{sameTime})
			wantEqual(e.t, len(list(e, 0)), 0)
		},
		"Conflicts": func(e *env) {
			admin := e.account(e.person("Ada", "Admin"), "ada@example.org")
			member := e.account(e.person("Ben", "Member"), "ben@example.org")

			ev := event(e, t0, admin, member, domain.AuditImpersonationStarted)
			if err := create(e, ev); err != nil {
				e.t.Fatalf("Create: %v", err)
			}

			wantErr(e.t, create(e, ev), domain.ErrConflict) // same ID

			noActor := ev
			noActor.ID, noActor.ActorID = newID[domain.AuditEventID](e), newID[domain.AccountID](e)
			wantErr(e.t, create(e, noActor), domain.ErrConflict)

			noTarget := ev
			noTarget.ID, noTarget.TargetID = newID[domain.AuditEventID](e), newID[domain.AccountID](e)
			wantErr(e.t, create(e, noTarget), domain.ErrConflict)
		},
		"ReadOnlyRejectsWrites": func(e *env) {
			admin := e.account(e.person("Ada", "Admin"), "ada@example.org")
			member := e.account(e.person("Ben", "Member"), "ben@example.org")
			ev := event(e, t0, admin, member, domain.AuditImpersonationStarted)

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Audit().Create(ctx, ev) })
			wantErr(e.t, err, store.ErrReadOnly)
		},
	})
}
