package domain

import (
	"slices"
	"time"
)

// FeedScope selects which lessons a calendar feed contains (ADR 0011).
type FeedScope string

// Known feed scopes.
const (
	FeedBookings FeedScope = "bookings" // the lessons the account's family is booked for
	FeedTeaching FeedScope = "teaching" // the lessons the account's staff records teach or accompany
	FeedSchedule FeedScope = "schedule" // the schedule as the account sees it, or one location of it
)

// FeedScopes returns all feed scopes.
func FeedScopes() []FeedScope { return []FeedScope{FeedBookings, FeedTeaching, FeedSchedule} }

// Valid reports whether s is a known feed scope.
func (s FeedScope) Valid() bool { return slices.Contains(FeedScopes(), s) }

// CalendarFeed is an iCalendar subscription URL of an account, secured by a
// random token (ADR 0011). Only the SHA-256 of the token is stored. An
// account has at most one active (not revoked) feed per subject: scope and
// location.
type CalendarFeed struct {
	ID        CalendarFeedID
	AccountID AccountID
	Scope     FeedScope
	// LocationID narrows a schedule feed to one location; optional, and only
	// for FeedSchedule.
	LocationID LocationID
	TokenHash  [32]byte
	CreatedAt  time.Time
	// LastUsedAt is when a calendar last fetched the feed, updated at most
	// once a day; optional.
	LastUsedAt time.Time
	RevokedAt  time.Time // optional
}

// Active reports whether the feed is not revoked.
func (f CalendarFeed) Active() bool { return f.RevokedAt.IsZero() }

// Validate checks the feed for consistency. All errors wrap ErrValidation.
func (f CalendarFeed) Validate() error {
	return validate(
		rule{!f.ID.IsZero(), "calendar feed id must be set"},
		rule{!f.AccountID.IsZero(), "calendar feed account must be set"},
		on("scope", f.Scope.Valid(), "invalid calendar feed scope"),
		on("locationId", f.LocationID.IsZero() || f.Scope == FeedSchedule, "only a schedule feed has a location"),
		rule{f.TokenHash != [32]byte{}, "calendar feed token hash must be set"},
		rule{!f.CreatedAt.IsZero(), "calendar feed creation time must be set"},
		rule{notBefore(f.LastUsedAt, f.CreatedAt), "calendar feed cannot be used before it was created"},
		rule{notBefore(f.RevokedAt, f.CreatedAt), "calendar feed cannot be revoked before it was created"},
	)
}

// MaxImportErrors is the most row errors an import run records; its counts
// still cover every row.
const MaxImportErrors = 200

// ImportRun records an applied run of the member import (ADR 0015): who ran
// it, on which file, what it did, and the rows it could not take. A dry run
// leaves no record.
type ImportRun struct {
	ID       ImportRunID
	FileName string
	// ActorID is the account that ran the import, also on the command line
	// (its --as, ADR 0020); zero only for a run of the command line before
	// that.
	ActorID    AccountID
	StartedAt  time.Time
	FinishedAt time.Time
	Counts     ImportCounts
	// Errors are rows that were not imported, at most MaxImportErrors, in
	// file order.
	Errors []ImportRowError
}

// ImportCounts count what an import did. Every row of the file is created,
// updated, unchanged or failed.
type ImportCounts struct {
	Rows      int `json:"rows"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Failed    int `json:"failed"`
	// GuardiansCreated and GuardiansUpdated count the guardians the rows
	// created or changed, LinksAdded the guardian links they added.
	GuardiansCreated int `json:"guardiansCreated"`
	GuardiansUpdated int `json:"guardiansUpdated"`
	LinksAdded       int `json:"linksAdded"`
}

// ImportRowError is a row an import could not take, with the first reason:
// a code the import defines and the target field it is about, if any.
type ImportRowError struct {
	// Line is the row's line in the file, the header being line 1.
	Line  int    `json:"line"`
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
}

// Validate checks the run for consistency. All errors wrap ErrValidation.
func (r ImportRun) Validate() error {
	c := r.Counts

	return validate(
		rule{!r.ID.IsZero(), "import run id must be set"},
		rule{!blank(r.FileName), "import file name must not be empty"},
		rule{!r.StartedAt.IsZero() && !r.FinishedAt.IsZero(), "import start and finish must be set"},
		rule{notBefore(r.FinishedAt, r.StartedAt), "import cannot finish before it started"},
		rule{min(c.Rows, c.Created, c.Updated, c.Unchanged, c.Failed, c.GuardiansCreated, c.GuardiansUpdated,
			c.LinksAdded) >= 0, "import counts must not be negative"},
		rule{c.Rows == c.Created+c.Updated+c.Unchanged+c.Failed, "every row is counted once"},
		rule{len(r.Errors) <= min(c.Failed, MaxImportErrors), "an import records one error per failed row at most"},
		rule{
			!slices.ContainsFunc(r.Errors, func(e ImportRowError) bool { return e.Line < 1 || blank(e.Code) }),
			"an import error has a line and a code",
		},
	)
}
