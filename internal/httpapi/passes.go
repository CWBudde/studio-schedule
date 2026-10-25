package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerPasses registers the passes (ADR 0012): the office's pass
// catalog, sales, assignments and voids, the redemption mode, and the
// members' view of their family's passes.
func registerPasses(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"passes"}

		return withConflict(api, op)
	}

	register(api, AccessOffice, huma.Operation{
		OperationID: "listPassTypes",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/pass-types",
		Summary:     "List pass types",
		Description: "Returns every pass type ordered by name, retired ones included.",
		Tags:        []string{"passes"},
	}, h.listPassTypes)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "createPassType",
		Method:        http.MethodPost,
		Path:          BasePath + "/admin/pass-types",
		Summary:       "Create a pass type",
		Description:   "Adds a pass type. A name another pass type has (ignoring case) is a 409 `duplicate`.",
		DefaultStatus: http.StatusCreated,
	}), h.createPassType)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updatePassType",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/pass-types/{passTypeId}",
		Summary:     "Update a pass type",
		Description: "Replaces the fields of a pass type; `active: false` retires it, so it is no longer sold. " +
			"Passes sold already keep their visits, days and price. A name another pass type has is a 409 " +
			"`duplicate`.",
	}), h.updatePassType)

	register(api, AccessOffice, huma.Operation{
		OperationID: "retirePassType",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/pass-types/{passTypeId}/retire",
		Summary:     "Retire a pass type",
		Description: "Stops the sale of a pass type (`active: false`) and changes nothing else; retiring a retired " +
			"type changes nothing. Passes sold keep what they were sold with. An unknown type is a 404.",
		Tags: []string{"passes"},
	}, h.retirePassType)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deletePassType",
		Method:        http.MethodDelete,
		Path:          BasePath + "/admin/pass-types/{passTypeId}",
		Summary:       "Delete a pass type",
		Description:   "Deletes a pass type that was never sold; a sold one is a 409 `in-use` and is retired instead.",
		DefaultStatus: http.StatusNoContent,
	}), h.deletePassType)

	register(api, AccessOffice, huma.Operation{
		OperationID: "listPersonPasses",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/persons/{personId}/passes",
		Summary:     "List a person's passes",
		Description: "Returns the passes of a person with what they paid for: the usable ones first, then the " +
			"others, each newest sale first. An unknown person is a 404.",
		Tags: []string{"passes"},
	}, h.listPersonPasses)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "sellPass",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/persons/{personId}/passes/sell",
		Summary:     "Sell a pass",
		Description: "Sells a pass of an active type to the person: it copies the type's visits and days from " +
			"`validFrom` (default: today in the school's time zone) and records the price paid (default: the " +
			"type's price) and who paid (`payerId`, default: the person). An unknown person is a 404; an " +
			"unknown or retired type a 422 on `body.passTypeId`, an unknown payer on `body.payerId`. A new " +
			"pass pays for nothing booked before. The sale issues its receipt (ADR 0012): numbered, with the " +
			"payment method and the recipient, rendered as a PDF and mailed to the payer's account (else to " +
			"`recipientEmail`, else nowhere). Incomplete receipt settings are a 409 " +
			"`receipt-settings-incomplete`: every sale has a receipt.",
		DefaultStatus: http.StatusCreated,
	}), h.sellPass)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "assignPass",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/persons/{personId}/passes/assign",
		Summary:     "Give a pass away",
		Description: "Gives the person a pass of an active type without payment, with an optional note why. It " +
			"fails like `sellPass`.",
		DefaultStatus: http.StatusCreated,
	}), h.assignPass)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "voidPass",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/passes/{passId}/void",
		Summary:     "Void a pass",
		Description: "Voids a pass: it pays for nothing more, and what it paid for stays paid. A voided pass is a " +
			"409 `already-voided`.",
	}), h.voidPass)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getPassSettings",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/settings/passes",
		Summary:     "Pass settings",
		Description: "Returns when bookings use a pass.",
		Tags:        []string{"passes"},
	}, h.getPassSettings)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "updatePassSettings",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/settings/passes",
		Summary:     "Update the pass settings",
		Description: "Sets when bookings use a pass from now on. Bookings redeemed before stay as they are; a " +
			"booking follows the new mode the next time it changes.",
		Tags: []string{"passes"},
	}, h.updatePassSettings)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "listMyPasses",
		Method:      http.MethodGet,
		Path:        BasePath + "/my/passes",
		Summary:     "List my passes",
		Description: "Returns the passes of the signed-in person and their dependents (managed ones included) " +
			"with what they paid for: the usable ones first, then the others, each newest sale first.",
		Tags: []string{"passes"},
	}, h.listMyPasses)
}

