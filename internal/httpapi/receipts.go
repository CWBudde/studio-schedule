package httpapi

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// registerReceipts registers the receipts (ADR 0012): the office's list,
// details, PDFs and cancellations, the members' receipts, and the receipt
// settings.
func registerReceipts(api huma.API, h *handlers) {
	tags := []string{"receipts"}
	pdfResponses := func(description string) map[string]*huma.Response {
		return map[string]*huma.Response{
			"200": {
				Description: description,
				Content:     map[string]*huma.MediaType{"application/pdf": {Schema: &huma.Schema{Type: "string", Format: "binary"}}},
			},
			"default": problem(api, reflect.TypeFor[ErrorModel](), "Error"),
		}
	}

	register(api, AccessOffice, huma.Operation{
		OperationID: "searchReceipts",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/receipts/search",
		Summary:     "Search receipts",
		Description: "Returns the receipts that match the filter, newest first (by number), and how many match in " +
			"all. A POST, so that the names searched for stay out of URLs and the access logs of proxies " +
			"(ASVS 8.3.1); it changes nothing.",
		Tags: tags,
	}, h.searchReceipts)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getReceipt",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/receipts/{receiptId}",
		Summary:     "Get a receipt",
		Description: "Returns a receipt with its lines, the cancellation that canceled it or the receipt it cancels, " +
			"and whether it can be canceled. An unknown receipt is a 404.",
		Tags: tags,
	}, h.getReceipt)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getReceiptPdf",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/receipts/{receiptId}/pdf",
		Summary:     "Download a receipt",
		Description: "Returns the PDF of a receipt as it was issued (an attachment named after its number). An " +
			"unknown receipt is a 404.",
		Tags:      tags,
		Responses: pdfResponses("The receipt's PDF."),
	}, h.getReceiptPdf)

	register(api, AccessOffice, withConflict(api, huma.Operation{
		OperationID: "cancelReceipt",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/receipts/{receiptId}/cancel",
		Summary:     "Cancel a receipt",
		Description: "Issues the cancellation receipt (Storno) of a sale: the next number, every line negated, the " +
			"same payer, recipient and payment method, mailed to the payer like the receipt. It voids the pass " +
			"the receipt confirms. A pass that paid for visits is a 409 `pass-used` (void the pass instead); a " +
			"receipt canceled before a 409 `already-canceled`, a cancellation a 409 `cancellation`, incomplete " +
			"receipt settings a 409 `receipt-settings-incomplete`. An unknown receipt is a 404.",
		Tags:          tags,
		DefaultStatus: http.StatusCreated,
	}), h.cancelReceipt)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "listMyReceipts",
		Method:      http.MethodGet,
		Path:        BasePath + "/my/receipts",
		Summary:     "List my receipts",
		Description: "Returns the receipts of the passes the signed-in person paid for, newest first.",
		Tags:        tags,
	}, h.listMyReceipts)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "getMyReceiptPdf",
		Method:      http.MethodGet,
		Path:        BasePath + "/my/receipts/{receiptId}/pdf",
		Summary:     "Download my receipt",
		Description: "Returns the PDF of a receipt the signed-in person paid; any other is a 404.",
		Tags:        tags,
		Responses:   pdfResponses("The receipt's PDF."),
	}, h.getMyReceiptPdf)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getReceiptSettings",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/settings/receipts",
		Summary:     "Receipt settings",
		Description: "Returns the issuer, the tax mode and the texts printed on receipts, and whether they are " +
			"complete: passes are sold only when they are.",
		Tags: tags,
	}, h.getReceiptSettings)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "updateReceiptSettings",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/settings/receipts",
		Summary:     "Update the receipt settings",
		Description: "Replaces the receipt settings and the number prefix. Receipts issued before keep what they " +
			"were issued with. Invalid fields are a 422 on them; incomplete settings are allowed, but block sales.",
		Tags: tags,
	}, h.updateReceiptSettings)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getReceiptLogo",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/settings/receipts/logo",
		Summary:     "Receipt logo",
		Description: "Returns the logo printed on receipts; a 404 without one.",
		Tags:        tags,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The logo.",
				Content: map[string]*huma.MediaType{
					domain.LogoPNG:  {Schema: &huma.Schema{Type: "string", Format: "binary"}},
					domain.LogoJPEG: {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
			"default": problem(api, reflect.TypeFor[ErrorModel](), "Error"),
		},
	}, h.getReceiptLogo)

	register(api, AccessAdmin, huma.Operation{
		OperationID: "updateReceiptLogo",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/settings/receipts/logo",
		Summary:     "Set the receipt logo",
		Description: "Sets the logo printed on receipts from now on: a PNG or JPEG of at most 512 KiB, base64 " +
			"encoded. A larger image, or data that does not match the type, is a 422 on `body.data`.",
		Tags:          tags,
		DefaultStatus: http.StatusNoContent,
	}, h.updateReceiptLogo)

	register(api, AccessAdmin, huma.Operation{
		OperationID:   "deleteReceiptLogo",
		Method:        http.MethodDelete,
		Path:          BasePath + "/admin/settings/receipts/logo",
		Summary:       "Remove the receipt logo",
		Description:   "Removes the logo; later receipts have none.",
		Tags:          tags,
		DefaultStatus: http.StatusNoContent,
	}, h.deleteReceiptLogo)
}

