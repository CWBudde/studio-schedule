package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	authpassword "github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Session and token lifetimes (ADR 0004).
const (
	// SessionIdleTimeout ends a session that was not used for this long.
	SessionIdleTimeout = 14 * 24 * time.Hour
	// SessionMaxAge ends every session this long after the login: members
	// sign in again at least every 30 days (ADR 0004, amendment; ASVS 3.3.2).
	SessionMaxAge = 30 * 24 * time.Hour
	// sessionRefreshAfter limits how often a used session is extended, so
	// that most requests only read.
	sessionRefreshAfter = 24 * time.Hour
	// ConfirmTokenTTL is how long an email confirmation link is valid.
	ConfirmTokenTTL = 24 * time.Hour
)

// Password limits, in characters as the API counts them. The upper limit
// bounds the hashing work per request.
const (
	MinPasswordLength = authpassword.MinLength
	MaxPasswordLength = 256
)

// tokenBytes is the entropy of session and email tokens.
const tokenBytes = 32

var (
	// ErrInvalidCredentials is the one answer to a failed login, whether the
	// email is unknown, the password wrong or the account disabled, so that
	// a login reveals nothing about which accounts exist.
	ErrInvalidCredentials = fmt.Errorf("%w: invalid email or password", domain.ErrUnauthorized)
	// ErrEmailNotConfirmed rejects a login with the right password to an
	// account whose email is not confirmed yet.
	ErrEmailNotConfirmed = fmt.Errorf("%w: email address not confirmed", domain.ErrForbidden)
	// ErrInvalidToken reports an unknown, used or expired email link.
	ErrInvalidToken = fmt.Errorf("%w: the link is invalid or has expired", domain.ErrValidation)
	// ErrNoSession reports a request without a valid session.
	ErrNoSession = fmt.Errorf("%w: not signed in", domain.ErrUnauthorized)
)

// PasswordHasher hashes and verifies passwords; package auth/password
// implements it with argon2id. Verify reads the parameters from the encoded
// hash; NeedsRehash reports a hash made with other parameters.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(encoded, password string) (bool, error)
	NeedsRehash(encoded string) bool
}

// MailRenderer renders a mail template; *mail.Renderer implements it.
type MailRenderer interface {
	Render(locale, name string, data any) (mail.Rendered, error)
	// RenderPush renders the web push notification of a template (one of
	// mail.PushTemplateNames) with the data of its mail (ADR 0017).
	RenderPush(locale, name string, data any) (mail.PushText, error)
	// MatchLocale returns the template locale for a requested one, or false
	// if there is none.
	MatchLocale(locale string) (string, bool)
}

// Principal is the signed-in account of a request.
type Principal struct {
	AccountID domain.AccountID
	PersonID  domain.PersonID
	Email     string
	Role      domain.Role
	SessionID domain.SessionID
	// ImpersonatorID is the admin who signed in as this account; zero
	// unless the session is an impersonation.
	ImpersonatorID domain.AccountID
	// VisibleDays is the account's early-access override (0: the school's
	// default).
	VisibleDays int
}

// SignedIn is a new or extended session together with its secret token,
// which only the client keeps (in a cookie).
type SignedIn struct {
	Token     string
	Principal Principal
	ExpiresAt time.Time
}

// Registration is the input of AuthService.Register.
type Registration struct {
	GivenName  string
	FamilyName string
	Email      string
	Password   string
	// Locale is the language of the confirmation mail (e.g. "de") and is
	// stored as the account's language if a mail template matches it;
	// empty means the school's default.
	Locale string
}

// AuthService registers accounts, confirms their email and manages
// sessions.
type AuthService struct {
	store  store.Store
	clock  clock.Clock
	ids    ids.Source
	hasher PasswordHasher
	mails  MailRenderer
	appURL string

	dummyOnce sync.Once
	dummyHash string
	dummyErr  error
}

