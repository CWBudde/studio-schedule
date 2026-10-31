package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// outboxMailer records the mails it sends; send, if set, answers instead
// of succeeding.
type outboxMailer struct {
	mu   sync.Mutex
	sent []mail.Message
	send func(ctx context.Context, m mail.Message) error
}

func (o *outboxMailer) Send(ctx context.Context, m mail.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.send != nil {
		if err := o.send(ctx, m); err != nil {
			return err
		}
	}

	o.sent = append(o.sent, m)

	return nil
}

func (o *outboxMailer) sentTo() map[string]bool {
	o.mu.Lock()
	defer o.mu.Unlock()

	out := map[string]bool{}
	for _, m := range o.sent {
		out[m.To] = true
	}

	return out
}

// outboxMail returns the stored mail.
func (f *mailLogFixture) outboxMail(t *testing.T, id domain.OutboxMailID) domain.OutboxMail {
	t.Helper()

	var m domain.OutboxMail

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		m, err = tx.Outbox().Get(ctx, id)

		return err
	})

	return m
}

// TestOutboxDeliverStoreFaults: delivery claims, sends and records each
// mail in transactions of their own, so a store failure can strike between
// them. Whichever call fails, Deliver returns the failure and reports only
// the mails recorded as sent; a mail the server took but that could not be
// recorded stays claimed, so it is retried once its lease is over (sent at
// least once, never lost, and never given up for it); a mail not sent
// stays as it was.
func TestOutboxDeliverStoreFaults(t *testing.T) {
	t.Parallel()

	now := mustInstant(t, bookingNow)

	var (
		f      *mailLogFixture
		mailer *outboxMailer
		queued []domain.OutboxMail
		sent   int
	)

	SweepFaults(t, FaultSweep{
		Setup: func(t *testing.T) (*FaultStore, func() error) {
			f = &mailLogFixture{newBookingFixture(t)}
			queued = []domain.OutboxMail{
				f.addMail(t, "ada@example.org", mail.TemplateReceipt, now.Add(-2*time.Minute), domain.MailPending),
				f.addMail(t, "bea@example.org", mail.TemplateReceipt, now.Add(-time.Minute), domain.MailPending),
			}
			mailer = &outboxMailer{}
			fs := NewFaultStore(f.st)
			o := NewOutboxService(fs, clock.NewFixed(now), mailer, nil)

			return fs, func() error {
				var err error
				sent, err = o.Deliver(t.Context())

				return err
			}
		},
		Check: func(t *testing.T, _ *FaultStore, _ StoreData, call FaultCall) {
			recorded := 0
			took := mailer.sentTo()

			for _, q := range queued {
				m := f.outboxMail(t, q.ID)

				switch {
				case m.Status() == domain.MailSent:
					recorded++

					if !took[m.To] {
						t.Errorf("with %s failing, %s is recorded sent but was not", call, m.To)
					}
				case took[m.To]:
					if m.Attempts != 1 || !m.NextAttemptAt.Equal(now.Add(mailRetryDelay(1))) || m.LastError != "" {
						t.Errorf("with %s failing, the unrecorded %s is %+v, want it claimed until its retry", call, m.To, m)
					}
				case m != q:
					t.Errorf("with %s failing, the unsent %s changed: %+v", call, m.To, m)
				}
			}

			if sent != recorded {
				t.Errorf("with %s failing, Deliver reported %d sent, %d are recorded", call, sent, recorded)
			}
		},
	})
}

// TestOutboxDeliverBatch: a run sends at most a batch; the rest wait for
// the next run.
func TestOutboxDeliverBatch(t *testing.T) {
	t.Parallel()

	f := &mailLogFixture{newBookingFixture(t)}
	now := mustInstant(t, bookingNow)

	for i := range outboxBatch + 1 {
		f.addMail(t, fmt.Sprintf("m%02d@example.org", i), mail.TemplateReceipt, now.Add(-time.Hour+time.Duration(i)*time.Second),
			domain.MailPending)
	}

	mailer := &outboxMailer{}
	o := NewOutboxService(f.st, clock.NewFixed(now), mailer, nil)

	for _, want := range []int{outboxBatch, 1, 0} {
		if n, err := o.Deliver(t.Context()); err != nil || n != want {
			t.Fatalf("Deliver = %d, %v; want %d", n, err, want)
		}
	}

	if len(mailer.sent) != outboxBatch+1 {
		t.Errorf("sent %d mails, want %d", len(mailer.sent), outboxBatch+1)
	}
}

