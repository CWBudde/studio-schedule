package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerDuplicates registers the duplicate detection and the merge of
// persons (ADR 0015).
func registerDuplicates(api huma.API, h *handlers) {
	register(api, AccessOffice, huma.Operation{
		OperationID: "listDuplicates",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/duplicates",
		Summary:     "List possible duplicates",
		Description: fmt.Sprintf("Returns pairs of persons that may be the same, at most %d: strong ones "+
			"(the same email, a person's or its account's, ignoring case; or the same name and birth date) "+
			"first, then weak ones (the same name, a birth date unknown). Names compare ignoring case, "+
			"accents and hyphens, with umlauts spelled out. A guardian and its dependent, or two dependents of "+
			"one guardian, do not match by the email alone. Dismissed pairs and merged persons are left out.",
			service.MaxDuplicatePairs),
		Tags: []string{"admin"},
	}, h.listDuplicates)

	register(api, AccessOffice, huma.Operation{
		OperationID: "dismissDuplicate",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/duplicates/dismissals",
		Summary:     "Dismiss a pair",
		Description: "Records that two persons are not the same, so the pair is no longer listed. Dismissing a " +
			"pair again changes nothing; an unknown person is a 404.",
		Tags:          []string{"admin"},
		DefaultStatus: http.StatusNoContent,
	}, h.dismissDuplicate)

	register(api, AccessOffice, withConflict(api, huma.Operation{
		OperationID: "mergePersons",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/persons/merge",
		Summary:     "Merge two persons",
		Description: "Merges a duplicate person into the survivor: its bookings, waiting-list entries, class " +
			"memberships, passes (owned and paid for), guardian links, staff links and account move to the " +
			"survivor. Where both have a booking of the same lesson that has not started, the duplicate's is " +
			"canceled (a visit it cost comes back); of a lesson that has started, both stay as history. Where " +
			"both are members of a class, the survivor's membership stays, and the duplicate's automatic " +
			"bookings of the class follow it. The survivor takes the fields `take` names from the duplicate " +
			"and keeps both notes, or the `notes` given (at most 2000 characters, else a 422); the duplicate " +
			"stays as a hidden record for its receipts, which are listed under the survivor. Without `apply` " +
			"it only tells what would happen. Both with an account is a 409 `two-accounts` (disable one " +
			"first: a disabled account of the duplicate stays with it); a merge after which a guardian would " +
			"also be a dependent of the survivor a 409 " +
			"`guardian-cycle`. Recorded in the audit log.",
		Tags: []string{"admin"},
	}), h.mergePersons)
}

// DuplicateCandidate is a person of a pair.
type DuplicateCandidate struct {
	AdminPerson

	AccountEmail string `doc:"The email of the person's account; absent without one." json:"accountEmail,omitempty"`
}

// DuplicatePair is two persons that may be the same.
type DuplicatePair struct {
	A        DuplicateCandidate `json:"a"`
	B        DuplicateCandidate `json:"b"`
	Strength string             `doc:"strong: the same email, or the same name and birth date; weak: the same name, a birth date unknown." enum:"strong,weak" json:"strength"`
	Reasons  []string           `doc:"What matched." enum:"email,name-birth-date,name" json:"reasons" nullable:"false"`
}

// DuplicateList is the response body of listDuplicates.
type DuplicateList struct {
	Items []DuplicatePair `doc:"The pairs, strong ones first." json:"items" nullable:"false"`
}

// DuplicateListOutput is the response of listDuplicates.
type DuplicateListOutput struct {
	Body DuplicateList
}

// DismissDuplicateInput is the request of dismissDuplicate.
type DismissDuplicateInput struct {
	Body struct {
		PersonA domain.PersonID `doc:"One person of the pair."   json:"personA"`
		PersonB domain.PersonID `doc:"The other person; it differs." json:"personB"`
	}
}

