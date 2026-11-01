package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func registerAdmin(api huma.API, h *handlers) {
	register(api, AccessAdmin, huma.Operation{
		OperationID: "listAccounts",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/accounts",
		Summary:     "List accounts",
		Description: "Returns every account with its person, ordered by email.",
		Tags:        []string{"admin"},
	}, h.listAccounts)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "impersonateAccount",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/accounts/{accountId}/impersonate",
		Summary:     "Sign in as an account",
		Description: "Signs the admin in as another account (\"login as\") for at most one hour and sets " +
			"the session cookie; the admin's own session ends. Recorded in the audit log. Admins cannot " +
			"be impersonated (403); a disabled or unconfirmed account is a 409.",
		Tags: []string{"admin"},
	}, h.impersonateAccount)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "listAuditEvents",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/audit",
		Summary:     "List audit events",
		Description: fmt.Sprintf("Returns the latest %d audit events, newest first, with the accounts they name.",
			service.AuditLogLimit),
		Tags: []string{"admin"},
	}, h.listAuditEvents)
}

// AccountEntry is an account in the admin's account list.
type AccountEntry struct {
	ID         domain.AccountID `doc:"Account ID."                 json:"id"`
	Email      string           `doc:"Email address (login name)." json:"email"`
	Role       string           `doc:"Role of the account." enum:"member,instructor,office,admin" json:"role"`
	GivenName  string           `doc:"Given name."                 json:"givenName"`
	FamilyName string           `doc:"Family name."                json:"familyName"`
	Confirmed  bool             `doc:"Whether the email address is confirmed." json:"confirmed"`
	Disabled   bool             `doc:"Whether the account may no longer sign in." json:"disabled"`
}

// AccountList is the response body of listAccounts.
type AccountList struct {
	Items []AccountEntry `doc:"Accounts ordered by email." json:"items" nullable:"false"`
}

// AccountListOutput is the response of listAccounts.
type AccountListOutput struct {
	Body AccountList
}

// AuditAccount is an account named by an audit event. An account that can
// no longer be found has only its ID, one whose person can no longer be
// found no names.
type AuditAccount struct {
	ID         domain.AccountID `doc:"Account ID."                                                                         json:"id"`
	Email      string           `doc:"Email address (login name). Absent if the account can no longer be found."           json:"email,omitempty"`
	GivenName  string           `doc:"Given name. Absent if the account or its person can no longer be found."             json:"givenName,omitempty"`
	FamilyName string           `doc:"Family name. Absent if the account or its person can no longer be found."            json:"familyName,omitempty"`
}

// AuditPerson is a person named by an audit event; one that can no longer
// be found has only its ID.
type AuditPerson struct {
	ID         domain.PersonID `doc:"Person ID."                                                  json:"id"`
	GivenName  string          `doc:"Given name. Absent if the person can no longer be found."   json:"givenName,omitempty"`
	FamilyName string          `doc:"Family name. Absent if the person can no longer be found."  json:"familyName,omitempty"`
	Anonymized bool            `doc:"Whether the office anonymized the person (ADR 0016); its name is then the fixed one (Deleted person), which clients show translated." json:"anonymized,omitempty"`
}

// AuditDetails is what an audit event records beyond its target.
type AuditDetails struct {
	MailID    domain.OutboxMailID `doc:"The mail of mail-requeued."        json:"mailId,omitzero"`
	Recipient string              `doc:"Its recipient."                    json:"recipient,omitempty"`
	Template  string              `doc:"Its template."                     json:"template,omitempty"`
	// ADR 0015.
	MergedPersonID domain.PersonID `doc:"The person of person-merged that was merged into the target, now a tombstone." json:"mergedPersonId,omitzero"`
	MergedName     string          `doc:"Its name at the time."                                                           json:"mergedName,omitempty"`
	// ADR 0016.
	PreviousRole string `doc:"The role before account-role-changed." enum:"member,instructor,office,admin" json:"previousRole,omitempty"`
	Role         string `doc:"The role after account-role-changed."  enum:"member,instructor,office,admin" json:"role,omitempty"`
	// ADR 0020.
	Delivery string    `doc:"How the link of password-reset-issued or setup-link-issued reached its holder; the link itself is never recorded." enum:"mailed,printed" json:"delivery,omitempty"`
	Sessions int       `doc:"How many running sessions sessions-revoked ended; absent for none."                                                  json:"sessions,omitempty"`
	Via      *AuditVia `doc:"The channel the actor acted through, for any action; absent for the API."                                           json:"via,omitempty"`
}

// AuditVia is the channel an actor acted through when not the API.
type AuditVia struct {
	Channel string `doc:"The channel the actor acted through when not the API: cli, the command line (ADR 0020), with --as the actor's email; later versions may add channels." enum:"cli" json:"channel"`
	OSUser  string `doc:"The operating system user who ran the command (unknown if it could not be told)." json:"osUser"`
}

