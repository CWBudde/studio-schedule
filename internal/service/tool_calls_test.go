package service_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// TestRecordToolCall: a call of the MCP server's tool is one audit event
// mcp-tool-called of the principal, without a target, naming the tool, the
// outcome and the principal's channel (ADR 0022).
func TestRecordToolCall(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	ctx := t.Context()
	office := f.signedUp("office@example.org", domain.RoleOffice)

	p := office.Principal
	p.Via = domain.AuditVia{Channel: domain.ChannelMCP, OSUser: "operator"}

	if err := f.svc.RecordToolCall(ctx, p, "schedule", "ok"); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.AuditLog(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("AuditLog = %+v, %v", got, err)
	}

	e := got[0].Event
	want := domain.AuditDetails{Tool: "schedule", Outcome: "ok", Via: p.Via}

	if e.Action != domain.AuditMCPToolCalled || e.ActorID != p.AccountID || e.TargetKind != "" || e.Details != want {
		t.Errorf("event = %+v", e)
	}

	// A principal of another channel is refused: the event is the MCP
	// server's.
	cli := office.Principal
	cli.Via = domain.AuditVia{Channel: domain.ChannelCLI, OSUser: "operator"}

	for _, p := range []service.Principal{cli, office.Principal} {
		if err := f.svc.RecordToolCall(ctx, p, "schedule", "ok"); err == nil {
			t.Errorf("RecordToolCall via %+v: no error", p.Via)
		}
	}
}