// ReceiptIssuer is who issued a receipt.
type ReceiptIssuer struct {
	Name    string `doc:"Legal name."                                  json:"name"`
	Address string `doc:"Address, one line per printed line."         json:"address"`
	TaxID   string `doc:"Tax number or VAT ID."                       json:"taxNumber"`
	Email   string `doc:"Contact email; absent if none."              json:"email,omitempty"`
	Phone   string `doc:"Contact phone; absent if none."              json:"phone,omitempty"`
}

// ReceiptLine is one position of a receipt.
type ReceiptLine struct {
	Position    int    `doc:"Position, from 1."                                                 json:"position"`
	Description string `doc:"What was paid."                                                    json:"description"`
	Quantity    int    `doc:"Quantity."                                                         json:"quantity"`
	UnitPrice   int64  `doc:"Gross price of one unit in minor units (negative on a cancellation)." json:"unitPrice"`
	VATRate     int    `doc:"VAT rate charged, in basis points (0 unless the tax mode is standard)." json:"vatRate"`
	Net         int64  `doc:"Net amount in minor units."                                        json:"net"`
	VAT         int64  `doc:"VAT in minor units."                                               json:"vat"`
	Gross       int64  `doc:"Gross amount (quantity × unit price) in minor units."              json:"gross"`
}

// ReceiptRef names another receipt.
type ReceiptRef struct {
	ID        domain.ReceiptID `doc:"Receipt ID."       json:"id"`
	Number    string           `doc:"Receipt number."   json:"number"`
	IssueDate domain.Date      `doc:"School date it was issued." format:"date" json:"issueDate"`
}

