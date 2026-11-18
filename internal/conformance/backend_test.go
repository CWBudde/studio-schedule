package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

var runBackend = flag.Bool("conformance", false, "run HTTP scenarios against BASE_URL and its test fixture")

type backend struct {
	base     string
	token    string
	contract *contract
}

type request struct {
	method string
	path   string // OpenAPI path, including names of any path parameters
	params map[string]string
	query  url.Values
	header http.Header
	body   string
}

type answer struct {
	header http.Header
	body   []byte
}

func openBackend(t *testing.T) *backend {
	t.Helper()

	base := os.Getenv("BASE_URL")
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" {
		t.Fatal("BASE_URL must be the HTTP URL of a test fixture, including its optional mount path")
	}
	token := os.Getenv("TEST_CONTROL_TOKEN")
	if len(token) < 32 {
		t.Fatal("set TEST_CONTROL_TOKEN to the credential of the running test fixture")
	}

	spec, err := os.Open("../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = spec.Close() }()

	c, err := readContract(spec)
	if err != nil {
		t.Fatal(err)
	}

	return &backend{base: strings.TrimSuffix(base, "/"), token: token, contract: c}
}

func browser(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	now, err := time.Parse(time.RFC3339, demoNow)
	if err != nil {
		t.Fatal(err)
	}

	return &http.Client{
		Jar: &fixtureCookies{CookieJar: jar, now: now}, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// The store and API use a frozen school clock. Preserve the login
// cookie's lifetime relative to that clock in the client's wall-clock jar.
type fixtureCookies struct {
	http.CookieJar
	now time.Time
}

func (j *fixtureCookies) SetCookies(target *url.URL, cookies []*http.Cookie) {
	adjusted := make([]*http.Cookie, len(cookies))
	offset := time.Since(j.now)
	for i, cookie := range cookies {
		value := *cookie
		if !value.Expires.IsZero() {
			value.Expires = value.Expires.Add(offset)
		}

		adjusted[i] = &value
	}

	j.CookieJar.SetCookies(target, adjusted)
}

func (r request) target(t *testing.T, base string) string {
	t.Helper()

	path := r.path
	for name, value := range r.params {
		parameter := "{" + name + "}"
		if !strings.Contains(path, parameter) {
			t.Fatalf("unexpected parameter %q for %s", name, r.path)
		}

		path = strings.ReplaceAll(path, parameter, url.PathEscape(value))
	}
	if strings.ContainsAny(path, "{}") {
		t.Fatalf("missing parameter for %s", r.path)
	}
	if len(r.query) > 0 {
		path += "?" + r.query.Encode()
	}

	return base + path
}

func (b *backend) call(t *testing.T, client *http.Client, in request, want int) answer {
	t.Helper()

	got, status := b.send(t, client, in)
	if err := b.contract.validate(in.method, in.path, status, got.header, got.body); err != nil {
		t.Fatalf("%s %s: response violates OpenAPI: %v", in.method, in.path, err)
	}
	if status != want {
		t.Fatalf("%s %s: got %d, want %d (%s)", in.method, in.path, status, want, got.body)
	}

	return got
}

func (b *backend) fixture(t *testing.T, method, name, body string, want int) answer {
	t.Helper()

	got, status := b.send(t, browser(t), request{
		method: method, path: "/api/test/" + name, body: body,
		header: http.Header{"Authorization": {"Bearer " + b.token}},
	})
	if status != want {
		t.Fatalf("test fixture %s: got %d, want %d (%s)", name, status, want, got.body)
	}

	return got
}

func (b *backend) send(t *testing.T, client *http.Client, in request) (answer, int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, in.method, in.target(t, b.base), strings.NewReader(in.body))
	if err != nil {
		t.Fatal(err)
	}

	for name, values := range in.header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	if in.body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", in.method, in.path, err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	return answer{header: res.Header, body: body}, res.StatusCode
}

const (
	demoNow    = "2026-10-23T16:00:00Z"
	demoSchool = "programme"
	demoDate   = "2026-10-23"
)

func (b *backend) seed(t *testing.T) {
	t.Helper()

	b.fixture(t, http.MethodPost, "reset", "", http.StatusNoContent)
	b.fixture(t, http.MethodPut, "clock", `{"now":"`+demoNow+`"}`, http.StatusNoContent)
	b.fixture(t, http.MethodPost, "seed", `{"anchor":"`+demoDate+`","school":"`+demoSchool+`"}`, http.StatusNoContent)
}

func get(t *testing.T, b *backend, client *http.Client, path string) answer {
	t.Helper()

	return b.call(t, client, request{method: http.MethodGet, path: path}, http.StatusOK)
}

func read[T any](t *testing.T, res answer) T {
	t.Helper()

	var result T
	if err := json.Unmarshal(res.body, &result); err != nil {
		t.Fatal(err)
	}

	return result
}

// Check a value or field without binding the suite to JSON whitespace.
func containsJSON(body []byte, fragment string) bool {
	var compact bytes.Buffer
	return json.Compact(&compact, body) == nil && bytes.Contains(compact.Bytes(), []byte(fragment))
}

func TestBackendConformance(t *testing.T) {
	if !*runBackend {
		t.Skip("enable the HTTP suite with just conformance")
	}

	b := openBackend(t)
	for _, scenario := range []struct {
		name string
		run  func(*testing.T, *backend)
	}{
		{"public settings and catalog", publicCatalog},
		{"schedule release and visibility", publicSchedule},
		{"registration and session", registrationSession},
		{"password recovery", recoverPassword},
		{"login rate limit", loginRateLimit},
		{"admin impersonation", adminImpersonation},
		{"member booking and cancellation", memberBooking},
		{"lesson waitlist", lessonWaitlist},
		{"participant roles", participantRoles},
		{"calendar subscription", calendarSubscription},
		{"family and privacy", familyPrivacy},
		{"announcements", announcements},
		{"mail log", mailLog},
		{"statistics", statistics},
		{"settings and branding", settingsAndBranding},
		{"push subscriptions", pushSubscriptions},
		{"member import", importMembers},
		{"duplicate persons", mergeDuplicates},
		{"events and registrations", eventRegistration},
		{"pass sales and receipts", passReceipt},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			b.seed(t)
			scenario.run(t, b)
		})
	}
}