// PassTypeData is the editable part of a pass type.
type PassTypeData struct {
	Name         string `doc:"Display name, unique (ignoring case)." json:"name" maxLength:"100" minLength:"1"`
	Kind         string `doc:"visits: a number of visits, valid validityDays days (0: no expiry); time: any number of visits for validityDays days." enum:"visits,time" json:"kind"`
	Visits       int    `doc:"Number of visits of a visit pass (1–1000); 0 or absent for a time pass." json:"visits,omitempty" maximum:"1000" minimum:"0" required:"false"`
	ValidityDays int    `doc:"Visit pass: days valid from its first day, 0 for no expiry. Time pass: its length in days (at least 1)." json:"validityDays" maximum:"3660" minimum:"0"`
	Price        int64  `doc:"Gross price in minor units of the school currency (cents)." json:"price" maximum:"100000000" minimum:"0"`
	VATRate      int    `doc:"VAT rate of the price in basis points (1900 = 19 %); 0 for none." json:"vatRate" maximum:"10000" minimum:"0"`
	Active       bool   `doc:"Whether the type is sold; a retired type stays for the passes sold." json:"active"`
}

// PassType is a product of the pass catalog.
type PassType struct {
	ID domain.PassTypeID `doc:"Pass type ID." json:"id"`
	PassTypeData

	CreatedAt time.Time `doc:"When the type was created." json:"createdAt"`
}

// PassTypeRef names the type of a pass.
type PassTypeRef struct {
	ID   domain.PassTypeID `doc:"Pass type ID."   json:"id"`
	Name string            `doc:"Its current name." json:"name"`
}

// PassPerson names a person a pass refers to.
type PassPerson struct {
	ID   domain.PersonID `doc:"Person ID."              json:"id"`
	Name string          `doc:"Given and family name." json:"name"`
}

// Pass is a pass of a person.
type Pass struct {
	ID              domain.PassID    `doc:"Pass ID." json:"id"`
	PassType        PassTypeRef      `json:"passType"`
	Owner           PassPerson       `doc:"Whom the pass belongs to." json:"owner"`
	Kind            string           `doc:"visits: a number of visits; time: any number of visits within its days." enum:"visits,time" json:"kind"`
	VisitsTotal     int              `doc:"Visits of a visit pass; 0 for a time pass." json:"visitsTotal"`
	VisitsRemaining int              `doc:"Visits left of a visit pass; 0 for a time pass." json:"visitsRemaining"`
	ValidFrom       domain.Date      `doc:"First school date the pass pays for." format:"date" json:"validFrom"`
	ValidUntil      domain.Date      `doc:"Last school date the pass pays for; absent for a visit pass without expiry." format:"date" json:"validUntil,omitzero"`
	Status          string           `doc:"active, or voided by the office." enum:"active,voided" json:"status"`
	Usable          bool             `doc:"Whether the pass pays for a visit today: active, valid today, and a visit pass has visits left." json:"usable"`
	Origin          string           `doc:"sale: sold; assignment: given without payment." enum:"sale,assignment" json:"origin"`
	SoldAt          time.Time        `doc:"When it was sold or given." json:"soldAt"`
	Payer           *PassPerson      `doc:"Who paid, if not the owner." json:"payer,omitempty"`
	PricePaid       int64            `doc:"Price paid in minor units of the school currency; 0 for an assignment." json:"pricePaid"`
	VoidedAt        time.Time        `doc:"When it was voided, if so." json:"voidedAt,omitzero"`
	ReceiptID       domain.ReceiptID `doc:"The receipt of the sale; absent for an assignment." json:"receiptId,omitzero"`
	Uses            []PassUse        `doc:"The latest bookings it paid for (at most 10, by when it paid), the latest lesson first." json:"uses" nullable:"false"`
	UseCount        int              `doc:"How many bookings it paid for in all." json:"useCount"`
}