// Receipt is an issued receipt, immutable.
type Receipt struct {
	ID               domain.ReceiptID `doc:"Receipt ID." json:"id"`
	Number           string           `doc:"Printed number, e.g. SD-2026-0001: the prefix, the year and a gapless sequence per year." json:"number"`
	Kind             string           `doc:"sale: confirms the payment of a pass or an event fee; cancellation: cancels a sale receipt (Storno), every amount negated." enum:"sale,cancellation" json:"kind"`
	IssuedAt         time.Time        `doc:"When it was issued." json:"issuedAt"`
	IssueDate        domain.Date      `doc:"School date it was issued." format:"date" json:"issueDate"`
	Issuer           ReceiptIssuer    `json:"issuer"`
	TaxMode          string           `doc:"The school's tax mode at issuance." enum:"small_business,exempt,standard" json:"taxMode"`
	TaxNote          string           `doc:"The tax note printed; absent if none." json:"taxNote,omitempty"`
	Payer            *PassPerson      `doc:"Who paid." json:"payer,omitempty"`
	RecipientName    string           `doc:"The name printed as the payer." json:"recipientName"`
	RecipientAddress string           `doc:"The address printed; absent if none." json:"recipientAddress,omitempty"`
	RecipientEmail   string           `doc:"The address typed at the sale that the receipt was mailed to, because the payer has no account; absent otherwise (a payer with an account gets it there)." json:"recipientEmail,omitempty"`
	PaymentMethod    string           `doc:"How it was paid (or refunded)." enum:"cash,transfer,card" json:"paymentMethod"`
	Currency         string           `doc:"ISO 4217 currency of the amounts." json:"currency"`
	Net              int64            `doc:"Net total in minor units." json:"net"`
	VAT              int64            `doc:"VAT total in minor units." json:"vat"`
	Gross            int64            `doc:"Total in minor units." json:"gross"`
	Reason           string           `doc:"Why a cancellation was issued; absent if not given." json:"reason,omitempty"`
	Lines            []ReceiptLine    `doc:"The lines." json:"lines" nullable:"false"`
	Cancels          *ReceiptRef      `doc:"The receipt a cancellation cancels." json:"cancels,omitempty"`
	CanceledBy       *ReceiptRef      `doc:"The cancellation of a receipt, if canceled." json:"canceledBy,omitempty"`
	PassID           domain.PassID    `doc:"The pass a sale receipt confirms." json:"passId,omitzero"`
	EventID          domain.EventID   `doc:"The event whose fee a sale receipt, or the receipt a cancellation cancels, confirms." json:"eventId,omitzero"`
	EventTitle       string           `doc:"That event's title." json:"eventTitle,omitempty"`
	Cancelable       bool             `doc:"Whether the office can cancel it now: a sale, not canceled, whose pass paid for no visit and whose event has not started." json:"cancelable"`
}

func receiptRef(r *domain.Receipt) *ReceiptRef {
	if r == nil {
		return nil
	}

	return &ReceiptRef{ID: r.ID, Number: r.Number, IssueDate: r.IssueDate}
}

func receiptBody(v service.ReceiptView) Receipt {
	r := v.Receipt
	out := Receipt{
		ID: r.ID, Number: r.Number, Kind: string(r.Kind), IssuedAt: r.IssuedAt.UTC(), IssueDate: r.IssueDate,
		Issuer: ReceiptIssuer{
			Name: r.Issuer.Name, Address: r.Issuer.Address, TaxID: r.Issuer.TaxID, Email: r.Issuer.Email,
			Phone: r.Issuer.Phone,
		},
		TaxMode: string(r.TaxMode), TaxNote: r.TaxNote, RecipientName: r.RecipientName,
		RecipientAddress: r.RecipientAddress, RecipientEmail: r.RecipientEmail,
		PaymentMethod: string(r.PaymentMethod), Currency: r.Currency, Net: int64(r.Net), VAT: int64(r.VAT),
		Gross: int64(r.Gross), Reason: r.Reason, Lines: make([]ReceiptLine, len(r.Lines)),
		Cancels: receiptRef(v.Cancels), CanceledBy: receiptRef(v.CanceledBy), PassID: v.Pass.ID,
		EventID: v.EventID, EventTitle: v.EventTitle, Cancelable: v.Cancelable(),
	}

	if !r.PayerID.IsZero() {
		out.Payer = &PassPerson{ID: r.PayerID, Name: v.PayerName, Anonymized: v.PayerAnonymized}
	}

	for i, l := range r.Lines {
		out.Lines[i] = ReceiptLine{
			Position: l.Position, Description: l.Description, Quantity: l.Quantity, UnitPrice: int64(l.UnitPrice),
			VATRate: l.VATRate, Net: int64(l.Net), VAT: int64(l.VAT), Gross: int64(l.Gross),
		}
	}

	return out
}

