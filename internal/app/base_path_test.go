package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/webui"
)

func TestAppBasePath(t *testing.T) {
	t.Parallel()

	const prefix = "/dance/schedule"
	const origin = "https://school.example.org"
	a := newAuthApp(t, app.Deps{
		AppURL: origin + prefix + "/", BasePath: prefix, HostCookie: true,
		UI: webui.NewHandler(fstest.MapFS{
			"index.html":    {Data: []byte(`<html><base href="./"><div>app</div></html>`)},
			"assets/app.js": {Data: []byte("export {};")},
		}, prefix),
	})

	get := func(path string) []byte {
		t.Helper()

		res, body := a.call(http.MethodGet, prefix+path, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, res.StatusCode, body)
		}

		return body
	}

	res, _ := a.call(http.MethodGet, prefix+"?next=events", "")
	if res.StatusCode != http.StatusPermanentRedirect || res.Header.Get("Location") != prefix+"/?next=events" {
		t.Fatalf("mount redirect: %d %q", res.StatusCode, res.Header.Get("Location"))
	}

	for _, path := range []string{
		"/healthz", "/api/v1/health", prefix + "-other/api/v1/health",
		prefix + "/api/v1/missing", prefix + "/assets/missing.js",
	} {
		a.want(http.MethodGet, path, "", http.StatusNotFound)
	}

	for _, path := range []string{"/healthz", "/readyz", "/assets/app.js"} {
		get(path)
	}

	for _, path := range []string{"/", "/index.html", "/events/workshop"} {
		body := get(path)
		if !strings.Contains(string(body), `<base href="`+prefix+`/">`) {
			t.Errorf("%s: %s", path, body)
		}
	}

	res, _ = a.call(http.MethodGet, prefix+"/api/v1/health", "")
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("API health: %d, Cache-Control %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}

	docs := get("/api/v1/docs")
	if !strings.Contains(string(docs), prefix+"/api/v1/openapi.") {
		t.Error("API documentation points outside the base path")
	}

	var apiDoc struct {
		Servers []struct{ URL string }
	}
	if err := json.Unmarshal(get("/api/v1/openapi.json"), &apiDoc); err != nil {
		t.Fatal(err)
	}
	if len(apiDoc.Servers) != 1 || apiDoc.Servers[0].URL != prefix {
		t.Errorf("OpenAPI servers: %+v", apiDoc.Servers)
	}

	body := get("/manifest.webmanifest")
	var manifest httpapi.WebManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ID != prefix+"/" || manifest.Scope != prefix+"/" || manifest.StartURL != prefix+"/" {
		t.Errorf("manifest paths: %+v", manifest)
	}
	for _, icon := range manifest.Icons {
		if !strings.HasPrefix(icon.Src, prefix+"/icons/") {
			t.Errorf("manifest icon: %s", icon.Src)
		}
	}

	member := demoMember(t)
	res, _ = a.call(http.MethodPost, prefix+"/api/v1/auth/login", loginBody(member.Email, member.Password))
	cookie := sessionCookie(t, res)
	if res.StatusCode != http.StatusOK || cookie.Name != httpapi.SessionCookie || cookie.Path != prefix || !cookie.Secure {
		t.Fatalf("login: %d, cookie %+v", res.StatusCode, cookie)
	}
	a.want(http.MethodGet, prefix+"/api/v1/auth/me", "", http.StatusOK, cookie)

	body = a.expect(http.StatusCreated, http.MethodPost, prefix+"/api/v1/my/calendar-feeds", `{"scope":"bookings"}`, cookie)
	var feed httpapi.NewCalendarFeed
	if err := json.Unmarshal(body, &feed); err != nil {
		t.Fatal(err)
	}
	if path := prefix + "/ical/" + feed.Token + ".ics"; feed.Path != path || feed.URL != origin+path {
		t.Fatalf("calendar feed path and URL: %+v", feed)
	}
	a.want(http.MethodGet, feed.Path, "", http.StatusOK)

	res, _ = a.call(http.MethodPost, prefix+"/api/v1/auth/logout", "", cookie)
	cleared := sessionCookie(t, res)
	if res.StatusCode != http.StatusNoContent || cleared.Path != prefix || cleared.Name != cookie.Name ||
		!cleared.Secure || cleared.MaxAge >= 0 {
		t.Errorf("logout: %d, cookie %+v", res.StatusCode, cleared)
	}
}

func TestDemoCalendarFeedUnderBasePath(t *testing.T) {
	t.Parallel()

	const prefix = "/demo"
	a := newAuthApp(t, app.Deps{Demo: true, BasePath: prefix})

	member := demoMember(t)
	res, _ := a.call(http.MethodPost, prefix+"/api/v1/auth/login", loginBody(member.Email, member.Password))
	cookie := sessionCookie(t, res)
	body := a.expect(http.StatusCreated, http.MethodPost, prefix+"/api/v1/my/calendar-feeds", `{"scope":"bookings"}`, cookie)
	var feed httpapi.NewCalendarFeed
	if err := json.Unmarshal(body, &feed); err != nil {
		t.Fatal(err)
	}
	if feed.URL != "" || !strings.HasPrefix(feed.Path, prefix+"/ical/") {
		t.Fatalf("demo feed links: %+v", feed)
	}
	a.want(http.MethodGet, feed.Path, "", http.StatusOK)
}