// MergeTake names the fields the survivor takes from the duplicate.
type MergeTake struct {
	Name      bool `doc:"The given and family name." json:"name,omitempty"`
	Email     bool `doc:"The email."                 json:"email,omitempty"`
	Phone     bool `doc:"The phone."                 json:"phone,omitempty"`
	BirthDate bool `doc:"The birth date."            json:"birthDate,omitempty"`
	MemberNo  bool `doc:"The member number."         json:"memberNo,omitempty"`
}

// MergeInput is the request of mergePersons.
type MergeInput struct {
	Body struct {
		SurvivorID domain.PersonID `doc:"The person that stays."                   json:"survivorId"`
		MergedID   domain.PersonID `doc:"The duplicate merged into it; it differs." json:"mergedId"`
		Take       MergeTake       `doc:"The fields the survivor takes from the duplicate; the others keep the survivor's value." json:"take"`
		Notes      *string         `doc:"The survivor's notes after the merge, as the office edited them; absent: the notes of both. Longer than 2000 characters is a 422 on notes." json:"notes,omitempty" maxLength:"2000"`
		Apply      bool            `doc:"Merge; without it, only tell what would happen." json:"apply,omitempty"`
	}
}

// MergeCanceled is a booking of the duplicate the merge cancels.
type MergeCanceled struct {
	LessonID       domain.LessonID `doc:"The lesson."                json:"lessonId"`
	StartsAt       time.Time       `doc:"Its start instant (UTC)."   json:"startsAt"`
	CourseTypeName string          `doc:"Its course type's name."    json:"courseTypeName"`
	Reason         string          `doc:"booked: the survivor has a booking of the lesson; membership: an automatic class booking the survivor's membership of the class does not have (a pause, a later start, a date it gave up)." enum:"booked,membership" json:"reason"`
}

// MergeClass is a class membership of the duplicate that ends, as the
// survivor is a member already.
type MergeClass struct {
	ClassID        domain.ClassID   `doc:"The class."                                   json:"classId"`
	CourseTypeName string           `doc:"Its course type's name."                      json:"courseTypeName"`
	TermName       string           `doc:"Its term's name."                             json:"termName"`
	Weekday        int              `doc:"Day of the week, 0 = Sunday to 6 = Saturday." json:"weekday" maximum:"6" minimum:"0"`
	StartTime      domain.TimeOfDay `doc:"Start time (HH:MM) in the school time zone."  example:"18:00" json:"startTime" pattern:"^[0-2][0-9]:[0-5][0-9]$"`
}

// MergeSummary is the response body of mergePersons.
type MergeSummary struct {
	Applied            bool            `doc:"Whether the merge was done; false for a preview." json:"applied"`
	Survivor           AdminPerson     `doc:"The survivor as the merge leaves it." json:"survivor"`
	BookingsMoved      int             `doc:"Bookings that move, canceled ones included." json:"bookingsMoved"`
	Canceled           []MergeCanceled `doc:"Bookings of the duplicate of lessons that have not started which the merge cancels, by start." json:"canceled" nullable:"false"`
	History            int             `doc:"Bookings of the duplicate of lessons that have started which the survivor has a booking of too; both stay, as they were." json:"history"`
	WaitlistMoved      int             `doc:"Waiting-list entries that move." json:"waitlistMoved"`
	WaitlistWithdrawn  int             `doc:"Waiting-list entries withdrawn because the survivor waits for, or is booked into, the lesson." json:"waitlistWithdrawn"`
	MembershipsMoved   int             `doc:"Class memberships that move." json:"membershipsMoved"`
	MembershipsDropped []MergeClass    `doc:"Memberships of classes the survivor is a member of already; they end." json:"membershipsDropped" nullable:"false"`
	PassesMoved        int             `doc:"Passes the duplicate owns."             json:"passesMoved"`
	PaidPassesMoved    int             `doc:"Passes the duplicate paid for."         json:"paidPassesMoved"`
	LinksMoved         int             `doc:"Guardian links that move."              json:"linksMoved"`
	LinksDropped       int             `doc:"Guardian links the survivor has already, or between the two." json:"linksDropped"`
	StaffMoved         int             `doc:"Staff members linked to the duplicate." json:"staffMoved"`
	AccountMoved       bool            `doc:"The duplicate's account moves to the survivor." json:"accountMoved"`
	Receipts           int             `doc:"Receipts the duplicate paid; they stay with it and are listed under the survivor." json:"receipts"`
}

