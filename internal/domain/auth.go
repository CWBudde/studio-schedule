package domain

import (
	"crypto/sha256"
	"slices"
	"time"
)

// HashToken returns the SHA-256 hash under which a secret token (a session
// cookie or a link sent by email) is stored. Only the hash is stored, so a
// leaked database does not reveal usable tokens.
func HashToken(token string) [32]byte { return sha256.Sum256([]byte(token)) }

// Credential is the password of an account. An account has at most one, so
// it has no ID of its own.
type Credential struct {
	AccountID AccountID
	// PasswordHash is an encoded hash (PHC string format), never the
	// password itself.
	PasswordHash string
	ChangedAt    time.Time
}

// Validate checks the credential for consistency. All errors wrap
// ErrValidation.
func (c Credential) Validate() error {
	return validate(
		rule{!c.AccountID.IsZero(), "credential account must be set"},
		rule{!blank(c.PasswordHash), "password hash must not be empty"},
		rule{!c.ChangedAt.IsZero(), "credential change time must be set"},
	)
}

// Session is a signed-in browser session. The client holds the token in a
// cookie; the store knows only its hash.
type Session struct {
	ID        SessionID
	AccountID AccountID
	TokenHash [32]byte
	CreatedAt time.Time
	// LastSeenAt is when the session was last extended (sliding expiry).
	LastSeenAt time.Time
	// ExpiresAt is when the session ends unless it is extended before.
	ExpiresAt time.Time
	// ImpersonatorID is the admin who signed in as AccountID ("login as");
	// zero for a normal session. Such a session is never extended.
	ImpersonatorID AccountID
}

// Validate checks the session for consistency. All errors wrap
// ErrValidation.
func (s Session) Validate() error {
	return validate(
		rule{!s.ID.IsZero(), "session id must be set"},
		rule{!s.AccountID.IsZero(), "session account must be set"},
		rule{s.TokenHash != [32]byte{}, "session token hash must be set"},
		rule{!s.CreatedAt.IsZero(), "session creation time must be set"},
		rule{!s.LastSeenAt.Before(s.CreatedAt), "session cannot be seen before it is created"},
		rule{s.ExpiresAt.After(s.LastSeenAt), "session must expire after it was last seen"},
		rule{s.ImpersonatorID != s.AccountID, "an account cannot impersonate itself"},
	)
}

// TokenPurpose says what a single-use account token (sent by email) does.
type TokenPurpose string

// Known token purposes.
const (
	// TokenConfirmEmail confirms the email address of a new account.
	TokenConfirmEmail TokenPurpose = "confirm-email"
)

// TokenPurposes returns all token purposes.
func TokenPurposes() []TokenPurpose { return []TokenPurpose{TokenConfirmEmail} }

// Valid reports whether p is a known token purpose.
func (p TokenPurpose) Valid() bool { return slices.Contains(TokenPurposes(), p) }

// AccountToken is a single-use secret sent to an email address, such as an
// email confirmation link. Only its hash is stored; it is deleted when used.
type AccountToken struct {
	Hash      [32]byte
	AccountID AccountID
	Purpose   TokenPurpose
	// Email is the address the token was sent to. The token is only valid
	// while the account still has that address.
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Validate checks the token for consistency. All errors wrap
// ErrValidation.
func (t AccountToken) Validate() error {
	return validate(
		rule{t.Hash != [32]byte{}, "token hash must be set"},
		rule{!t.AccountID.IsZero(), "token account must be set"},
		rule{t.Purpose.Valid(), "invalid token purpose"},
		rule{validEmail(t.Email), "invalid token email"},
		rule{!t.CreatedAt.IsZero(), "token creation time must be set"},
		rule{t.ExpiresAt.After(t.CreatedAt), "token must expire after it is created"},
	)
}
