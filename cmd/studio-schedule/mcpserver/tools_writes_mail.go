package mcpserver

import (
	"context"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// mailWriteTools queue a failed mail of the mail log again (ADR 0014).
func mailWriteTools() []tool {
	return []tool{{
		name: "requeue_mail",
		description: "Queue a failed mail of the mail log again, as the admins' mail log does (requeueMail; " +
			"`mail requeue <id> --yes`): its next attempt is now and its attempts start over, so the server's " +
			"deliver-mail job sends it again. Recorded in the audit log as mail-requeued. A mail with a secret " +
			"link (confirm-email, change-email, reset-password, invite, waitlist-invite) is never queued " +
			"again, and a pending or sent mail is refused too: both are conflicts. The mail IDs come from " +
			"mail_log. Returns the mail as the mail log lists it, without its body.",
		access:       httpapi.AccessAdmin,
		personalData: true, // the mail log names the recipient
		write:        WriteMail,
		annotations:  writeAnnotations("Queue a failed mail again", true, false),
		add:          handler(requeueMail),
	}}
}

// requeueMailInput are the arguments of requeue_mail.
type requeueMailInput struct {
	MailID string `json:"mailId" jsonschema:"The failed mail (its ID, as mail_log lists it)."`
}

// requeueMail is the requeue_mail tool: MailLogService.Requeue, and the
// body of requeueMail, as `mail requeue <id>` prints it.
func requeueMail(ctx context.Context, s *Server, p service.Principal, in requeueMailInput) (any, error) {
	id, err := requiredID[domain.OutboxMailID]("mailId", in.MailID)
	if err != nil {
		return nil, err
	}

	e, err := s.svc.MailLog.Requeue(ctx, p, id)
	if err != nil {
		return nil, err
	}

	return httpapi.NewMailLogItem(e), nil
}
