package seed

import (
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

// demoAnnouncements are two announcements of the office: one for everyone,
// and one for the members of the Creative Dance classes, which the demo
// member sees through her son.
var demoAnnouncements = []announcementSpec{
	{
		title: "Welcome to the new booking system",
		body: "From now on you book and cancel lessons here, for yourself and your children.\n" +
			"Questions? Just ask at the front desk.",
		days: -3,
	},
	{
		title: "Costume fitting for the winter show",
		body: "Please come to the front desk on Saturday morning for the costume fitting.\n" +
			"It takes about twenty minutes.",
		courseType: "Creative Dance", days: -1, expiresDays: 14,
	},
}

func (b *builder) announcements(anchor domain.Date) {
	office := b.accountOf(lookup(b.persons, "Olga Ledgerby"))

	for _, spec := range demoAnnouncements {
		publish := anchor.AddDays(spec.days).At(at(9, 0), b.loc)
		a := domain.Announcement{
			ID: ids.New[domain.AnnouncementID](b.seq), AuthorID: office, Title: spec.title, Body: spec.body,
			Audience: domain.AudienceEveryone, PublishAt: publish, CreatedAt: publish, UpdatedAt: publish,
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