// TestOutboxDeliverShutdown: a send that fails because the job is shutting
// down is not recorded as a failed attempt: the mail stays claimed and is
// retried after its lease, the run stops, and the next mail is left alone.
func TestOutboxDeliverShutdown(t *testing.T) {
	t.Parallel()

	f := &mailLogFixture{newBookingFixture(t)}
	now := mustInstant(t, bookingNow)
	first := f.addMail(t, "ada@example.org", mail.TemplateReceipt, now.Add(-2*time.Minute), domain.MailPending)
	second := f.addMail(t, "bea@example.org", mail.TemplateReceipt, now.Add(-time.Minute), domain.MailPending)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mailer := &outboxMailer{send: func(context.Context, mail.Message) error {
		cancel()

		return errors.New("connection closed")
	}}

	n, err := NewOutboxService(f.st, clock.NewFixed(now), mailer, nil).Deliver(ctx)
	if !errors.Is(err, context.Canceled) || n != 0 {
		t.Fatalf("Deliver = %d, %v; want the cancellation", n, err)
	}

	if m := f.outboxMail(t, first.ID); m.Attempts != 1 || m.LastError != "" || m.Status() != domain.MailPending ||
		!m.NextAttemptAt.Equal(now.Add(mailRetryDelay(1))) {
		t.Errorf("first mail %+v, want it claimed, without an error", m)
	}

	if m := f.outboxMail(t, second.ID); m != second {
		t.Errorf("second mail %+v, want it untouched", m)
	}
}

// TestOutboxLongError: a long delivery error is stored cut to its limit,
// on a rune boundary.
func TestOutboxLongError(t *testing.T) {
	t.Parallel()

	f := &mailLogFixture{newBookingFixture(t)}
	now := mustInstant(t, bookingNow)
	q := f.addMail(t, "ada@example.org", mail.TemplateReceipt, now, domain.MailPending)

	// "ü" is two bytes; the limit falls into the middle of one.
	long := "451 " + strings.Repeat("x", maxMailErrorLen-5) + strings.Repeat("ü", 50)
	mailer := &outboxMailer{send: func(context.Context, mail.Message) error { return errors.New(long) }}

	if _, err := NewOutboxService(f.st, clock.NewFixed(now), mailer, nil).Deliver(t.Context()); err != nil {
		t.Fatal(err)
	}

	got := f.outboxMail(t, q.ID).LastError
	if len(got) != maxMailErrorLen-1 || !utf8.ValidString(got) || !strings.HasPrefix(long, got) {
		t.Errorf("stored error of %d bytes (valid UTF-8 %v), want the first %d bytes of it", len(got),
			utf8.ValidString(got), maxMailErrorLen-1)
	}

	if truncate("short", maxMailErrorLen) != "short" {
		t.Error("a short error was changed")
	}
}

// receiptMail sells Anna a pass, which queues the receipt mail, and
// returns that mail.
func receiptMail(t *testing.T, f *passFixture) domain.OutboxMail {
	t.Helper()

	pass := f.sell(t, f.anna, f.tenVisits, "")

	for _, m := range f.mailsOf(t, mail.TemplateReceipt) {
		if m.ReceiptID == pass.ReceiptID {
			return m
		}
	}

	t.Fatal("no receipt mail")

	return domain.OutboxMail{}
}

