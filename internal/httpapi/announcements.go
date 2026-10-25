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

// registerAnnouncements registers the announcements (ADR 0014): what an
// account reads and dismisses, and the office's writing them.
func registerAnnouncements(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"announcements"}

		return withConflict(api, op)
	}

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "listAnnouncements",
		Method:      http.MethodGet,
		Path:        BasePath + "/announcements",
		Summary:     "List my announcements",
		Description: "Returns the announcements shown now (published, not expired) that reach the signed-in " +
			"account, newest first, each with whether the account dismissed it. One reaches an account if it is " +
			"for everyone, for the account's role, or for persons among which are the account's person or one " +
			"of its dependents: the members of a class, or of the classes of a course type whose term has not " +
			"ended, and those booked for one of their lessons that is not over; or those booked for a lesson.",
		Tags: []string{"announcements"},
	}, h.listAnnouncements)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "dismissAnnouncement",
		Method:      http.MethodPost,
		Path:        BasePath + "/announcements/{announcementId}/dismiss",
		Summary:     "Dismiss an announcement",
		Description: "Hides an announcement from the account's banner; it stays in its list. Dismissing it again " +
			"changes nothing. While an admin is signed in as the account, nothing is stored. One that is not " +
			"shown or does not reach the account is a 404.",
		Tags:          []string{"announcements"},
		DefaultStatus: http.StatusNoContent,
	}, h.dismissAnnouncement)

	register(api, AccessOffice, huma.Operation{
		OperationID: "listAllAnnouncements",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/announcements",
		Summary:     "List all announcements",
		Description: "Returns every announcement, scheduled and expired ones included, newest publication first.",
		Tags:        []string{"announcements"},
	}, h.listAllAnnouncements)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "createAnnouncement",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/announcements",
		Summary:     "Publish an announcement",
		Description: fmt.Sprintf("Publishes an announcement at `publishAt` (default: now) until `expiresAt` "+
			"(optional). The title is one line of 1 to %d characters, the text plain text of 1 to %d (line "+
			"breaks normalized to `\\n`, trimmed, no control characters but line breaks and tabs, no invisible "+
			"format characters but the zero-width joiners; a 422 on the field otherwise). With `email`, it is "+
			"mailed once to its audience, each address once, in its account's language: at once if it publishes "+
			"now, else at `publishAt`. Guests without an account are not reached. A school day has at most %d "+
			"emailed announcements, sent and scheduled together; beyond that a 409 `mail-limit`. An unknown "+
			"class, course type or lesson is a 422 on its field.",
			domain.MaxAnnouncementTitleLen, domain.MaxAnnouncementBodyLen, domain.AnnouncementMailsPerDay),
		DefaultStatus: http.StatusCreated,
	}), h.createAnnouncement)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateAnnouncement",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/announcements/{announcementId}",
		Summary:     "Update an announcement",
		Description: "Replaces an announcement's fields, like `createAnnouncement`, until it expires; an expired " +
			"one is a 409 `expired`. An edit never mails again: one emailed already stays emailed, whatever " +
			"`email` says; one not emailed yet is emailed like a new one.",
	}), h.updateAnnouncement)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deleteAnnouncement",
		Method:        http.MethodDelete,
		Path:          BasePath + "/admin/announcements/{announcementId}",
		Summary:       "Delete an announcement",
		Description:   "Deletes an announcement with its dismissals. If it was emailed, it still counts for that day's limit.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteAnnouncement)

	register(api, AccessOffice, huma.Operation{
		OperationID: "countAnnouncementRecipients",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/announcements/recipients",
		Summary:     "Count an announcement's recipients",
		Description: "Returns how many addresses an announcement for the audience would be mailed to now, for the " +
			"confirmation before emailing. An unknown class, course type or lesson is a 422 on its field.",
		Tags: []string{"announcements"},
	}, h.countAnnouncementRecipients)
}

// AnnouncementAudienceData names whom an announcement is for.
type AnnouncementAudienceData struct {
	Audience     string              `doc:"everyone: every account; role: the accounts of role; class: the members of the class and those booked for one of its lessons that is not over; course-type: the same for every class (of a term not ended) and lesson of the course type; lesson: those booked for the lesson. Persons are reached through their guardians too." enum:"everyone,role,class,course-type,lesson" json:"audience"`
	Role         string              `doc:"The role; only with audience role." enum:"member,instructor,office,admin" json:"role,omitempty" required:"false"`
	ClassID      domain.ClassID      `doc:"The class; only with audience class." json:"classId,omitzero" required:"false"`
	CourseTypeID domain.CourseTypeID `doc:"The course type; only with audience course-type." json:"courseTypeId,omitzero" required:"false"`
	LessonID     domain.LessonID     `doc:"The lesson; only with audience lesson." json:"lessonId,omitzero" required:"false"`
}

func (d AnnouncementAudienceData) service() service.AnnouncementAudience {
	return service.AnnouncementAudience{
		Audience: domain.Audience(d.Audience), Role: domain.Role(d.Role), ClassID: d.ClassID,
		CourseTypeID: d.CourseTypeID, LessonID: d.LessonID,
	}
}