// MergeSummaryOutput is the response of mergePersons.
type MergeSummaryOutput struct {
	Body MergeSummary
}

func (h *handlers) listDuplicates(ctx context.Context, _ *struct{}) (*DuplicateListOutput, error) {
	pairs, err := h.deps.Persons.Duplicates(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]DuplicatePair, 0, len(pairs))

	for _, p := range pairs {
		pair := DuplicatePair{
			A: duplicateCandidate(p.A), B: duplicateCandidate(p.B), Strength: string(p.Strength),
			Reasons: make([]string, 0, len(p.Reasons)),
		}

		for _, r := range p.Reasons {
			pair.Reasons = append(pair.Reasons, string(r))
		}

		items = append(items, pair)
	}

	return &DuplicateListOutput{Body: DuplicateList{Items: items}}, nil
}

func duplicateCandidate(c service.DuplicateCandidate) DuplicateCandidate {
	out := DuplicateCandidate{AdminPerson: adminPerson(c.Person)}
	if c.Account != nil {
		out.AccountEmail = c.Account.Email
	}

	return out
}

func (h *handlers) dismissDuplicate(ctx context.Context, in *DismissDuplicateInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	if err := h.deps.Persons.DismissDuplicate(ctx, p, in.Body.PersonA, in.Body.PersonB); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) mergePersons(ctx context.Context, in *MergeInput) (*MergeSummaryOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice
	t := in.Body.Take

	sum, err := h.deps.Persons.Merge(ctx, p, service.MergeRequest{
		SurvivorID: in.Body.SurvivorID, MergedID: in.Body.MergedID,
		Take:  service.MergeTake{Name: t.Name, Email: t.Email, Phone: t.Phone, BirthDate: t.BirthDate, MemberNo: t.MemberNo},
		Notes: in.Body.Notes,
	}, in.Body.Apply)

	switch {
	case errors.Is(err, service.ErrMergeTwoAccounts):
		return nil, conflict(reasonTwoAccounts, "both persons have an account; disable one first")
	case errors.Is(err, service.ErrMergeCycle):
		return nil, conflict(reasonGuardianCycle, "a guardian of the survivor would also be its dependent")
	case err != nil:
		return nil, h.toHTTPError(ctx, err)
	}

	out := MergeSummary{
		Applied: in.Body.Apply, Survivor: adminPerson(sum.Survivor), BookingsMoved: sum.BookingsMoved, History: sum.History,
		Canceled: make([]MergeCanceled, 0, len(sum.Canceled)), WaitlistMoved: sum.WaitlistMoved,
		WaitlistWithdrawn: sum.WaitlistWithdrawn, MembershipsMoved: sum.MembershipsMoved,
		MembershipsDropped: make([]MergeClass, 0, len(sum.MembershipsDropped)), PassesMoved: sum.PassesMoved,
		PaidPassesMoved: sum.PaidPassesMoved, LinksMoved: sum.LinksMoved, LinksDropped: sum.LinksDropped,
		StaffMoved: sum.StaffMoved, AccountMoved: sum.AccountMoved, Receipts: sum.Receipts,
	}

	for _, c := range sum.Canceled {
		out.Canceled = append(out.Canceled, MergeCanceled{
			LessonID: c.Lesson.ID, StartsAt: c.Lesson.StartsAt, CourseTypeName: c.CourseType.Name,
			Reason: string(c.Reason),
		})
	}

	for _, c := range sum.MembershipsDropped {
		out.MembershipsDropped = append(out.MembershipsDropped, MergeClass{
			ClassID: c.Class.ID, CourseTypeName: c.CourseType.Name, TermName: c.Term.Name,
			Weekday: int(c.Class.Weekday), StartTime: c.Class.StartTime,
		})
	}

	return &MergeSummaryOutput{Body: out}, nil
}
