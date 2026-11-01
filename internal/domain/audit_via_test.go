package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// operator is the channel of a command the OS user operator ran.
var operator = domain.AuditVia{Channel: domain.ChannelCLI, OSUser: "operator"}

func TestAuditChannel(t *testing.T) {
	t.Parallel()
	checkEnum(t, domain.AuditChannels())
}

// TestAuditVia: an event records the command line as its channel; an
// unknown channel, or an OS user without one, is invalid.
func TestAuditVia(t *testing.T) {
	t.Parallel()

	if operator.IsZero() || !(domain.AuditVia{}).IsZero() {
		t.Errorf("IsZero: %v for the command line, %v for the API", operator.IsZero(), domain.AuditVia{}.IsZero())
	}

	if (domain.AuditDetails{Via: operator}).IsZero() {
		t.Error("details with only a channel record nothing")
	}

	checkValidate(t, func() domain.AuditEvent {
		return domain.AuditEvent{
			ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
			Action: domain.AuditAccountEnabled, TargetKind: domain.AuditTargetAccount,
			TargetID: newID[domain.AccountID](), Details: domain.AuditDetails{Via: operator},
		}
	}, map[string]func(*domain.AuditEvent){
		"unknown channel": func(e *domain.AuditEvent) { e.Details.Via.Channel = "telnet" },
		"no channel":      func(e *domain.AuditEvent) { e.Details.Via.Channel = "" },
	})
}

// TestAuditDetailsJSON: the channel is stored as "via", and left out for
// the API.
func TestAuditDetailsJSON(t *testing.T) {
	t.Parallel()

	const stored = `{"via":{"channel":"cli","osUser":"operator"}}`

	b, err := json.Marshal(domain.AuditDetails{Via: operator})
	if err != nil || string(b) != stored {
		t.Fatalf("Marshal = %s, %v; want %s", b, err, stored)
	}

	var d domain.AuditDetails
	if err := json.Unmarshal([]byte(stored), &d); err != nil || d != (domain.AuditDetails{Via: operator}) {
		t.Errorf("Unmarshal = %+v, %v", d, err)
	}

	b, err = json.Marshal(domain.AuditDetails{Recipient: "ada@example.org"})
	if err != nil || string(b) != `{"recipient":"ada@example.org"}` {
		t.Errorf("Marshal without a channel = %s, %v", b, err)
	}
}
