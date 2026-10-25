package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerPrivacy registers privacy and account administration (ADR 0016):
// the data export of a person (office, and members for their family), the
// office's anonymization of a person, the admin's account administration and
// a member deleting their own account.
func registerPrivacy(api huma.API, h *handlers) {
	register(api, AccessOffice, huma.Operation{
		OperationID: "exportPerson",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/persons/{personId}/export",
		Summary:     "Export a person's data",
		Description: "Returns everything the school stores about a person as one document (GDPR): the person, " +
			"its family links (names only), account, bookings, waiting-list entries, passes with their " +
			"redemptions, receipts (no PDFs), calendar feeds, dismissed announcements and class memberships. " +
			"No secrets: no password hash, no session, link or feed token. A merged or anonymized person is " +
			"exported too.",
		Tags: []string{"admin"},
	}, h.exportPerson)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "exportMyData",
		Method:      http.MethodGet,
		Path:        BasePath + "/my/export",
		Summary:     "Export my data",
		Description: "Returns the data of the signed-in person, or of one of their dependents, like exportPerson. " +
			"A dependent with an account of its own is exported without what belongs to that account (its " +
			"holder exports it). Anyone else is a 404.",
		Tags: []string{"family"},
	}, h.exportMyData)

	register(api, AccessOffice, withConflict(api, huma.Operation{
		OperationID: "anonymizePerson",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/persons/{personId}/anonymize",
		Summary:     "Anonymize a person",
		Description: "Erases a person's personal data: the name becomes \"Deleted person\", and email, phone, " +
			"birth date, notes and member number are cleared. Guardian links in both directions, class " +
			"memberships, open waiting-list entries and staff links are removed, bookings of lessons that have " +
			"not started are canceled on time (a visit they cost comes back), and the account is deleted with " +
			"its sessions, links and calendar feeds. Past bookings, passes and receipts stay. Recorded in the " +
			"audit log. Refused: an unknown or merged person (404), an anonymized one (409 `anonymized`), the " +
			"only guardian of a dependent (409 `sole-guardian`), a person whose account receipts, the " +
			"audit log, announcements or duplicate dismissals name (409 `in-use`), the caller's own person " +
			"(409 `own-account`), the person of an admin account unless an admin asks (403), and that of " +
			"the last admin who can sign in (409 `last-admin`).",
		Tags: []string{"admin"},
	}), h.anonymizePerson)

	registerAccountAdmin(api, h)

	register(api, AccessSignedIn, withConflict(api, huma.Operation{
		OperationID: "deleteMyAccount",
		Method:      http.MethodDelete,
		Path:        BasePath + "/my/account",
		Summary:     "Delete my account",
		Description: "Deletes the signed-in account after checking its password (a wrong one is a 422 on " +
			"body.password, rate-limited like a password change), signs out and mails a confirmation to its " +
			"address. The person and its bookings stay with the school. Refused: while an admin is signed in " +
			"as the account (403), for the last admin who can sign in (409 `last-admin`), and for an account that " +
			"receipts, the audit log, announcements or duplicate dismissals name (409 `in-use`).",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}), h.deleteMyAccount)
}

