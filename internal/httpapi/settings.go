package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// Reasons of a Conflict of the regional settings (ADR 0013).
const (
	reasonTimezoneLocked = "timezone-locked"
	reasonCurrencyLocked = "currency-locked"
)

// LogoCacheControl lets browsers and proxies keep the logo, but ask with
// its ETag before every use: a new logo shows at once.
const LogoCacheControl = "public, no-cache"

// registerSettings registers the school settings (ADR 0013): what visitors
// see (the public settings and the logo) and the settings page's cards,
// which the office reads and an admin changes. The pass and receipt cards
// are registered with passes and receipts.
func registerSettings(api huma.API, h *handlers) {
	tags := []string{"settings"}

	register(api, AccessPublic, huma.Operation{
		OperationID: "getPublicSettings",
		Method:      http.MethodGet,
		Path:        BasePath + "/settings/public",
		Summary:     "Public school settings",
		Description: "Returns the school settings that are visible without signing in: the name, legal notice " +
			"and branding, the time zone, language and currency.",
		Tags: tags,
	}, h.getPublicSettings)

	register(api, AccessPublic, huma.Operation{
		OperationID: "getSchoolLogo",
		Method:      http.MethodGet,
		Path:        BasePath + "/settings/logo",
		Summary:     "School logo",
		Description: "Returns the school's logo (the one printed on receipts) for the app's header; a 404 without " +
			"one. Cacheable: with the `ETag` in `If-None-Match` an unchanged logo is a 304.",
		Tags: tags,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The logo.",
				Content: map[string]*huma.MediaType{
					domain.LogoPNG:  {Schema: &huma.Schema{Type: "string", Format: "binary"}},
					domain.LogoJPEG: {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
			"304":     {Description: "The logo did not change since the ETag in If-None-Match."},
			"default": problem(api, reflect.TypeFor[ErrorModel](), "Error"),
		},
	}, h.getSchoolLogo)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getSchoolSettings",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/settings/school",
		Summary:     "School profile",
		Description: "Returns the school's name and legal notice.",
		Tags:        tags,
	}, h.getSchoolSettings)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "updateSchoolSettings",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/settings/school",
		Summary:     "Update the school profile",
		Description: "Replaces the school's name (shown in the header, the page title and mails) and its legal " +
			"notice (shown in the footer). Invalid fields are a 422 on them.",
		Tags: tags,
	}, h.updateSchoolSettings)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getBrandingSettings",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/settings/branding",
		Summary:     "Branding",
		Description: "Returns the accent color, its dark-mode variant and whether a logo exists.",
		Tags:        tags,
	}, h.getBrandingSettings)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "updateBrandingSettings",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/settings/branding",
		Summary:     "Update the branding",
		Description: "Sets the accent color, or the app's own without one. It must reach a WCAG contrast of " +
			"4.5:1 against the text on it and the page; else a 422 on `body.accentColor`. The dark mode shows " +
			"a variant derived from it. The logo is the receipts' logo (updateReceiptLogo).",
		Tags: tags,
	}, h.updateBrandingSettings)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getRegionalSettings",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/settings/regional",
		Summary:     "Time and language",
		Description: "Returns the time zone, the default language and the currency, and which of them are " +
			"fixed by the data.",
		Tags: tags,
	}, h.getRegionalSettings)

	register(api, AccessAdmin, withConflict(api, huma.Operation{
		OperationID: "updateRegionalSettings",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/settings/regional",
		Summary:     "Update time and language",
		Description: "Replaces the time zone, the default language and the currency. The language may always " +
			"change. A new time zone once lessons exist is a 409 `timezone-locked`, a new currency once a " +
			"price or a receipt exists a 409 `currency-locked`: they are chosen at the first setup. " +
			"Invalid fields are a 422 on them.",
		Tags: tags,
	}), h.updateRegionalSettings)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getBookingRules",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/settings/booking-rules",
		Summary:     "Booking rules",
		Description: "Returns the weekly release rule, the cutoffs, the hold of waiting-list invites and the " +
			"members' early access, with the release window now.",
		Tags: tags,
	}, h.getBookingRules)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "updateBookingRules",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/settings/booking-rules",
		Summary:     "Update the booking rules",
		Description: "Replaces the booking rules from now on. A wider window (more weeks ahead, more days of " +
			"early access) generates the lessons it reaches at once (`generatedLessons`). Invalid fields are a " +
			"422 on them.",
		Tags: tags,
	}, h.updateBookingRules)

	register(api, AccessOffice, huma.Operation{
		OperationID: "previewReleaseWindow",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/settings/booking-rules/preview",
		Summary:     "Preview a release rule",
		Description: "Returns the release window that a release rule would have now, without changing anything.",
		Tags:        tags,
	}, h.previewReleaseWindow)
}

