package mail_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// noticeData is a message from the instructor Ivy about the Tuesday lesson,
// over two paragraphs, with characters HTML must escape.
func noticeData(t *testing.T) mail.LessonNoticeData {
	t.Helper()

	d := lessonRestoredData(t)

	return mail.LessonNoticeData{
		LessonData: d,
		Sender:     "Ivy Example",
		Message:    "Please bring ballet shoes & a water bottle.\nWe start <on time>.\n\nSee you!",
	}
}

// TestRenderLessonNoticeGolden pins the notice in both locales.
func TestRenderLessonNoticeGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for _, locale := range []string{"en", "de"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()

			got, err := r.Render(locale, mail.TemplateLessonNotice, noticeData(t))
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			golden(t, locale+"/lesson-notice.subject.golden", got.Subject+"\n")
			golden(t, locale+"/lesson-notice.txt.golden", got.Text)
			golden(t, locale+"/lesson-notice.html.golden", got.HTML)
		})
	}
}

// TestRenderLessonNoticeMessage keeps the message's text and line breaks,
// escapes it in HTML, and tells about a canceled lesson.
func TestRenderLessonNoticeMessage(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := noticeData(t)

	got, err := r.Render("en", mail.TemplateLessonNotice, data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got.Text, data.Message) || !strings.Contains(got.Text, "Ivy Example sends a message") {
		t.Errorf("text part:\n%s", got.Text)
	}

	for _, want := range []string{
		"Please bring ballet shoes &amp; a water bottle.<br>We start &lt;on time&gt;.<br><br>See you!",
		"To answer Ivy Example, reply",
	} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("HTML part does not contain %q:\n%s", want, got.HTML)
		}
	}

	if strings.Contains(got.HTML, "<on time>") || strings.Contains(got.Text, "canceled") {
		t.Errorf("unescaped message or a cancellation:\n%s", got.HTML)
	}

	data.Canceled = true

	for locale, want := range map[string]string{"en": "The lesson is canceled.", "de": "Die Stunde fällt aus."} {
		got, err := r.Render(locale, mail.TemplateLessonNotice, data)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(got.Text, want) || !strings.Contains(got.HTML, want) {
			t.Errorf("%s: canceled lesson not told:\n%s", locale, got.Text)
		}
	}
}

func TestRenderValidatesLessonNoticeData(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for name, modify := range map[string]func(d *mail.LessonNoticeData){
		"no sender":       func(d *mail.LessonNoticeData) { d.Sender = " " },
		"no message":      func(d *mail.LessonNoticeData) { d.Message = "\n" },
		"no participants": func(d *mail.LessonNoticeData) { d.Participants = nil },
	} {
		data := noticeData(t)
		modify(&data)

		if _, err := r.Render("en", mail.TemplateLessonNotice, data); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: Render = %v, want an error wrapping domain.ErrValidation", name, err)
		}
	}
}
