package app_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// TestAuditViaOverHTTP: an event written for a principal of the command
// line (ADR 0020) lists its channel under details.via; one written for a
// request has none.
func TestAuditViaOverHTTP(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	admin := a.login("arno.keystone@example.net")

	renderer, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	// The command line's service on the app's store.
	cli := service.NewAuthService(a.deps.Store, a.clock, ids.V7{}, nil, renderer, a.deps.AppURL)

	actor, err := cli.Actor(t.Context(), "arno.keystone@example.net",
		domain.AuditVia{Channel: domain.ChannelCLI, OSUser: "operator"})
	if err != nil {
		t.Fatal(err)
	}

	var oskar string

	for _, item := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/accounts", "", admin))["items"].([]any) {
		if acc, _ := item.(map[string]any); acc["email"] == "oskar.nettlebury@example.org" {
			oskar, _ = acc["id"].(string)
		}
	}

	id, err := domain.ParseID[domain.AccountID](oskar)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := cli.SetAccountDisabled(t.Context(), actor, id, true); err != nil {
		t.Fatal(err)
	}

	a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/accounts/"+oskar+"/enable", "", admin)

	items, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/audit", "", admin))["items"].([]any)
	if len(items) < 2 {
		t.Fatalf("audit %v", items)
	}

	enabled, _ := items[0].(map[string]any)
	if enabled["action"] != "account-enabled" || enabled["details"] != nil {
		t.Errorf("the request's event %v, want no details", enabled)
	}

	disabled, _ := items[1].(map[string]any)
	details, _ := disabled["details"].(map[string]any)

	if want := map[string]any{"via": map[string]any{"channel": "cli", "osUser": "operator"}}; disabled["action"] != "account-disabled" ||
		!reflect.DeepEqual(details, want) {
		t.Errorf("the command's event %v, want details %v", disabled, want)
	}
}
