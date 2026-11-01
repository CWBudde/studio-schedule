package domain_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// TestAuditCommandLineActions: the audit actions of the command line's
// account recovery (ADR 0020).
func TestAuditCommandLineActions(t *testing.T) {
	t.Parallel()

	t.Run("LinkDelivery", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.LinkDeliveries()) })

	cli := domain.AuditVia{Channel: domain.ChannelCLI, OSUser: "operator"}

	t.Run("PasswordResetIssued", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
				Action: domain.AuditPasswordResetIssued, TargetKind: domain.AuditTargetAccount,
				TargetID: newID[domain.AccountID](), Details: domain.AuditDetails{Delivery: domain.LinkMailed},
			}
		}, map[string]func(*domain.AuditEvent){
			"no delivery":  func(e *domain.AuditEvent) { e.Details.Delivery = "" },
			"bad delivery": func(e *domain.AuditEvent) { e.Details.Delivery = "faxed" },
			"no actor":     func(e *domain.AuditEvent) { e.ActorID = domain.AccountID{} },
			"no target":    func(e *domain.AuditEvent) { e.TargetKind, e.TargetID = "", domain.AccountID{} },
			"sessions":     func(e *domain.AuditEvent) { e.Details.Sessions = 1 },
		})
	})

	t.Run("SessionsRevoked", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
				Action: domain.AuditSessionsRevoked, Details: domain.AuditDetails{Sessions: 3, Via: cli},
			}
		}, map[string]func(*domain.AuditEvent){
			"negative": func(e *domain.AuditEvent) { e.Details.Sessions = -1 },
			"delivery": func(e *domain.AuditEvent) { e.Details.Delivery = domain.LinkPrinted },
			"no actor": func(e *domain.AuditEvent) { e.ActorID = domain.AccountID{} },
		})

		// One account's, none ended, or its person's once it is deleted.
		one := domain.AuditEvent{
			ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
			Action: domain.AuditSessionsRevoked, TargetKind: domain.AuditTargetAccount,
			TargetID: newID[domain.AccountID](),
		}
		for _, e := range []domain.AuditEvent{one, one.Retarget(newID[domain.PersonID]())} {
			if err := e.Validate(); err != nil {
				t.Errorf("%+v: %v", e, err)
			}
		}
	})

	t.Run("SetupLinkIssued", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, Action: domain.AuditSetupLinkIssued,
				TargetKind: domain.AuditTargetAccount, TargetID: newID[domain.AccountID](),
				Details: domain.AuditDetails{Delivery: domain.LinkPrinted, Via: cli},
			}
		}, map[string]func(*domain.AuditEvent){
			"an actor":  func(e *domain.AuditEvent) { e.ActorID = newID[domain.AccountID]() },
			"the API":   func(e *domain.AuditEvent) { e.Details.Via = domain.AuditVia{} },
			"mailed":    func(e *domain.AuditEvent) { e.Details.Delivery = domain.LinkMailed },
			"no target": func(e *domain.AuditEvent) { e.TargetKind, e.TargetID = "", domain.AccountID{} },
		})

		// Its account deleted, it targets the account's person.
		e := domain.AuditEvent{
			ID: newID[domain.AuditEventID](), At: t0, Action: domain.AuditSetupLinkIssued,
			TargetKind: domain.AuditTargetAccount, TargetID: newID[domain.AccountID](),
			Details: domain.AuditDetails{Delivery: domain.LinkPrinted, Via: cli},
		}
		if err := e.Retarget(newID[domain.PersonID]()).Validate(); err != nil {
			t.Errorf("a setup link of a deleted account: %v", err)
		}
	})

	t.Run("ActorOptional", func(t *testing.T) {
		t.Parallel()

		for _, a := range domain.AuditActions() {
			want := a == domain.AuditAccountDeleted || a == domain.AuditSetupLinkIssued
			if got := a.ActorOptional(); got != want {
				t.Errorf("%s.ActorOptional() = %v, want %v", a, got, want)
			}
		}
	})

	// Only an issued link records a delivery.
	requeued := domain.AuditEvent{
		ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
		Action:  domain.AuditMailRequeued,
		Details: domain.AuditDetails{MailID: newID[domain.OutboxMailID](), Delivery: domain.LinkMailed},
	}
	if err := requeued.Validate(); err == nil {
		t.Error("a requeued mail with a link's delivery")
	}
}