// SearchReceiptsInput is the request of searchReceipts.
type SearchReceiptsInput struct {
	Body struct {
		Year   int    `doc:"Only the receipts of this year; 0 or absent: all." json:"year,omitempty" maximum:"9999" minimum:"0"`
		Query  string `doc:"Matches the number or the recipient's name (ignoring ASCII case); empty or absent matches all." json:"query,omitempty" maxLength:"100"`
		Offset int    `doc:"Receipts to skip." json:"offset,omitempty" minimum:"0"`
		Limit  int    `default:"50" doc:"Most receipts to return." json:"limit,omitempty" maximum:"100" minimum:"1"`
	}
}

// ReceiptList is the response body of searchReceipts.
type ReceiptList struct {
	Items []Receipt `doc:"The receipts, newest first." json:"items" nullable:"false"`
	Total int       `doc:"How many receipts match in all." json:"total"`
	Years []int     `doc:"The years with receipts, the latest first, whatever the filter: the choices of the year filter." json:"years" nullable:"false"`
}

// ReceiptListOutput is the response of searchReceipts.
type ReceiptListOutput struct {
	Body ReceiptList
}

// ReceiptRefInput names a receipt in the path.
type ReceiptRefInput struct {
	ReceiptID domain.ReceiptID `doc:"The receipt." path:"receiptId"`
}

// ReceiptOutput is the response of getReceipt and cancelReceipt.
type ReceiptOutput struct {
	Body Receipt
}

// CancelReceiptInput is the request of cancelReceipt.
type CancelReceiptInput struct {
	ReceiptID domain.ReceiptID `doc:"The receipt to cancel." path:"receiptId"`
	Body      struct {
		Reason string `doc:"Why; printed on the cancellation." json:"reason,omitempty" maxLength:"200" required:"false"`
	}
}

// FileOutput is a downloaded file.
type FileOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	CacheControl       string `header:"Cache-Control"`
	Body               []byte
}

// MyReceiptList is the response body of listMyReceipts.
type MyReceiptList struct {
	Items []Receipt `doc:"The receipts of what the signed-in person paid, newest first." json:"items" nullable:"false"`
}

// MyReceiptListOutput is the response of listMyReceipts.
type MyReceiptListOutput struct {
	Body MyReceiptList
}

// ReceiptSettingsData is the editable part of the receipt settings.
type ReceiptSettingsData struct {
	IssuerName    string `doc:"The issuer's legal name." json:"issuerName,omitempty" maxLength:"200" required:"false"`
	IssuerAddress string `doc:"The issuer's address, one line per printed line." json:"issuerAddress,omitempty" maxLength:"500" required:"false"`
	TaxID         string `doc:"Tax number or VAT ID." json:"taxNumber,omitempty" maxLength:"50" required:"false"`
	Email         string `doc:"Contact email printed on receipts." json:"email,omitempty" maxLength:"254" required:"false"`
	Phone         string `doc:"Contact phone printed on receipts." json:"phone,omitempty" maxLength:"50" required:"false"`
	TaxMode       string `doc:"small_business: no VAT, with the § 19 UStG note (or taxNote); exempt: no VAT, with taxNote naming the legal basis; standard: the VAT rate of each pass type is included in its price, and net and VAT are printed. Absent until chosen." enum:"small_business,exempt,standard" json:"taxMode,omitempty" required:"false"`
	TaxNote       string `doc:"Printed on every receipt; required in the exempt mode." json:"taxNote,omitempty" maxLength:"500" required:"false"`
	Footer        string `doc:"Printed at the bottom, e.g. bank details." json:"footer,omitempty" maxLength:"500" required:"false"`
	ReceiptPrefix string `doc:"Starts every receipt number, 1–16 of A–Z and 0–9." json:"receiptPrefix" maxLength:"16" minLength:"1"`
}