// NewAuthService returns an AuthService. appURL is the absolute URL of the
// web app that mail links point to, ending in "/" (or "#/" with hash
// routing); a route is appended to it, e.g. appURL + "confirm?token=...".
func NewAuthService(
	st store.Store, clk clock.Clock, src ids.Source, hasher PasswordHasher, mails MailRenderer, appURL string,
) *AuthService {
	return &AuthService{store: st, clock: clk, ids: src, hasher: hasher, mails: mails, appURL: appURL}
}

// Register creates a member account with an unconfirmed email and mails a
// confirmation link to it. To keep account existence private it succeeds
// without a mail for an email that already has an account, unless that
// account is itself an unconfirmed registration: then the name and password
// are replaced and a new link is sent (older links stop working). The latest
// registration wins, and since confirming needs the password, someone who
// registered a foreign address first cannot take the account over. Accounts
// created by the school are never changed here.
func (s *AuthService) Register(ctx context.Context, r Registration) error {
	r.Email = strings.TrimSpace(r.Email)
	r.GivenName, r.FamilyName = strings.TrimSpace(r.GivenName), strings.TrimSpace(r.FamilyName)

	if err := checkPassword("password", r.Password); err != nil {
		return err
	}

	// Hashing is slow; it runs before the transaction, which may be retried.
	hash, err := s.hasher.Hash(r.Password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	token, err := newToken()
	if err != nil {
		return err
	}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		now := s.clock.Now()

		acc, err := tx.Accounts().GetByEmail(ctx, r.Email)

		switch {
		case err == nil:
			pending, err := isPendingRegistration(ctx, tx, acc)
			if err != nil || !pending {
				return err // an account exists; say nothing
			}

			if acc, err = s.renewPending(ctx, tx, acc, r); err != nil {
				return err
			}
		case errors.Is(err, domain.ErrNotFound):
			if acc, err = s.createPending(ctx, tx, r); err != nil {
				return err
			}
		default:
			return err
		}

		cred := domain.Credential{AccountID: acc.ID, PasswordHash: hash, ChangedAt: now}
		if err := tx.Credentials().Put(ctx, cred); err != nil {
			return err
		}

		return s.sendConfirmation(ctx, tx, acc, r, token)
	})
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}

	return nil
}

// createPending creates the person and the unconfirmed account of a new
// registration.
func (s *AuthService) createPending(ctx context.Context, tx store.Tx, r Registration) (domain.Account, error) {
	p := domain.Person{ID: ids.New[domain.PersonID](s.ids), GivenName: r.GivenName, FamilyName: r.FamilyName}
	acc := domain.Account{ID: ids.New[domain.AccountID](s.ids), PersonID: p.ID, Email: r.Email, Role: domain.RoleMember}
	acc.Locale, _ = s.mails.MatchLocale(r.Locale)

	if err := p.Validate(); err != nil {
		return domain.Account{}, err
	}

	if err := acc.Validate(); err != nil {
		return domain.Account{}, err
	}

	if err := tx.Persons().Create(ctx, p); err != nil {
		return domain.Account{}, err
	}

	return acc, tx.Accounts().Create(ctx, acc)
}