// AnnouncementData is the editable part of an announcement.
type AnnouncementData struct {
	AnnouncementAudienceData

	Title     string    `doc:"One line." json:"title" maxLength:"500" minLength:"1"`
	Body      string    `doc:"Plain text with line breaks." json:"body" maxLength:"20000" minLength:"1"`
	PublishAt time.Time `doc:"When it shows; default: now. It may lie in the future." json:"publishAt,omitzero" required:"false"`
	ExpiresAt time.Time `doc:"When it stops showing; absent: never." json:"expiresAt,omitzero" required:"false"`
	Email     bool      `doc:"Whether it is mailed to its audience once, at publishAt." json:"email,omitempty" required:"false"`
}

func (d AnnouncementData) service() service.AnnouncementInput {
	return service.AnnouncementInput{
		AnnouncementAudience: d.AnnouncementAudienceData.service(), Title: d.Title, Body: d.Body,
		PublishAt: d.PublishAt, ExpiresAt: d.ExpiresAt, Email: d.Email,
	}
}

// AnnouncementItem is an announcement shown to an account.
type AnnouncementItem struct {
	ID          domain.AnnouncementID `doc:"Announcement ID."                         json:"id"`
	Title       string                `doc:"Title, one line."                         json:"title"`
	Body        string                `doc:"Plain text with line breaks (\\n)."         json:"body"`
	PublishedAt time.Time             `doc:"When it was published."                   json:"publishedAt"`
	ExpiresAt   time.Time             `doc:"When it stops showing; absent: never."    json:"expiresAt,omitzero"`
	Dismissed   bool                  `doc:"Whether the account dismissed it."        json:"dismissed"`
}

// AnnouncementItemList is the response body of listAnnouncements.
type AnnouncementItemList struct {
	Items []AnnouncementItem `doc:"The announcements, newest first." json:"items" nullable:"false"`
}

// AnnouncementItemListOutput is the response of listAnnouncements.
type AnnouncementItemListOutput struct {
	Body AnnouncementItemList
}

// AnnouncementClass names the class of an announcement.
type AnnouncementClass struct {
	ID         domain.ClassID `doc:"Class ID."                                json:"id"`
	CourseType CourseTypeRef  `json:"courseType"`
	Weekday    int            `doc:"Day of the week, 0 = Sunday." json:"weekday" maximum:"6" minimum:"0"`
	StartTime  string         `doc:"Start time, HH:MM." json:"startTime"`
	TermID     domain.TermID  `doc:"Its term."         json:"termId"`
	TermName   string         `doc:"Name of its term." json:"termName"`
}

// AnnouncementLesson names the lesson of an announcement.
type AnnouncementLesson struct {
	ID         domain.LessonID `doc:"Lesson ID."           json:"id"`
	StartsAt   time.Time       `doc:"Start instant (UTC)." json:"startsAt"`
	EndsAt     time.Time       `doc:"End instant (UTC)."   json:"endsAt"`
	CourseType CourseTypeRef   `json:"courseType"`
}

// Announcement is an announcement as the office lists it.
type Announcement struct {
	ID domain.AnnouncementID `doc:"Announcement ID." json:"id"`
	AnnouncementData

	Author          string              `doc:"Name of the author; absent if it can no longer be found." json:"author,omitempty"`
	Class           *AnnouncementClass  `doc:"The class; only with audience class." json:"class,omitempty"`
	CourseType      *CourseTypeRef      `doc:"The course type; only with audience course-type." json:"courseType,omitempty"`
	Lesson          *AnnouncementLesson `doc:"The lesson; only with audience lesson." json:"lesson,omitempty"`
	EmailedAt       time.Time           `doc:"When it was mailed; absent if not (yet)." json:"emailedAt,omitzero"`
	EmailRecipients int                 `doc:"How many addresses it was mailed to." json:"emailRecipients"`
	CreatedAt       time.Time           `doc:"When it was written." json:"createdAt"`
	UpdatedAt       time.Time           `doc:"When it was last changed." json:"updatedAt"`
}

// AnnouncementList is the response body of listAllAnnouncements.
type AnnouncementList struct {
	Items []Announcement `doc:"All announcements, newest publication first." json:"items" nullable:"false"`
}

// AnnouncementListOutput is the response of listAllAnnouncements.
type AnnouncementListOutput struct {
	Body AnnouncementList
}

// AnnouncementOutput is the response of createAnnouncement and
// updateAnnouncement.
type AnnouncementOutput struct {
	Body Announcement
}

// AnnouncementDataInput is the request of createAnnouncement.
type AnnouncementDataInput struct {
	Body AnnouncementData
}

// AnnouncementUpdateInput is the request of updateAnnouncement.
type AnnouncementUpdateInput struct {
	AnnouncementID domain.AnnouncementID `doc:"The announcement." path:"announcementId"`
	Body           AnnouncementData
}

// AnnouncementRefInput names an announcement.
type AnnouncementRefInput struct {
	AnnouncementID domain.AnnouncementID `doc:"The announcement." path:"announcementId"`
}

