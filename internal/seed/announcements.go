package seed

import (
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
)

// announcementSpec is an announcement of the office (ADR 0014); days count
// from the anchor, expiresDays 0 means no expiry. They are not emailed.
type announcementSpec struct {
	title, body string
	courseType  string // empty: for everyone
	days        int
	expiresDays int
}

// announcements adds announcements written by the office: drafts (without
// a publish time; days is ignored) or published ones.
func (b *builder) announcements(anchor domain.Date, specs []announcementSpec, drafts bool) {
	office := b.accountOf(lookup(b.persons, b.s.office))

	for _, spec := range specs {
		publish := anchor.AddDays(spec.days).At(at(9, 0), b.loc)
		a := domain.Announcement{
			ID: ids.New[domain.AnnouncementID](b.seq), AuthorID: office, Title: spec.title, Body: spec.body,
			Audience: domain.AudienceEveryone, PublishAt: publish, CreatedAt: publish, UpdatedAt: publish,
		}

		if drafts {
			a.PublishAt = time.Time{}
		}

		if spec.courseType != "" {
			a.Audience, a.CourseTypeID = domain.AudienceCourseType, lookup(b.courseTypes, spec.courseType)
		}

		if spec.expiresDays != 0 {
			a.ExpiresAt = anchor.AddDays(spec.expiresDays).At(at(9, 0), b.loc)
		}

		b.d.Announcements = append(b.d.Announcements, a)
	}
}
