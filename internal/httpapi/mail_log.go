package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerMailLog registers the mail log (ADR 0014): the outbox as admins
// see it, and queuing a failed mail again.
func registerMailLog(api huma.API, h *handlers) {
	register(api, AccessAdmin, huma.Operation{
		OperationID: "searchMails",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/mails/search",
		Summary:     "Search the mail log",
		Description: fmt.Sprintf("Returns the mails of the outbox, newest first, %d at most per page, without "+
			"their bodies; `nextCursor` asks for the next page. Filters by status, template and a part of the "+
			"recipient (ignoring ASCII case). The bodies of mails sent more than 30 days ago are removed, and "+
			"mails older than 180 days deleted. A POST, so that the addresses searched for stay out of URLs "+
			"and the access logs of proxies (ASVS 8.3.1); it changes nothing.", service.MailLogPageSize),
		Tags: []string{"admin"},
	}, h.searchMails)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "getMail",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/mails/{mailId}",
		Summary:     "Show a mail",
		Description: "Returns a mail of the log with its text and HTML, except for the templates that send a " +
			"secret link (confirm-email, change-email, reset-password, invite, waitlist-invite), whose bodies " +
			"never leave the server, and for bodies the purge removed (see `body`).",
		Tags: []string{"admin"},
	}, h.getMail)

	register(api, AccessAdmin, withConflict(api, huma.Operation{
		OperationID: "requeueMail",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/mails/{mailId}/requeue",
		Summary:     "Queue a failed mail again",
		Description: "Queues a failed mail again: its next attempt is now and its attempts start over. A pending " +
			"or sent mail is a 409 `not-failed`; a mail with a secret link is a 409 `secret-link` (the flows " +
			"that send a new link do that). Recorded in the audit log.",
		Tags: []string{"admin"},
	}), h.requeueMail)
}

// MailTemplate is the name of a mail template. Its schema is the enum of
// mail.TemplateNames, so the client's generated types know every template.
type MailTemplate string

// Schema implements huma.SchemaProvider. Every call returns a new schema,
// because Huma sets per-field details (doc, ...) on the returned value.
func (MailTemplate) Schema(huma.Registry) *huma.Schema {
	names := mail.TemplateNames()

	enum := make([]any, len(names))
	for i, n := range names {
		enum[i] = n
	}

	return &huma.Schema{Type: huma.TypeString, Enum: enum}
}

// MailLogItem is a mail of the log, without its bodies.
type MailLogItem struct {
	ID            domain.OutboxMailID `doc:"Mail ID."                                       json:"id"`
	To            string              `doc:"Recipient address."                             json:"to"`
	Subject       string              `doc:"Subject."                                       json:"subject"`
	Template      MailTemplate        `doc:"Template the mail was rendered from."           json:"template,omitempty"`
	ReplyTo       string              `doc:"Address replies go to; absent if the sender's." json:"replyTo,omitempty"`
	CreatedAt     time.Time           `doc:"When the mail was written."                     json:"createdAt"`
	Status        string              `doc:"Delivery state." enum:"pending,sent,failed"    json:"status"`
	Attempts      int                 `doc:"Delivery attempts so far."                      json:"attempts"`
	NextAttemptAt time.Time           `doc:"When the next attempt is due; absent once sent or given up." json:"nextAttemptAt,omitzero"`
	SentAt        time.Time           `doc:"When it was sent; absent if not."              json:"sentAt,omitzero"`
	LastError     string              `doc:"The error of the last failed attempt."          json:"lastError,omitempty"`
	Attachment    string              `doc:"File name of the attached PDF; absent if none." json:"attachment,omitempty"`
	Body          string              `doc:"shown: getMail returns the bodies; withheld: they contain a secret link and never leave the server; purged: removed 30 days after sending." enum:"shown,withheld,purged" json:"body"`
}

// MailLogDetail is a mail of the log with its bodies, where they are
// shown.
type MailLogDetail struct {
	MailLogItem

	Text string `doc:"Plain text body; only with body shown." json:"text,omitempty"`
	HTML string `doc:"HTML body; only with body shown, and only if the mail has one." json:"html,omitempty"`
}

// MailLogInput is the request of searchMails: its filter.
type MailLogInput struct {
	Body struct {
		Status    string       `doc:"Only mails in this state; absent: all." enum:"pending,sent,failed" json:"status,omitempty"`
		Template  MailTemplate `doc:"Only mails of this template; absent: all." json:"template,omitempty"`
		Recipient string       `doc:"Only mails whose recipient contains this, ignoring ASCII case." json:"recipient,omitempty" maxLength:"254"`
		Cursor    string       `doc:"Where the page starts: the nextCursor of the previous page." json:"cursor,omitempty" maxLength:"200"`
	}
}