// registerAccountAdmin registers the admin's account administration.
func registerAccountAdmin(api huma.API, h *handlers) {
	guards := "Refused: the admin's own account (409 `own-account`) and the last admin who can sign in (409 `last-admin`)."

	register(api, AccessAdmin, withConflict(api, huma.Operation{
		OperationID: "disableAccount",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/accounts/{accountId}/disable",
		Summary:     "Disable an account",
		Description: "Disables an account: it may no longer sign in, and its sessions and the impersonations " +
			"it started end. A disabled account stays disabled. Recorded in the audit log. " + guards,
		Tags: []string{"admin"},
	}), h.disableAccount)

	register(api, AccessAdmin, withConflict(api, huma.Operation{
		OperationID: "enableAccount",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/accounts/{accountId}/enable",
		Summary:     "Enable an account",
		Description: "Enables a disabled account again. Recorded in the audit log. The admin's own account " +
			"is a 409 `own-account`.",
		Tags: []string{"admin"},
	}), h.enableAccount)

	register(api, AccessAdmin, withConflict(api, huma.Operation{
		OperationID: "setAccountRole",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/accounts/{accountId}/role",
		Summary:     "Change the role of an account",
		Description: "Gives an account another role; it takes effect with the account's next request. " +
			"Recorded in the audit log. " + guards,
		Tags: []string{"admin"},
	}), h.setAccountRole)

	register(api, AccessAdmin, withConflict(api, huma.Operation{
		OperationID: "deleteAccount",
		Method:      http.MethodDelete,
		Path:        BasePath + "/admin/accounts/{accountId}",
		Summary:     "Delete an account",
		Description: "Deletes an account with its password, sessions, links, calendar feeds and announcement " +
			"dismissals; its person stays. Records that only name it as who did something (booking events, " +
			"lesson cancellations, pass sales, …) forget it. An account that receipts, the audit log, " +
			"announcements or duplicate dismissals name is a 409 `in-use`: disable it instead. Recorded in " +
			"the audit log. " + guards,
		Tags:          []string{"admin"},
		DefaultStatus: http.StatusNoContent,
	}), h.deleteAccount)
}

// AccountRefInput names an account in the path.
type AccountRefInput struct {
	AccountID domain.AccountID `doc:"The account." path:"accountId"`
}

// AccountRoleInput is the request of setAccountRole.
type AccountRoleInput struct {
	AccountID domain.AccountID `doc:"The account." path:"accountId"`
	Body      struct {
		Role string `doc:"The new role." enum:"member,instructor,office,admin" json:"role"`
	}
}

// AccountEntryOutput is the response of the account administration.
type AccountEntryOutput struct {
	Body AccountEntry
}

// DeleteMyAccountInput is the request of deleteMyAccount.
type DeleteMyAccountInput struct {
	Body struct {
		Password string `doc:"The account's password." json:"password" maxLength:"256" minLength:"1"`
		Locale   string `doc:"Language of the confirmation mail (BCP 47), e.g. de; defaults to the account's, then the school's." json:"locale,omitempty" maxLength:"35"`
	}
}

// DeleteMyAccountOutput clears the session cookie.
type DeleteMyAccountOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

// ExportMyDataInput is the request of exportMyData.
type ExportMyDataInput struct {
	Person domain.PersonID `doc:"The person to export: the signed-in person or one of their dependents." query:"person" required:"true"`
}

// AnonymizeResult is the response body of anonymizePerson.
type AnonymizeResult struct {
	Person            AdminPerson `json:"person"`
	BookingsCanceled  int         `doc:"Bookings of lessons that had not started, canceled on time." json:"bookingsCanceled"`
	WaitlistWithdrawn int         `doc:"Open waiting-list entries withdrawn."                     json:"waitlistWithdrawn"`
	MembershipsEnded  int         `doc:"Class memberships ended."                                 json:"membershipsEnded"`
	LinksRemoved      int         `doc:"Guardian links removed, in both directions."              json:"linksRemoved"`
	StaffUnlinked     int         `doc:"Staff records no longer linked to the person."            json:"staffUnlinked"`
	AccountDeleted    bool        `doc:"Whether the person's account was deleted."                json:"accountDeleted"`
}

// AnonymizeOutput is the response of anonymizePerson.
type AnonymizeOutput struct {
	Body AnonymizeResult
}

