package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// recordingPusher is a PushSender that keeps what it is given.
type recordingPusher struct {
	mu   sync.Mutex
	sent []domain.PushSubscription
	body []string
}

func (p *recordingPusher) PublicKey() string { return "BTestKey" }

func (p *recordingPusher) Send(_ context.Context, sub domain.PushSubscription, payload []byte, _ time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.sent = append(p.sent, sub)
	p.body = append(p.body, string(payload))

	return nil
}

// A valid browser subscription (the keys of RFC 8291's example).
const pushSubscription = `{"endpoint":"https://push.example.net/push/clara",` +
	`"keys":{"p256dh":"BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",` +
	`"auth":"BTBZMqHH6r4Tts7J_aSIgg"},"deviceLabel":"Firefox on Linux"}`

// TestPushOff: without a sender the API says so and takes no subscription,
// but a device can always leave.
func TestPushOff(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	clara := a.login("clara.amberly@example.org")

	if got := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/push", "", clara)); got["enabled"] != false ||
		got["publicKey"] != nil {
		t.Errorf("status %v", got)
	}

	a.expect(http.StatusNotFound, http.MethodPost, "/api/v1/my/push/subscriptions", pushSubscription, clara)
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/my/push/subscriptions",
		`{"endpoint":"https://push.example.net/push/clara"}`, clara)
}

// TestPush runs web push over HTTP (ADR 0017): a device subscribes, an
// invite queues a notification for it and wakes the deliver-push job, the
// job sends it, and the device leaves. The rules are tested in package
// service.
func TestPush(t *testing.T) {
	t.Parallel()

	pusher := &recordingPusher{}
	waker := jobs.NewWaker()
	a := newAuthApp(t, app.Deps{Demo: true, Pusher: pusher, Wake: waker})
	office := a.login("olga.ledgerby@example.net")
	clara := a.login("clara.amberly@example.org")
	family := a.family(clara)
	me, ella := family.Me.ID, family.Dependents[0].ID

	a.want(http.MethodGet, "/api/v1/my/push", "", http.StatusUnauthorized)
	a.want(http.MethodPost, "/api/v1/my/push/subscriptions", pushSubscription, http.StatusUnauthorized)

	if got := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/push", "", clara)); got["enabled"] != true ||
		got["publicKey"] != "BTestKey" {
		t.Errorf("status %v", got)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/my/push/subscriptions",
		`{"endpoint":"http://push.example.net/x","keys":{"p256dh":"BCVx","auth":"BTBZ"}}`, clara)
	a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/my/push/subscriptions",
		strings.Replace(pushSubscription, "BTBZMqHH6r4Tts7J_aSIgg", "BTBZ", 1), clara)
	a.expect(http.StatusNoContent, http.MethodPost, "/api/v1/my/push/subscriptions", pushSubscription, clara)
	// Saving again changes nothing.
	a.expect(http.StatusNoContent, http.MethodPost, "/api/v1/my/push/subscriptions", pushSubscription, clara)

	// A full workshop with Ella waiting; Clara cancels, Ella is invited.
	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	demo := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", office))
	existing, _ := demo["items"].([]any)[0].(map[string]any)

	l := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(`{"courseTypeId":%q,"date":"2026-11-03","startTime":"10:00","durationMinutes":60,"roomId":%q,`+
			`"instructorId":%q,"capacity":1,"visibility":"public","kind":"workshop","confirmRoomOverlap":true}`,
			existing["courseTypeId"], existing["roomId"], existing["instructor"].(map[string]any)["id"]), office))
	lesson, _ := l["id"].(string)

	booking := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+lesson+"/bookings",
		`{"personId":"`+me+`"}`, clara))
	a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+lesson+"/waitlist", `{"personId":"`+ella+`"}`, clara)

	select {
	case <-waker.Chan("deliver-push"):
		t.Fatal("woken before anything was queued")
	default:
	}

	a.expect(http.StatusOK, http.MethodPost, "/api/v1/bookings/"+booking["id"].(string)+"/cancel", "", clara)

	select {
	case <-waker.Chan("deliver-push"):
	default:
		t.Fatal("the invite did not wake the deliver-push job")
	}

	// What the job does: send it.
	push := service.NewPushService(a.deps.Store, a.clock, ids.V7{}, pusher)
	if n, err := push.Deliver(t.Context()); err != nil || n != 1 {
		t.Fatalf("deliver: %d, %v", n, err)
	}

	token := a.linkToken("waitlist-invite", "waitlist/accept")

	var payload struct{ Title, Body, URL, Tag, Lang string }
	if err := json.Unmarshal([]byte(pusher.body[0]), &payload); err != nil {
		t.Fatal(err)
	}

	if pusher.sent[0].Endpoint != "https://push.example.net/push/clara" ||
		payload.URL != "https://school.example.org/waitlist/accept?token="+token ||
		!strings.HasPrefix(payload.Title, "A place is free: ") || strings.Contains(pusher.body[0], "Ella") {
		t.Errorf("sent to %s: %s", pusher.sent[0].Endpoint, pusher.body[0])
	}

	// The data export names the device, without its endpoint and keys.
	export := string(a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/export?person="+me, "", clara))
	if !strings.Contains(export, `"deviceLabel":"Firefox on Linux"`) || strings.Contains(export, "push.example.net") ||
		strings.Contains(export, "BTBZMqHH6r4Tts7J_aSIgg") {
		t.Errorf("export: %s", export)
	}

	// Leaving: the device gets nothing more.
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/my/push/subscriptions",
		`{"endpoint":"https://push.example.net/push/clara"}`, clara)

	if export := string(a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/export?person="+me, "", clara)); !strings.Contains(export,
		`"pushDevices":[]`) {
		t.Errorf("export after leaving: %s", export)
	}
}

// TestWebManifest: the manifest carries the school's name, language and
// accent, as a manifest with a short cache.
func TestWebManifest(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	res, body := a.call(http.MethodGet, "/manifest.webmanifest", "")

	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/manifest+json" ||
		res.Header.Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("manifest: %d %v", res.StatusCode, res.Header)
	}

	var m struct {
		Name, ShortName, Lang, Display, StartURL, Scope, ThemeColor string
		Icons                                                       []struct{ Src, Sizes, Purpose string }
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}

	pub := decode(t, a.want(http.MethodGet, "/api/v1/settings/public", "", http.StatusOK))
	if m.Name != pub["schoolName"] || m.Name == "" || m.Display != "standalone" || m.Lang != pub["defaultLocale"] ||
		len(m.Icons) != 3 || m.Icons[2].Purpose != "maskable" {
		t.Errorf("manifest %+v (public settings %v)", m, pub)
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}

	accent, _ := pub["accentColor"].(string)
	if want := map[bool]string{true: accent, false: "#7f1f35"}[accent != ""]; raw["theme_color"] != want ||
		raw["start_url"] != "/" || raw["short_name"] != m.Name {
		t.Errorf("manifest %v, want theme color %s", raw, want)
	}
}