// MailLogPage is the response body of searchMails.
type MailLogPage struct {
	Items      []MailLogItem `doc:"The mails, newest first." json:"items" nullable:"false"`
	NextCursor string        `doc:"The cursor of the next page; absent after the last." json:"nextCursor,omitempty"`
}

// MailLogOutput is the response of searchMails.
type MailLogOutput struct {
	Body MailLogPage
}

// MailRefInput names a mail of the log.
type MailRefInput struct {
	MailID domain.OutboxMailID `doc:"The mail." path:"mailId"`
}

// MailLogDetailOutput is the response of getMail.
type MailLogDetailOutput struct {
	Body MailLogDetail
}

// MailLogItemOutput is the response of requeueMail.
type MailLogItemOutput struct {
	Body MailLogItem
}

func (h *handlers) searchMails(ctx context.Context, in *MailLogInput) (*MailLogOutput, error) {
	f := in.Body

	after, err := parseMailCursor(f.Cursor)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "invalid cursor", Location: "body.cursor",
		})
	}

	page, err := h.deps.MailLog.List(ctx, service.MailLogQuery{
		Status: domain.MailStatus(f.Status), Template: string(f.Template), Recipient: f.Recipient, After: after,
	})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]MailLogItem, 0, len(page.Mails))
	for _, e := range page.Mails {
		items = append(items, mailLogItem(e))
	}

	return &MailLogOutput{Body: MailLogPage{Items: items, NextCursor: mailCursor(page.Next)}}, nil
}

func (h *handlers) getMail(ctx context.Context, in *MailRefInput) (*MailLogDetailOutput, error) {
	e, err := h.deps.MailLog.Mail(ctx, in.MailID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &MailLogDetailOutput{Body: MailLogDetail{MailLogItem: mailLogItem(e)}}
	if e.Body == service.MailBodyShown {
		out.Body.Text, out.Body.HTML = e.Mail.Text, e.Mail.HTML
	}

	return out, nil
}

func (h *handlers) requeueMail(ctx context.Context, in *MailRefInput) (*MailLogItemOutput, error) {
	p, _ := principalFrom(ctx) // AccessAdmin

	e, err := h.deps.MailLog.Requeue(ctx, p, in.MailID)

	switch {
	case errors.Is(err, service.ErrSecretLinkMail):
		return nil, conflict(reasonSecretLink, "a mail with a secret link is not queued again")
	case errors.Is(err, service.ErrMailNotFailed):
		return nil, conflict(reasonNotFailed, "only a failed mail is queued again")
	case err != nil:
		return nil, h.toHTTPError(ctx, err)
	}

	return &MailLogItemOutput{Body: mailLogItem(e)}, nil
}

func mailLogItem(e service.MailLogEntry) MailLogItem {
	m := e.Mail

	return MailLogItem{
		ID: m.ID, To: m.To, Subject: m.Subject, Template: MailTemplate(m.Template), ReplyTo: m.ReplyTo, CreatedAt: m.CreatedAt,
		Status: string(m.Status()), Attempts: m.Attempts, NextAttemptAt: m.NextAttemptAt, SentAt: m.SentAt,
		LastError: m.LastError, Attachment: e.Attachment, Body: string(e.Body),
	}
}

// mailCursor encodes a cursor as an opaque string: the creation time in
// microseconds and the ID, base64url; "" for the zero cursor.
func mailCursor(c service.MailCursor) string {
	if c.IsZero() {
		return ""
	}

	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.FormatInt(c.CreatedAt.UnixMicro(), 10) + "_" + c.ID.String()))
}

// parseMailCursor decodes mailCursor; "" is the zero cursor.
func parseMailCursor(s string) (service.MailCursor, error) {
	if s == "" {
		return service.MailCursor{}, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.MailCursor{}, err
	}

	micros, id, ok := strings.Cut(string(raw), "_")
	if !ok {
		return service.MailCursor{}, errors.New("no ID")
	}

	n, err := strconv.ParseInt(micros, 10, 64)
	if err != nil || n <= 0 {
		return service.MailCursor{}, errors.New("invalid time")
	}

	mailID, err := domain.ParseID[domain.OutboxMailID](id)
	if err != nil {
		return service.MailCursor{}, err
	}

	return service.MailCursor{CreatedAt: time.UnixMicro(n).UTC(), ID: mailID}, nil
}