func (h *handlers) exportPerson(ctx context.Context, in *PersonRefInput) (*PersonExportOutput, error) {
	x, err := h.deps.Persons.Export(ctx, in.PersonID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &PersonExportOutput{Body: personExport(x)}, nil
}

func (h *handlers) exportMyData(ctx context.Context, in *ExportMyDataInput) (*PersonExportOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	x, err := h.deps.Family.Export(ctx, p, in.Person)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &PersonExportOutput{Body: personExport(x)}, nil
}

func (h *handlers) anonymizePerson(ctx context.Context, in *PersonRefInput) (*AnonymizeOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	sum, err := h.deps.Persons.Anonymize(ctx, p, in.PersonID)
	if err != nil {
		return nil, h.privacyError(ctx, err)
	}

	return &AnonymizeOutput{Body: AnonymizeResult{
		Person: adminPerson(sum.Person), BookingsCanceled: sum.BookingsCanceled, WaitlistWithdrawn: sum.WaitlistWithdrawn,
		MembershipsEnded: sum.MembershipsEnded, LinksRemoved: sum.LinksRemoved, StaffUnlinked: sum.StaffUnlinked,
		AccountDeleted: sum.AccountDeleted,
	}}, nil
}

func (h *handlers) disableAccount(ctx context.Context, in *AccountRefInput) (*AccountEntryOutput, error) {
	return h.setDisabled(ctx, in.AccountID, true)
}

func (h *handlers) enableAccount(ctx context.Context, in *AccountRefInput) (*AccountEntryOutput, error) {
	return h.setDisabled(ctx, in.AccountID, false)
}

func (h *handlers) setDisabled(ctx context.Context, id domain.AccountID, disabled bool) (*AccountEntryOutput, error) {
	p, _ := principalFrom(ctx) // AccessAdmin

	a, err := h.deps.Auth.SetAccountDisabled(ctx, p, id, disabled)
	if err != nil {
		return nil, h.privacyError(ctx, err)
	}

	return &AccountEntryOutput{Body: accountEntry(a)}, nil
}

func (h *handlers) setAccountRole(ctx context.Context, in *AccountRoleInput) (*AccountEntryOutput, error) {
	p, _ := principalFrom(ctx) // AccessAdmin

	a, err := h.deps.Auth.SetAccountRole(ctx, p, in.AccountID, domain.Role(in.Body.Role))
	if err != nil {
		return nil, h.privacyError(ctx, err)
	}

	return &AccountEntryOutput{Body: accountEntry(a)}, nil
}

func (h *handlers) deleteAccount(ctx context.Context, in *AccountRefInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessAdmin

	if err := h.deps.Auth.DeleteAccount(ctx, p, in.AccountID); err != nil {
		return nil, h.privacyError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) deleteMyAccount(ctx context.Context, in *DeleteMyAccountInput) (*DeleteMyAccountOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	// Under the limit of wrong passwords per email, like changePassword.
	limit, email := h.deps.RateLimits.PasswordFailures, emailKey(p.Email)

	if err := notBlocked(limit, email); err != nil {
		return nil, err
	}

	switch err := h.deps.Auth.DeleteOwnAccount(ctx, p, in.Body.Password, in.Body.Locale); {
	case errors.Is(err, service.ErrWrongPassword):
		record(limit, email)

		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "the password is wrong", Location: "body.password",
		})
	case err != nil:
		return nil, h.privacyError(ctx, err)
	}

	return &DeleteMyAccountOutput{SetCookie: h.clearSessionCookie()}, nil
}

// privacyError maps the errors of anonymizing and the account
// administration; the rest like authError.
func (h *handlers) privacyError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrAlreadyAnonymized):
		return conflict(reasonAnonymized, "the person is anonymized already")
	case errors.Is(err, service.ErrSoleGuardian):
		return conflict(reasonSoleGuardian,
			"the person is the only guardian of a dependent; link another guardian or remove the link first")
	case errors.Is(err, service.ErrAccountKept):
		return conflict(reasonInUse,
			"receipts, the audit log, announcements or duplicate dismissals name the account; disable it instead")
	case errors.Is(err, service.ErrLastAdmin):
		return conflict(reasonLastAdmin, "the school needs at least one admin account that can sign in")
	case errors.Is(err, service.ErrOwnAccount):
		return conflict(reasonOwnAccount, "another admin changes this account")
	default:
		return h.authError(ctx, err)
	}
}