// isPendingRegistration reports whether acc is an unconfirmed account that
// was created by a registration (which sets a password).
func isPendingRegistration(ctx context.Context, tx store.Tx, acc domain.Account) (bool, error) {
	if !acc.ConfirmedAt.IsZero() {
		return false, nil
	}

	_, err := tx.Credentials().Get(ctx, acc.ID)

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

// renewPending updates the person and the language of an unconfirmed
// account to a new registration and returns the account.
func (s *AuthService) renewPending(
	ctx context.Context, tx store.Tx, acc domain.Account, r Registration,
) (domain.Account, error) {
	p, err := tx.Persons().Get(ctx, acc.PersonID)
	if err != nil {
		return domain.Account{}, err
	}

	p.GivenName, p.FamilyName = r.GivenName, r.FamilyName
	if err := p.Validate(); err != nil {
		return domain.Account{}, err
	}

	if err := tx.Persons().Update(ctx, p); err != nil {
		return domain.Account{}, err
	}

	if locale, ok := s.mails.MatchLocale(r.Locale); ok && locale != acc.Locale {
		acc.Locale = locale
		if err := tx.Accounts().Update(ctx, acc); err != nil {
			return domain.Account{}, err
		}
	}

	return acc, nil
}

// sendConfirmation replaces the account's confirmation links with a new one
// and writes the mail to the outbox.
func (s *AuthService) sendConfirmation(
	ctx context.Context, tx store.Tx, acc domain.Account, r Registration, token string,
) error {
	return s.sendLink(ctx, tx, acc, token, linkMail{
		purpose: domain.TokenConfirmEmail, to: acc.Email, ttl: ConfirmTokenTTL,
		template: mail.TemplateConfirmEmail, route: "confirm", locale: r.Locale,
	})
}

// Confirm confirms the email of the account a confirmation link was sent
// for and signs it in. It needs the account's password, so a link alone
// does not confirm someone else's registration.
func (s *AuthService) Confirm(ctx context.Context, token, password string) (SignedIn, error) {
	var (
		acc  domain.Account
		cred domain.Credential
	)

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		acc, cred, err = s.tokenAccount(ctx, tx, token)

		return err
	})
	if err != nil {
		return SignedIn{}, err
	}

	if err := s.verify(cred, password); err != nil {
		return SignedIn{}, err
	}

	if !acc.DisabledAt.IsZero() {
		return SignedIn{}, ErrInvalidCredentials
	}

	return s.signIn(ctx, acc, password, cred, func(ctx context.Context, tx store.Tx, now time.Time) error {
		// The password must still be the one just verified: a registration
		// in between replaces it and sends a new link.
		cur, curCred, err := s.tokenAccount(ctx, tx, token)
		if err != nil {
			return err
		}

		if curCred.PasswordHash != cred.PasswordHash {
			return ErrInvalidToken
		}

		// The token is used up here; a concurrent use finds it gone.
		if err := tx.AccountTokens().Delete(ctx, domain.HashToken(token)); err != nil {
			return mapTokenErr(err)
		}

		if cur.ConfirmedAt.IsZero() {
			cur.ConfirmedAt = now
		}

		return tx.Accounts().Update(ctx, cur)
	})
}

// tokenAccount returns the account and credential of a valid confirmation
// token.
func (s *AuthService) tokenAccount(
	ctx context.Context, tx store.Tx, token string,
) (domain.Account, domain.Credential, error) {
	_, acc, err := s.linkToken(ctx, tx, token, domain.TokenConfirmEmail)
	if err != nil {
		return domain.Account{}, domain.Credential{}, err
	}

	cred, err := tx.Credentials().Get(ctx, acc.ID)
	if err != nil {
		return domain.Account{}, domain.Credential{}, err
	}

	return acc, cred, nil
}

func mapTokenErr(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return ErrInvalidToken
	}

	return err
}

// Login signs an account in with its email and password.
func (s *AuthService) Login(ctx context.Context, email, password string) (SignedIn, error) {
	if tooLong(password) {
		return SignedIn{}, ErrInvalidCredentials
	}

	var (
		acc   domain.Account
		cred  domain.Credential
		found bool
	)

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		acc, err = tx.Accounts().GetByEmail(ctx, strings.TrimSpace(email))
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}

		cred, err = tx.Credentials().Get(ctx, acc.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}

		found = err == nil

		return err
	})
	if err != nil {
		return SignedIn{}, fmt.Errorf("login: %w", err)
	}

	if !found {
		// Spend the same time as for a wrong password.
		dummy, err := s.dummy()
		if err != nil {
			return SignedIn{}, err
		}

		_, _ = s.hasher.Verify(dummy, password)

		return SignedIn{}, ErrInvalidCredentials
	}

	if err := s.verify(cred, password); err != nil {
		return SignedIn{}, err
	}

	switch {
	case !acc.DisabledAt.IsZero():
		return SignedIn{}, ErrInvalidCredentials
	case acc.ConfirmedAt.IsZero():
		return SignedIn{}, ErrEmailNotConfirmed
	}

	return s.signIn(ctx, acc, password, cred, nil)
}

