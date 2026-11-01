package domain_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestJobRunValidate(t *testing.T) {
	t.Parallel()

	t.Run("Failed", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.JobRun {
			return domain.JobRun{
				ID: newID[domain.JobRunID](), Job: "generate-lessons", StartedAt: t0, EndedAt: t0.Add(time.Second),
				Result: domain.JobFailed, Error: `job "generate-lessons": store down`,
			}
		}, map[string]func(*domain.JobRun){
			"zero id":        func(r *domain.JobRun) { r.ID = domain.JobRunID{} },
			"blank job":      func(r *domain.JobRun) { r.Job = " " },
			"long job":       func(r *domain.JobRun) { r.Job = strings.Repeat("j", domain.MaxJobNameLen+1) },
			"no start":       func(r *domain.JobRun) { r.StartedAt = time.Time{} },
			"no end":         func(r *domain.JobRun) { r.EndedAt = time.Time{} },
			"ended early":    func(r *domain.JobRun) { r.EndedAt = t0.Add(-time.Second) },
			"unknown result": func(r *domain.JobRun) { r.Result = "done" },
			"no error":       func(r *domain.JobRun) { r.Error = "" },
			"long error":     func(r *domain.JobRun) { r.Error = strings.Repeat("é", domain.MaxJobRunErrorLen+1) },
			"invalid utf-8":  func(r *domain.JobRun) { r.Error = "bad \xff byte" },
		})
	})

	t.Run("OK", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.JobRun {
			return domain.JobRun{
				ID: newID[domain.JobRunID](), Job: strings.Repeat("j", domain.MaxJobNameLen), StartedAt: t0, EndedAt: t0,
				Result: domain.JobOK,
			}
		}, map[string]func(*domain.JobRun){
			"with error": func(r *domain.JobRun) { r.Error = "oops" },
		})
	})

	t.Run("Skipped", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.JobRun {
			return domain.JobRun{
				ID: newID[domain.JobRunID](), Job: "deliver-mail", StartedAt: t0, EndedAt: t0, Result: domain.JobSkipped,
			}
		}, map[string]func(*domain.JobRun){
			"with error": func(r *domain.JobRun) { r.Error = "held elsewhere" },
		})
	})
}

func TestJobRunResults(t *testing.T) {
	t.Parallel()

	for _, r := range domain.JobRunResults() {
		if !r.Valid() {
			t.Errorf("%q is not valid", r)
		}
	}

	for _, r := range []domain.JobRunResult{"", "OK", "running"} {
		if r.Valid() {
			t.Errorf("%q is valid", r)
		}
	}
}

func TestJobRunError(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", domain.MaxJobRunErrorLen+5)

	for name, tc := range map[string]struct{ in, want string }{
		"kept":            {in: `job "purge-mail": store down`, want: `job "purge-mail": store down`},
		"empty":           {in: "", want: "unknown error"},
		"blank":           {in: " \n", want: "unknown error"},
		"invalid utf-8":   {in: "bad \xff byte", want: "bad � byte"},
		"exactly the max": {in: long[:2*domain.MaxJobRunErrorLen], want: long[:2*domain.MaxJobRunErrorLen]},
		"cut":             {in: long, want: strings.Repeat("é", domain.MaxJobRunErrorLen-1) + "…"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := domain.JobRunError(tc.in)
			if got != tc.want {
				t.Errorf("JobRunError(%q) = %q, want %q", tc.in, got, tc.want)
			}

			if n := utf8.RuneCountInString(got); n > domain.MaxJobRunErrorLen {
				t.Errorf("JobRunError returned %d characters", n)
			}

			run := domain.JobRun{
				ID: newID[domain.JobRunID](), Job: "purge-mail", StartedAt: t0, EndedAt: t0,
				Result: domain.JobFailed, Error: got,
			}
			if err := run.Validate(); err != nil {
				t.Errorf("a run with the error is invalid: %v", err)
			}
		})
	}
}