// TestOutboxAttachFails: a receipt mail whose PDF cannot be read, for want
// of the receipts or through a store failure, is not sent without it: the
// attempt fails like a delivery, is recorded and retried, and Deliver
// itself succeeds.
func TestOutboxAttachFails(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		receipts func(f *passFixture, fs *FaultStore) *ReceiptService
		want     string
	}{
		"no receipts": {
			receipts: func(*passFixture, *FaultStore) *ReceiptService { return nil },
			want:     "no receipts to attach",
		},
		"store failure": {
			receipts: func(f *passFixture, fs *FaultStore) *ReceiptService {
				fs.Fail("Receipts.Get", 1)

				return NewReceiptService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, Mails{}, f.pdf)
			},
			want: "attach receipt: ",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newPassFixture(t, domain.RedeemOnAttendance)
			q := receiptMail(t, f)
			now := mustInstant(t, bookingNow)
			mailer := &outboxMailer{}
			fs := NewFaultStore(f.st)
			o := NewOutboxService(fs, clock.NewFixed(now), mailer, tc.receipts(f, fs))

			if _, err := o.Deliver(t.Context()); err != nil {
				t.Fatalf("Deliver: %v", err)
			}

			for _, m := range mailer.sent {
				if m.Subject == q.Subject {
					t.Errorf("sent %q without its PDF", m.Subject)
				}
			}

			var m domain.OutboxMail

			f.write(t, func(ctx context.Context, tx store.Tx) error {
				var err error
				m, err = tx.Outbox().Get(ctx, q.ID)

				return err
			})

			if m.Status() != domain.MailPending || m.Attempts != 1 || !strings.HasPrefix(m.LastError, tc.want) ||
				!m.NextAttemptAt.Equal(now.Add(mailRetryDelay(1))) {
				t.Errorf("receipt mail %+v, want a failed attempt %q retried later", m, tc.want)
			}
		})
	}
}

// TestOutboxAttachment: only a mail with a receipt has an attachment, and
// only while the receipts are at hand.
func TestOutboxAttachment(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	withReceipt := receiptMail(t, f)
	clk := clock.NewFixed(mustInstant(t, bookingNow))
	plain := (&mailLogFixture{f.bookingFixture}).addMail(t, "ada@example.org", mail.TemplateInvite, clk.Now(),
		domain.MailPending)

	o := NewOutboxService(f.st, clk, nil, f.receiptService(t, bookingNow))
	bare := NewOutboxService(f.st, clk, nil, nil)

	if file, err := o.Attachment(t.Context(), withReceipt.ID); err != nil || len(file.PDF) == 0 {
		t.Errorf("receipt mail: %v", err)
	}

	for name, tc := range map[string]struct {
		svc *OutboxService
		id  domain.OutboxMailID
	}{
		"without a receipt":  {o, plain.ID},
		"without receipts":   {bare, withReceipt.ID},
		"of an unknown mail": {o, domain.OutboxMailID{9}},
	} {
		if _, err := tc.svc.Attachment(t.Context(), tc.id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v, want not found", name, err)
		}
	}
}

// TestOutboxListStoreFaults: the mailbox, which names the receipts of its
// mails, returns a store failure rather than a list without them.
func TestOutboxListStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnAttendance)
		receiptMail(t, f)

		fs := NewFaultStore(f.st)
		o := NewOutboxService(fs, clock.NewFixed(mustInstant(t, bookingNow)), nil, nil)

		return fs, func() error {
			entries, err := o.List(t.Context(), 10)
			if err == nil && entries[0].Attachment != "SD-2026-0001.pdf" {
				t.Fatalf("entries %+v, want the receipt's name first", entries)
			}

			return err
		}
	}})
}

// failingRenderer renders no mail.
type failingRenderer struct {
	*mail.Renderer

	err error
}

func (r failingRenderer) Render(string, string, any) (mail.Rendered, error) {
	return mail.Rendered{}, r.err
}

// TestSendMailsRenderFailure: a mail that cannot be rendered is broken
// templates or data, not the caller's input: even a validation error of
// the renderer is reported as an internal error, and nothing is queued.
func TestSendMailsRenderFailure(t *testing.T) {
	t.Parallel()

	f := newAnnouncementFixture(t)
	now := mustInstant(t, bookingNow)
	f.create(t, f.serviceAt(t, now), AnnouncementInput{
		AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Hello all", Body: "Hi.", Email: true,
	})

	r := failingRenderer{Renderer: newTestRenderer(t), err: fmt.Errorf("%w: missing field", domain.ErrValidation)}
	svc := NewAnnouncementService(f.st, clock.NewFixed(now), f.seq, r, nil)

	_, err := svc.SendDue(t.Context())
	if err == nil || errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "missing field") {
		t.Fatalf("SendDue = %v, want an internal error naming the cause", err)
	}

	if mails := f.mailsOf(t, mail.TemplateAnnouncement); len(mails) != 0 {
		t.Errorf("queued %d mails", len(mails))
	}
}
