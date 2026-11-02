package mcpserver

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// mailTools are the admins' mail log tools (ADR 0014).
func mailTools() []tool {
	return []tool{{
		name: "mail_log",
		description: "The mails of the outbox as the admins' mail log lists them (searchMails; `mail list`), " +
			"newest first, at most " + strconv.Itoa(service.MailLogPageSize) + " per page: when, the delivery " +
			"state, the template, the recipient, the subject, the attempts and the last error. Bodies are " +
			"never returned. status and template filter; nextCursor, when another page follows, is the cursor " +
			"of the next call.",
		access:       httpapi.AccessAdmin,
		personalData: true,
		annotations:  readOnly("Mail log"),
		add:          handler(mailLog),
	}}
}

// mailLogInput are the arguments of the mail_log tool.
type mailLogInput struct {
	Status   string `json:"status,omitempty"   jsonschema:"Only mails in this state: pending, sent or failed."`
	Template string `json:"template,omitempty" jsonschema:"Only mails of this template, e.g. receipt."`
	Cursor   string `json:"cursor,omitempty"   jsonschema:"Where the page starts: the nextCursor of the previous page."`
}

// mailLog is the mail_log tool: the body of searchMails, as `mail list`
// prints it.
func mailLog(ctx context.Context, s *Server, _ service.Principal, in mailLogInput) (any, error) {
	if in.Template != "" && !slices.Contains(mail.TemplateNames(), in.Template) {
		return nil, &domain.FieldError{
			Field: "template", Msg: "template is not one of " + strings.Join(mail.TemplateNames(), ", "),
		}
	}

	after, err := httpapi.ParseMailCursor(in.Cursor)
	if err != nil {
		return nil, &domain.FieldError{Field: "cursor", Msg: "cursor is no cursor of a page"}
	}

	page, err := s.svc.MailLog.List(ctx, service.MailLogQuery{
		Status: domain.MailStatus(in.Status), Template: in.Template, After: after,
	})
	if err != nil {
		return nil, err
	}

	return httpapi.NewMailLogPage(page), nil
}