// accountEntry is the API form of an account with its person.
func accountEntry(a service.AccountEntry) AccountEntry {
	return AccountEntry{
		ID: a.Account.ID, Email: a.Account.Email, Role: string(a.Account.Role),
		GivenName: a.Person.GivenName, FamilyName: a.Person.FamilyName,
		Confirmed: !a.Account.ConfirmedAt.IsZero(), Disabled: !a.Account.DisabledAt.IsZero(),
	}
}

// ---------------------------------------------------------------- export

// exportFormat names the format of the export document.
const exportFormat = "studio-schedule.person-export"

// PersonExport is the response body of exportPerson and exportMyData. Times
// are instants (UTC); dates and times of day are the school's.
type PersonExport struct {
	Format        string                `doc:"The format of this document." enum:"studio-schedule.person-export" json:"format"`
	Version       int                   `doc:"The version of the format; it changes when a field changes its meaning or goes." json:"version"`
	ExportedAt    time.Time             `doc:"When the document was made." json:"exportedAt"`
	Person        ExportPersonData      `json:"person"`
	Guardians     []ExportPersonRef     `doc:"The person's guardians, names only." json:"guardians" nullable:"false"`
	Dependents    []ExportPersonRef     `doc:"The person's dependents, names only." json:"dependents" nullable:"false"`
	MergedPersons []ExportPersonRef     `doc:"Persons merged into this one (duplicates); their receipts are listed here." json:"mergedPersons" nullable:"false"`
	Account       *ExportAccount        `doc:"The person's account; absent without one, or for a dependent's own account exported by a guardian." json:"account,omitempty"`
	CalendarFeeds []ExportCalendarFeed  `doc:"The account's active calendar feeds, without their secret links." json:"calendarFeeds" nullable:"false"`
	Dismissed     []ExportAnnouncement  `doc:"Announcements the account dismissed." json:"dismissedAnnouncements" nullable:"false"`
	Classes       []PersonClass         `doc:"Class memberships." json:"classMemberships" nullable:"false"`
	Bookings      []ExportBooking       `doc:"Every booking, canceled ones included, by lesson start." json:"bookings" nullable:"false"`
	Waitlist      []ExportWaitlistEntry `doc:"Every waiting-list entry, ended ones included." json:"waitlistEntries" nullable:"false"`
	Passes        []ExportPass          `doc:"The passes the person owns." json:"passes" nullable:"false"`
	Receipts      []ExportReceipt       `doc:"The receipts the person paid, newest first (no PDFs)." json:"receipts" nullable:"false"`
}

// PersonExportOutput is the response of exportPerson and exportMyData.
type PersonExportOutput struct {
	Body PersonExport
}

// ExportPersonData is every field of the exported person.
type ExportPersonData struct {
	ID         domain.PersonID `doc:"Person ID."      json:"id"`
	GivenName  string          `doc:"Given name."     json:"givenName"`
	FamilyName string          `doc:"Family name."    json:"familyName"`
	Email      string          `doc:"Contact email, if any." json:"email,omitempty"`
	Phone      string          `doc:"Phone number, if any."  json:"phone,omitempty"`
	BirthDate  domain.Date     `doc:"Date of birth, if known." format:"date" json:"birthDate,omitzero"`
	Notes      string          `doc:"The school's notes about the person, if any." json:"notes,omitempty"`
	MemberNo   string          `doc:"The school's member number, if any." json:"memberNo,omitempty"`
	MergedInto domain.PersonID `doc:"The person this one was merged into, if it was." json:"mergedIntoId,omitzero"`
	// ADR 0016.
	AnonymizedAt time.Time `doc:"When the person was anonymized, if it was." json:"anonymizedAt,omitzero"`
}