// AdminPass is a pass as the office sees it: with its note.
type AdminPass struct {
	Pass

	Note string `doc:"The office's note, e.g. why it was given away." json:"note,omitempty"`
}

// PassUse is a booking a pass paid for.
type PassUse struct {
	BookingID     domain.BookingID `doc:"The booking."                json:"bookingId"`
	PersonName    string           `doc:"Given and family name of the booked person." json:"personName"`
	BookingStatus string           `doc:"State of the booking; a late cancellation is canceled." enum:"booked,canceled,attended,no_show" json:"bookingStatus"`
	Late          bool             `doc:"Whether the booking was canceled late." json:"late"`
	RedeemedAt    time.Time        `doc:"When the pass paid for it." json:"redeemedAt"`
	Lesson        LessonSummary    `json:"lesson"`
}

// BookingPayment is how a booking is paid (ADR 0012).
type BookingPayment struct {
	Status string       `doc:"paid: a pass paid for it (see pass). unpaid: it costs a visit, but no pass paid for it; the office settles it at the desk. free: it costs nothing (a class member's booking, a trial, a lesson the school canceled, a timely cancellation, a removal by staff). pending: the school uses passes on attendance, which has not been taken yet." enum:"paid,unpaid,free,pending" json:"status"`
	Pass   *PaymentPass `doc:"The pass that paid; only when paid." json:"pass,omitempty"`
}

// PaymentPass is the pass that paid for a booking.
type PaymentPass struct {
	ID              domain.PassID `doc:"Pass ID."                    json:"id"`
	Name            string        `doc:"Name of its type."          json:"name"`
	Kind            string        `doc:"visits or time."             enum:"visits,time" json:"kind"`
	VisitsRemaining int           `doc:"Visits left of a visit pass; 0 for a time pass." json:"visitsRemaining"`
	ValidUntil      domain.Date   `doc:"Last school date it pays for; absent without expiry." format:"date" json:"validUntil,omitzero"`
}

// PassTypeList is the response body of listPassTypes.
type PassTypeList struct {
	Items []PassType `doc:"All pass types ordered by name." json:"items" nullable:"false"`
}

// PassTypeListOutput is the response of listPassTypes.
type PassTypeListOutput struct {
	Body PassTypeList
}

// PassTypeDataInput is the request of createPassType.
type PassTypeDataInput struct {
	Body PassTypeData
}

// PassTypeUpdateInput is the request of updatePassType.
type PassTypeUpdateInput struct {
	PassTypeID domain.PassTypeID `doc:"The pass type." path:"passTypeId"`
	Body       PassTypeData
}

// PassTypeRefInput is the request of retirePassType and deletePassType.
type PassTypeRefInput struct {
	PassTypeID domain.PassTypeID `doc:"The pass type." path:"passTypeId"`
}

// PassTypeOutput is the response of createPassType and updatePassType.
type PassTypeOutput struct {
	Body PassType
}

// PersonPassesInput is the request of listPersonPasses.
type PersonPassesInput struct {
	PersonID domain.PersonID `doc:"The person." path:"personId"`
}

// AdminPassList is the response body of listPersonPasses.
type AdminPassList struct {
	Items []AdminPass `doc:"The person's passes: usable first, then the others, each newest sale first." json:"items" nullable:"false"`
}