// verify checks password against the stored credential.
func (s *AuthService) verify(cred domain.Credential, password string) error {
	if tooLong(password) {
		return ErrInvalidCredentials
	}

	ok, err := s.hasher.Verify(cred.PasswordHash, password)
	if err != nil {
		return fmt.Errorf("verify password of account %s: %w", cred.AccountID, err)
	}

	if !ok {
		return ErrInvalidCredentials
	}

	return nil
}

// dummy returns a hash to verify against when the email is unknown, made
// once with the hasher's current parameters.
func (s *AuthService) dummy() (string, error) {
	s.dummyOnce.Do(func() {
		s.dummyHash, s.dummyErr = s.hasher.Hash("not a password of anyone")
	})

	if s.dummyErr != nil {
		return "", fmt.Errorf("dummy password hash: %w", s.dummyErr)
	}

	return s.dummyHash, nil
}

// signIn creates a session for acc. If the password hash uses outdated
// parameters it is replaced. extra runs first in the same transaction.
func (s *AuthService) signIn(
	ctx context.Context, acc domain.Account, password string, cred domain.Credential,
	extra func(ctx context.Context, tx store.Tx, now time.Time) error,
) (SignedIn, error) {
	var rehash string

	if s.hasher.NeedsRehash(cred.PasswordHash) {
		var err error
		if rehash, err = s.hasher.Hash(password); err != nil {
			return SignedIn{}, fmt.Errorf("rehash password: %w", err)
		}
	}

	token, err := newToken()
	if err != nil {
		return SignedIn{}, err
	}

	var sess domain.Session

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		now := s.clock.Now()

		if extra != nil {
			if err := extra(ctx, tx, now); err != nil {
				return err
			}
		}

		if rehash != "" {
			c := domain.Credential{AccountID: acc.ID, PasswordHash: rehash, ChangedAt: cred.ChangedAt}
			if err := tx.Credentials().Put(ctx, c); err != nil {
				return err
			}
		}

		var err error
		sess, err = s.createSession(ctx, tx, acc.ID, token, now, domain.AccountID{})

		return err
	})
	if err != nil {
		return SignedIn{}, fmt.Errorf("sign in: %w", err)
	}

	return SignedIn{Token: token, Principal: principal(acc, sess), ExpiresAt: sess.ExpiresAt}, nil
}

// createSession stores a new session of account for token. An
// impersonation session (impersonator set) lasts ImpersonationMaxAge and is
// never extended.
func (s *AuthService) createSession(
	ctx context.Context, tx store.Tx, account domain.AccountID, token string, now time.Time,
	impersonator domain.AccountID,
) (domain.Session, error) {
	sess := domain.Session{
		ID: ids.New[domain.SessionID](s.ids), AccountID: account, TokenHash: domain.HashToken(token),
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(SessionIdleTimeout), ImpersonatorID: impersonator,
	}
	if !impersonator.IsZero() {
		sess.ExpiresAt = now.Add(ImpersonationMaxAge)
	}

	if err := sess.Validate(); err != nil {
		return domain.Session{}, err
	}

	return sess, tx.Sessions().Create(ctx, sess)
}

