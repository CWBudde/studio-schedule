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

// TestAuditEventActions: the office's event actions (ADR 0019) name the
// event; the registration actions also the registration and target the
// person, or nothing for a guest; a canceled event has no target; only a
// canceled registration records a refund, and no other action names an
// event or a registration.
func TestAuditEventActions(t *testing.T) {
	t.Parallel()

	registration := func(a domain.AuditAction) func() domain.AuditEvent {
		return func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](), Action: a,
				TargetKind: domain.AuditTargetPerson, TargetPersonID: newID[domain.PersonID](),
				Details: domain.AuditDetails{
					EventID: newID[domain.EventID](), RegistrationID: newID[domain.EventRegistrationID](),
				},
			}
		}
	}

	for _, a := range []domain.AuditAction{
		domain.AuditEventRegistrationAdded, domain.AuditEventRegistrationCanceled, domain.AuditEventRegistrationPaid,
	} {
		t.Run(string(a), func(t *testing.T) {
			t.Parallel()

			cases := map[string]func(*domain.AuditEvent){
				"no event":        func(e *domain.AuditEvent) { e.Details.EventID = domain.EventID{} },
				"no registration": func(e *domain.AuditEvent) { e.Details.RegistrationID = domain.EventRegistrationID{} },
				"an account": func(e *domain.AuditEvent) {
					e.TargetKind, e.TargetPersonID, e.TargetID = domain.AuditTargetAccount, domain.PersonID{},
						newID[domain.AccountID]()
				},
				"no actor": func(e *domain.AuditEvent) { e.ActorID = domain.AccountID{} },
			}
			if a != domain.AuditEventRegistrationCanceled {
				cases["a refund"] = func(e *domain.AuditEvent) { e.Details.Refund = true }
			}

			checkValidate(t, registration(a), cases)

			// A guest's registration has no target.
			guest := registration(a)()
			guest.TargetKind, guest.TargetPersonID = "", domain.PersonID{}

			if err := guest.Validate(); err != nil {
				t.Errorf("a guest's registration: %v", err)
			}
		})
	}

	refunded := registration(domain.AuditEventRegistrationCanceled)()
	refunded.Details.Refund = true

	if err := refunded.Validate(); err != nil {
		t.Errorf("a refunded registration: %v", err)
	}

	t.Run("EventCanceled", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
				Action: domain.AuditEventCanceled, Details: domain.AuditDetails{EventID: newID[domain.EventID]()},
			}
		}, map[string]func(*domain.AuditEvent){
			"no event": func(e *domain.AuditEvent) { e.Details.EventID = domain.EventID{} },
			"a registration": func(e *domain.AuditEvent) {
				e.Details.RegistrationID = newID[domain.EventRegistrationID]()
			},
			"a target": func(e *domain.AuditEvent) {
				e.TargetKind, e.TargetPersonID = domain.AuditTargetPerson, newID[domain.PersonID]()
			},
			"a refund": func(e *domain.AuditEvent) { e.Details.Refund = true },
		})
	})

	t.Run("OtherActions", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
				Action: domain.AuditSessionsRevoked,
			}
		}, map[string]func(*domain.AuditEvent){
			"an event":       func(e *domain.AuditEvent) { e.Details.EventID = newID[domain.EventID]() },
			"a registration": func(e *domain.AuditEvent) { e.Details.RegistrationID = newID[domain.EventRegistrationID]() },
			"a refund":       func(e *domain.AuditEvent) { e.Details.Refund = true },
		})
	})
}