// ExportPersonRef names a linked person.
type ExportPersonRef struct {
	ID         domain.PersonID `doc:"Person ID."   json:"id"`
	GivenName  string          `doc:"Given name."  json:"givenName"`
	FamilyName string          `doc:"Family name." json:"familyName"`
}

// ExportAccount is the person's account, without secrets.
type ExportAccount struct {
	ID          domain.AccountID `doc:"Account ID."                 json:"id"`
	Email       string           `doc:"Email address (login name)." json:"email"`
	Role        string           `doc:"Role of the account." enum:"member,instructor,office,admin" json:"role"`
	Locale      string           `doc:"Language of the account's mails, if chosen." json:"locale,omitempty"`
	VisibleDays int              `doc:"Days ahead the account sees the schedule, if it has an override." json:"visibleDays,omitempty"`
	ConfirmedAt time.Time        `doc:"When the email address was confirmed, if it was." json:"confirmedAt,omitzero"`
	DisabledAt  time.Time        `doc:"When the account was disabled, if it is." json:"disabledAt,omitzero"`
}

// ExportCalendarFeed is a calendar feed without its link.
type ExportCalendarFeed struct {
	ID         domain.CalendarFeedID `doc:"Feed ID."  json:"id"`
	Scope      string                `doc:"What the feed lists." enum:"bookings,teaching,schedule" json:"scope"`
	LocationID domain.LocationID     `doc:"The location a schedule feed is narrowed to, if any." json:"locationId,omitzero"`
	CreatedAt  time.Time             `doc:"When the feed was created." json:"createdAt"`
	LastUsedAt time.Time             `doc:"When a calendar last fetched it, if ever (updated daily)." json:"lastUsedAt,omitzero"`
}

// ExportAnnouncement names a dismissed announcement.
type ExportAnnouncement struct {
	ID        domain.AnnouncementID `doc:"Announcement ID." json:"id"`
	Title     string                `doc:"Title."           json:"title"`
	PublishAt time.Time             `doc:"When it was published." json:"publishAt"`
}

// ExportLesson names the lesson of a booking or waiting-list entry.
type ExportLesson struct {
	ID         domain.LessonID `doc:"Lesson ID."         json:"id"`
	StartsAt   time.Time       `doc:"Start instant."     json:"startsAt"`
	EndsAt     time.Time       `doc:"End instant."       json:"endsAt"`
	CourseType string          `doc:"Course type name."  json:"courseType"`
}

// ExportBooking is a booking.
type ExportBooking struct {
	ID         domain.BookingID `doc:"Booking ID."  json:"id"`
	Lesson     ExportLesson     `json:"lesson"`
	Status     string           `doc:"Status." enum:"booked,canceled,attended,no_show" json:"status"`
	Source     string           `doc:"How it came about." enum:"self,member,staff" json:"source"`
	Trial      bool             `doc:"Whether it is a trial lesson." json:"trial"`
	TrialNote  string           `doc:"The note on a trial lesson, if any." json:"trialNote,omitempty"`
	BookedAt   time.Time        `doc:"When it was booked." json:"bookedAt"`
	CanceledAt time.Time        `doc:"When it was canceled, if it was." json:"canceledAt,omitzero"`
	Late       bool             `doc:"Whether it was canceled late." json:"late"`
}

// ExportWaitlistEntry is a waiting-list entry.
type ExportWaitlistEntry struct {
	ID        domain.WaitlistEntryID `doc:"Entry ID." json:"id"`
	Lesson    ExportLesson           `json:"lesson"`
	Status    string                 `doc:"Status." enum:"waiting,invited,accepted,expired,withdrawn" json:"status"`
	JoinedAt  time.Time              `doc:"When the person joined the list." json:"joinedAt"`
	InvitedAt time.Time              `doc:"When the person was invited, if they were." json:"invitedAt,omitzero"`
}

