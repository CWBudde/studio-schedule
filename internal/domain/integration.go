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