// AdminPassListOutput is the response of listPersonPasses.
type AdminPassListOutput struct {
	Body AdminPassList
}

// SellPassData is the request body of sellPass.
type SellPassData struct {
	PassTypeID domain.PassTypeID `doc:"The pass type; it must be active." json:"passTypeId"`
	PayerID    domain.PersonID   `doc:"Who paid; default: the person." json:"payerId,omitzero" required:"false"`
	ValidFrom  domain.Date       `doc:"First school date of the pass; default: today." format:"date" json:"validFrom,omitzero" required:"false"`
	Price      *int64            `doc:"Price paid in minor units; default: the type's price." json:"price,omitempty" maximum:"100000000" minimum:"0" required:"false"`
	Note       string            `doc:"A note for the office." json:"note,omitempty" maxLength:"500" required:"false"`
	// The receipt (ADR 0012).
	PaymentMethod    string `default:"cash" doc:"How it was paid; printed on the receipt." enum:"cash,transfer,card" json:"paymentMethod,omitempty" required:"false"`
	RecipientName    string `doc:"The name the receipt prints as the payer; default: the payer's name." json:"recipientName,omitempty" maxLength:"200" required:"false"`
	RecipientAddress string `doc:"The payer's address printed on the receipt, one line per printed line; none by default." json:"recipientAddress,omitempty" maxLength:"500" required:"false"`
	RecipientEmail   string `doc:"Where the receipt is mailed if the payer has no account; ignored (not stored) if the payer has one, which gets the receipt." json:"recipientEmail,omitempty" maxLength:"254" required:"false"`
}

// SellPassInput is the request of sellPass.
type SellPassInput struct {
	PersonID domain.PersonID `doc:"The person who gets the pass." path:"personId"`
	Body     SellPassData
}

// AssignPassData is the request body of assignPass.
type AssignPassData struct {
	PassTypeID domain.PassTypeID `doc:"The pass type; it must be active." json:"passTypeId"`
	ValidFrom  domain.Date       `doc:"First school date of the pass; default: today." format:"date" json:"validFrom,omitzero" required:"false"`
	Note       string            `doc:"Why it is given away." json:"note,omitempty" maxLength:"500" required:"false"`
}

// AssignPassInput is the request of assignPass.
type AssignPassInput struct {
	PersonID domain.PersonID `doc:"The person who gets the pass." path:"personId"`
	Body     AssignPassData
}

// PassRefInput is the request of voidPass.
type PassRefInput struct {
	PassID domain.PassID `doc:"The pass." path:"passId"`
}

// AdminPassOutput is the response of sellPass, assignPass and voidPass.
type AdminPassOutput struct {
	Body AdminPass
}

// PassSettings is the request and response body of the pass settings.
type PassSettings struct {
	RedemptionMode string `doc:"When bookings use a pass. booking: when booked; a timely cancellation gives the visit back, a late one keeps it. attendance: when marked attended or no-show, or canceled late." enum:"booking,attendance" json:"redemptionMode"`
}

// PassSettingsInput is the request of updatePassSettings.
type PassSettingsInput struct {
	Body PassSettings
}

// PassSettingsOutput is the response of getPassSettings and
// updatePassSettings.
type PassSettingsOutput struct {
	Body PassSettings
}

// MyPasses is the response body of listMyPasses.
type MyPasses struct {
	Timezone       string      `doc:"IANA time zone of the school; school dates are local to it." json:"timezone"`
	Today          domain.Date `doc:"Today's school date, which usable refers to." format:"date" json:"today"`
	RedemptionMode string      `doc:"When bookings use a pass. booking: when booked; a timely cancellation gives the visit back, a late one keeps it. attendance: when marked attended or no-show, or canceled late." enum:"booking,attendance" json:"redemptionMode"`
	Items          []Pass      `doc:"The family's passes: usable first, then the others, each newest sale first." json:"items" nullable:"false"`
}