// ExportPass is a pass with its redemptions.
type ExportPass struct {
	ID              domain.PassID      `doc:"Pass ID."             json:"id"`
	Type            string             `doc:"Name of the pass type." json:"type"`
	Kind            string             `doc:"Kind of pass." enum:"visits,time" json:"kind"`
	VisitsTotal     int                `doc:"Visits of a visit pass; 0 for a time pass." json:"visitsTotal"`
	VisitsRemaining int                `doc:"Visits left." json:"visitsRemaining"`
	ValidFrom       domain.Date        `doc:"First school date the pass pays for." format:"date" json:"validFrom"`
	ValidUntil      domain.Date        `doc:"Last school date, if it expires." format:"date" json:"validUntil,omitzero"`
	Status          string             `doc:"Status." enum:"active,voided" json:"status"`
	SoldAt          time.Time          `doc:"When it was sold or assigned." json:"soldAt"`
	PricePaid       int64              `doc:"Price paid in minor units of the school currency; 0 for an assignment." json:"pricePaid"`
	Note            string             `doc:"The school's note, if any." json:"note,omitempty"`
	VoidedAt        time.Time          `doc:"When it was voided, if it was." json:"voidedAt,omitzero"`
	Redemptions     []ExportRedemption `doc:"The bookings the pass paid for." json:"redemptions" nullable:"false"`
}

// ExportRedemption is a visit a pass paid for.
type ExportRedemption struct {
	BookingID domain.BookingID `doc:"The booking."      json:"bookingId"`
	At        time.Time        `doc:"When it was paid." json:"at"`
}

// ExportReceipt is the metadata of a receipt.
type ExportReceipt struct {
	ID            domain.ReceiptID `doc:"Receipt ID."  json:"id"`
	Number        string           `doc:"Receipt number." json:"number"`
	Kind          string           `doc:"sale or cancellation." enum:"sale,cancellation" json:"kind"`
	IssueDate     domain.Date      `doc:"Date of issue." format:"date" json:"issueDate"`
	RecipientName string           `doc:"The recipient as printed." json:"recipientName"`
	Currency      string           `doc:"Currency (ISO 4217)." json:"currency"`
	Gross         int64            `doc:"Total in minor units; negative for a cancellation." json:"gross"`
	Canceled      bool             `doc:"Whether a sale was canceled." json:"canceled"`
}