// ReceiptSettingsBody is the response body of the receipt settings.
type ReceiptSettingsBody struct {
	ReceiptSettingsData

	Complete bool   `doc:"Whether receipts, and so pass sales, can be issued." json:"complete"`
	Missing  string `doc:"The first setting receipts need that is missing; absent when complete." enum:"issuerName,issuerAddress,taxNumber,taxMode,taxNote" json:"missing,omitempty"`
	LogoType string `doc:"The content type of the logo; absent without one." enum:"image/png,image/jpeg" json:"logoType,omitempty"`
	// Hints for the forms.
	LogoMaxBytes         int   `doc:"The largest logo updateReceiptLogo takes, in bytes (before base64)." json:"logoMaxBytes"`
	AddressRequiredAbove int64 `doc:"The gross amount in minor units above which a receipt should name the recipient's address; absent if no such limit applies. Set in the standard tax mode in euros: the German small-amount invoice limit, 250 € (§ 33 UStDV). A hint for the sale form; sales above it are not refused." json:"addressRequiredAbove,omitempty"`
}

// ReceiptSettingsInput is the request of updateReceiptSettings.
type ReceiptSettingsInput struct {
	Body ReceiptSettingsData
}

// ReceiptSettingsOutput is the response of the receipt settings.
type ReceiptSettingsOutput struct {
	Body ReceiptSettingsBody
}

// ReceiptLogoInput is the request of updateReceiptLogo.
type ReceiptLogoInput struct {
	Body struct {
		ContentType string `doc:"The image type." enum:"image/png,image/jpeg" json:"contentType"`
		Data        []byte `doc:"The image, base64 encoded; at most 512 KiB." json:"data" maxLength:"699052"`
	}
}

func (h *handlers) searchReceipts(ctx context.Context, in *SearchReceiptsInput) (*ReceiptListOutput, error) {
	f := in.Body
	page, err := h.deps.Receipts.List(ctx, store.ReceiptFilter{
		Year: f.Year, Query: f.Query, Offset: f.Offset, Limit: f.Limit,
	})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &ReceiptListOutput{Body: NewReceiptList(page)}, nil
}

// NewReceiptList is the response body of searchReceipts for page; the
// command line prints it too (ADR 0020).
func NewReceiptList(page service.ReceiptPage) ReceiptList {
	out := ReceiptList{
		Items: make([]Receipt, len(page.Items)), Total: page.Total, Years: append([]int{}, page.Years...),
	}
	for i, v := range page.Items {
		out.Items[i] = receiptBody(v)
	}

	return out
}

func (h *handlers) getReceipt(ctx context.Context, in *ReceiptRefInput) (*ReceiptOutput, error) {
	v, err := h.deps.Receipts.Get(ctx, in.ReceiptID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &ReceiptOutput{Body: receiptBody(v)}, nil
}

func (h *handlers) cancelReceipt(ctx context.Context, in *CancelReceiptInput) (*ReceiptOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	v, err := h.deps.Receipts.Cancel(ctx, p, in.ReceiptID, in.Body.Reason)
	if err != nil {
		return nil, h.receiptError(ctx, err)
	}

	return &ReceiptOutput{Body: receiptBody(v)}, nil
}

// receiptError maps the conflicts of receipts (and sales), else like
// toHTTPError.
func (h *handlers) receiptError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrReceiptSettingsIncomplete):
		return conflict(reasonReceiptSettings, settingsMissing(err))
	case errors.Is(err, service.ErrPassUsed):
		return conflict(reasonPassUsed, "the pass paid for visits; void the pass instead")
	case errors.Is(err, service.ErrReceiptCanceled):
		return conflict(reasonAlreadyCanceled, "the receipt is canceled already")
	case errors.Is(err, service.ErrCancelCancellation):
		return conflict(reasonCancellation, "a cancellation cannot be canceled")
	case errors.Is(err, service.ErrEventAttended):
		return conflict(reasonEventAttended, "the event has started; cancel the registration with a refund instead")
	default:
		return h.toHTTPError(ctx, err)
	}
}

// settingsMissing is the detail of a receipt-settings-incomplete conflict:
// the service's message, which names the missing setting and nothing else.
func settingsMissing(err error) string {
	const prefix = "the receipt settings are incomplete"

	if msg := err.Error(); strings.Contains(msg, prefix) {
		return msg[strings.Index(msg, prefix):]
	}

	return prefix
}

