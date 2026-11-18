package conformance

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"
)

func pushSubscriptions(t *testing.T, b *backend) {
	t.Helper()

	member := browser(t)
	login(t, b, member, "clara.amberly@example.org", demoPassword)
	status := read[struct {
		Enabled   bool
		PublicKey string
	}](t, get(t, b, member, "/api/v1/my/push"))
	if !status.Enabled || status.PublicKey == "" {
		t.Fatal("enable VAPID on the conformance fixture before running the web push case")
	}

	const keys = `{"p256dh":"BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4","auth":"BTBZMqHH6r4Tts7J_aSIgg"}`
	const endpoint = "https://push.example.net/push/conformance"
	subscription := request{
		method: http.MethodPost, path: "/api/v1/my/push/subscriptions",
		body: `{"endpoint":"http://push.example.net/push/conformance","keys":` + keys + `}`,
	}
	b.call(t, member, subscription, http.StatusUnprocessableEntity)
	subscription.body = `{"endpoint":"` + endpoint + `","keys":` + keys + `,"deviceLabel":"Fixture Browser"}`
	b.call(t, browser(t), subscription, http.StatusUnauthorized)
	b.call(t, member, subscription, http.StatusNoContent)
	b.call(t, member, subscription, http.StatusNoContent)

	family := read[struct{ Me struct{ ID string } }](t, get(t, b, member, "/api/v1/family"))
	export := request{method: http.MethodGet, path: "/api/v1/my/export", query: url.Values{"person": {family.Me.ID}}}
	res := b.call(t, member, export, http.StatusOK)
	devices := read[struct {
		PushDevices []struct{ DeviceLabel string }
	}](t, res).PushDevices
	if len(devices) != 1 || devices[0].DeviceLabel != "Fixture Browser" ||
		bytes.Contains(res.body, []byte(endpoint)) || bytes.Contains(res.body, []byte("BTBZMqHH6r4Tts7J_aSIgg")) {
		t.Fatal("export did not identify the device without exposing its credentials")
	}

	subscription.method, subscription.body = http.MethodDelete, `{"endpoint":"`+endpoint+`"}`
	b.call(t, member, subscription, http.StatusNoContent)
	res = b.call(t, member, export, http.StatusOK)
	if !containsJSON(res.body, `"pushDevices":[]`) {
		t.Error("removed device still appears in the export")
	}
}