// personExport is the API form of an export.
func personExport(x service.PersonExport) PersonExport {
	p := x.Person
	out := PersonExport{
		Format: exportFormat, Version: service.ExportVersion, ExportedAt: x.ExportedAt.UTC(),
		Person: ExportPersonData{
			ID: p.ID, GivenName: p.GivenName, FamilyName: p.FamilyName, Email: p.Email, Phone: p.Phone,
			BirthDate: p.BirthDate, Notes: p.Notes, MemberNo: p.MemberNo, MergedInto: p.MergedInto,
			AnonymizedAt: p.AnonymizedAt.UTC(),
		},
		Guardians: personRefs(x.Guardians), Dependents: personRefs(x.Dependents), MergedPersons: personRefs(x.Merged),
		CalendarFeeds: make([]ExportCalendarFeed, 0, len(x.CalendarFeeds)),
		Dismissed:     make([]ExportAnnouncement, 0, len(x.Dismissed)),
		Classes:       make([]PersonClass, 0, len(x.Classes)),
		Bookings:      make([]ExportBooking, 0, len(x.Bookings)),
		Waitlist:      make([]ExportWaitlistEntry, 0, len(x.Waitlist)),
		Passes:        make([]ExportPass, 0, len(x.Passes)),
		Receipts:      make([]ExportReceipt, 0, len(x.Receipts)),
	}

	if a := x.Account; a != nil {
		out.Account = &ExportAccount{
			ID: a.ID, Email: a.Email, Role: string(a.Role), Locale: a.Locale, VisibleDays: a.VisibleDays,
			ConfirmedAt: a.ConfirmedAt.UTC(), DisabledAt: a.DisabledAt.UTC(),
		}
	}

	for _, f := range x.CalendarFeeds {
		out.CalendarFeeds = append(out.CalendarFeeds, ExportCalendarFeed{
			ID: f.ID, Scope: string(f.Scope), LocationID: f.LocationID, CreatedAt: f.CreatedAt.UTC(),
			LastUsedAt: f.LastUsedAt.UTC(),
		})
	}

	for _, a := range x.Dismissed {
		out.Dismissed = append(out.Dismissed, ExportAnnouncement{ID: a.ID, Title: a.Title, PublishAt: a.PublishAt.UTC()})
	}

	for _, c := range x.Classes {
		out.Classes = append(out.Classes, PersonClass{
			ClassID: c.Class.ID, TermID: c.Term.ID, TermName: c.Term.Name, CourseTypeName: c.CourseType.Name,
			Weekday: int(c.Class.Weekday), StartTime: c.Class.StartTime, Retired: c.Class.Retired, Since: c.Since,
		})
	}

	for _, b := range x.Bookings {
		out.Bookings = append(out.Bookings, ExportBooking{
			ID: b.Booking.ID, Lesson: exportLesson(b.Lesson, b.CourseType), Status: string(b.Booking.Status),
			Source: string(b.Booking.Source), Trial: b.Booking.Trial, TrialNote: b.Booking.TrialNote,
			BookedAt: b.Booking.BookedAt.UTC(), CanceledAt: b.Booking.CanceledAt.UTC(), Late: b.Booking.Late,
		})
	}

	for _, e := range x.Waitlist {
		out.Waitlist = append(out.Waitlist, ExportWaitlistEntry{
			ID: e.Entry.ID, Lesson: exportLesson(e.Lesson, e.CourseType), Status: string(e.Entry.Status),
			JoinedAt: e.Entry.JoinedAt.UTC(), InvitedAt: e.Entry.InvitedAt.UTC(),
		})
	}

	for _, ep := range x.Passes {
		out.Passes = append(out.Passes, exportPass(ep))
	}

	for _, r := range x.Receipts {
		out.Receipts = append(out.Receipts, ExportReceipt{
			ID: r.Receipt.ID, Number: r.Receipt.Number, Kind: string(r.Receipt.Kind), IssueDate: r.Receipt.IssueDate,
			RecipientName: r.Receipt.RecipientName, Currency: r.Receipt.Currency, Gross: int64(r.Receipt.Gross),
			Canceled: r.Canceled,
		})
	}

	return out
}

func exportPass(ep service.ExportPass) ExportPass {
	p := ep.Pass
	out := ExportPass{
		ID: p.ID, Type: ep.Type.Name, Kind: string(p.Kind), VisitsTotal: p.VisitsTotal,
		VisitsRemaining: p.VisitsRemaining, ValidFrom: p.ValidFrom, ValidUntil: p.ValidUntil, Status: string(p.Status),
		SoldAt: p.SoldAt.UTC(), PricePaid: int64(p.PricePaid), Note: p.Note, VoidedAt: p.VoidedAt.UTC(),
		Redemptions: make([]ExportRedemption, 0, len(ep.Redemptions)),
	}

	for _, r := range ep.Redemptions {
		out.Redemptions = append(out.Redemptions, ExportRedemption{BookingID: r.BookingID, At: r.At.UTC()})
	}

	return out
}

func exportLesson(l domain.Lesson, ct domain.CourseType) ExportLesson {
	return ExportLesson{ID: l.ID, StartsAt: l.StartsAt.UTC(), EndsAt: l.EndsAt.UTC(), CourseType: ct.Name}
}

func personRefs(persons []domain.Person) []ExportPersonRef {
	out := make([]ExportPersonRef, 0, len(persons))
	for _, p := range persons {
		out = append(out, ExportPersonRef{ID: p.ID, GivenName: p.GivenName, FamilyName: p.FamilyName})
	}

	return out
}
