package app_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
)

type blockedPasswordHasher struct {
	service.PasswordHasher
	checking chan<- struct{}
	proceed  <-chan struct{}
}

func (h blockedPasswordHasher) Verify(encoded, password string) (bool, error) {
	h.checking <- struct{}{}
	<-h.proceed

	return h.PasswordHasher.Verify(encoded, password)
}

func TestConcurrentCredentialRequestsRespectRateLimits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, path, body string
		free, status     int
		retry            string
	}{
		{"password guesses", "/api/v1/auth/login", "", 5, http.StatusUnauthorized, "900"},
		{"email requests", "/api/v1/auth/email", `{"newEmail":"updated@example.test","password":"` + seed.DemoPassword + `"}`, 1, http.StatusAccepted, "3600"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := newAuthApp(t, app.Deps{})
			checking := make(chan struct{}, tc.free+1)
			proceed := make(chan struct{})
			deps := a.deps
			deps.Hasher = blockedPasswordHasher{deps.Hasher, checking, proceed}
			// Create a session before installing the hasher barrier. The new
			// handler shares its store and clock, with fresh limits.
			member := demoMember(t)
			body := tc.body
			var cookies []*http.Cookie
			if tc.path == "/api/v1/auth/email" {
				cookies = append(cookies, a.login(member.Email))
			}
			a.h, _ = app.New(deps)
			if tc.path == "/api/v1/auth/email" {
				for range 4 {
					a.want(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"unknown@example.test"}`, http.StatusAccepted)
				}
			} else {
				body = loginBody(member.Email, "wrong fixture password")
			}

			results := make(chan authResponse, tc.free+1)
			var wg sync.WaitGroup
			closeBarrier := sync.OnceFunc(func() { close(proceed) })
			defer func() {
				closeBarrier()
				wg.Wait()
			}()
			deadline := time.After(10 * time.Second)
			for range tc.free + 1 {
				wg.Go(func() {
					res, _ := a.call(http.MethodPost, tc.path, body, cookies...)
					results <- res
				})
			}

			for range tc.free {
				select {
				case <-checking:
				case <-deadline:
					t.Fatal("timed out waiting for the admitted password checks")
				}
			}
			select {
			case <-checking:
				t.Fatal("an extra request reached password verification despite all slots being in use")
			case res := <-results:
				if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") != tc.retry {
					t.Fatalf("excess request returned %d with Retry-After %q", res.StatusCode, res.Header.Get("Retry-After"))
				}
			case <-deadline:
				t.Fatal("excess request did not return while password checks were in progress")
			}

			closeBarrier()
			for range tc.free {
				select {
				case res := <-results:
					if res.StatusCode != tc.status {
						t.Errorf("admitted request returned %d, want %d", res.StatusCode, tc.status)
					}
				case <-deadline:
					t.Fatal("timed out waiting for an admitted request")
				}
			}
		})
	}
}