func publicCatalog(t *testing.T, b *backend) {
	t.Helper()

	client := browser(t)
	get(t, b, client, "/api/v1/health")
	settings := read[struct {
		SchoolName    string
		DefaultLocale string
		Timezone      string
	}](t, get(t, b, client, "/api/v1/settings/public"))
	if settings.SchoolName != "Studio Demo — Ballet & Dance" || settings.DefaultLocale != "en" ||
		settings.Timezone != "Europe/Berlin" {
		t.Fatalf("public school settings: %+v", settings)
	}

	for _, path := range []string{
		"/api/v1/locations", "/api/v1/rooms", "/api/v1/course-types", "/api/v1/levels",
		"/api/v1/terms", "/api/v1/staff", "/api/v1/events",
	} {
		items := read[struct{ Items []json.RawMessage }](t, get(t, b, client, path)).Items
		if len(items) == 0 {
			t.Errorf("%s has no seeded entries", path)
		}
	}

	manifest := read[struct {
		Name  string
		Scope string
	}](t, get(t, b, client, "/manifest.webmanifest"))
	u, _ := url.Parse(b.base)
	if manifest.Name != settings.SchoolName || manifest.Scope != u.Path+"/" {
		t.Errorf("installed app manifest: %+v", manifest)
	}
}

type scheduledLesson struct {
	ID         string
	Kind       string
	Visibility string
	StartsAt   time.Time
}

type scheduledRange struct {
	Now           time.Time
	VisibleUntil  time.Time
	ReleasedUntil time.Time
	Lessons       []scheduledLesson
}

