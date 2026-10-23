package httpapi

import (
	"context"
	"log/slog"
	"maps"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Access says who may call an operation (ADR 0005). Every operation
// declares one when it is registered; the access middleware rejects
// requests that do not qualify before the input is even parsed.
type Access string

// Access levels, from open to restricted. Staff, office and admin include
// the roles above them in domain.Roles.
const (
	AccessPublic   Access = "public"   // anyone, signed in or not
	AccessSignedIn Access = "signedIn" // any signed-in account
	AccessStaff    Access = "staff"    // instructor, office, admin
	AccessOffice   Access = "office"   // office, admin
	AccessAdmin    Access = "admin"    // admin only
)

// accessKey is the operation metadata key of the access level; the same
// value is published as the x-access extension in the OpenAPI document.
const accessKey = "x-access"

// minRole is the least role an access level admits; RoleGuest for public.
func (a Access) minRole() (domain.Role, bool) {
	switch a {
	case AccessPublic:
		return domain.RoleGuest, true
	case AccessSignedIn:
		return domain.RoleMember, true
	case AccessStaff:
		return domain.RoleInstructor, true
	case AccessOffice:
		return domain.RoleOffice, true
	case AccessAdmin:
		return domain.RoleAdmin, true
	default:
		return "", false
	}
}

// register adds an operation with its access level. Every operation must be
// registered through it (or Declare); one without a level is refused at
// runtime.
func register[I, O any](
	api huma.API, access Access, op huma.Operation, handler func(context.Context, *I) (*O, error),
) {
	huma.Register(api, Declare(op, access), handler)
}

// Declare returns op with its access level set, for operations registered
// with huma.Register directly.
func Declare(op huma.Operation, access Access) huma.Operation {
	if _, ok := access.minRole(); !ok {
		panic("httpapi: operation " + op.OperationID + " has an unknown access level " + string(access))
	}

	op.Metadata = maps.Clone(op.Metadata)
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}

	op.Extensions = maps.Clone(op.Extensions)
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}

	op.Metadata[accessKey] = access
	op.Extensions[accessKey] = string(access)

	return op
}

// accessMiddleware enforces the access level of the operation: 401 without a
// session where one is needed, 403 for a role below the level. It runs after
// sessionMiddleware, which resolves the principal.
func (h *handlers) accessMiddleware(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		access, _ := ctx.Operation().Metadata[accessKey].(Access)

		minimum, ok := access.minRole()
		if !ok {
			// Fail closed: an operation registered without a level.
			h.deps.Logger.ErrorContext(ctx.Context(), "operation without access level",
				slog.String("operation", ctx.Operation().OperationID))
			_ = huma.WriteErr(api, ctx, http.StatusInternalServerError, "internal server error")

			return
		}

		if minimum == domain.RoleGuest {
			next(ctx)

			return
		}

		p, signedIn := principalFrom(ctx.Context())

		switch {
		case !signedIn:
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "not signed in")
		case !p.Role.AtLeast(minimum):
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "forbidden")
		default:
			next(ctx)
		}
	}
}
