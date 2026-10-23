package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// SessionCookie is the name of the cookie that holds the session token.
const SessionCookie = "studio_session"

func registerAuth(api huma.API, h *handlers) {
	register(api, AccessPublic, huma.Operation{
		OperationID: "register",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/register",
		Summary:     "Register an account",
		Description: "Creates a member account and mails a confirmation link to the email address. " +
			"The answer is the same whether the address is new or already registered.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusAccepted,
	}, h.register)

	register(api, AccessPublic, huma.Operation{
		OperationID: "confirmEmail",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/confirm",
		Summary:     "Confirm the email address",
		Description: "Confirms the email address with the token from the confirmation link and the " +
			"account's password, and signs in. An unknown, used or expired link is a 410.",
		Tags: []string{"auth"},
	}, h.confirmEmail)

	register(api, AccessPublic, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/login",
		Summary:     "Sign in",
		Description: "Signs in with email and password and sets the session cookie. A wrong email or " +
			"password is a 401; a correct password for an unconfirmed email is a 403.",
		Tags: []string{"auth"},
	}, h.login)

	register(api, AccessPublic, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          BasePath + "/auth/logout",
		Summary:       "Sign out",
		Description:   "Ends the session and clears the session cookie.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, h.logout)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "getMe",
		Method:      http.MethodGet,
		Path:        BasePath + "/auth/me",
		Summary:     "Signed-in account",
		Description: "Returns the signed-in account, or 401 without a valid session. While an admin is " +
			"signed in as the account, impersonator names the admin.",
		Tags: []string{"auth"},
	}, h.getMe)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "stopImpersonation",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/impersonation/stop",
		Summary:     "Return to the admin account",
		Description: "Ends an impersonation and signs the admin in again with a new session cookie. " +
			"A 409 if the session is no impersonation.",
		Tags: []string{"auth"},
	}, h.stopImpersonation)
}

// principalKey is the context key of the request's service.Principal.
type principalKey struct{}

// principalFrom returns the signed-in principal of the request, if any.
func principalFrom(ctx context.Context) (service.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(service.Principal)

	return p, ok
}

// sessionMiddleware resolves the session cookie to the request's principal
// and renews the cookie when the session was extended. An invalid cookie is
// cleared; the request continues without a principal.
func (h *handlers) sessionMiddleware(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		c, err := huma.ReadCookie(ctx, SessionCookie)
		if err != nil || c.Value == "" {
			next(ctx)

			return
		}

		p, renewed, err := h.deps.Auth.Authenticate(ctx.Context(), c.Value)

		switch {
		case errors.Is(err, domain.ErrUnauthorized):
			cleared := h.clearSessionCookie()
			ctx.AppendHeader("Set-Cookie", cleared.String())
			next(ctx)

			return
		case err != nil:
			h.deps.Logger.ErrorContext(ctx.Context(), "authenticate", slog.Any("err", err))
			_ = huma.WriteErr(api, ctx, http.StatusInternalServerError, "internal server error")

			return
		}

		if renewed.Token != "" {
			cookie := h.sessionCookie(renewed)
			ctx.AppendHeader("Set-Cookie", cookie.String())
		}

		next(huma.WithValue(ctx, principalKey{}, p))
	}
}

// sessionCookie is the cookie that carries a session. It is persistent
// until the session expires; SameSite=Lax plus the cross-origin protection
// of unsafe methods keep other sites from using it.
func (h *handlers) sessionCookie(s service.SignedIn) http.Cookie {
	//nolint:gosec // G124: Secure is off only with InsecureCookies, for local development over HTTP.
	return http.Cookie{
		Name:     SessionCookie,
		Value:    s.Token,
		Path:     "/",
		Expires:  s.ExpiresAt.UTC(),
		HttpOnly: true,
		Secure:   h.deps.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	}
}

func (h *handlers) clearSessionCookie() http.Cookie {
	//nolint:gosec // G124: Secure is off only with InsecureCookies, for local development over HTTP.
	return http.Cookie{
		Name:     SessionCookie,
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.deps.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	}
}

// RegisterInput is the request of register.
type RegisterInput struct {
	Body struct {
		GivenName  string `doc:"Given name."  json:"givenName"  maxLength:"200" minLength:"1"`
		FamilyName string `doc:"Family name." json:"familyName" maxLength:"200" minLength:"1"`
		Email      string `doc:"Email address; it becomes the login name." format:"email" json:"email" maxLength:"254"`
		Password   string `doc:"Password, at least 8 characters." json:"password" maxLength:"256" minLength:"8"`
		Locale     string `doc:"Language of the confirmation mail (BCP 47), e.g. de; defaults to the school's." json:"locale,omitempty" maxLength:"35"`
	}
}

