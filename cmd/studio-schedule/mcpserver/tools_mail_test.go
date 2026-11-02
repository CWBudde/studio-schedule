package mcpserver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// mailFixture is a fixture whose outbox has mails, of which the demo data
// has none, written by the services that mail: a password-reset link (a
// secret link, its body withheld) to the member, the instructor and the
// office, then a password-changed mail (its body shown) to the member. The
// instructor's is then marked sent, the office's failed; the others stay
// pending.
func mailFixture(t *testing.T) *fixture {
	t.Helper()

	f := newFixture(t)
	mailResets(t, f, demoMember, demoInstructor, demoOffice)

	err := f.svc.Auth.ChangePassword(t.Context(), f.principal(t, demoMember), seed.DemoPassword,
		"a-fresh-zebra-password")
	if err != nil {
		t.Fatal(err)
	}

	mails := mailOutbox(t, f)

	err = f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for _, m := range mails {
			switch m.To {
			case demoInstructor: // sent at the first attempt
				m.Attempts, m.SentAt, m.NextAttemptAt = 1, testNow.Add(time.Minute), time.Time{}
			case demoOffice: // given up
				m.Attempts, m.LastError, m.NextAttemptAt = 5, "550 mailbox unavailable", time.Time{}
			default:
				continue
			}

			if err := tx.Outbox().Update(ctx, m); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return f
}

// mailResets has the services mail a password-reset link to each of
// emails, in order.
func mailResets(t *testing.T, f *fixture, emails ...string) {
	t.Helper()

	for _, email := range emails {
		if err := f.svc.Auth.RequestPasswordReset(t.Context(), email, ""); err != nil {
			t.Fatal(err)
		}
	}
}

// mailOutbox returns every mail of f's outbox with its bodies, newest
// first.
func mailOutbox(t *testing.T, f *fixture) []domain.OutboxMail {
	t.Helper()

	var mails []domain.OutboxMail

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		mails, err = tx.Outbox().List(ctx, store.OutboxFilter{Limit: 1000})

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return mails
}

// mailItems are the items of a mail_log document.
func mailItems(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()

	items, ok := doc["items"].([]any)
	if !ok {
		t.Fatalf("no items in %v", doc)
	}

	out := make([]map[string]any, len(items))
	for i, it := range items {
		out[i], _ = it.(map[string]any)
	}

	return out
}

// mailField is the field key of each item, in order, joined by spaces.
func mailField(items []map[string]any, key string) string {
	values := make([]string, len(items))
	for i, it := range items {
		values[i], _ = it[key].(string)
	}

	return strings.Join(values, " ")
}

// TestMailLogGolden: the first page of the outbox, newest first, with the
// delivery state of each mail and whether its body is shown or withheld;
// one page, so no nextCursor.
func TestMailLogGolden(t *testing.T) {
	t.Parallel()

	f := mailFixture(t)
	cs := f.session(t, demoAdmin, Options{PersonalData: true}, registry)

	text, _ := callTool(t, cs, "mail_log", nil)
	golden(t, "mail_log.json", text)

	items := mailItems(t, document(t, cs, "mail_log", map[string]any{}))
	if got := mailField(items, "template"); got != "password-changed reset-password reset-password reset-password" {
		t.Errorf("templates %s", got)
	}

	if got := mailField(items, "status"); got != "pending failed sent pending" {
		t.Errorf("states %s", got)
	}

	if got := mailField(items, "body"); got != "shown withheld withheld withheld" {
		t.Errorf("bodies %s", got)
	}
}

// TestMailLogNoBodies: no item has the mail's text or HTML, nor a secret
// link's token: body only says whether getMail would show them.
func TestMailLogNoBodies(t *testing.T) {
	t.Parallel()

	f := mailFixture(t)
	cs := f.session(t, demoAdmin, Options{PersonalData: true}, registry)

	text, _ := callTool(t, cs, "mail_log", map[string]any{})

	var looked int

	for _, m := range mailOutbox(t, f) {
		for _, line := range strings.Split(m.Text, "\n") {
			if len(line) < 40 { // too short to be the body's alone
				continue
			}

			looked++

			if strings.Contains(text, line) {
				t.Errorf("the log has the text of %v: %q", m.ID, line)
			}
		}
	}

	if looked < 4 {
		t.Fatalf("only %d lines of the mails' texts to look for", looked)
	}

	if strings.Contains(text, "token=") {
		t.Errorf("the log has a secret link: %s", text)
	}

	for _, it := range mailItems(t, document(t, cs, "mail_log", map[string]any{})) {
		if _, has := it["text"]; has {
			t.Errorf("%v has a text", it["id"])
		}

		if _, has := it["html"]; has {
			t.Errorf("%v has an HTML body", it["id"])
		}
	}
}

// TestMailLogFilters: status and template filter, alone and together;
// an unknown template is a field error that lists the templates, as `mail
// list` refuses it, an unknown status the service's field error, a cursor
// that no page returned a field error; none of them lists anything.
func TestMailLogFilters(t *testing.T) {
	t.Parallel()

	f := mailFixture(t)
	cs := f.session(t, demoAdmin, Options{PersonalData: true}, registry)

	for _, tc := range []struct {
		args map[string]any
		to   string
	}{
		{map[string]any{"status": "pending"}, demoMember + " " + demoMember},
		{map[string]any{"status": "sent"}, demoInstructor},
		{map[string]any{"status": "failed"}, demoOffice},
		{map[string]any{"template": "reset-password"}, demoOffice + " " + demoInstructor + " " + demoMember},
		{map[string]any{"template": "password-changed"}, demoMember},
		{map[string]any{"status": "pending", "template": "reset-password"}, demoMember},
		{map[string]any{"status": "sent", "template": "password-changed"}, ""},
		{map[string]any{"template": "receipt"}, ""},
	} {
		doc := document(t, cs, "mail_log", tc.args)
		if got := mailField(mailItems(t, doc), "to"); got != tc.to || doc["nextCursor"] != nil {
			t.Errorf("%v: %q, next %v; want %q", tc.args, got, doc["nextCursor"], tc.to)
		}
	}

	for _, tc := range []struct {
		args map[string]any
		text string
	}{
		{
			map[string]any{"template": "Zebulon"},
			"validation failed: template is not one of " + strings.Join(mail.TemplateNames(), ", "),
		},
		{map[string]any{"template": mail.TemplateTest}, "validation failed: template is not one of " +
			strings.Join(mail.TemplateNames(), ", ")},
		{map[string]any{"status": "bounced"}, "validation failed: invalid mail status"},
		{map[string]any{"cursor": "Zebulon!"}, "validation failed: cursor is no cursor of a page"},
		{map[string]any{"cursor": "MTIzNDU"}, "validation failed: cursor is no cursor of a page"}, // "12345", no ID
		{
			map[string]any{"recipient": "clara"},
			`validation failed: the arguments do not fit the tool's input schema: json: unknown field "recipient"`,
		},
	} {
		wantError(t, cs, "mail_log", tc.args, tc.text)
	}

	calls := toolCalls(t, f.st)
	if last := calls[len(calls)-1]; last.Details.Tool != "mail_log" || last.Details.Outcome != "invalid" {
		t.Errorf("the last call was audited as %+v", last.Details)
	}
}

// TestMailLogPages: with more mails than a page holds the page ends at
// service.MailLogPageSize with a nextCursor, and a call with it returns
// the rest, the next mails without overlap and without a nextCursor; the
// cursor keeps to the filter it is given with.
func TestMailLogPages(t *testing.T) {
	t.Parallel()

	const extra = 3

	f := newFixture(t)
	resets := make([]string, service.MailLogPageSize+extra)

	for i := range resets {
		resets[i] = []string{demoMember, demoInstructor}[i%2]
	}

	mailResets(t, f, resets...)

	cs := f.session(t, demoAdmin, Options{PersonalData: true}, registry)

	first := document(t, cs, "mail_log", map[string]any{})
	cursor, _ := first["nextCursor"].(string)

	if n := len(mailItems(t, first)); n != service.MailLogPageSize || cursor == "" {
		t.Fatalf("the first page has %d mails, nextCursor %q", n, cursor)
	}

	second := document(t, cs, "mail_log", map[string]any{"cursor": cursor})
	if n := len(mailItems(t, second)); n != extra || second["nextCursor"] != nil {
		t.Errorf("the second page has %d mails, nextCursor %v", n, second["nextCursor"])
	}

	ids := strings.Fields(mailField(mailItems(t, first), "id") + " " + mailField(mailItems(t, second), "id"))

	var want []string
	for _, m := range mailOutbox(t, f) {
		want = append(want, m.ID.String())
	}

	if !slices.Equal(ids, want) {
		t.Errorf("the pages list %v, the outbox has %v", ids, want)
	}

	// The cursor with a filter: the pending mails after it, here all of
	// them.
	filtered := document(t, cs, "mail_log", map[string]any{"cursor": cursor, "status": "pending"})
	if got := mailField(mailItems(t, filtered), "id"); got != strings.Join(want[service.MailLogPageSize:], " ") {
		t.Errorf("after the cursor, pending: %s", got)
	}
}

// TestMailLogStoreFails: a store that fails is an unexpected error, in the
// log and not in the result.
func TestMailLogStoreFails(t *testing.T) {
	t.Parallel()

	st := &faultStore{Store: newFixture(t).st}
	f := fixtureOn(st)
	admin := f.principal(t, demoAdmin)
	cs := f.sessionOf(t, admin, func(context.Context) (service.Principal, error) { return admin, nil },
		Options{PersonalData: true}, registry)

	st.failing(store.ReadOnly, errors.New("connection reset"))

	wantError(t, cs, "mail_log", nil, "internal error")

	if log := f.log.String(); !strings.Contains(log, "connection reset") || !strings.Contains(log, "tool=mail_log") {
		t.Errorf("log %q", log)
	}
}
