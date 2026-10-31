package httpapi

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// TestSettingsMissing: the detail of a receipt-settings-incomplete conflict
// starts at the service's sentence, dropping what wraps it (the domain
// error's "conflict: " prefix), and is the bare sentence for an error that
// matches the sentinel without carrying its text.
func TestSettingsMissing(t *testing.T) {
	t.Parallel()

	missing := fmt.Errorf("%w: the school address is missing", service.ErrReceiptSettingsIncomplete)
	if got, want := settingsMissing(missing), "the receipt settings are incomplete: the school address is missing"; got != want {
		t.Errorf("settingsMissing(%q) = %q, want %q", missing, got, want)
	}

	// An error whose Is matches the sentinel, but whose text differs.
	other := silentIncomplete{}
	if !errors.Is(other, service.ErrReceiptSettingsIncomplete) {
		t.Fatal("silentIncomplete does not match the sentinel")
	}

	if got, want := settingsMissing(other), "the receipt settings are incomplete"; got != want {
		t.Errorf("settingsMissing(%q) = %q, want %q", other, got, want)
	}
}

// silentIncomplete is ErrReceiptSettingsIncomplete without its text.
type silentIncomplete struct{}

func (silentIncomplete) Error() string { return "receipts are not set up" }

func (silentIncomplete) Is(target error) bool { return target == service.ErrReceiptSettingsIncomplete }

// TestWaitlistEntryBody: an entry carries its invite's expiry only while
// invited; a waiting entry has its position and no expiry, and one whose
// invite ended keeps neither, though the stored entry still has the old
// window.
func TestWaitlistEntryBody(t *testing.T) {
	t.Parallel()

	joined := time.Date(2026, time.October, 20, 9, 30, 0, 0, time.FixedZone("CEST", 2*60*60))
	expires := joined.Add(12 * time.Hour)
	entry := domain.WaitlistEntry{
		ID: domain.WaitlistEntryID{1}, LessonID: domain.LessonID{2}, PersonID: domain.PersonID{3},
		JoinedAt: joined, StayOnList: true, ExpiresAt: expires,
	}

	cases := []struct {
		status    domain.WaitlistStatus
		position  int
		expiresAt time.Time
	}{
		{domain.WaitlistWaiting, 2, time.Time{}},
		{domain.WaitlistInvited, 0, expires.UTC()},
		{domain.WaitlistExpired, 0, time.Time{}},
		{domain.WaitlistAccepted, 0, time.Time{}},
	}

	for _, c := range cases {
		e := entry
		e.Status = c.status

		got := waitlistEntryBody(service.WaitlistPlace{Entry: e, Position: c.position})
		want := WaitlistEntry{
			ID: e.ID, LessonID: e.LessonID, PersonID: e.PersonID, Status: string(c.status), StayOnList: true,
			JoinedAt: joined.UTC(), Position: c.position, ExpiresAt: c.expiresAt,
		}

		if got != want {
			t.Errorf("%s: body = %+v, want %+v", c.status, got, want)
		}

		if got.JoinedAt.Location() != time.UTC || got.ExpiresAt.Location() != time.UTC {
			t.Errorf("%s: times not in UTC: %v, %v", c.status, got.JoinedAt, got.ExpiresAt)
		}
	}
}