// AnnouncementAudienceInput is the request of countAnnouncementRecipients.
type AnnouncementAudienceInput struct {
	Body AnnouncementAudienceData
}

// RecipientCountOutput is the response of countAnnouncementRecipients.
type RecipientCountOutput struct {
	Body struct {
		Recipients int `doc:"How many addresses it would be mailed to now." json:"recipients" minimum:"0"`
	}
}

func (h *handlers) listAnnouncements(ctx context.Context, _ *struct{}) (*AnnouncementItemListOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	views, err := h.deps.Announcements.ForAccount(ctx, p)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]AnnouncementItem, 0, len(views))
	for _, v := range views {
		a := v.Announcement
		items = append(items, AnnouncementItem{
			ID: a.ID, Title: a.Title, Body: a.Body, PublishedAt: a.PublishAt, ExpiresAt: a.ExpiresAt,
			Dismissed: v.Dismissed,
		})
	}

	return &AnnouncementItemListOutput{Body: AnnouncementItemList{Items: items}}, nil
}

func (h *handlers) dismissAnnouncement(ctx context.Context, in *AnnouncementRefInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	if err := h.deps.Announcements.Dismiss(ctx, p, in.AnnouncementID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) listAllAnnouncements(ctx context.Context, _ *struct{}) (*AnnouncementListOutput, error) {
	entries, err := h.deps.Announcements.List(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]Announcement, 0, len(entries))
	for _, e := range entries {
		items = append(items, announcementBody(e))
	}

	return &AnnouncementListOutput{Body: AnnouncementList{Items: items}}, nil
}

func (h *handlers) createAnnouncement(ctx context.Context, in *AnnouncementDataInput) (*AnnouncementOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	return h.announcementOutput(ctx)(h.deps.Announcements.Create(ctx, p, in.Body.service()))
}

func (h *handlers) updateAnnouncement(ctx context.Context, in *AnnouncementUpdateInput) (*AnnouncementOutput, error) {
	return h.announcementOutput(ctx)(h.deps.Announcements.Update(ctx, in.AnnouncementID, in.Body.service()))
}

func (h *handlers) deleteAnnouncement(ctx context.Context, in *AnnouncementRefInput) (*struct{}, error) {
	if err := h.deps.Announcements.Delete(ctx, in.AnnouncementID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) countAnnouncementRecipients(
	ctx context.Context, in *AnnouncementAudienceInput,
) (*RecipientCountOutput, error) {
	n, err := h.deps.Announcements.CountRecipients(ctx, in.Body.service())
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &RecipientCountOutput{}
	out.Body.Recipients = n

	return out, nil
}

// announcementOutput maps the result of a write: the conflicts of the
// announcements to their reasons.
func (h *handlers) announcementOutput(
	ctx context.Context,
) func(service.AnnouncementEntry, error) (*AnnouncementOutput, error) {
	return func(e service.AnnouncementEntry, err error) (*AnnouncementOutput, error) {
		var limited *service.TooManyAnnouncementMailsError

		switch {
		case errors.As(err, &limited):
			return nil, conflict(reasonMailLimit, fmt.Sprintf("%v has %d emailed announcements already",
				limited.Day, domain.AnnouncementMailsPerDay))
		case errors.Is(err, service.ErrAnnouncementExpired):
			return nil, conflict(reasonExpired, "the announcement has expired")
		case err != nil:
			return nil, h.toHTTPError(ctx, err)
		}

		return &AnnouncementOutput{Body: announcementBody(e)}, nil
	}
}

func announcementBody(e service.AnnouncementEntry) Announcement {
	a := e.Announcement
	out := Announcement{
		ID: a.ID,
		AnnouncementData: AnnouncementData{
			AnnouncementAudienceData: AnnouncementAudienceData{
				Audience: string(a.Audience), Role: string(a.Role), ClassID: a.ClassID, CourseTypeID: a.CourseTypeID,
				LessonID: a.LessonID,
			},
			Title: a.Title, Body: a.Body, PublishAt: a.PublishAt, ExpiresAt: a.ExpiresAt, Email: a.Email,
		},
		EmailedAt: a.EmailedAt, EmailRecipients: a.EmailRecipients, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}

	if !e.Author.ID.IsZero() {
		out.Author = e.Author.GivenName + " " + e.Author.FamilyName
	}

	ct := CourseTypeRef{ID: e.CourseType.ID, Name: e.CourseType.Name}

	switch a.Audience {
	case domain.AudienceClass:
		out.Class = &AnnouncementClass{
			ID: e.Class.ID, CourseType: ct, Weekday: int(e.Class.Weekday), StartTime: e.Class.StartTime.String(),
			TermID: e.Term.ID, TermName: e.Term.Name,
		}
	case domain.AudienceCourseType:
		out.CourseType = &ct
	case domain.AudienceLesson:
		out.Lesson = &AnnouncementLesson{
			ID: e.Lesson.ID, StartsAt: e.Lesson.StartsAt, EndsAt: e.Lesson.EndsAt, CourseType: ct,
		}
	}

	return out
}
