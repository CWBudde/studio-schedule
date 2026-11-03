package mcpserver

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// wmOptions are the flags of the requeue_mail tests: the mail group, and
// the person data that the mail log names.
var wmOptions = Options{PersonalData: true, AllowWrites: []WriteGroup{WriteMail}}

// wmUnknownMail is an ID no mail has.
const wmUnknownMail = "00000000-0000-7000-8000-000000000999"

// wmMails are the mails of wmFixture by their state.
type wmMails struct {
	failed  domain.OutboxMailID // password-changed to the member, given up
	pending domain.OutboxMailID // password-changed to the office, not tried yet
	sent    domain.OutboxMailID // password-changed to the instructor, sent
	secret  domain.OutboxMailID // reset-password to the admin, given up
}

// wmFixture is a fixture whose outbox has a mail in each state the tool
// tells apart, written by the services: a password-changed mail (no
// secret link, mail.SecretLink) to the member, the office and the
// instructor, and a password-reset link to the admin. The member's and the
// admin's are then marked failed, the instructor's sent.
func wmFixture(t *testing.T) (*fixture, wmMails) {
	t.Helper()

	f := newFixture(t)

	for _, email := range []string{demoMember, demoOffice, demoInstructor} {
		err := f.svc.Auth.ChangePassword(t.Context(), f.principal(t, email), seed.DemoPassword, "a-fresh-zebra-password")
		if err != nil {
			t.Fatal(err)
		}
	}

	mailResets(t, f, demoAdmin)

	var m wmMails

	err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		mails, err := tx.Outbox().List(ctx, store.OutboxFilter{Limit: 1000})
		if err != nil {
			return err
		}

		for _, o := range mails {
			switch {
			case o.To == demoAdmin && o.Template == mail.TemplateResetPassword:
				m.secret = o.ID
				o.Attempts, o.LastError, o.NextAttemptAt = 5, "550 mailbox unavailable", time.Time{}
			case o.Template != mail.TemplatePasswordChanged:
				continue
			case o.To == demoMember:
				m.failed = o.ID
				o.Attempts, o.LastError, o.NextAttemptAt = 5, "550 mailbox unavailable", time.Time{}
			case o.To == demoInstructor:
				m.sent = o.ID
				o.Attempts, o.SentAt, o.NextAttemptAt = 1, testNow.Add(-time.Minute), time.Time{}
			case o.To == demoOffice:
				m.pending = o.ID

				continue
			default:
				continue
			}

			if err := tx.Outbox().Update(ctx, o); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if m.failed.IsZero() || m.pending.IsZero() || m.sent.IsZero() || m.secret.IsZero() {
		t.Fatalf("the outbox lacks a mail: %+v", m)
	}

	return f, m
}

// wmMail returns the mail id of f's outbox.
func wmMail(t *testing.T, f *fixture, id domain.OutboxMailID) domain.OutboxMail {
	t.Helper()

	var m domain.OutboxMail

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		m, err = tx.Outbox().Get(ctx, id)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// wmAudit returns every audit event of st, oldest first.
func wmAudit(t *testing.T, st store.Store) []domain.AuditEvent {
	t.Helper()

	var events []domain.AuditEvent

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		events, err = tx.Audit().List(ctx, 1000)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	slices.Reverse(events)

	return events
}

// wmActions are the audit events of action among events.
func wmActions(events []domain.AuditEvent, action domain.AuditAction) []domain.AuditEvent {
	return slices.DeleteFunc(slices.Clone(events), func(e domain.AuditEvent) bool { return e.Action != action })
}

// TestRequeueMail: a failed mail without a secret link is queued again,
// its next attempt now and its attempts starting over, and returned as the
// mail log lists it, without its body. The admin's requeue is audited as
// mail-requeued through the MCP channel, beside the call's own row.
func TestRequeueMail(t *testing.T) {
	t.Parallel()

	f, m := wmFixture(t)
	cs := f.session(t, demoAdmin, wmOptions, registry)

	text, isError := callTool(t, cs, "requeue_mail", map[string]any{"mailId": m.failed.String()})
	if isError {
		t.Fatalf("requeue_mail: error result %q", text)
	}

	golden(t, "requeue_mail.json", text)

	for _, key := range []string{`"text"`, `"html"`, "a-fresh-zebra-password"} {
		if strings.Contains(text, key) {
			t.Errorf("the result has %s: %s", key, text)
		}
	}

	got := wmMail(t, f, m.failed)
	if got.Status() != domain.MailPending || got.Attempts != 0 || !got.NextAttemptAt.Equal(testNow) {
		t.Errorf("the mail is %s with %d attempts, next at %v; want pending, 0, %v",
			got.Status(), got.Attempts, got.NextAttemptAt, testNow)
	}

	admin := f.principal(t, demoAdmin)

	requeued := wmActions(wmAudit(t, f.st), domain.AuditMailRequeued)
	if len(requeued) != 1 {
		t.Fatalf("%d mail-requeued events, want 1", len(requeued))
	}

	if e := requeued[0]; e.ActorID != admin.AccountID || e.Details.MailID != m.failed ||
		e.Details.Recipient != demoMember || e.Details.Template != mail.TemplatePasswordChanged ||
		e.Details.Via.Channel != domain.ChannelMCP {
		t.Errorf("mail-requeued event %+v", e)
	}

	calls := toolCalls(t, f.st)
	if len(calls) != 1 || calls[0].Details.Tool != "requeue_mail" || calls[0].Details.Outcome != "ok" ||
		calls[0].Details.Via.Channel != domain.ChannelMCP {
		t.Errorf("tool calls %+v", calls)
	}

	// Queued again, it is pending: a second call is a conflict.
	wantError(t, cs, "requeue_mail", map[string]any{"mailId": m.failed.String()},
		"requeue mail "+m.failed.String()+": conflict: only a failed mail can be queued again")
}

// TestRequeueMailRefused: a pending or sent mail is a conflict, a failed
// mail with a secret link too, and it stays failed; an unknown mail is
// not found. None is audited as requeued, each call as its outcome.
func TestRequeueMailRefused(t *testing.T) {
	t.Parallel()

	f, m := wmFixture(t)
	cs := f.session(t, demoAdmin, wmOptions, registry)

	for _, tc := range []struct {
		id   string
		text string
	}{
		{m.pending.String(), "requeue mail " + m.pending.String() + ": conflict: only a failed mail can be queued again"},
		{m.sent.String(), "requeue mail " + m.sent.String() + ": conflict: only a failed mail can be queued again"},
		{m.secret.String(), "requeue mail " + m.secret.String() + ": conflict: a mail with a secret link is not queued again"},
		{wmUnknownMail, "requeue mail " + wmUnknownMail + ": outbox mail: not found"},
	} {
		wantError(t, cs, "requeue_mail", map[string]any{"mailId": tc.id}, tc.text)
	}

	if got := wmMail(t, f, m.secret); got.Status() != domain.MailFailed || got.Attempts != 5 {
		t.Errorf("the secret-link mail is %s with %d attempts, want failed with 5", got.Status(), got.Attempts)
	}

	if got := wmMail(t, f, m.sent); got.Status() != domain.MailSent {
		t.Errorf("the sent mail is %s", got.Status())
	}

	if got := wmMail(t, f, m.pending); got.Status() != domain.MailPending || !got.NextAttemptAt.Equal(testNow) {
		t.Errorf("the pending mail is %s, next at %v", got.Status(), got.NextAttemptAt)
	}

	if requeued := wmActions(wmAudit(t, f.st), domain.AuditMailRequeued); len(requeued) != 0 {
		t.Errorf("refused requeues were audited: %+v", requeued)
	}

	calls := toolCalls(t, f.st)

	var outcomes []string
	for _, c := range calls {
		outcomes = append(outcomes, c.Details.Tool+" "+c.Details.Outcome)
	}

	want := []string{"requeue_mail conflict", "requeue_mail conflict", "requeue_mail conflict", "requeue_mail not-found"}
	if !slices.Equal(outcomes, want) {
		t.Errorf("the calls were audited as %v, want %v", outcomes, want)
	}
}

// TestRequeueMailInput: mailId is required and must be an ID; the errors
// name the field, never the value, which reaches neither the audit log
// nor the server's log (Review focus 2).
func TestRequeueMailInput(t *testing.T) {
	t.Parallel()

	f, m := wmFixture(t)
	cs := f.session(t, demoAdmin, wmOptions, registry)

	for _, tc := range []struct {
		args any
		text string
	}{
		{nil, "validation failed: mailId is required"},
		{map[string]any{}, "validation failed: mailId is required"},
		{map[string]any{"mailId": ""}, "validation failed: mailId is required"},
		{map[string]any{"mailId": "Zebulon"}, "validation failed: mailId is not an ID"},
		{
			map[string]any{"mailId": m.failed.String(), "note": "Zebulon"},
			`validation failed: the arguments do not fit the tool's input schema: json: unknown field "note"`,
		},
	} {
		wantError(t, cs, "requeue_mail", tc.args, tc.text)
	}

	if got := wmMail(t, f, m.failed); got.Status() != domain.MailFailed {
		t.Errorf("an invalid call queued the mail: %s", got.Status())
	}

	events := wmAudit(t, f.st)
	if dump := fmt.Sprintf("%+v", events); strings.Contains(dump, "Zebulon") {
		t.Errorf("the audit log has the argument: %s", dump)
	}

	if log := f.log.String(); strings.Contains(log, "Zebulon") {
		t.Errorf("the log has the argument: %s", log)
	}

	calls := toolCalls(t, f.st)
	if len(calls) != 5 {
		t.Fatalf("%d calls audited, want 5", len(calls))
	}

	for _, c := range calls {
		if c.Details.Tool != "requeue_mail" || c.Details.Outcome != "invalid" {
			t.Errorf("an invalid call was audited as %+v", c.Details)
		}
	}
}
