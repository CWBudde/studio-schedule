package mcpserver

import (
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// announcementWriteTools save an announcement draft for the office to
// review (ADR 0022, amendment of 2026-11-18; ADR 0014, amendment).
func announcementWriteTools() []tool {
	return []tool{{
		name: "draft_announcement",
		description: "Save an announcement as a draft, as the office's announcement form does with " +
			"\"Save as draft\" (createAnnouncement with draft): it is neither shown nor mailed, and takes no " +
			"place in the daily limit of emailed announcements, until the office reviews and publishes it in the " +
			"app. There is no tool to publish, edit or delete it. The title is one line of at most 120 " +
			"characters, the body plain text of at most 4000 characters. The audience is everyone, role (with role), " +
			"class (with classId), course-type (with courseTypeId), lesson (with lessonId) or event (with " +
			"eventId); the IDs are those the schedule tool and the school://timetable resource give. Returns " +
			"the draft as the office's announcement list shows it; without --personal-data the server leaves " +
			"out its author.",
		access:      httpapi.AccessOffice,
		write:       WriteAnnouncements,
		annotations: writeAnnotations("Save an announcement draft", false, false),
		add:         handler(draftAnnouncement),
	}}
}

// draftAnnouncementInput are the arguments of draft_announcement. The
// length limits are domain.MaxAnnouncementTitleLen and
// domain.MaxAnnouncementBodyLen (a test checks the description).
type draftAnnouncementInput struct {
	Title        string `json:"title"                  jsonschema:"The title, one line (at most 120 characters)."`
	Body         string `json:"body"                   jsonschema:"The text, plain text with line breaks (at most 4000 characters)."`
	Audience     string `json:"audience"               jsonschema:"Whom it is for: everyone, role, class, course-type, lesson or event."`
	Role         string `json:"role,omitempty"         jsonschema:"With audience role: member, instructor, office or admin."`
	ClassID      string `json:"classId,omitempty"      jsonschema:"With audience class: the class (its ID)."`
	CourseTypeID string `json:"courseTypeId,omitempty" jsonschema:"With audience course-type: the course type (its ID)."`
	LessonID     string `json:"lessonId,omitempty"     jsonschema:"With audience lesson: the lesson (its ID)."`
	EventID      string `json:"eventId,omitempty"      jsonschema:"With audience event: the event (its ID)."`
	ExpiresAt    string `json:"expiresAt,omitempty"    jsonschema:"When it stops showing, an RFC 3339 instant in the future; absent: never."`
	Email        bool   `json:"email,omitempty"        jsonschema:"Whether it is mailed to its audience once, when the office publishes it."`
}

// parse returns the service's input of a draft, or the field error of
// the first argument that is no ID or instant.
func (in draftAnnouncementInput) parse() (service.AnnouncementInput, error) {
	out := service.AnnouncementInput{
		AnnouncementAudience: service.AnnouncementAudience{
			Audience: domain.Audience(in.Audience), Role: domain.Role(in.Role),
		},
		Title: in.Title, Body: in.Body, Email: in.Email, Draft: true,
	}

	var err error

	if out.ClassID, err = parseID[domain.ClassID]("classId", in.ClassID); err != nil {
		return out, err
	}

	if out.CourseTypeID, err = parseID[domain.CourseTypeID]("courseTypeId", in.CourseTypeID); err != nil {
		return out, err
	}

	if out.LessonID, err = parseID[domain.LessonID]("lessonId", in.LessonID); err != nil {
		return out, err
	}

	if out.EventID, err = parseID[domain.EventID]("eventId", in.EventID); err != nil {
		return out, err
	}

	if in.ExpiresAt != "" {
		if out.ExpiresAt, err = time.Parse(time.RFC3339, in.ExpiresAt); err != nil {
			return out, &domain.FieldError{Field: "expiresAt", Msg: "expiresAt is not an RFC 3339 instant"}
		}
	}

	return out, nil
}

// draftAnnouncement is the draft_announcement tool:
// AnnouncementService.Create with Draft, and the body of
// createAnnouncement.
func draftAnnouncement(
	ctx context.Context, s *Server, p service.Principal, in draftAnnouncementInput,
) (any, error) {
	input, err := in.parse()
	if err != nil {
		return nil, err
	}

	e, err := s.svc.Announcements.Create(ctx, p, input)
	if err != nil {
		return nil, err
	}

	body := httpapi.NewAnnouncement(e)
	if !s.opts.PersonalData {
		// The author's name is person data, left out without the flag
		// (ADR 0022); the field is omitempty, so its key goes.
		body.Author = ""
	}

	return body, nil
}
