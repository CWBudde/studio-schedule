package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/service"
)

func registerCredentials(api huma.API, h *handlers) {
	register(api, AccessPublic, huma.Operation{
		OperationID: "requestPasswordReset",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/password-reset",
		Summary:     "Request a password-reset link",
		Description: "Mails a link to set a new password to the address if it belongs to an account that may " +
			"sign in. The answer is the same for every address. Too many mail requests per client are a " +
			"429 with Retry-After.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusAccepted,
	}, h.requestPasswordReset)

	register(api, AccessPublic, huma.Operation{
		OperationID: "resetPassword",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/password-reset/confirm",
		Summary:     "Set a new password with a reset link",
		Description: "Sets the password with the token from the reset link and ends every session of the " +
			"account; it does not sign in. If the account had a password, it gets a mail that its password " +
			"was changed. An unknown, used or expired link is a 410.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, h.resetPassword)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "changePassword",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/password",
		Summary:     "Change the password",
		Description: "Replaces the password after checking the current one (a wrong one is a 422 on " +
			"body.currentPassword), ends every other session of the account and mails the account that its " +
			"password was changed. Refused (403) while an admin is signed in as the account.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, h.changePassword)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "requestEmailChange",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/email",
		Summary:     "Request an email change",
		Description: "Checks the password (a wrong one is a 422 on body.password) and mails a confirmation " +
			"link to the new address; the account keeps its address until the link is used. The answer is " +
			"the same if the new address already has an account (then no mail is sent). Refused (403) " +
			"while an admin is signed in as the account.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusAccepted,
	}, h.requestEmailChange)

	register(api, AccessPublic, huma.Operation{
		OperationID: "confirmEmailChange",
		Method:      http.MethodPost,
		Path:        BasePath + "/auth/email/confirm",
		Summary:     "Confirm a new email address",
		Description: "Moves the account to the new address with the token from the link and mails a notice " +
			"to the old address. An unknown, used or expired link is a 410; an address taken in the " +
			"meantime is a 409.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, h.confirmEmailChange)
}

// RequestPasswordResetInput is the request of requestPasswordReset.
type RequestPasswordResetInput struct {
	Body struct {
		Email  string `doc:"Email address of the account." json:"email" maxLength:"254" minLength:"1"`
		Locale string `doc:"Language of the mail (BCP 47), e.g. de; defaults to the school's." json:"locale,omitempty" maxLength:"35"`
	}
}

// ResetPasswordInput is the request of resetPassword.
type ResetPasswordInput struct {
	Body struct {
		Token    string `doc:"Token from the reset link."           json:"token"    maxLength:"100" minLength:"1"`
		Password string `doc:"New password: 8 to 256 characters and none of the 10,000 most common passwords (ignoring case)." json:"password" maxLength:"256" minLength:"8"`
	}
}

// ChangePasswordInput is the request of changePassword.
type ChangePasswordInput struct {
	Body struct {
		CurrentPassword string `doc:"The current password."                json:"currentPassword" maxLength:"256" minLength:"1"`
		NewPassword     string `doc:"New password: 8 to 256 characters and none of the 10,000 most common passwords (ignoring case)." json:"newPassword" maxLength:"256" minLength:"8"`
	}
}

// RequestEmailChangeInput is the request of requestEmailChange.
type RequestEmailChangeInput struct {
	Body struct {
		Password string `doc:"The account's password." json:"password" maxLength:"256" minLength:"1"`
		NewEmail string `doc:"The new email address."  format:"email"  json:"newEmail" maxLength:"254"`
		Locale   string `doc:"Language of the mail (BCP 47), e.g. de; defaults to the school's." json:"locale,omitempty" maxLength:"35"`
	}
}

// ConfirmEmailChangeInput is the request of confirmEmailChange.
type ConfirmEmailChangeInput struct {
	Body struct {
		Token  string `doc:"Token from the confirmation link." json:"token" maxLength:"100" minLength:"1"`
		Locale string `doc:"Language of the notice to the old address (BCP 47); defaults to the school's." json:"locale,omitempty" maxLength:"35"`
	}
}

func (h *handlers) requestPasswordReset(ctx context.Context, in *RequestPasswordResetInput) (*struct{}, error) {
	if err := allow(h.deps.RateLimits.Mails, clientIP(ctx)); err != nil {
		return nil, err
	}

	if err := h.deps.Auth.RequestPasswordReset(ctx, in.Body.Email, in.Body.Locale); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) resetPassword(ctx context.Context, in *ResetPasswordInput) (*struct{}, error) {
	if err := h.deps.Auth.ResetPassword(ctx, in.Body.Token, in.Body.Password); err != nil {
		return nil, h.authError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) changePassword(ctx context.Context, in *ChangePasswordInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	err := h.checkingPassword(ctx, p, "body.currentPassword", func() error {
		return h.deps.Auth.ChangePassword(ctx, p, in.Body.CurrentPassword, in.Body.NewPassword)
	})
	if err != nil {
		return nil, err
	}

	return &struct{}{}, nil
}

func (h *handlers) requestEmailChange(ctx context.Context, in *RequestEmailChangeInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn
	mails, ip := h.deps.RateLimits.Mails, clientIP(ctx)

	// Only a request that passes the checks counts: a mistyped password
	// sends no mail.
	if err := notBlocked(mails, ip); err != nil {
		return nil, err
	}

	err := h.checkingPassword(ctx, p, "body.password", func() error {
		return h.deps.Auth.RequestEmailChange(ctx, p, in.Body.Password, in.Body.NewEmail, in.Body.Locale)
	})
	if err != nil {
		return nil, err
	}

	record(mails, ip)

	return &struct{}{}, nil
}

// checkingPassword runs change, which checks the password of p, under the
// limit of wrong passwords per email and returns its error as an HTTP error:
// a blocked email is a 429, a wrong password a 422 at location.
func (h *handlers) checkingPassword(
	ctx context.Context, p service.Principal, location string, change func() error,
) error {
	limit, email := h.deps.RateLimits.PasswordFailures, emailKey(p.Email)

	if err := notBlocked(limit, email); err != nil {
		return err
	}

	switch err := change(); {
	case err == nil:
		return nil
	case errors.Is(err, service.ErrWrongPassword):
		record(limit, email)

		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "the password is wrong", Location: location,
		})
	default:
		return h.authError(ctx, err)
	}
}

func (h *handlers) confirmEmailChange(ctx context.Context, in *ConfirmEmailChangeInput) (*struct{}, error) {
	if err := h.deps.Auth.ConfirmEmailChange(ctx, in.Body.Token, in.Body.Locale); err != nil {
		return nil, h.authError(ctx, err)
	}

	return &struct{}{}, nil
}
