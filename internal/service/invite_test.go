package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// newPerson stores a person without an account, such as a grown-up
// dependent, and returns its ID.
func (f *authFixture) newPerson(given string) domain.PersonID {
	f.t.Helper()

	p := domain.Person{ID: ids.New[domain.PersonID](ids.V7{}), GivenName: given, FamilyName: "Example"}

	err := f.st.InTx(f.t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Persons().Create(ctx, p)
	})
	if err != nil {
		f.t.Fatal(err)
	}

	return p.ID
}

var (
	officePrincipal = service.Principal{Role: domain.RoleOffice}
	adminPrincipal  = service.Principal{Role: domain.RoleAdmin}
)

func TestCreateAccountInvites(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	kim := f.newPerson("Kim")

	acc, err := f.svc.CreateAccount(t.Context(), officePrincipal, kim, service.NewAccount{
		Email: " kim@example.org ", Role: domain.RoleInstructor,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if acc.PersonID != kim || acc.Email != "kim@example.org" || acc.Role != domain.RoleInstructor ||
		!acc.ConfirmedAt.IsZero() || acc.ID.IsZero() {
		t.Errorf("account %+v", acc)
	}

	token, to := f.link(mail.TemplateInvite, "reset-password")
	if to != "kim@example.org" {
		t.Errorf("invite to %q", to)
	}

	if m := f.mails()[0]; !strings.Contains(m.Text, "Kim") || !strings.Contains(m.Text, "72 hours") {
		t.Errorf("invite text %q", m.Text) // the school's default language: English
	}

	// Until the password is set, nobody can sign in, and a registration
	// leaves the account alone.
	f.wantLogin("kim@example.org", "correct horse", false)
	f.register("kim@example.org", "attacker pass")

	if n := f.count(mail.TemplateConfirmEmail); n != 0 {
		t.Errorf("the registration sent %d confirmation mails", n)
	}

	if err := f.svc.ResetPassword(t.Context(), token, "battery staple"); err != nil {
		t.Fatalf("ResetPassword with the invite: %v", err)
	}

	if f.account("kim@example.org").ConfirmedAt.IsZero() {
		t.Error("setting the password did not confirm the account")
	}

	f.wantLogin("kim@example.org", "battery staple", true)
	f.wantLogin("kim@example.org", "attacker pass", false)
	wantIs(t, f.svc.ResetPassword(t.Context(), token, "another password"), service.ErrInvalidToken)
}

func TestCreateAccountRefusals(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	f.signedUp("ada@example.org", domain.RoleMember)
	ada := f.account("ada@example.org").PersonID
	kim := f.newPerson("Kim")

	create := func(caller service.Principal, person domain.PersonID, email string, role domain.Role) error {
		_, err := f.svc.CreateAccount(t.Context(), caller, person, service.NewAccount{Email: email, Role: role})

		return err
	}

	wantIs(t, create(officePrincipal, ada, "ada2@example.org", domain.RoleMember), service.ErrPersonHasAccount)
	wantIs(t, create(officePrincipal, kim, "ADA@example.org", domain.RoleMember), service.ErrEmailInUse)
	wantIs(t, create(officePrincipal, kim, "kim@example.org", domain.RoleAdmin), domain.ErrForbidden)
	wantIs(t, create(officePrincipal, ids.New[domain.PersonID](ids.V7{}), "x@example.org", domain.RoleMember), domain.ErrNotFound)
	wantField(t, create(officePrincipal, kim, "kim at example", domain.RoleMember), "email")
	wantField(t, create(officePrincipal, kim, "kim@example.org", domain.RoleGuest), "role")

	for _, err := range []error{service.ErrPersonHasAccount, service.ErrEmailInUse} {
		wantIs(t, err, domain.ErrDuplicate)
	}

	if n := len(f.mails()); n != 1 { // Ada's confirmation
		t.Errorf("%d mails, want no invite", n-1)
	}

	// An admin may create admins.
	if err := create(adminPrincipal, kim, "kim@example.org", domain.RoleAdmin); err != nil {
		t.Errorf("admin creating an admin: %v", err)
	}
}

func TestInvitedAccountRequestsANewLink(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)

	_, err := f.svc.CreateAccount(t.Context(), officePrincipal, f.newPerson("Kim"), service.NewAccount{
		Email: "kim@example.org", Role: domain.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}

	f.clock.advance(service.InviteTokenTTL)

	invite, _ := f.link(mail.TemplateInvite, "reset-password")
	wantIs(t, f.svc.ResetPassword(t.Context(), invite, "battery staple"), service.ErrInvalidToken)

	// "Forgot password" sends a fresh link to an invited account.
	if err := f.svc.RequestPasswordReset(t.Context(), "kim@example.org", ""); err != nil {
		t.Fatal(err)
	}

	token, _ := f.link(mail.TemplateResetPassword, "reset-password")
	if err := f.svc.ResetPassword(t.Context(), token, "battery staple"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	f.wantLogin("kim@example.org", "battery staple", true)

	// A disabled invited account gets no link.
	_, err = f.svc.CreateAccount(t.Context(), officePrincipal, f.newPerson("Lou"), service.NewAccount{
		Email: "lou@example.org", Role: domain.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}

	f.update("lou@example.org", func(a *domain.Account) { a.DisabledAt = f.clock.Now() })

	before := f.count(mail.TemplateResetPassword)
	if err := f.svc.RequestPasswordReset(t.Context(), "lou@example.org", ""); err != nil {
		t.Fatal(err)
	}

	if f.count(mail.TemplateResetPassword) != before {
		t.Error("a disabled invited account got a reset link")
	}

	lou, _ := f.link(mail.TemplateInvite, "reset-password")
	if err := f.svc.ResetPassword(t.Context(), lou, "battery staple"); !errors.Is(err, service.ErrInvalidToken) {
		t.Errorf("the invite of a disabled account: %v, want ErrInvalidToken", err)
	}
}

func TestCreateAccountRaces(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	kim := f.newPerson("Kim")

	renderer, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	create := func(storeErr error) error {
		svc := service.NewAuthService(racingStore{Store: f.st, accountErr: storeErr}, f.clock, ids.NewSequence(), nil, renderer, appURL)
		_, err := svc.CreateAccount(t.Context(), officePrincipal, kim, service.NewAccount{Email: "kim@example.org", Role: domain.RoleMember})

		return err
	}

	// An account for the person or the address, created meanwhile, is a
	// duplicate like the one the service finds itself.
	wantIs(t, create(fmt.Errorf("account email is taken: %w", domain.ErrDuplicate)), domain.ErrDuplicate)

	// A person deleted meanwhile is gone, not a duplicate.
	err = create(fmt.Errorf("foreign key violated: %w", domain.ErrConflict))
	wantIs(t, err, domain.ErrNotFound)

	if errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("a vanished person is reported as a duplicate: %v", err)
	}
}

func TestCreateAccountChecksUnderTheLock(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	kim := f.newPerson("Kim")

	renderer, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	var calls []string

	svc := service.NewAuthService(recordingStore{Store: f.st, calls: &calls}, f.clock, ids.NewSequence(), nil, renderer, appURL)
	if _, err := svc.CreateAccount(t.Context(), officePrincipal, kim, service.NewAccount{Email: "kim@example.org", Role: domain.RoleMember}); err != nil {
		t.Fatal(err)
	}

	wantBefore(t, calls, "Lock "+kim.String(), "GetByPerson")
}