// Authenticate resolves a session token to its principal. A session that
// was last extended more than a day ago is extended (sliding expiry, capped
// by SessionMaxAge); then the returned SignedIn carries the new expiry for
// the cookie, otherwise it is the zero value. An impersonation session is
// never extended, and it ends as soon as its admin may no longer sign in or
// is no admin any more, or its account may no longer sign in or becomes an
// admin.
func (s *AuthService) Authenticate(ctx context.Context, token string) (Principal, SignedIn, error) {
	if token == "" {
		return Principal{}, SignedIn{}, ErrNoSession
	}

	hash := domain.HashToken(token)

	var (
		acc, admin domain.Account
		sess       domain.Session
	)

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		if sess, err = tx.Sessions().GetByToken(ctx, hash); err != nil {
			return err
		}

		if !sess.ImpersonatorID.IsZero() {
			if admin, err = tx.Accounts().Get(ctx, sess.ImpersonatorID); err != nil {
				return err
			}
		}

		acc, err = tx.Accounts().Get(ctx, sess.AccountID)

		return err
	})

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return Principal{}, SignedIn{}, ErrNoSession
	case err != nil:
		return Principal{}, SignedIn{}, fmt.Errorf("authenticate: %w", err)
	}

	now := s.clock.Now()
	if !now.Before(sessionEnd(sess)) || !mayActAs(acc) ||
		(!sess.ImpersonatorID.IsZero() && (!mayActAs(admin) || admin.Role != domain.RoleAdmin ||
			acc.Role == domain.RoleAdmin)) { // an admin is never impersonated, even once promoted (ADR 0016)
		return Principal{}, SignedIn{}, ErrNoSession
	}

	p := principal(acc, sess)
	if now.Sub(sess.LastSeenAt) < sessionRefreshAfter || !sess.ImpersonatorID.IsZero() {
		return p, SignedIn{}, nil
	}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		cur, err := tx.Sessions().GetByToken(ctx, hash)
		if err != nil {
			return err
		}

		cur.LastSeenAt = now
		cur.ExpiresAt = minTime(now.Add(SessionIdleTimeout), cur.CreatedAt.Add(SessionMaxAge))
		sess = cur

		return tx.Sessions().Update(ctx, cur)
	})

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return Principal{}, SignedIn{}, ErrNoSession // logged out meanwhile
	case err != nil:
		return Principal{}, SignedIn{}, fmt.Errorf("extend session: %w", err)
	}

	return p, SignedIn{Token: token, Principal: p, ExpiresAt: sess.ExpiresAt}, nil
}

// sessionEnd is when sess ends unless it is extended before.
func sessionEnd(sess domain.Session) time.Time {
	return minTime(sess.ExpiresAt, sess.CreatedAt.Add(SessionMaxAge))
}

// Logout ends the session of token. Unknown tokens are ignored. Signing out
// of an impersonation ends it (audited) without signing the admin in again.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		sess, err := tx.Sessions().GetByToken(ctx, domain.HashToken(token))
		if err != nil {
			return err
		}

		// An impersonation that expired already ended then; it is not
		// ended again.
		if !sess.ImpersonatorID.IsZero() && s.clock.Now().Before(sessionEnd(sess)) {
			err := s.audit(ctx, tx, sess.ImpersonatorID, domain.AuditImpersonationEnded, sess.AccountID)
			if err != nil {
				return err
			}
		}

		return tx.Sessions().Delete(ctx, sess.ID)
	})
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("logout: %w", err)
	}

	return nil
}

// PurgeExpired deletes expired sessions and email tokens and returns how
// many it deleted.
func (s *AuthService) PurgeExpired(ctx context.Context) (int, error) {
	var n int

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		now := s.clock.Now()

		sessions, err := tx.Sessions().DeleteExpired(ctx, now)
		if err != nil {
			return err
		}

		tokens, err := tx.AccountTokens().DeleteExpired(ctx, now)
		n = sessions + tokens

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("purge expired sessions and tokens: %w", err)
	}

	return n, nil
}

