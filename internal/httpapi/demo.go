package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// mailboxLimit is how many mails the mailbox lists.
const mailboxLimit = 50

// Demo endpoints exist in every build but answer only in a demo
// installation (Deps.Demo); elsewhere they are a 404 like unknown routes.
func registerDemo(api huma.API, h *handlers) {
	register(api, AccessPublic, huma.Operation{
		OperationID: "listDemoMailbox",
		Method:      http.MethodGet,
		Path:        BasePath + "/demo/mailbox",
		Summary:     "Demo mailbox",
		Description: "Lists the newest mails of the outbox, so that the demo, which delivers no mail, " +
			"can show them (e.g. the confirmation link). Only in a demo installation; otherwise a 404.",
		Tags: []string{"demo"},
	}, h.listDemoMailbox)

	register(api, AccessPublic, huma.Operation{
		OperationID: "listDemoLogins",
		Method:      http.MethodGet,
		Path:        BasePath + "/demo/logins",
		Summary:     "Demo accounts",
		Description: "Lists fictional demo accounts, one per role, with their password. Only in a demo " +
			"installation; otherwise a 404.",
		Tags: []string{"demo"},
	}, h.listDemoLogins)
}

// MailboxMail is a mail in the demo mailbox.
type MailboxMail struct {
	ID        domain.OutboxMailID `doc:"Mail ID."                               json:"id"`
	To        string              `doc:"Recipient address."                     json:"to"`
	Subject   string              `doc:"Subject."                               json:"subject"`
	Text      string              `doc:"Plain text body."                       json:"text"`
	HTML      string              `doc:"HTML body; absent for text-only mails." json:"html,omitempty"`
	Template  string              `doc:"Template the mail was rendered from."   json:"template,omitempty"`
	ReplyTo   string              `doc:"Address replies go to; absent if the sender's." json:"replyTo,omitempty"`
	CreatedAt time.Time           `doc:"When the mail was written."             json:"createdAt"`
	Status    string              `doc:"Delivery state." enum:"pending,sent,failed" json:"status"`
}

// DemoMailboxOutput is the response of listDemoMailbox.
type DemoMailboxOutput struct {
	Body struct {
		Mails []MailboxMail `doc:"Mails, newest first." json:"mails" nullable:"false"`
	}
}

func (h *handlers) listDemoMailbox(ctx context.Context, _ *struct{}) (*DemoMailboxOutput, error) {
	if !h.deps.Demo {
		return nil, huma.Error404NotFound("resource not found")
	}

	mails, err := h.deps.Outbox.List(ctx, mailboxLimit)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &DemoMailboxOutput{}
	out.Body.Mails = make([]MailboxMail, len(mails))

	for i, m := range mails {
		out.Body.Mails[i] = MailboxMail{
			ID: m.ID, To: m.To, Subject: m.Subject, Text: m.Text, HTML: m.HTML, Template: m.Template,
			ReplyTo: m.ReplyTo, CreatedAt: m.CreatedAt, Status: string(m.Status()),
		}
	}

	return out, nil
}

// DemoLogin is a fictional demo account.
type DemoLogin struct {
	Email      string `doc:"Email address (login name)."   json:"email"`
	Password   string `doc:"Password of the demo account." json:"password"`
	Role       string `doc:"Role of the account." enum:"member,instructor,office,admin" json:"role"`
	GivenName  string `doc:"Given name."                   json:"givenName"`
	FamilyName string `doc:"Family name."                  json:"familyName"`
}

// DemoLoginsOutput is the response of listDemoLogins.
type DemoLoginsOutput struct {
	Body struct {
		Logins []DemoLogin `doc:"Demo accounts, from least to most privileged role." json:"logins" nullable:"false"`
	}
}

func (h *handlers) listDemoLogins(_ context.Context, _ *struct{}) (*DemoLoginsOutput, error) {
	if !h.deps.Demo {
		return nil, huma.Error404NotFound("resource not found")
	}

	out := &DemoLoginsOutput{}
	out.Body.Logins = append([]DemoLogin{}, h.deps.DemoLogins...)

	return out, nil
}