// MyPassesOutput is the response of listMyPasses.
type MyPassesOutput struct {
	Body MyPasses
}

func (h *handlers) listPassTypes(ctx context.Context, _ *struct{}) (*PassTypeListOutput, error) {
	types, err := h.deps.Passes.PassTypes(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]PassType, 0, len(types))
	for _, t := range types {
		items = append(items, passTypeBody(t))
	}

	return &PassTypeListOutput{Body: PassTypeList{Items: items}}, nil
}

func (h *handlers) createPassType(ctx context.Context, in *PassTypeDataInput) (*PassTypeOutput, error) {
	return h.passTypeOutput(ctx)(h.deps.Passes.CreatePassType(ctx, in.Body.service()))
}

func (h *handlers) updatePassType(ctx context.Context, in *PassTypeUpdateInput) (*PassTypeOutput, error) {
	return h.passTypeOutput(ctx)(h.deps.Passes.UpdatePassType(ctx, in.PassTypeID, in.Body.service()))
}

func (h *handlers) retirePassType(ctx context.Context, in *PassTypeRefInput) (*PassTypeOutput, error) {
	return h.passTypeOutput(ctx)(h.deps.Passes.RetirePassType(ctx, in.PassTypeID))
}

func (h *handlers) deletePassType(ctx context.Context, in *PassTypeRefInput) (*struct{}, error) {
	if err := h.deps.Passes.DeletePassType(ctx, in.PassTypeID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) listPersonPasses(ctx context.Context, in *PersonPassesInput) (*AdminPassListOutput, error) {
	views, err := h.deps.Passes.PersonPasses(ctx, in.PersonID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]AdminPass, 0, len(views))
	for _, v := range views {
		items = append(items, adminPassBody(v))
	}

	return &AdminPassListOutput{Body: AdminPassList{Items: items}}, nil
}

func (h *handlers) sellPass(ctx context.Context, in *SellPassInput) (*AdminPassOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	b := in.Body
	sale := service.PassSale{
		PassTypeID: b.PassTypeID, PayerID: b.PayerID, ValidFrom: b.ValidFrom, Note: b.Note,
		PaymentMethod: domain.PaymentMethod(cmp.Or(b.PaymentMethod, string(domain.PaymentCash))),
		Recipient:     service.ReceiptRecipient{Name: b.RecipientName, Address: b.RecipientAddress, Email: b.RecipientEmail},
	}
	if b.Price != nil {
		price := domain.Money(*b.Price)
		sale.Price = &price
	}

	return h.adminPassOutput(ctx)(h.deps.Passes.Sell(ctx, p, in.PersonID, sale))
}

func (h *handlers) assignPass(ctx context.Context, in *AssignPassInput) (*AdminPassOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	return h.adminPassOutput(ctx)(h.deps.Passes.Assign(ctx, p, in.PersonID, service.PassAssignment{
		PassTypeID: in.Body.PassTypeID, ValidFrom: in.Body.ValidFrom, Note: in.Body.Note,
	}))
}

func (h *handlers) voidPass(ctx context.Context, in *PassRefInput) (*AdminPassOutput, error) {
	return h.adminPassOutput(ctx)(h.deps.Passes.Void(ctx, in.PassID))
}

func (h *handlers) getPassSettings(ctx context.Context, _ *struct{}) (*PassSettingsOutput, error) {
	mode, err := h.deps.Passes.RedemptionMode(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &PassSettingsOutput{Body: PassSettings{RedemptionMode: string(mode)}}, nil
}

func (h *handlers) updatePassSettings(ctx context.Context, in *PassSettingsInput) (*PassSettingsOutput, error) {
	if err := h.deps.Passes.SetRedemptionMode(ctx, domain.RedemptionMode(in.Body.RedemptionMode)); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return h.getPassSettings(ctx, nil)
}

func (h *handlers) listMyPasses(ctx context.Context, _ *struct{}) (*MyPassesOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	mine, err := h.deps.Passes.Mine(ctx, p)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]Pass, 0, len(mine.Passes))
	for _, v := range mine.Passes {
		items = append(items, passBody(v))
	}

	return &MyPassesOutput{Body: MyPasses{
		Timezone: mine.Timezone, Today: mine.Today, RedemptionMode: string(mine.RedemptionMode), Items: items,
	}}, nil
}