func publicSchedule(t *testing.T, b *backend) {
	t.Helper()

	guest := browser(t)
	query := url.Values{"from": {"2026-10-19"}, "to": {"2026-11-09"}}
	in := request{method: http.MethodGet, path: "/api/v1/schedule", query: query}

	released := read[scheduledRange](t, b.call(t, guest, in, http.StatusOK))
	if len(released.Lessons) == 0 || released.Now.Format(time.RFC3339) != demoNow ||
		!released.VisibleUntil.Equal(released.ReleasedUntil) {
		t.Fatalf("public release window: %+v", released)
	}
	for _, lesson := range released.Lessons {
		if lesson.Visibility != "public" || lesson.Kind == "regular" && !lesson.StartsAt.Before(released.VisibleUntil) {
			t.Fatalf("unreleased or private lesson in public schedule: %+v", lesson)
		}
	}

	member := browser(t)
	login(t, b, member, "lina.mistlewood@example.com", "demo-password")
	all := read[scheduledRange](t, b.call(t, member, in, http.StatusOK))
	if !all.VisibleUntil.After(released.VisibleUntil) {
		t.Fatalf("member visible until %s, public until %s", all.VisibleUntil, released.VisibleUntil)
	}

	pos := slices.IndexFunc(all.Lessons, func(l scheduledLesson) bool {
		return l.Visibility == "members" || l.Kind == "regular" && !l.StartsAt.Before(released.VisibleUntil)
	})
	if pos < 0 {
		t.Fatal("fixture has no upcoming lesson beyond public visibility")
	}
	detail := request{
		method: http.MethodGet, path: "/api/v1/lessons/{lessonId}",
		params: map[string]string{"lessonId": all.Lessons[pos].ID},
	}
	b.call(t, guest, detail, http.StatusNotFound)
	b.call(t, member, detail, http.StatusOK)

	in.query = url.Values{"from": {"not-a-date"}}
	b.call(t, guest, in, http.StatusUnprocessableEntity)
}

func login(t *testing.T, b *backend, client *http.Client, email, password string) answer {
	t.Helper()

	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		t.Fatal(err)
	}

	return b.call(t, client, request{
		method: http.MethodPost, path: "/api/v1/auth/login", body: string(body),
	}, http.StatusOK)
}

func registrationSession(t *testing.T, b *backend) {
	t.Helper()

	const email = "conformance-new@example.test"
	const password = "conformance-new-password"
	client := browser(t)
	me := request{method: http.MethodGet, path: "/api/v1/auth/me"}
	b.call(t, client, me, http.StatusUnauthorized)
	b.call(t, client, request{
		method: http.MethodPost, path: "/api/v1/auth/register",
		body: `{"email":"` + email + `","password":"` + password + `","givenName":"Fixture","familyName":"Member","locale":"en"}`,
	}, http.StatusAccepted)

	token := mailToken(t, b, email, "confirm-email", "/confirm")

	confirm := request{
		method: http.MethodPost, path: "/api/v1/auth/confirm",
		body: `{"token":"` + token + `","password":"` + password + `"}`,
	}
	b.call(t, client, confirm, http.StatusOK)
	account := read[struct{ Email, Role string }](t, b.call(t, client, me, http.StatusOK))
	if account.Email != email || account.Role != "member" {
		t.Fatalf("signed-in account: %+v", account)
	}

	b.call(t, client, confirm, http.StatusGone)
	b.call(t, client, request{method: http.MethodPost, path: "/api/v1/auth/logout"}, http.StatusNoContent)
	b.call(t, client, me, http.StatusUnauthorized)
	login(t, b, client, email, password)
	b.call(t, client, me, http.StatusOK)
}

func mailToken(t *testing.T, b *backend, email, template, route string) string {
	t.Helper()

	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))

	token := ""
	for _, m := range outbox.Mails {
		if m.To != email || m.Template != template {
			continue
		}
		_, tail, found := strings.Cut(m.Text, b.base+route+"?token=")
		if found && len(strings.Fields(tail)) > 0 {
			token = strings.Fields(tail)[0]

			break
		}
	}
	if token == "" {
		t.Fatalf("expected a %s mail with a link under BASE_URL", template)
	}

	return token
}
