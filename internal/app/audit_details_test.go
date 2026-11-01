package app_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestAuditLinkAndSessionDetailsOverHTTP: the events of the account
// commands (ADR 0020) list their delivery and session count under details;
// a setup link has no actor, a revocation of every account's sessions no
// target.
func TestAuditLinkAndSessionDetailsOverHTTP(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	admin := a.login("arno.keystone@example.net")

	accountIDs := map[string]domain.AccountID{}

	for _, item := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/accounts", "", admin))["items"].([]any) {
		acc, _ := item.(map[string]any)
		email, _ := acc["email"].(string)
		raw, _ := acc["id"].(string)

		id, err := domain.ParseID[domain.AccountID](raw)
		if err != nil {
			t.Fatal(err)
		}

		accountIDs[email] = id
	}

	adminID, oskar := accountIDs["arno.keystone@example.net"], accountIDs["oskar.nettlebury@example.org"]
	cli := domain.AuditVia{Channel: domain.ChannelCLI, OSUser: "operator"}
	src := ids.V7{}
	at := a.clock.Now()

	events := []domain.AuditEvent{
		{
			ActorID: adminID, Action: domain.AuditPasswordResetIssued, TargetKind: domain.AuditTargetAccount,
			TargetID: oskar, Details: domain.AuditDetails{Delivery: domain.LinkMailed, Via: cli},
		},
		{
			ActorID: adminID, Action: domain.AuditSessionsRevoked, TargetKind: domain.AuditTargetAccount,
			TargetID: oskar, Details: domain.AuditDetails{Sessions: 2, Via: cli},
		},
		{ActorID: adminID, Action: domain.AuditSessionsRevoked, Details: domain.AuditDetails{Via: cli}},
		{
			Action: domain.AuditSetupLinkIssued, TargetKind: domain.AuditTargetAccount, TargetID: oskar,
			Details: domain.AuditDetails{Delivery: domain.LinkPrinted, Via: cli},
		},
	}

	err := a.deps.Store.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for i, e := range events {
			e.ID, e.At = ids.New[domain.AuditEventID](src), at.Add(time.Duration(i)*time.Second)
			if err := tx.Audit().Create(ctx, e); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	items, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/audit", "", admin))["items"].([]any)
	if len(items) < len(events) {
		t.Fatalf("audit %v", items)
	}

	via := map[string]any{"channel": "cli", "osUser": "operator"}
	want := []struct {
		action        string
		actor, target bool
		details       map[string]any
	}{
		{"setup-link-issued", false, true, map[string]any{"delivery": "printed", "via": via}},
		{"sessions-revoked", true, false, map[string]any{"via": via}},
		{"sessions-revoked", true, true, map[string]any{"sessions": float64(2), "via": via}},
		{"password-reset-issued", true, true, map[string]any{"delivery": "mailed", "via": via}},
	}

	for i, w := range want {
		got, _ := items[i].(map[string]any)
		_, hasActor := got["actor"]
		_, hasTarget := got["target"]

		if got["action"] != w.action || hasActor != w.actor || hasTarget != w.target ||
			!reflect.DeepEqual(got["details"], w.details) {
			t.Errorf("event %d = %v, want %s (actor %t, target %t) with details %v",
				i, got, w.action, w.actor, w.target, w.details)
		}
	}
}