func (h *handlers) passTypeOutput(ctx context.Context) func(domain.PassType, error) (*PassTypeOutput, error) {
	return func(t domain.PassType, err error) (*PassTypeOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &PassTypeOutput{Body: passTypeBody(t)}, nil
	}
}

func (h *handlers) adminPassOutput(ctx context.Context) func(service.PassView, error) (*AdminPassOutput, error) {
	return func(v service.PassView, err error) (*AdminPassOutput, error) {
		if errors.Is(err, service.ErrPassVoided) {
			return nil, conflict(reasonAlreadyVoided, "the pass is voided already")
		} else if err != nil {
			return nil, h.receiptError(ctx, err)
		}

		return &AdminPassOutput{Body: adminPassBody(v)}, nil
	}
}

func (d PassTypeData) service() service.PassTypeData {
	return service.PassTypeData{
		Name: d.Name, Kind: domain.PassKind(d.Kind), Visits: d.Visits, ValidityDays: d.ValidityDays,
		Price: domain.Money(d.Price), VATRate: d.VATRate, Active: d.Active,
	}
}

func passTypeBody(t domain.PassType) PassType {
	return PassType{
		ID: t.ID, CreatedAt: t.CreatedAt.UTC(),
		PassTypeData: PassTypeData{
			Name: t.Name, Kind: string(t.Kind), Visits: t.Visits, ValidityDays: t.ValidityDays, Price: int64(t.Price),
			VATRate: t.VATRate, Active: t.Active,
		},
	}
}

func passBody(v service.PassView) Pass {
	p := v.Pass
	out := Pass{
		ID: p.ID, PassType: PassTypeRef{ID: v.PassType.ID, Name: v.PassType.Name},
		Owner: PassPerson{ID: p.PersonID, Name: v.OwnerName}, Kind: string(p.Kind),
		VisitsTotal: p.VisitsTotal, VisitsRemaining: p.VisitsRemaining, ValidFrom: p.ValidFrom, ValidUntil: p.ValidUntil,
		Status: string(p.Status), Usable: v.Usable, Origin: string(p.Origin), SoldAt: p.SoldAt.UTC(),
		PricePaid: int64(p.PricePaid), VoidedAt: p.VoidedAt.UTC(), ReceiptID: p.ReceiptID, Uses: make([]PassUse, 0, len(v.Uses)),
		UseCount: v.UseCount,
	}

	if !p.PayerID.IsZero() {
		out.Payer = &PassPerson{ID: p.PayerID, Name: v.PayerName}
	}

	for _, u := range v.Uses {
		out.Uses = append(out.Uses, PassUse{
			BookingID: u.Booking.ID, PersonName: v.OwnerName, BookingStatus: string(u.Booking.Status),
			Late: u.Booking.Late, RedeemedAt: u.Redemption.At.UTC(), Lesson: lessonSummary(u.Lesson),
		})
	}

	return out
}

func adminPassBody(v service.PassView) AdminPass {
	return AdminPass{Pass: passBody(v), Note: v.Pass.Note}
}

// paymentBody is the API form of a booking's payment.
func paymentBody(pay service.BookingPayment) BookingPayment {
	out := BookingPayment{Status: string(pay.Status)}
	if pay.Status == service.PaymentPaid {
		out.Pass = &PaymentPass{
			ID: pay.Pass.ID, Name: pay.PassType.Name, Kind: string(pay.Pass.Kind),
			VisitsRemaining: pay.Pass.VisitsRemaining, ValidUntil: pay.Pass.ValidUntil,
		}
	}

	return out
}
