package domain

import (
	"slices"
	"time"
)

// FeedScope selects which lessons a calendar feed contains.
type FeedScope string

// Known feed scopes.
const (
	FeedBookings FeedScope = "bookings" // the account's own bookings
	FeedTeaching FeedScope = "teaching" // lessons the account's staff member teaches
	FeedSchedule FeedScope = "schedule" // the public schedule
)

// FeedScopes returns all feed scopes.
func FeedScopes() []FeedScope { return []FeedScope{FeedBookings, FeedTeaching, FeedSchedule} }

// Valid reports whether s is a known feed scope.
func (s FeedScope) Valid() bool { return slices.Contains(FeedScopes(), s) }

// CalendarFeed is an iCal subscription URL secured by a random token. Only
// the SHA-256 of the token is stored.
type CalendarFeed struct {
	ID        CalendarFeedID
	AccountID AccountID
	Scope     FeedScope
	TokenHash [32]byte
	CreatedAt time.Time
	RevokedAt time.Time // optional
}

// Validate checks the feed for consistency. All errors wrap ErrValidation.
func (f CalendarFeed) Validate() error {
	return validate(
		rule{!f.ID.IsZero(), "calendar feed id must be set"},
		rule{!f.AccountID.IsZero(), "calendar feed account must be set"},
		rule{f.Scope.Valid(), "invalid calendar feed scope"},
		rule{f.TokenHash != [32]byte{}, "calendar feed token hash must be set"},
		rule{!f.CreatedAt.IsZero(), "calendar feed creation time must be set"},
		rule{notBefore(f.RevokedAt, f.CreatedAt), "calendar feed cannot be revoked before it was created"},
	)
}

// ImportStatus is the state of an import run.
type ImportStatus string

// Known import statuses.
const (
	ImportRunning   ImportStatus = "running"
	ImportSucceeded ImportStatus = "succeeded"
	ImportFailed    ImportStatus = "failed"
)

// ImportStatuses returns all import statuses.
func ImportStatuses() []ImportStatus {
	return []ImportStatus{ImportRunning, ImportSucceeded, ImportFailed}
}

// Valid reports whether s is a known import status.
func (s ImportStatus) Valid() bool { return slices.Contains(ImportStatuses(), s) }

// ImportRun records one run of a data import.
type ImportRun struct {
	ID         ImportRunID
	Source     string // what was imported, e.g. a file or system name
	Status     ImportStatus
	StartedAt  time.Time
	FinishedAt time.Time // set exactly when the run is no longer running
	Summary    string    // optional
}

// Validate checks the run for consistency. All errors wrap ErrValidation.
func (r ImportRun) Validate() error {
	return validate(
		rule{!r.ID.IsZero(), "import run id must be set"},
		rule{!blank(r.Source), "import source must not be empty"},
		rule{r.Status.Valid(), "invalid import status"},
		rule{!r.StartedAt.IsZero(), "import start must be set"},
		rule{(r.Status == ImportRunning) == r.FinishedAt.IsZero(), "finish time must be set exactly for finished runs"},
		rule{notBefore(r.FinishedAt, r.StartedAt), "import cannot finish before it started"},
	)
}
