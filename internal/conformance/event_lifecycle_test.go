package conformance

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

func officeEventLifecycle(t *testing.T, b *backend) {
	t.Helper()

	const events = "/api/v1/admin/events"
	guest, member, office := browser(t), browser(t), browser(t)
	login(t, b, member, demoMember, demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)

	fixture := lessonFixture(t, b, office, "workshop", 1600)
	course := fixture["courseTypeId"]
	fields := map[string]any{
		"title": " Äpfel & Süßes ", "courseTypeId": course, "capacity": 1600, "price": 1500, "vatRate": 1900,
	}
	create := func(client *http.Client, status int) answer {
		t.Helper()
		return b.call(t, client, request{method: http.MethodPost, path: events, body: jsonBody(t, fields)}, status)
	}
	create(guest, http.StatusUnauthorized)
	create(member, http.StatusForbidden)
	delete(fields, "courseTypeId")
	if failed := create(office, http.StatusUnprocessableEntity); !containsJSON(failed.body, `"location":"body"`) {
		t.Fatalf("missing course type: %s", failed.body)
	}
	fields["courseTypeId"] = course
	type event struct {
		ID, Slug, Title, Status string
		Sessions                []scheduledLesson
		Image                   *struct{ ContentType, Digest string }
	}
	draft := read[event](t, create(office, http.StatusCreated))
	if draft.ID == "" || draft.Status != "draft" || draft.Slug != "aepfel-suesses" || draft.Title != "Äpfel & Süßes" ||
		len(draft.Sessions) != 0 {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	another := read[event](t, create(office, http.StatusCreated))
	if another.Slug != draft.Slug+"-2" {
		t.Fatalf("slug of second draft: %q", another.Slug)
	}
	fields["slug"] = draft.Slug
	if failed := create(office, http.StatusConflict); !containsJSON(failed.body, `"reason":"duplicate"`) {
		t.Fatalf("explicit duplicate slug: %s", failed.body)
	}
	delete(fields, "slug")
	fields["signupOpensAt"], fields["signupClosesAt"] = "2026-10-26T10:00:00Z", "2026-10-26T10:00:00Z"
	if failed := create(office, http.StatusUnprocessableEntity); !containsJSON(failed.body, `"location":"body.signupClosesAt"`) {
		t.Fatalf("invalid sign-up window: %s", failed.body)
	}

	adminEvent := request{method: http.MethodGet, path: events + "/{eventId}", params: map[string]string{"eventId": draft.ID}}
	publicEvent := request{method: http.MethodGet, path: "/api/v1/events/{slug}", params: map[string]string{"slug": draft.Slug}}
	b.call(t, guest, publicEvent, http.StatusNotFound)
	publish := adminEvent
	publish.method, publish.path = http.MethodPost, publish.path+"/publish"
	if failed := b.call(t, office, publish, http.StatusConflict); !containsJSON(failed.body, `"reason":"no-sessions"`) {
		t.Fatalf("event without a session: %s", failed.body)
	}
	uploadAndReadEventImage(t, b, office, member, adminEvent, publicEvent)

	session := adminEvent
	session.method, session.path = http.MethodPost, session.path+"/sessions"
	sessionData := map[string]any{
		"date": "2026-10-01", "startTime": "02:00", "durationMinutes": 60,
		"roomId": fixture["roomId"], "instructorId": fixture["instructorId"],
	}
	session.body = jsonBody(t, sessionData)
	if failed := b.call(t, office, session, http.StatusConflict); !containsJSON(failed.body, `"reason":"past"`) {
		t.Fatalf("past session: %s", failed.body)
	}
	sessionData["date"] = "2026-10-24"
	session.body = jsonBody(t, sessionData)
	type newSession struct {
		ID, EventID, EventSlug, Kind, Visibility, StartsAt string
		Capacity, FreePlaces                               int
	}
	scheduled := read[newSession](t, b.call(t, office, session, http.StatusCreated))
	if scheduled.ID == "" || scheduled.EventID != draft.ID || scheduled.EventSlug != draft.Slug ||
		scheduled.Kind != "workshop" || scheduled.Visibility != "public" || scheduled.Capacity != 1600 ||
		scheduled.FreePlaces != 1600 || scheduled.StartsAt != "2026-10-24T00:00:00Z" {
		t.Fatalf("unexpected event session: %+v", scheduled)
	}
	if failed := b.call(t, office, session, http.StatusConflict); !containsJSON(failed.body, `"reason":"room-taken"`) ||
		!containsJSON(failed.body, `"id":"`+scheduled.ID+`"`) {
		t.Fatalf("overlapping session: %s", failed.body)
	}
	lesson := request{method: http.MethodGet, path: "/api/v1/lessons/{lessonId}", params: map[string]string{"lessonId": scheduled.ID}}
	b.call(t, member, lesson, http.StatusNotFound)

	published := read[event](t, b.call(t, office, publish, http.StatusOK))
	if published.Status != "published" || len(published.Sessions) != 1 || published.Image == nil {
		t.Fatalf("unexpected published event: %+v", published)
	}
	b.call(t, guest, publicEvent, http.StatusOK)
	image := publicEvent
	image.path += "/image"
	b.call(t, guest, image, http.StatusOK)
	b.call(t, member, lesson, http.StatusOK)
	if failed := b.call(t, office, publish, http.StatusConflict); !containsJSON(failed.body, `"reason":"not-draft"`) {
		t.Fatalf("publishing twice: %s", failed.body)
	}

	unpublish := publish
	unpublish.path = adminEvent.path + "/unpublish"
	if got := read[event](t, b.call(t, office, unpublish, http.StatusOK)); got.Status != "draft" {
		t.Fatalf("event taken back: %+v", got)
	}
	b.call(t, guest, publicEvent, http.StatusNotFound)
	if failed := b.call(t, office, unpublish, http.StatusConflict); !containsJSON(failed.body, `"reason":"not-published"`) {
		t.Fatalf("taking back a draft: %s", failed.body)
	}

	deleteImage := adminEvent
	deleteImage.method, deleteImage.path = http.MethodDelete, deleteImage.path+"/image"
	b.call(t, office, deleteImage, http.StatusNoContent)
	b.call(t, office, deleteImage, http.StatusNoContent)
	deleteImage.method = http.MethodGet
	b.call(t, office, deleteImage, http.StatusNotFound)
	remove := adminEvent
	remove.method = http.MethodDelete
	b.call(t, office, remove, http.StatusNoContent)
	b.call(t, office, adminEvent, http.StatusNotFound)
	b.call(t, office, lesson, http.StatusNotFound)

	list := read[struct{ Items []event }](t, get(t, b, office, events))
	for _, item := range list.Items {
		if item.Slug != "open-ballet-taster" {
			continue
		}
		used := request{method: http.MethodDelete, path: adminEvent.path, params: map[string]string{"eventId": item.ID}}
		if failed := b.call(t, office, used, http.StatusConflict); !containsJSON(failed.body, `"reason":"in-use"`) {
			t.Fatalf("deleting an event with registrations: %s", failed.body)
		}
		used.method, used.path = http.MethodPost, used.path+"/unpublish"
		if failed := b.call(t, office, used, http.StatusConflict); !containsJSON(failed.body, `"reason":"in-use"`) {
			t.Fatalf("taking back an event with open registrations: %s", failed.body)
		}
		return
	}
	t.Fatal("the seed has no registered event")
}

func uploadAndReadEventImage(t *testing.T, b *backend, office, member *http.Client, adminEvent, publicEvent request) {
	t.Helper()

	const encoded = "iVBORw0KGgo="
	image := adminEvent
	image.method, image.path, image.body = http.MethodPut, image.path+"/image", `{"contentType":"image/png","data":"`+encoded+`"}`
	b.call(t, member, image, http.StatusForbidden)
	wrongType := image
	wrongType.body = strings.Replace(wrongType.body, "image/png", "image/jpeg", 1)
	b.call(t, office, wrongType, http.StatusUnprocessableEntity)
	b.call(t, office, image, http.StatusNoContent)

	image.method, image.body = http.MethodGet, ""
	asset := b.call(t, office, image, http.StatusOK)
	etag := asset.header.Get("ETag")
	if !bytes.Equal(asset.body, []byte("\x89PNG\r\n\x1a\n")) || etag == "" || asset.header.Get("Content-Type") != "image/png" ||
		!containsJSON(b.call(t, office, adminEvent, http.StatusOK).body, `"digest":"`+strings.Trim(etag, `"`)+`"`) {
		t.Fatalf("unexpected event image: %s %v (%s)", base64.StdEncoding.EncodeToString(asset.body), asset.header, etag)
	}
	image.header = http.Header{"If-None-Match": {"W/" + etag}}
	b.call(t, office, image, http.StatusNotModified)
	publicEvent.path += "/image"
	b.call(t, member, publicEvent, http.StatusNotFound)
}
