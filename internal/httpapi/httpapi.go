// Package httpapi defines the HTTP/JSON API (Huma operations, request and
// response types) on top of the service layer. All operations live below
// BasePath, except the calendar feeds that calendars fetch (FeedPathPrefix);
// the OpenAPI 3.1 document is generated from the registered operations.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

const (
	// BasePath is the path prefix of every API route.
	BasePath = "/api/v1"
	// Title is the API title in the OpenAPI document.
	Title = "Studio Schedule API"
	// SpecVersion is the version of the API contract reported in the OpenAPI
	// document. It is independent of the build version so that the
	// generated api/openapi.json stays stable across builds.
	SpecVersion = "0.1.0"
)

// Deps are the dependencies of the API handlers.
type Deps struct {
	Settings  *service.SettingsService
	Catalog   *service.CatalogService
	Timetable *service.TimetableService
	Schedule  *service.ScheduleService
	Lessons   *service.LessonService
	Bookings  *service.BookingService
	Waitlist  *service.WaitlistService
	// Participants are the staff actions on the participants of lessons.
	Participants *service.ParticipantService
	// Teach is the instructor view: the lessons of staff and the notices
	// to their participants.
	Teach *service.TeachService
	// MyLessons are the bookings and waiting-list entries of a family.
	MyLessons *service.MyLessonsService
	// Feeds are the calendar feeds of accounts.
	Feeds *service.CalendarFeedService
	// Passes are the pass catalog, the passes of persons and their
	// redemption mode.
	Passes *service.PassService
	// Receipts are the receipts of pass sales and their settings.
	Receipts *service.ReceiptService
	Auth     *service.AuthService
	Family   *service.FamilyService
	Persons  *service.PersonService
	Outbox   *service.OutboxService
	Version  string
	Logger   *slog.Logger
	// SecureCookies sets the Secure attribute on the session cookie; off
	// only for local development over plain HTTP.
	SecureCookies bool
	// Demo enables the demo endpoints: the outbox as a public mailbox and
	// the list of DemoLogins.
	Demo       bool
	DemoLogins []DemoLogin
	// TrustedProxies are the reverse proxies whose X-Forwarded-For entries
	// name the client (clientip.From); empty: the connection's address.
	TrustedProxies []netip.Prefix
	// RateLimits limit logins and mail-sending requests; the zero value
	// imposes no limits.
	RateLimits RateLimits

	// Statistics are the office's figures of lessons, bookings and sales.
	Statistics *service.StatisticsService
	// Announcements are the school's announcements (ADR 0014).
	Announcements *service.AnnouncementService
	// MailLog is the outbox as admins see it (ADR 0014).
	MailLog *service.MailLogService
	// Imports is the member import (ADR 0015).
	Imports *service.ImportService
}

// NewConfig returns the Huma configuration for the API: OpenAPI document at
// BasePath/openapi.json, docs at BasePath/docs, and no `$schema` response
// field or `Link` header (keeps generated client types clean). Domain IDs
// are documented as `{type: string, format: uuid}` (see registerIDSchemas).
func NewConfig() huma.Config {
	cfg := huma.DefaultConfig(Title, SpecVersion)
	registerIDSchemas(cfg.Components.Schemas)
	cfg.Info.Description = "Schedule and booking API for dance and ballet schools."
	cfg.OpenAPIPath = BasePath + "/openapi"
	cfg.DocsPath = BasePath + "/docs"
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil

	return cfg
}

// Register adds all API operations to api. Each area registers its
// operations in its own file.
func Register(api huma.API, deps Deps) {
	h := &handlers{deps: deps}

	// Before any operation: Huma applies the middlewares registered so far.
	// The session middleware resolves the principal that access checks.
	api.UseMiddleware(h.clientIPMiddleware, h.sessionMiddleware(api), h.accessMiddleware(api))

	registerSystem(api, h)
	registerSettings(api, h)
	registerCatalog(api, h)
	registerCatalogAdmin(api, h)
	registerTimetable(api, h)
	registerTimetableAdmin(api, h)
	registerSchedule(api, h)
	registerLessonAdmin(api, h)
	registerBookings(api, h)
	registerWaitlist(api, h)
	registerParticipants(api, h)
	registerTeach(api, h)
	registerMyLessons(api, h)
	registerCalendarFeeds(api, h)
	registerPasses(api, h)
	registerReceipts(api, h)
	registerAuth(api, h)
	registerCredentials(api, h)
	registerFamily(api, h)
	registerDemo(api, h)
	registerAdmin(api, h)
	registerPersons(api, h)
	registerStatistics(api, h)
	registerAnnouncements(api, h)
	registerMailLog(api, h)
	registerImports(api, h)
	registerDuplicates(api, h)
}

type handlers struct {
	deps Deps
	// untrustedProxy warns once of X-Forwarded-For without TRUSTED_PROXIES.
	untrustedProxy sync.Once
}

// toHTTPError maps domain errors to RFC 9457 problem responses. Unknown
// errors are logged and reported as 500 without leaking details.
func (h *handlers) toHTTPError(ctx context.Context, err error) error {
	var field *domain.FieldError

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return huma.Error404NotFound("resource not found")
	case errors.As(err, &field):
		// Located like Huma's own schema errors, so forms can mark the field.
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: field.Msg, Location: "body." + field.Field,
		})
	case errors.Is(err, domain.ErrValidation):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, domain.ErrInUse):
		return conflict(reasonInUse, "the record is still in use")
	case errors.Is(err, domain.ErrDuplicate):
		// Operations whose duplicates the UI explains differently map
		// their own errors first, e.g. createPersonAccount.
		return conflict(reasonDuplicate, "a record with these values exists already")
	case errors.Is(err, domain.ErrConflict):
		return huma.Error409Conflict("conflict")
	case errors.Is(err, domain.ErrUnauthorized):
		return huma.Error401Unauthorized("not signed in")
	case errors.Is(err, domain.ErrForbidden):
		return huma.Error403Forbidden("forbidden")
	default:
		h.deps.Logger.ErrorContext(ctx, "internal error", slog.Any("err", err))

		return huma.Error500InternalServerError("internal server error")
	}
}