func (h *handlers) register(ctx context.Context, in *RegisterInput) (*struct{}, error) {
	err := h.deps.Auth.Register(ctx, service.Registration{
		GivenName: in.Body.GivenName, FamilyName: in.Body.FamilyName,
		Email: in.Body.Email, Password: in.Body.Password, Locale: in.Body.Locale,
	})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

// ConfirmEmailInput is the request of confirmEmail.
type ConfirmEmailInput struct {
	Session string `cookie:"studio_session" doc:"Session cookie, if any; it ends on success."`
	Body    struct {
		Token    string `doc:"Token from the confirmation link." json:"token"    maxLength:"100" minLength:"1"`
		Password string `doc:"The account's password."           json:"password" maxLength:"256" minLength:"1"`
	}
}

// LoginInput is the request of login.
type LoginInput struct {
	Session string `cookie:"studio_session" doc:"Session cookie, if any; it ends on success."`
	Body    struct {
		Email    string `doc:"Email address." json:"email"    maxLength:"254" minLength:"1"`
		Password string `doc:"Password."      json:"password" maxLength:"256" minLength:"1"`
	}
}

// LogoutInput is the request of logout.
type LogoutInput struct {
	Session string `cookie:"studio_session" doc:"Session cookie."`
}

// Me is the signed-in account.
type Me struct {
	AccountID  domain.AccountID `doc:"Account ID."                 json:"accountId"`
	PersonID   domain.PersonID  `doc:"ID of the account's person." json:"personId"`
	Email      string           `doc:"Email address (login name)." json:"email"`
	Role       string           `doc:"Role of the account." enum:"member,instructor,office,admin" json:"role"`
	GivenName  string           `doc:"Given name."                 json:"givenName"`
	FamilyName string           `doc:"Family name."                json:"familyName"`
	// Impersonator is set while an admin is signed in as this account.
	Impersonator *Impersonator `doc:"The admin signed in as this account (\"login as\"); absent otherwise." json:"impersonator,omitempty"`
}

// Impersonator is the admin behind an impersonation.
type Impersonator struct {
	AccountID  domain.AccountID `doc:"Account ID of the admin." json:"accountId"`
	Email      string           `doc:"Email address of the admin." json:"email"`
	GivenName  string           `doc:"Given name of the admin."  json:"givenName"`
	FamilyName string           `doc:"Family name of the admin." json:"familyName"`
}

// MeOutput is the response of getMe.
type MeOutput struct {
	Body Me
}

// SignedInOutput is the response of login and confirmEmail: the account
// and the new session cookie.
type SignedInOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      Me
}

// LogoutOutput is the response of logout.
type LogoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

func (h *handlers) confirmEmail(ctx context.Context, in *ConfirmEmailInput) (*SignedInOutput, error) {
	s, err := h.deps.Auth.Confirm(ctx, in.Body.Token, in.Body.Password)
	if err != nil {
		return nil, h.authError(ctx, err)
	}

	h.endPrevious(ctx, in.Session)

	return h.signedIn(ctx, s)
}

func (h *handlers) login(ctx context.Context, in *LoginInput) (*SignedInOutput, error) {
	s, err := h.deps.Auth.Login(ctx, in.Body.Email, in.Body.Password)
	if err != nil {
		return nil, h.authError(ctx, err)
	}

	h.endPrevious(ctx, in.Session)

	return h.signedIn(ctx, s)
}

func (h *handlers) signedIn(ctx context.Context, s service.SignedIn) (*SignedInOutput, error) {
	me, err := h.me(ctx, s.Principal)
	if err != nil {
		return nil, err
	}

	return &SignedInOutput{SetCookie: h.sessionCookie(s), Body: me}, nil
}

// endPrevious ends the session the client signed in over, so that it does
// not stay valid behind the new cookie; an impersonation ends audited. A
// failure is logged: the new session is valid either way.
func (h *handlers) endPrevious(ctx context.Context, token string) {
	if err := h.deps.Auth.Logout(ctx, token); err != nil {
		h.deps.Logger.WarnContext(ctx, "end previous session", slog.Any("err", err))
	}
}

// authError maps the errors of login, confirmEmail and impersonation; their
// messages are meant for the user.
func (h *handlers) authError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		return huma.Error401Unauthorized("invalid email or password")
	case errors.Is(err, service.ErrEmailNotConfirmed):
		return huma.Error403Forbidden("email address not confirmed")
	case errors.Is(err, service.ErrInvalidToken):
		return huma.Error410Gone("the link is invalid or has expired")
	case errors.Is(err, service.ErrNotImpersonable):
		return huma.Error403Forbidden("this account cannot be impersonated")
	case errors.Is(err, service.ErrAccountInactive):
		return huma.Error409Conflict("the account is disabled or not confirmed")
	case errors.Is(err, service.ErrNotImpersonating):
		return huma.Error409Conflict("not signed in as another account")
	default:
		return h.toHTTPError(ctx, err)
	}
}

func (h *handlers) logout(ctx context.Context, in *LogoutInput) (*LogoutOutput, error) {
	if err := h.deps.Auth.Logout(ctx, in.Session); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &LogoutOutput{SetCookie: h.clearSessionCookie()}, nil
}

func (h *handlers) getMe(ctx context.Context, _ *struct{}) (*MeOutput, error) {
	p, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not signed in")
	}

	me, err := h.me(ctx, p)
	if err != nil {
		return nil, err
	}

	return &MeOutput{Body: me}, nil
}

func (h *handlers) me(ctx context.Context, p service.Principal) (Me, error) {
	m, err := h.deps.Auth.Me(ctx, p)
	if err != nil {
		return Me{}, h.toHTTPError(ctx, err)
	}

	out := Me{
		AccountID: m.Account.ID, PersonID: m.Person.ID, Email: m.Account.Email, Role: string(m.Account.Role),
		GivenName: m.Person.GivenName, FamilyName: m.Person.FamilyName,
	}

	if i := m.Impersonator; i != nil {
		out.Impersonator = &Impersonator{
			AccountID: i.Account.ID, Email: i.Account.Email, GivenName: i.Person.GivenName, FamilyName: i.Person.FamilyName,
		}
	}

	return out, nil
}

func (h *handlers) stopImpersonation(ctx context.Context, _ *struct{}) (*SignedInOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	s, err := h.deps.Auth.StopImpersonation(ctx, p)
	if err != nil {
		return nil, h.authError(ctx, err)
	}

	return h.signedIn(ctx, s)
}