// Me is the signed-in account together with its person. Impersonator is
// the admin who signed in as it, if any (with its own Impersonator nil).
type Me struct {
	Account domain.Account
	Person  domain.Person
	// Teaches tells that the account teaches lessons: it may have a
	// teaching calendar feed (see teaches); not set for the Impersonator.
	Teaches      bool
	Impersonator *Me
}

// Me returns the account and person of p, and those of its impersonator.
func (s *AuthService) Me(ctx context.Context, p Principal) (Me, error) {
	var me Me

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		if me, err = accountWithPerson(ctx, tx, p.AccountID); err != nil {
			return err
		}

		if me.Teaches, err = teaches(ctx, tx, me.Account.Role, me.Person.ID); err != nil || p.ImpersonatorID.IsZero() {
			return err
		}

		admin, err := accountWithPerson(ctx, tx, p.ImpersonatorID)
		me.Impersonator = &admin

		return err
	})
	if err != nil {
		return Me{}, fmt.Errorf("me: %w", err)
	}

	return me, nil
}

// SetLocale sets the language of the mails p's account receives, a mail
// template locale or a tag that matches one ("de-AT" is stored as "de").
// Empty means the school's default. An unknown language is a validation
// error.
func (s *AuthService) SetLocale(ctx context.Context, p Principal, locale string) error {
	if locale != "" {
		matched, ok := s.mails.MatchLocale(locale)
		if !ok {
			return fmt.Errorf("%w: unknown language %q", domain.ErrValidation, locale)
		}

		locale = matched
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().Get(ctx, p.AccountID)
		if err != nil || acc.Locale == locale {
			return err
		}

		acc.Locale = locale

		return tx.Accounts().Update(ctx, acc)
	})
	if err != nil {
		return fmt.Errorf("set locale: %w", err)
	}

	return nil
}

func accountWithPerson(ctx context.Context, tx store.Tx, id domain.AccountID) (Me, error) {
	acc, err := tx.Accounts().Get(ctx, id)
	if err != nil {
		return Me{}, err
	}

	person, err := tx.Persons().Get(ctx, acc.PersonID)

	return Me{Account: acc, Person: person}, err
}

func principal(acc domain.Account, sess domain.Session) Principal {
	return Principal{
		AccountID: acc.ID, PersonID: acc.PersonID, Email: acc.Email, Role: acc.Role, SessionID: sess.ID,
		ImpersonatorID: sess.ImpersonatorID, VisibleDays: acc.VisibleDays,
	}
}

// mayActAs reports whether a session may act as acc: it is confirmed and not
// disabled.
func mayActAs(acc domain.Account) bool {
	return !acc.ConfirmedAt.IsZero() && acc.DisabledAt.IsZero()
}

// checkPassword enforces the rules for a new password (ADR 0004): its length
// and that it is none of the most common passwords (ASVS 2.1.7). A broken
// rule is a field error on field, the JSON name of the new password; the
// web app tells the rules apart by the phrases "at least", "at most" and
// "most common" (passwordProblem, web/src/features/auth/form.ts).
func checkPassword(field, pw string) error {
	switch n := utf8.RuneCountInString(pw); {
	case n < MinPasswordLength:
		return &domain.FieldError{
			Field: field, Msg: fmt.Sprintf("the password must have at least %d characters", MinPasswordLength),
		}
	case n > MaxPasswordLength:
		return &domain.FieldError{
			Field: field, Msg: fmt.Sprintf("the password must have at most %d characters", MaxPasswordLength),
		}
	case authpassword.IsCommon(pw):
		return &domain.FieldError{Field: field, Msg: "the password is one of the most common ones; choose another"}
	}

	return nil
}

// tooLong reports a password longer than any that can be set, which
// therefore matches no stored hash.
func tooLong(pw string) bool {
	return len(pw) > utf8.UTFMax*MaxPasswordLength || utf8.RuneCountInString(pw) > MaxPasswordLength
}

// newToken returns a random URL-safe token with 256 bits of entropy.
func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}
