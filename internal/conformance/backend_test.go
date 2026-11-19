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
		{"catalog administration", catalogAdministration},
		{"term administration", termAdministration},
		{"class lists and roster", classListsAndRoster},
		{"class creation and deletion", classCreationAndDeletion},
		{"public event details", publicEventDetails},
		{"schedule release and visibility", publicSchedule},
		{"registration and session", registrationSession},
		{"password recovery", recoverPassword},
		{"credential changes", changeCredentials},
		{"login rate limit", loginRateLimit},
		{"admin impersonation", adminImpersonation},
		{"account lifecycle", accountLifecycle},
		{"member booking and cancellation", memberBooking},
		{"lesson waitlist", lessonWaitlist},
		{"participant roster and history", participantRosterAndHistory},
		{"participant roles", participantRoles},
		{"calendar subscription", calendarSubscription},
		{"family profiles", familyProfiles},
		{"family and privacy", familyPrivacy},
		{"announcements", announcements},
		{"mail log", mailLog},
		{"statistics", statistics},
		{"settings and branding", settingsAndBranding},
		{"push subscriptions", pushSubscriptions},
		{"import inspection", inspectImport},
		{"member import", importMembers},
		{"duplicate detection", detectDuplicates},
		{"person detail", personDetail},
		{"duplicate persons", mergeDuplicates},
		{"events and registrations", eventRegistration},
		{"pass type administration", passTypeAdministration},
		{"pass lists", passLists},
		{"pass sales and receipts", passReceipt},
		{"receipt pdf downloads", receiptPDFDownloads},
		{"receipt lists and search", receiptListsAndSearch},
		{"receipt settings and logo", receiptSettingsAndLogo},
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

	verifyPublicRoomFilter(t, b, client)
	readSeededTimetable(t, b, client)

	response := get(t, b, client, "/manifest.webmanifest")
	manifest := read[struct {
		ID          string
		Name        string
		ShortName   string `json:"short_name"`
		Description string
		Lang        string
		StartURL    string `json:"start_url"`
		Scope       string
		Display     string
		ThemeColor  string `json:"theme_color"`
		Background  string `json:"background_color"`
		Icons       []struct{ Src, Sizes, Type, Purpose string }
	}](t, response)
	u, _ := url.Parse(b.base)
	root := u.Path + "/"
	if manifest.Name != settings.SchoolName || manifest.ShortName != settings.SchoolName ||
		manifest.ID != root || manifest.Scope != root || manifest.StartURL != root ||
		manifest.Lang != settings.DefaultLocale || manifest.Description != "Class schedule and booking" ||
		manifest.Display != "standalone" || manifest.ThemeColor != "#7f1f35" || manifest.Background != "#faf7f2" {
		t.Errorf("installed app manifest: %+v", manifest)
	}
	if response.header.Get("Cache-Control") != "public, max-age=300" ||
		len(manifest.Icons) != 3 || manifest.Icons[0].Src != root+"icons/icon-192.png" ||
		manifest.Icons[1].Src != root+"icons/icon-512.png" ||
		manifest.Icons[2].Src != root+"icons/icon-maskable-512.png" || manifest.Icons[2].Purpose != "maskable" {
		t.Errorf("manifest cache or icons: %v %+v", response.header, manifest.Icons)
	}
}