// AuditEventEntry is an event in the audit log.
type AuditEventEntry struct {
	ID           domain.AuditEventID `doc:"Audit event ID."  json:"id"`
	At           time.Time           `doc:"When it happened." json:"at"`
	Action       string              `doc:"What happened: impersonation-started or impersonation-ended (target: the account), mail-requeued (no target; details: the mail), person-merged (target: the surviving person; details: the merged one), person-anonymized (target: the person), account-disabled, account-enabled and account-role-changed (target: the account; details of a role change: both roles), account-deleted (target: the account's person, which stays; without an actor its holder deleted it), password-reset-issued (target: the account; details: the delivery), sessions-revoked (target: the account, none when every account's sessions were ended; details: how many sessions ended), setup-link-issued (target: the account; no actor; details: the delivery and via); later versions may add actions. An event that targeted an account deleted since targets its person." json:"action"`
	Actor        *AuditAccount       `doc:"The account that acted; absent only for account-deleted when the account's holder deleted it, and for setup-link-issued, which an operator issues on the command line (details.via names the OS user)." json:"actor,omitempty"`
	Target       *AuditAccount       `doc:"The account acted upon; absent unless the event targets an account." json:"target,omitempty"`
	TargetPerson *AuditPerson        `doc:"The person acted upon; absent unless the event targets a person." json:"targetPerson,omitempty"`
	Details      *AuditDetails       `doc:"What the event records beyond its target; absent if nothing." json:"details,omitempty"`
}

// AuditEventList is the response body of listAuditEvents.
type AuditEventList struct {
	Items []AuditEventEntry `doc:"The latest audit events, newest first." json:"items" nullable:"false"`
}

// AuditEventListOutput is the response of listAuditEvents.
type AuditEventListOutput struct {
	Body AuditEventList
}

// ImpersonateInput are the parameters of impersonateAccount.
type ImpersonateInput struct {
	AccountID domain.AccountID `doc:"Account to sign in as." path:"accountId"`
}

func (h *handlers) listAccounts(ctx context.Context, _ *struct{}) (*AccountListOutput, error) {
	accounts, err := h.deps.Auth.Accounts(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]AccountEntry, 0, len(accounts))
	for _, a := range accounts {
		items = append(items, AccountEntry{
			ID: a.Account.ID, Email: a.Account.Email, Role: string(a.Account.Role),
			GivenName: a.Person.GivenName, FamilyName: a.Person.FamilyName,
			Confirmed: !a.Account.ConfirmedAt.IsZero(), Disabled: !a.Account.DisabledAt.IsZero(),
		})
	}

	return &AccountListOutput{Body: AccountList{Items: items}}, nil
}

func (h *handlers) listAuditEvents(ctx context.Context, _ *struct{}) (*AuditEventListOutput, error) {
	events, err := h.deps.Auth.AuditLog(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]AuditEventEntry, 0, len(events))
	for _, e := range events {
		entry := AuditEventEntry{ID: e.Event.ID, At: e.Event.At, Action: string(e.Event.Action)}

		if !e.Event.ActorID.IsZero() {
			actor := auditAccount(e.Actor)
			entry.Actor = &actor
		}

		switch e.Event.TargetKind {
		case domain.AuditTargetAccount:
			target := auditAccount(e.Target)
			entry.Target = &target
		case domain.AuditTargetPerson:
			entry.TargetPerson = &AuditPerson{
				ID: e.TargetPerson.ID, GivenName: e.TargetPerson.GivenName, FamilyName: e.TargetPerson.FamilyName,
				Anonymized: e.TargetPerson.Anonymized(),
			}
		}

		if d := e.Event.Details; !d.IsZero() {
			entry.Details = &AuditDetails{
				MailID: d.MailID, Recipient: d.Recipient, Template: d.Template, MergedPersonID: d.MergedPersonID,
				MergedName: d.MergedName, PreviousRole: string(d.PreviousRole), Role: string(d.Role),
				Delivery: string(d.Delivery), Sessions: d.Sessions,
			}

			if !d.Via.IsZero() {
				entry.Details.Via = &AuditVia{Channel: string(d.Via.Channel), OSUser: d.Via.OSUser}
			}
		}

		items = append(items, entry)
	}

	return &AuditEventListOutput{Body: AuditEventList{Items: items}}, nil
}

func auditAccount(a service.AccountEntry) AuditAccount {
	return AuditAccount{
		ID: a.Account.ID, Email: a.Account.Email, GivenName: a.Person.GivenName, FamilyName: a.Person.FamilyName,
	}
}

func (h *handlers) impersonateAccount(ctx context.Context, in *ImpersonateInput) (*SignedInOutput, error) {
	p, _ := principalFrom(ctx) // AccessAdmin

	s, err := h.deps.Auth.Impersonate(ctx, p, in.AccountID)
	if err != nil {
		return nil, h.authError(ctx, err)
	}

	return h.signedIn(ctx, s)
}
