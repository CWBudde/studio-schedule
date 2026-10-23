package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
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

func (h *handlers) impersonateAccount(ctx context.Context, in *ImpersonateInput) (*SignedInOutput, error) {
	p, _ := principalFrom(ctx) // AccessAdmin

	s, err := h.deps.Auth.Impersonate(ctx, p, in.AccountID)
	if err != nil {
		return nil, h.authError(ctx, err)
	}

	return h.signedIn(ctx, s)
}