// PublicSettings is the response body of getPublicSettings.
type PublicSettings struct {
	SchoolName      string `doc:"Display name of the school."                      json:"schoolName"`
	LegalNotice     string `doc:"The legal notice (imprint) for the footer, plain text with line breaks; absent if none." json:"legalNotice,omitempty"`
	AccentColor     string `doc:"The school's accent color (#rrggbb) for the light theme; absent: the app's own." json:"accentColor,omitempty"`
	AccentColorDark string `doc:"Its variant for the dark theme; absent without an accent." json:"accentColorDark,omitempty"`
	HasLogo         bool   `doc:"Whether getSchoolLogo has a logo."                json:"hasLogo"`
	Timezone        string `doc:"IANA time zone of the school, e.g. Europe/Berlin." json:"timezone"`
	DefaultLocale   string `doc:"The school's language for visitors who chose none: en or de." enum:"en,de" json:"defaultLocale"`
	Currency        string `doc:"ISO 4217 code of every amount, e.g. EUR; amounts are in its minor units." json:"currency"`
}

// PublicSettingsOutput is the response of getPublicSettings.
type PublicSettingsOutput struct {
	Body PublicSettings
}

func (h *handlers) getPublicSettings(ctx context.Context, _ *struct{}) (*PublicSettingsOutput, error) {
	s, err := h.deps.Settings.Public(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &PublicSettingsOutput{Body: PublicSettings{
		SchoolName:      s.SchoolName,
		LegalNotice:     s.LegalNotice,
		AccentColor:     s.AccentColor,
		AccentColorDark: service.AccentDark(s.AccentColor),
		HasLogo:         !s.Logo.IsZero(),
		Timezone:        s.Timezone,
		DefaultLocale:   s.DefaultLocale,
		Currency:        s.Currency,
	}}, nil
}

// SchoolLogoInput is the request of getSchoolLogo.
type SchoolLogoInput struct {
	IfNoneMatch string `doc:"The ETag of the logo the client has; unchanged: a 304." header:"If-None-Match"`
}

// SchoolLogoOutput is the response of getSchoolLogo: the logo, or nothing
// with status 304.
type SchoolLogoOutput struct {
	Status       int
	ContentType  string `header:"Content-Type"`
	ETag         string `header:"ETag"`
	CacheControl string `header:"Cache-Control"`
	Body         []byte
}

func (h *handlers) getSchoolLogo(ctx context.Context, in *SchoolLogoInput) (*SchoolLogoOutput, error) {
	// The ETag is the logo's stored digest: an unchanged logo is a 304
	// without reading the image.
	l, err := h.deps.Settings.Logo(ctx, func(digest string) bool {
		return etagMatches(in.IfNoneMatch, logoETag(digest))
	})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	if l.Digest == "" {
		return nil, huma.Error404NotFound("no logo")
	}

	out := &SchoolLogoOutput{ETag: logoETag(l.Digest), CacheControl: LogoCacheControl}

	if l.Logo.IsZero() {
		out.Status = http.StatusNotModified

		return out, nil
	}

	out.Status, out.ContentType, out.Body = http.StatusOK, l.Logo.ContentType, l.Logo.Data

	return out, nil
}

// logoETag is the strong ETag of the logo with digest.
func logoETag(digest string) string { return `"` + digest + `"` }

// SchoolSettings is the request and response body of the school profile.
type SchoolSettings struct {
	SchoolName  string `doc:"Display name of the school." json:"schoolName" maxLength:"200" minLength:"1"`
	LegalNotice string `doc:"The legal notice (imprint) shown in the footer, plain text with line breaks; empty or absent: none." json:"legalNotice,omitempty" maxLength:"2000" required:"false"`
}

// SchoolSettingsInput is the request of updateSchoolSettings.
type SchoolSettingsInput struct {
	Body SchoolSettings
}

// SchoolSettingsOutput is the response of the school profile.
type SchoolSettingsOutput struct {
	Body SchoolSettings
}

func (h *handlers) getSchoolSettings(ctx context.Context, _ *struct{}) (*SchoolSettingsOutput, error) {
	return h.schoolSettingsOutput(ctx)(h.deps.Settings.School(ctx))
}

func (h *handlers) updateSchoolSettings(ctx context.Context, in *SchoolSettingsInput) (*SchoolSettingsOutput, error) {
	return h.schoolSettingsOutput(ctx)(h.deps.Settings.SetSchool(ctx, service.SchoolProfile{
		Name: in.Body.SchoolName, LegalNotice: in.Body.LegalNotice,
	}))
}

// schoolSettingsOutput answers with the settings of the card, or maps err.
func (h *handlers) schoolSettingsOutput(ctx context.Context) func(service.SettingsView, error) (*SchoolSettingsOutput, error) {
	return func(v service.SettingsView, err error) (*SchoolSettingsOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &SchoolSettingsOutput{Body: SchoolSettings{SchoolName: v.SchoolName, LegalNotice: v.LegalNotice}}, nil
	}
}

// BrandingSettingsData is the editable part of the branding.
type BrandingSettingsData struct {
	AccentColor string `doc:"The accent color, #rrggbb; empty or absent: the app's own." json:"accentColor,omitempty" pattern:"^(#[0-9a-fA-F]{6})?$" required:"false"`
}

// BrandingSettings is the response body of the branding.
type BrandingSettings struct {
	BrandingSettingsData

	AccentColorDark string  `doc:"The dark theme's variant of the accent color; absent without one." json:"accentColorDark,omitempty"`
	MinContrast     float64 `doc:"The WCAG contrast the accent color needs against the text on it and the page: 4.5." json:"minContrast"`
	HasLogo         bool    `doc:"Whether the school has a logo (set with the receipt settings)." json:"hasLogo"`
}

// BrandingSettingsInput is the request of updateBrandingSettings.
type BrandingSettingsInput struct {
	Body BrandingSettingsData
}

// BrandingSettingsOutput is the response of the branding.
type BrandingSettingsOutput struct {
	Body BrandingSettings
}

func (h *handlers) getBrandingSettings(ctx context.Context, _ *struct{}) (*BrandingSettingsOutput, error) {
	return h.brandingSettingsOutput(ctx)(h.deps.Settings.Branding(ctx))
}

func (h *handlers) updateBrandingSettings(
	ctx context.Context, in *BrandingSettingsInput,
) (*BrandingSettingsOutput, error) {
	return h.brandingSettingsOutput(ctx)(h.deps.Settings.SetBranding(ctx, in.Body.AccentColor))
}

// brandingSettingsOutput answers with the settings of the card, or maps err.
func (h *handlers) brandingSettingsOutput(ctx context.Context) func(service.SettingsView, error) (*BrandingSettingsOutput, error) {
	return func(v service.SettingsView, err error) (*BrandingSettingsOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &BrandingSettingsOutput{Body: BrandingSettings{
			BrandingSettingsData: BrandingSettingsData{AccentColor: v.AccentColor},
			AccentColorDark:      v.AccentDark(), MinContrast: domain.MinAccentContrast, HasLogo: !v.Logo.IsZero(),
		}}, nil
	}
}

// RegionalSettingsData is the editable part of the time and language.
type RegionalSettingsData struct {
	Timezone      string `doc:"IANA time zone of the school, e.g. Europe/Berlin; fixed once lessons exist." json:"timezone" maxLength:"64" minLength:"1"`
	DefaultLocale string `doc:"The language of visitors, and of mails to accounts without one of their own." enum:"en,de" json:"defaultLocale"`
	Currency      string `doc:"ISO 4217 code of every amount, e.g. EUR; fixed once a price or a receipt exists." json:"currency" pattern:"^[A-Z]{3}$"`
}

// RegionalSettings is the response body of the time and language.
type RegionalSettings struct {
	RegionalSettingsData

	TimezoneLocked bool `doc:"Whether the time zone is fixed: lessons exist." json:"timezoneLocked"`
	CurrencyLocked bool `doc:"Whether the currency is fixed: a pass type has a price, a pass was paid or a receipt issued." json:"currencyLocked"`
}

// RegionalSettingsInput is the request of updateRegionalSettings.
type RegionalSettingsInput struct {
	Body RegionalSettingsData
}

// RegionalSettingsOutput is the response of the time and language.
type RegionalSettingsOutput struct {
	Body RegionalSettings
}

func (h *handlers) getRegionalSettings(ctx context.Context, _ *struct{}) (*RegionalSettingsOutput, error) {
	return h.regionalSettingsOutput(ctx)(h.deps.Settings.Regional(ctx))
}

func (h *handlers) updateRegionalSettings(
	ctx context.Context, in *RegionalSettingsInput,
) (*RegionalSettingsOutput, error) {
	v, err := h.deps.Settings.SetRegional(ctx, service.Regional{
		Timezone: in.Body.Timezone, DefaultLocale: in.Body.DefaultLocale, Currency: in.Body.Currency,
	})

	switch {
	case errors.Is(err, service.ErrTimezoneLocked):
		return nil, conflict(reasonTimezoneLocked, "the time zone cannot change once lessons exist")
	case errors.Is(err, service.ErrCurrencyLocked):
		return nil, conflict(reasonCurrencyLocked, "the currency cannot change once prices or receipts exist")
	}

	return h.regionalSettingsOutput(ctx)(v, err)
}

// regionalSettingsOutput answers with the settings of the card, or maps err.
func (h *handlers) regionalSettingsOutput(ctx context.Context) func(service.SettingsView, error) (*RegionalSettingsOutput, error) {
	return func(v service.SettingsView, err error) (*RegionalSettingsOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &RegionalSettingsOutput{Body: RegionalSettings{
			RegionalSettingsData: RegionalSettingsData{
				Timezone: v.Timezone, DefaultLocale: v.DefaultLocale, Currency: v.Currency,
			},
			TimezoneLocked: v.TimezoneLocked(), CurrencyLocked: v.CurrencyLocked(),
		}}, nil
	}
}

// BookingRulesData is the editable part of the booking rules.
type BookingRulesData struct {
	ReleaseWeekday              int              `doc:"The day of the weekly release, 0 = Sunday to 6 = Saturday." json:"releaseWeekday" maximum:"6" minimum:"0"`
	ReleaseTime                 domain.TimeOfDay `doc:"The time of the release (HH:MM) in the school time zone." example:"18:00" json:"releaseTime" pattern:"^[0-2][0-9]:[0-5][0-9]$"`
	ReleaseWeeksAhead           int              `doc:"Each release opens the lessons up to the same weekday this many weeks later." json:"releaseWeeksAhead" maximum:"52" minimum:"1"`
	BookingCutoffMinutes        int              `doc:"Booking closes this many minutes before a lesson starts; 0: at the start." json:"bookingCutoffMinutes" maximum:"43200" minimum:"0"`
	CancellationCutoffMinutes   int              `doc:"Canceling without losing the visit closes this many minutes before a lesson starts; 0: at the start." json:"cancellationCutoffMinutes" maximum:"43200" minimum:"0"`
	WaitlistInviteExpiryMinutes int              `doc:"How long a waiting-list invite holds its place, in minutes (never beyond the booking cutoff)." json:"waitlistInviteExpiryMinutes" maximum:"43200" minimum:"1"`
	DefaultVisibleDays          int              `doc:"Early access: members see the lessons they may see this many days ahead, released or not, unless their account says otherwise." json:"defaultVisibleDays" maximum:"366" minimum:"1"`
}

// ReleaseWindowBody is the release window of a rule at an instant.
type ReleaseWindowBody struct {
	Now           time.Time `doc:"The instant the window is computed at."                   json:"now"`
	LastReleaseAt time.Time `doc:"The latest release at or before now."                     json:"lastReleaseAt"`
	NextReleaseAt time.Time `doc:"The next release."                                        json:"nextReleaseAt"`
	ReleasedUntil time.Time `doc:"Regular lessons that start before this instant are released." json:"releasedUntil"`
}

// BookingRules is the response body of the booking rules.
type BookingRules struct {
	BookingRulesData

	Window           ReleaseWindowBody `doc:"The release window of these rules now." json:"window"`
	GeneratedLessons int               `doc:"How many lessons the change generated at once because the window grew; 0 for a read." json:"generatedLessons"`
}

// BookingRulesInput is the request of updateBookingRules.
type BookingRulesInput struct {
	Body BookingRulesData
}

// BookingRulesOutput is the response of the booking rules.
type BookingRulesOutput struct {
	Body BookingRules
}

// bookingRulesFields are the request fields of the domain's fields, which
// count durations in minutes.
var bookingRulesFields = map[string]string{
	"bookingCutoff":        "bookingCutoffMinutes",
	"cancellationCutoff":   "cancellationCutoffMinutes",
	"waitlistInviteExpiry": "waitlistInviteExpiryMinutes",
}

func (h *handlers) getBookingRules(ctx context.Context, _ *struct{}) (*BookingRulesOutput, error) {
	return h.bookingRulesOutput(ctx)(h.deps.Settings.BookingRules(ctx))
}

func (h *handlers) updateBookingRules(ctx context.Context, in *BookingRulesInput) (*BookingRulesOutput, error) {
	b := in.Body

	v, err := h.deps.Settings.SetBookingRules(ctx, service.BookingRules{
		ReleaseWeekday: time.Weekday(b.ReleaseWeekday), ReleaseTime: b.ReleaseTime, ReleaseWeeksAhead: b.ReleaseWeeksAhead,
		BookingCutoff:        time.Duration(b.BookingCutoffMinutes) * time.Minute,
		CancellationCutoff:   time.Duration(b.CancellationCutoffMinutes) * time.Minute,
		WaitlistInviteExpiry: time.Duration(b.WaitlistInviteExpiryMinutes) * time.Minute,
		DefaultVisibleDays:   b.DefaultVisibleDays,
	})

	var fe *domain.FieldError
	if errors.As(err, &fe) {
		if field, ok := bookingRulesFields[fe.Field]; ok {
			err = &domain.FieldError{Field: field, Msg: fe.Msg}
		}
	}

	if v.GenerateErr != nil {
		// The rules are stored; the hourly job generates the lessons.
		h.deps.Logger.WarnContext(ctx, "generate the lessons of a wider release window", slog.Any("err", v.GenerateErr))
	}

	return h.bookingRulesOutput(ctx)(v, err)
}

// bookingRulesOutput answers with the settings of the card, or maps err.
func (h *handlers) bookingRulesOutput(ctx context.Context) func(service.SettingsView, error) (*BookingRulesOutput, error) {
	return func(v service.SettingsView, err error) (*BookingRulesOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &BookingRulesOutput{Body: BookingRules{
			BookingRulesData: BookingRulesData{
				ReleaseWeekday: int(v.ReleaseWeekday), ReleaseTime: v.ReleaseTime, ReleaseWeeksAhead: v.ReleaseWeeksAhead,
				BookingCutoffMinutes:        int(v.BookingCutoff / time.Minute),
				CancellationCutoffMinutes:   int(v.CancellationCutoff / time.Minute),
				WaitlistInviteExpiryMinutes: int(v.WaitlistInviteExpiry / time.Minute),
				DefaultVisibleDays:          v.DefaultVisibleDays,
			},
			Window:           releaseWindowBody(v.Now, v.Window),
			GeneratedLessons: v.Generated,
		}}, nil
	}
}

func releaseWindowBody(now time.Time, w service.ReleaseWindow) ReleaseWindowBody {
	return ReleaseWindowBody{
		Now: now.UTC(), LastReleaseAt: w.LastRelease.UTC(), NextReleaseAt: w.NextRelease.UTC(),
		ReleasedUntil: w.ReleasedUntil.UTC(),
	}
}

// PreviewReleaseWindowInput is the request of previewReleaseWindow.
type PreviewReleaseWindowInput struct {
	ReleaseWeekday    int    `doc:"The day of the weekly release, 0 = Sunday to 6 = Saturday." maximum:"6" minimum:"0" query:"releaseWeekday" required:"true"`
	ReleaseTime       string `doc:"The time of the release (HH:MM) in the school time zone." example:"18:00" pattern:"^[0-2][0-9]:[0-5][0-9]$" query:"releaseTime" required:"true"`
	ReleaseWeeksAhead int    `doc:"How many weeks each release opens." maximum:"52" minimum:"1" query:"releaseWeeksAhead" required:"true"`
}

// PreviewReleaseWindowOutput is the response of previewReleaseWindow.
type PreviewReleaseWindowOutput struct {
	Body ReleaseWindowBody
}

func (h *handlers) previewReleaseWindow(
	ctx context.Context, in *PreviewReleaseWindowInput,
) (*PreviewReleaseWindowOutput, error) {
	at, err := domain.ParseTimeOfDay(in.ReleaseTime)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "invalid time of day", Location: "query.releaseTime", Value: in.ReleaseTime,
		})
	}

	p, err := h.deps.Settings.PreviewRelease(ctx, time.Weekday(in.ReleaseWeekday), at, in.ReleaseWeeksAhead)

	// The rule comes in the query, so its errors are located there.
	var fe *domain.FieldError
	if errors.As(err, &fe) {
		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: fe.Msg, Location: "query." + fe.Field,
		})
	}

	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &PreviewReleaseWindowOutput{Body: releaseWindowBody(p.Now, p.Window)}, nil
}