func (h *handlers) getReceiptPdf(ctx context.Context, in *ReceiptRefInput) (*FileOutput, error) {
	return h.fileOutput(ctx)(h.deps.Receipts.PDF(ctx, in.ReceiptID))
}

func (h *handlers) listMyReceipts(ctx context.Context, _ *struct{}) (*MyReceiptListOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	views, err := h.deps.Receipts.Mine(ctx, p)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &MyReceiptListOutput{Body: MyReceiptList{Items: make([]Receipt, len(views))}}
	for i, v := range views {
		out.Body.Items[i] = receiptBody(v)
	}

	return out, nil
}

func (h *handlers) getMyReceiptPdf(ctx context.Context, in *ReceiptRefInput) (*FileOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	return h.fileOutput(ctx)(h.deps.Receipts.MyPDF(ctx, p, in.ReceiptID))
}

// fileOutput returns a PDF as an attachment named after the receipt; it
// never changes, but holds personal data, so caches keep it private.
func (h *handlers) fileOutput(ctx context.Context) func(service.ReceiptFile, error) (*FileOutput, error) {
	return func(f service.ReceiptFile, err error) (*FileOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &FileOutput{
			ContentType: service.PDFContentType, ContentDisposition: `attachment; filename="` + f.Filename + `"`,
			CacheControl: "private, no-store", Body: f.PDF,
		}, nil
	}
}

func (h *handlers) getReceiptSettings(ctx context.Context, _ *struct{}) (*ReceiptSettingsOutput, error) {
	s, err := h.deps.Receipts.Settings(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &ReceiptSettingsOutput{Body: receiptSettingsBody(s)}, nil
}

func receiptSettingsBody(s service.ReceiptSettings) ReceiptSettingsBody {
	rs := s.Settings

	return ReceiptSettingsBody{
		ReceiptSettingsData: ReceiptSettingsData{
			IssuerName: rs.Issuer.Name, IssuerAddress: rs.Issuer.Address, TaxID: rs.Issuer.TaxID,
			Email: rs.Issuer.Email, Phone: rs.Issuer.Phone, TaxMode: string(rs.TaxMode), TaxNote: rs.TaxNote,
			Footer: rs.Footer, ReceiptPrefix: s.Prefix,
		},
		Complete: s.Missing == "", Missing: s.Missing, LogoType: s.LogoType,
		LogoMaxBytes: domain.MaxLogoBytes, AddressRequiredAbove: int64(s.AddressRequiredAbove),
	}
}

func (h *handlers) updateReceiptSettings(ctx context.Context, in *ReceiptSettingsInput) (*ReceiptSettingsOutput, error) {
	u := in.Body.Update()

	s, err := h.deps.Receipts.SetSettings(ctx, u.Settings, u.Prefix)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &ReceiptSettingsOutput{Body: receiptSettingsBody(s)}, nil
}

func (h *handlers) getReceiptLogo(ctx context.Context, _ *struct{}) (*FileOutput, error) {
	l, err := h.deps.Receipts.Logo(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	if l.IsZero() {
		return nil, huma.Error404NotFound("no logo")
	}

	return &FileOutput{ContentType: l.ContentType, ContentDisposition: "inline", CacheControl: "no-cache", Body: l.Data}, nil
}

func (h *handlers) updateReceiptLogo(ctx context.Context, in *ReceiptLogoInput) (*struct{}, error) {
	if len(in.Body.Data) == 0 {
		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "the logo is empty", Location: "body.data",
		})
	}

	if err := h.deps.Receipts.SetLogo(ctx, domain.Logo{ContentType: in.Body.ContentType, Data: in.Body.Data}); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) deleteReceiptLogo(ctx context.Context, _ *struct{}) (*struct{}, error) {
	if err := h.deps.Receipts.SetLogo(ctx, domain.Logo{}); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}