func verifyPublicRoomFilter(t *testing.T, b *backend, client *http.Client) {
	t.Helper()

	type room struct{ ID, LocationID string }
	all := read[struct{ Items []room }](t, get(t, b, client, "/api/v1/rooms")).Items
	if len(all) == 0 {
		t.Fatal("fixture has no rooms")
	}
	in := request{method: http.MethodGet, path: "/api/v1/rooms", query: url.Values{"location": {all[0].LocationID}}}
	filtered := read[struct{ Items []room }](t, b.call(t, client, in, http.StatusOK)).Items
	want := slices.DeleteFunc(slices.Clone(all), func(r room) bool { return r.LocationID != all[0].LocationID })
	if !slices.Equal(filtered, want) {
		t.Errorf("rooms at one location: %+v, want %+v", filtered, want)
	}

	in.query.Set("location", "")
	unfiltered := read[struct{ Items []room }](t, b.call(t, client, in, http.StatusOK)).Items
	if !slices.Equal(unfiltered, all) {
		t.Errorf("empty location should leave rooms unfiltered: %+v", unfiltered)
	}
	in.query.Set("location", "00000000-0000-0000-0000-000000000001")
	b.call(t, client, in, http.StatusNotFound)
	for _, invalid := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		in.query.Set("location", invalid)
		b.call(t, client, in, http.StatusUnprocessableEntity)
	}

	for _, rawQuery := range []string{"location=%ZZ", "location&x=y"} {
		got, status := b.send(t, client, request{method: http.MethodGet, path: "/api/v1/rooms?" + rawQuery})
		if status != http.StatusOK {
			t.Fatalf("raw room query %q: got %d, want 200 (%s)", rawQuery, status, got.body)
		}
		if err := b.contract.validate(http.MethodGet, "/api/v1/rooms", status, got.header, got.body); err != nil {
			t.Fatal(err)
		}
		if decoded := read[struct{ Items []room }](t, got).Items; !slices.Equal(decoded, all) {
			t.Errorf("raw room query %q returned %+v, want all rooms", rawQuery, decoded)
		}
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
	const oldPassword = "conformance-first-password"
	const password = "conformance-renewed-password"
	client := browser(t)
	me := request{method: http.MethodGet, path: "/api/v1/auth/me"}
	b.call(t, client, me, http.StatusUnauthorized)
	register := request{
		method: http.MethodPost, path: "/api/v1/auth/register",
		body: `{"email":"` + demoMember + `","password":"` + oldPassword + `","givenName":"Fixture","familyName":"Member","locale":"en"}`,
	}
	known := b.call(t, client, register, http.StatusAccepted)
	outbox := read[struct {
		Mails []struct{ To, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	for _, mail := range outbox.Mails {
		if mail.To == demoMember && mail.Template == "confirm-email" {
			t.Fatal("registering an existing account queued a confirmation mail")
		}
	}
	register.body = strings.Replace(register.body, demoMember, email, 1)
	created := b.call(t, client, register, http.StatusAccepted)
	if string(known.body) != string(created.body) {
		t.Fatal("the registration response depends on whether the account was already confirmed")
	}
	staleToken := mailToken(t, b, email, "confirm-email", "/confirm")

	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-23T16:00:01Z"}`, http.StatusNoContent)
	register.body = `{"email":"` + email + `","password":"` + password + `","givenName":"Renewed","familyName":"Member","locale":"de"}`
	b.call(t, client, register, http.StatusAccepted)
	token := mailToken(t, b, email, "confirm-email", "/confirm")
	if token == staleToken {
		t.Fatal("registering a pending account must issue a different confirmation link")
	}

	confirm := request{
		method: http.MethodPost, path: "/api/v1/auth/confirm",
		body: `{"token":"` + staleToken + `","password":"` + password + `"}`,
	}
	b.call(t, client, confirm, http.StatusGone)
	confirm.body = `{"token":"` + token + `","password":"` + oldPassword + `"}`
	b.call(t, client, confirm, http.StatusUnauthorized)
	b.call(t, client, me, http.StatusUnauthorized)
	confirm.body = `{"token":"` + token + `","password":"` + password + `"}`
	b.call(t, client, confirm, http.StatusOK)
	account := read[struct{ Email, Role, GivenName, Locale string }](t, b.call(t, client, me, http.StatusOK))
	if account.Email != email || account.Role != "member" || account.GivenName != "Renewed" || account.Locale != "de" {
		t.Fatalf("signed-in account: %+v", account)
	}

	b.call(t, client, confirm, http.StatusGone)
	b.call(t, client, request{method: http.MethodPost, path: "/api/v1/auth/logout"}, http.StatusNoContent)
	b.call(t, client, me, http.StatusUnauthorized)
	login(t, b, client, email, password)
	b.call(t, client, me, http.StatusOK)

	for _, address := range []string{email, "unregistered@example.test"} {
		register.body = `{"email":"` + address + `","password":"` + password + `","givenName":"  ","familyName":"Member"}`
		invalid := b.call(t, client, register, http.StatusUnprocessableEntity)
		if !strings.Contains(string(invalid.body), "body.givenName") {
			t.Error("blank name did not produce a givenName validation error")
		}
	}
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
