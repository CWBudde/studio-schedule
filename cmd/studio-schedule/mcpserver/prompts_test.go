package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// promptLesson is the lesson ID of the prompts about a lesson: a get
// reads nothing of the lesson, so any ID will do.
const promptLesson = "00000000-0000-7000-8000-00000000abcd"

// promptNames lists the prompts of cs by name, sorted.
func promptNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()

	res, err := cs.ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(res.Prompts))
	for _, p := range res.Prompts {
		names = append(names, p.Name)
	}

	slices.Sort(names)

	return names
}

// getPrompt gets the prompt name with args and returns the text of its one
// message; an error fails the test.
func getPrompt(t *testing.T, cs *mcp.ClientSession, name string, args map[string]string) string {
	t.Helper()

	res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("get %s %v: %v", name, args, err)
	}

	if len(res.Messages) != 1 || res.Messages[0].Role != "user" {
		t.Fatalf("get %s: messages %+v", name, res.Messages)
	}

	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("get %s: content %T", name, res.Messages[0].Content)
	}

	if strings.Contains(tc.Text, "{{") || strings.Contains(tc.Text, "}}") {
		t.Errorf("get %s: a placeholder is left in %q", name, tc.Text)
	}

	return tc.Text
}

// wantPromptError gets the prompt name with args and fails unless the get
// fails with the JSON-RPC error of code and message.
func wantPromptError(t *testing.T, cs *mcp.ClientSession, name string, args map[string]string, code int64, msg string) {
	t.Helper()

	res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: name, Arguments: args})

	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != code || rpcErr.Message != msg ||
		err.Error() != `calling "prompts/get": `+msg {
		t.Errorf("get %s %v: %+v, %v; want the error %d %q", name, args, res, err, code, msg)
	}
}

// setLocale stores locale as the school's default locale; the memory
// store takes any value, as settings written before ADR 0013 may hold.
func setLocale(t *testing.T, st store.Store, locale string) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		s.DefaultLocale = locale

		return tx.Settings().Put(ctx, s)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPromptsListed: a session lists the prompts whose access level the
// start's role reaches, waitlist_invitations, which leads to person data,
// only with PersonalData; members get none.
func TestPromptsListed(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	const (
		canceled = "canceled_lesson_announcement"
		waitlist = "waitlist_invitations"
		weekly   = "weekly_overview"
	)

	for _, tc := range []struct {
		as      string
		opts    Options
		prompts []string
	}{
		{demoMember, Options{}, []string{}},
		{demoMember, Options{PersonalData: true, AllowWrites: WriteGroups()}, []string{}},
		{demoInstructor, Options{}, []string{weekly}},
		{demoInstructor, Options{PersonalData: true}, []string{weekly}},
		{demoOffice, Options{}, []string{canceled, weekly}},
		{demoOffice, Options{AllowWrites: WriteGroups()}, []string{canceled, weekly}},
		{demoOffice, Options{PersonalData: true}, []string{canceled, waitlist, weekly}},
		{demoAdmin, Options{}, []string{canceled, weekly}},
		{demoAdmin, Options{PersonalData: true}, []string{canceled, waitlist, weekly}},
	} {
		cs := f.session(t, tc.as, tc.opts, nil)

		if got := promptNames(t, cs); !slices.Equal(got, tc.prompts) {
			t.Errorf("%s %+v: prompts %v, want %v", tc.as, tc.opts, got, tc.prompts)
		}
	}

	res, err := f.session(t, demoAdmin, Options{PersonalData: true}, nil).ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	lesson := []*mcp.PromptArgument{{Name: "lessonId", Title: "Lesson", Description: "The lesson's ID.", Required: true}}
	week := []*mcp.PromptArgument{{
		Name: "week", Title: "Week",
		Description: "A school date (YYYY-MM-DD) in the week, which runs from Monday to Sunday; default this week.",
	}}
	want := map[string]struct {
		title string
		args  []*mcp.PromptArgument
	}{
		canceled: {"Announcement for a canceled lesson", lesson},
		waitlist: {"Whom to invite from the waiting list", lesson},
		weekly:   {"Weekly overview", week},
	}

	for _, p := range res.Prompts {
		w := want[p.Name]
		if p.Title != w.title || p.Description == "" || len(p.Arguments) != len(w.args) {
			t.Errorf("%s: %+v", p.Name, p)

			continue
		}

		for i, a := range p.Arguments {
			if *a != *w.args[i] {
				t.Errorf("%s: argument %+v, want %+v", p.Name, a, w.args[i])
			}
		}
	}
}

// TestPromptsUnlisted (review focus 3): a prompt the session does not list
// is unknown: getting it by name fails as the SDK's unknown prompt, and
// nothing is audited.
func TestPromptsUnlisted(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	for _, tc := range []struct {
		as, name string
	}{
		{demoOffice, "waitlist_invitations"}, // without PersonalData
		{demoInstructor, "canceled_lesson_announcement"},
		{demoMember, "weekly_overview"},
		{demoAdmin, "no_such_prompt"},
	} {
		cs := f.session(t, tc.as, Options{}, nil)
		wantPromptError(t, cs, tc.name, map[string]string{"lessonId": promptLesson}, jsonrpc.CodeInvalidParams,
			fmt.Sprintf("unknown prompt %q", tc.name))
	}

	if calls := toolCalls(t, f.st); len(calls) != 0 {
		t.Errorf("audit events %+v", calls)
	}
}

// TestPromptsGolden: the prompts' texts in English, the demo school's
// language, and in German; no placeholder is left.
func TestPromptsGolden(t *testing.T) {
	t.Parallel()

	for _, locale := range domain.Locales {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			setLocale(t, f.st, locale)

			cs := f.session(t, demoAdmin, Options{PersonalData: true}, nil)

			for _, p := range prompts {
				args := map[string]string{}
				if p.name != "weekly_overview" {
					args["lessonId"] = promptLesson
				}

				getPrompt(t, cs, p.name, args) // no placeholder left

				res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: p.name, Arguments: args})
				if err != nil {
					t.Fatal(err)
				}

				if res.Description != p.description {
					t.Errorf("%s: description %q", p.name, res.Description)
				}

				doc, err := json.Marshal(res)
				if err != nil {
					t.Fatal(err)
				}

				golden(t, "prompts/"+p.name+"."+locale+".json", string(doc))
			}
		})
	}
}

// TestPromptsLanguage: the texts follow the school's default locale, a
// regional tag its base language and an unknown one English (as
// SettingsService.Public reads them).
func TestPromptsLanguage(t *testing.T) {
	t.Parallel()

	args := map[string]string{"lessonId": promptLesson}

	for _, tc := range []struct {
		locale, want string
	}{
		{"en", "en"}, {"de", "de"}, {"de-AT", "de"}, {"fr", "en"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			setLocale(t, f.st, tc.locale)

			cs := f.session(t, demoOffice, Options{}, nil)

			weekly := getPrompt(t, cs, "weekly_overview", nil)
			canceled := getPrompt(t, cs, "canceled_lesson_announcement", args)

			wantWeekly := map[string]string{
				"en": "Give me an overview of the school's week from Monday 2026-09-21 through Sunday 2026-09-27.",
				"de": "Gib mir einen Überblick über die Woche der Schule von Montag, 2026-09-21, bis Sonntag, 2026-09-27.",
			}[tc.want]
			wantCanceled := map[string]string{
				"en": "Draft an announcement about the canceled lesson " + promptLesson + ", in English.",
				"de": "Entwirf eine Mitteilung über die abgesagte Stunde " + promptLesson + ", auf Deutsch.",
			}[tc.want]

			if !strings.HasPrefix(weekly, wantWeekly+"\n") || !strings.HasPrefix(canceled, wantCanceled+"\n") {
				t.Errorf("texts\n%s\n%s", weekly, canceled)
			}

			for _, text := range []string{weekly, canceled} {
				if !strings.Contains(text, "Europe/Berlin") {
					t.Errorf("no school time zone in %q", text)
				}
			}

			if !strings.Contains(canceled, "2026-09-22") ||
				!strings.Contains(canceled, fmt.Sprint(service.MaxScheduleDays)) ||
				!strings.Contains(canceled, fmt.Sprint(domain.MaxAnnouncementTitleLen)) ||
				!strings.Contains(canceled, fmt.Sprint(domain.MaxAnnouncementBodyLen)) {
				t.Errorf("canceled_lesson_announcement lacks today or a limit: %q", canceled)
			}
		})
	}
}

// TestPromptsWeek: the week of weekly_overview runs from Monday to Sunday
// in the school's time zone: this week by default, else the week of the
// date given.
func TestPromptsWeek(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoInstructor, Options{}, nil)

	for _, tc := range []struct {
		week, from, to string
	}{
		{"", "2026-09-21", "2026-09-27"},           // testNow is a Tuesday
		{"2026-09-21", "2026-09-21", "2026-09-27"}, // a Monday
		{"2026-09-27", "2026-09-21", "2026-09-27"}, // a Sunday
		{"2026-09-28", "2026-09-28", "2026-10-04"},
		{"2026-10-01", "2026-09-28", "2026-10-04"},
		{"2027-01-01", "2026-12-28", "2027-01-03"}, // across the year
	} {
		var args map[string]string
		if tc.week != "" {
			args = map[string]string{"week": tc.week}
		}

		text := getPrompt(t, cs, "weekly_overview", args)
		if want := "from Monday " + tc.from + " through Sunday " + tc.to + "."; !strings.Contains(text, want) ||
			!strings.Contains(text, "with from "+tc.from+" and to "+tc.to+".") {
			t.Errorf("week %q: %q, want %q", tc.week, text, want)
		}
	}

	// Sunday 22:30 UTC is Monday 00:30 in Berlin: the school's week, and
	// its today, have begun.
	late := time.Date(2026, 9, 27, 22, 30, 0, 0, time.UTC)
	office := f.principal(t, demoOffice)

	s, err := newServer(f.svc, f.actorOf(demoOffice), office, clock.NewFixed(late), f.options(Options{}), "test", nil)
	if err != nil {
		t.Fatal(err)
	}

	cs = connect(t, s)

	if text := getPrompt(t, cs, "weekly_overview", nil); !strings.Contains(text, "Monday 2026-09-28 through Sunday 2026-10-04") {
		t.Errorf("late: %q", text)
	}

	text := getPrompt(t, cs, "canceled_lesson_announcement", map[string]string{"lessonId": promptLesson})
	if !strings.Contains(text, "from today, 2026-09-28,") {
		t.Errorf("late: %q", text)
	}
}

// TestPromptsInvalid (review focus 2): arguments the prompt does not take,
// a required one missing or empty, and values that do not parse are
// invalid params, audited as invalid. An error names the argument, never
// its value: the value reaches neither the error, the log nor the audit.
func TestPromptsInvalid(t *testing.T) {
	t.Parallel()

	const secret = "Zebulon"

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, nil)

	cases := []struct {
		name string
		args map[string]string
		msg  string
	}{
		{"canceled_lesson_announcement", nil, "validation failed: lessonId is required"},
		{"waitlist_invitations", map[string]string{"lessonId": ""}, "validation failed: lessonId is required"},
		{"canceled_lesson_announcement", map[string]string{"lessonId": secret}, "validation failed: lessonId is not an ID"},
		{"waitlist_invitations", map[string]string{"lessonId": secret}, "validation failed: lessonId is not an ID"},
		{"weekly_overview", map[string]string{"week": secret}, "validation failed: week is not a date (YYYY-MM-DD)"},
		{"weekly_overview", map[string]string{"week": "2026-02-30"}, "validation failed: week is not a date (YYYY-MM-DD)"},
		// An unknown argument is refused by its name, which the error
		// repeats, the first in name order, before a required one is
		// missed.
		{"weekly_overview", map[string]string{"lesson": secret}, "validation failed: the prompt takes no argument lesson"},
		{
			"canceled_lesson_announcement",
			map[string]string{"zone": secret, "date": secret},
			"validation failed: the prompt takes no argument date",
		},
	}

	for _, tc := range cases {
		wantPromptError(t, cs, tc.name, tc.args, jsonrpc.CodeInvalidParams, tc.msg)
	}

	calls := toolCalls(t, f.st)
	if len(calls) != len(cases) {
		t.Fatalf("%d audit events, want %d", len(calls), len(cases))
	}

	for i, e := range calls {
		if e.ActorID != office.AccountID || !e.TargetID.IsZero() || !e.TargetPersonID.IsZero() ||
			e.Details != (domain.AuditDetails{Tool: cases[i].name, Outcome: "invalid", Via: via}) {
			t.Errorf("audit event %d: %+v", i, e)
		}

		if doc, _ := json.Marshal(e); strings.Contains(string(doc), secret) {
			t.Errorf("audit event %d has the argument: %s", i, doc)
		}
	}

	if log := f.log.String(); strings.Contains(log, secret) || strings.Contains(log, "level=ERROR") {
		t.Errorf("log %q", log)
	}
}

// TestPromptsAudited: a get writes one mcp-tool-called event, actor the
// account, with the prompt's name and the outcome, and no target.
func TestPromptsAudited(t *testing.T) {
	t.Parallel()

	for _, p := range prompts {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			office := f.principal(t, demoOffice)
			cs := f.session(t, demoOffice, Options{PersonalData: true}, nil)

			args := map[string]string{"lessonId": promptLesson}
			if p.name == "weekly_overview" {
				args = map[string]string{"week": "2026-10-01"}
			}

			getPrompt(t, cs, p.name, args)

			calls := toolCalls(t, f.st)
			if len(calls) != 1 || calls[0].ActorID != office.AccountID || !calls[0].TargetID.IsZero() ||
				!calls[0].TargetPersonID.IsZero() ||
				calls[0].Details != (domain.AuditDetails{Tool: p.name, Outcome: "ok", Via: via}) {
				t.Errorf("audit events %+v", calls)
			}

			if strings.Contains(f.log.String(), "level=ERROR") {
				t.Errorf("log %q", f.log.String())
			}
		})
	}
}

// TestPromptsRefused (review focus 1): before every get the account is
// resolved again and its role checked; a refused get renders nothing and
// is audited as the start's principal.
func TestPromptsRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		actor   func(member service.Principal) (service.Principal, error)
		msg     string
		outcome string
	}{
		"disabled": {
			actor: func(service.Principal) (service.Principal, error) {
				return service.Principal{}, service.ErrActorInactive
			},
			msg: "not found: the account is not confirmed or is disabled", outcome: "forbidden",
		},
		"demoted": {
			actor: func(member service.Principal) (service.Principal, error) { return member, nil },
			msg:   "forbidden: the account's role member is below office, which the prompt canceled_lesson_announcement needs",
			// The refusal is no invalid param but an internal error:
			// JSON-RPC has no code for a refusal.
			outcome: "forbidden",
		},
		"store": {
			actor: func(service.Principal) (service.Principal, error) {
				return service.Principal{}, errors.New("actor: connection refused")
			},
			msg: "internal error", outcome: "error",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			office, member := f.principal(t, demoOffice), f.principal(t, demoMember)
			cs := f.sessionOf(t, office, func(context.Context) (service.Principal, error) { return tc.actor(member) },
				Options{}, nil)

			wantPromptError(t, cs, "canceled_lesson_announcement", map[string]string{"lessonId": promptLesson}, jsonrpc.CodeInternalError, tc.msg)

			calls := toolCalls(t, f.st)
			if len(calls) != 1 || calls[0].ActorID != office.AccountID || calls[0].Details != (domain.AuditDetails{
				Tool: "canceled_lesson_announcement", Outcome: tc.outcome, Via: via,
			}) {
				t.Errorf("audit events %+v", calls)
			}

			if logged := strings.Contains(f.log.String(), "connection refused"); logged != (tc.outcome == "error") {
				t.Errorf("log %q", f.log.String())
			}
		})
	}
}

// failingSettings is a store whose settings fail to read once fail is set;
// the rest, the accounts and the audit included, works.
type failingSettings struct {
	store.Store

	fail *atomic.Bool
}

func (s failingSettings) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, failingSettingsTx{tx, s.fail})
	})
}

type failingSettingsTx struct {
	store.Tx

	fail *atomic.Bool
}

func (tx failingSettingsTx) Settings() store.SettingsRepo {
	return failingSettingsRepo{tx.Tx.Settings(), tx.fail}
}

type failingSettingsRepo struct {
	store.SettingsRepo

	fail *atomic.Bool
}

func (r failingSettingsRepo) Get(ctx context.Context) (domain.Settings, error) {
	if r.fail.Load() {
		return domain.Settings{}, errors.New("connection reset")
	}

	return r.SettingsRepo.Get(ctx)
}

// TestPromptsFailures: settings that fail to load, a time zone the host
// lacks and an audit event that cannot be written are unexpected errors:
// the client sees "internal error", the log the cause by the prompt's
// name.
func TestPromptsFailures(t *testing.T) {
	t.Parallel()

	t.Run("settings fail", func(t *testing.T) {
		t.Parallel()

		var fail atomic.Bool

		f := fixtureOn(failingSettings{newFixture(t).st, &fail})
		office := f.principal(t, demoOffice)
		cs := f.session(t, demoOffice, Options{}, nil)

		fail.Store(true)

		wantPromptError(t, cs, "weekly_overview", nil, jsonrpc.CodeInternalError, "internal error")

		calls := toolCalls(t, f.st)
		if len(calls) != 1 || calls[0].ActorID != office.AccountID ||
			calls[0].Details != (domain.AuditDetails{Tool: "weekly_overview", Outcome: "error", Via: via}) {
			t.Errorf("audit events %+v", calls)
		}

		if log := f.log.String(); !strings.Contains(log, "level=ERROR") || !strings.Contains(log, "tool=weekly_overview") ||
			!strings.Contains(log, "connection reset") {
			t.Errorf("log %q", log)
		}
	})

	t.Run("unknown time zone", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)

		err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			s, err := tx.Settings().Get(ctx)
			if err != nil {
				return err
			}

			s.Timezone = "Atlantis/Poseidonia"

			return tx.Settings().Put(ctx, s)
		})
		if err != nil {
			t.Fatal(err)
		}

		cs := f.session(t, demoOffice, Options{}, nil)
		wantPromptError(t, cs, "canceled_lesson_announcement", map[string]string{"lessonId": promptLesson}, jsonrpc.CodeInternalError,
			"internal error")

		if log := f.log.String(); !strings.Contains(log, "tool=canceled_lesson_announcement") ||
			!strings.Contains(log, "the school's time zone") {
			t.Errorf("log %q", log)
		}
	})

	t.Run("not audited", func(t *testing.T) {
		t.Parallel()

		st := &faultStore{Store: newFixture(t).st}
		f := fixtureOn(st)
		cs := f.session(t, demoOffice, Options{}, nil)

		st.failing(store.ReadWrite, errors.New("disk full"))

		wantPromptError(t, cs, "weekly_overview", nil, jsonrpc.CodeInternalError, "internal error")

		if log := f.log.String(); !strings.Contains(log, "could not be audited") ||
			!strings.Contains(log, "tool=weekly_overview") || !strings.Contains(log, "disk full") {
			t.Errorf("log %q", log)
		}
	})
}

// TestPromptsWithoutSchool: before the school is set up, the prompts are
// in English and their times in UTC.
func TestPromptsWithoutSchool(t *testing.T) {
	t.Parallel()

	empty := memory.New()
	s := &Server{
		svc: app.NewServices(app.Deps{Store: empty, Clock: clock.NewFixed(testNow), IDs: ids.NewSequence()}),
		clk: clock.NewFixed(testNow),
	}

	locale, loc, err := s.school(t.Context())
	if err != nil || locale != "en" || loc != time.UTC {
		t.Errorf("school() = %q, %v, %v", locale, loc, err)
	}

	f := fixtureOn(noSchool{newFixture(t).st})
	cs := f.session(t, demoOffice, Options{}, nil)

	text := getPrompt(t, cs, "weekly_overview", nil)
	if !strings.HasPrefix(text, "Give me an overview") || !strings.Contains(text, "the school's time zone, UTC.") {
		t.Errorf("text %q", text)
	}
}

// promptFS returns the prompts' texts as files, with change applied to
// the texts of each locale before they are encoded.
func promptFS(t *testing.T, change func(locale string, texts map[string]string)) fstest.MapFS {
	t.Helper()

	files := fstest.MapFS{}

	for _, locale := range domain.Locales {
		texts := maps.Clone(promptTexts[locale])
		change(locale, texts)

		raw, err := json.Marshal(texts)
		if err != nil {
			t.Fatal(err)
		}

		files["prompts/"+locale+".json"] = &fstest.MapFile{Data: raw}
	}

	return files
}

// TestParsePrompts: the embedded texts load; a missing file or prompt, an
// extra prompt, invalid JSON or an unknown placeholder is an error, and
// mustParsePrompts panics on it.
func TestParsePrompts(t *testing.T) {
	t.Parallel()

	texts, err := parsePrompts(promptFiles)
	if err != nil || !maps.EqualFunc(texts, promptTexts, maps.Equal) || len(texts) != len(domain.Locales) {
		t.Fatalf("parsePrompts(promptFiles) = %v, %v", texts, err)
	}

	same := func(string, map[string]string) {}

	if _, err := parsePrompts(promptFS(t, same)); err != nil {
		t.Errorf("the texts re-encoded: %v", err)
	}

	missingFile := promptFS(t, same)
	delete(missingFile, "prompts/de.json")

	invalid := promptFS(t, same)
	invalid["prompts/de.json"] = &fstest.MapFile{Data: []byte(`{"weekly_overview": `)}

	names := "[canceled_lesson_announcement waitlist_invitations weekly_overview]"

	for name, tc := range map[string]struct {
		files fstest.MapFS
		err   string
	}{
		"missing file": {missingFile, "open prompts/de.json: file does not exist"},
		"invalid JSON": {invalid, "prompts/de.json: unexpected end of JSON input"},
		"missing prompt": {
			promptFS(t, func(locale string, texts map[string]string) {
				if locale == "de" {
					delete(texts, "weekly_overview")
				}
			}),
			"prompts/de.json: texts for [canceled_lesson_announcement waitlist_invitations], want " + names,
		},
		"extra prompt": {
			promptFS(t, func(_ string, texts map[string]string) { texts["birthdays"] = "Whose birthday is it?" }),
			"prompts/en.json: texts for [birthdays canceled_lesson_announcement waitlist_invitations weekly_overview], " +
				"want " + names,
		},
		"unknown placeholder": {
			promptFS(t, func(locale string, texts map[string]string) {
				if locale == "en" {
					texts["waitlist_invitations"] += " Greet {{personName}}."
				}
			}),
			"prompts/en.json: waitlist_invitations has the unknown placeholder {{personName}}",
		},
		"empty placeholder": {
			promptFS(t, func(locale string, texts map[string]string) {
				if locale == "de" {
					texts["weekly_overview"] += " {{}}"
				}
			}),
			"prompts/de.json: weekly_overview has the unknown placeholder {{}}",
		},
	} {
		if got, err := parsePrompts(tc.files); got != nil || err == nil || err.Error() != tc.err {
			t.Errorf("%s: %v, %v; want %q", name, got, err, tc.err)
		}
	}

	func() {
		defer func() {
			if r := recover(); fmt.Sprint(r) != "mcpserver: open prompts/de.json: file does not exist" {
				t.Errorf("recovered %v", r)
			}
		}()

		mustParsePrompts(missingFile)
	}()
}

// TestPromptTextsAgree: a prompt's texts use the same placeholders in
// every language, so a translation cannot drop the data it is given.
func TestPromptTextsAgree(t *testing.T) {
	t.Parallel()

	uses := func(text string) []string {
		var keys []string
		for _, m := range placeholder.FindAllStringSubmatch(text, -1) {
			keys = append(keys, m[1])
		}

		slices.Sort(keys)

		return slices.Compact(keys)
	}

	for _, p := range prompts {
		want := uses(promptTexts[domain.Locales[0]][p.name])
		if len(want) == 0 {
			t.Errorf("%s: no placeholder", p.name)
		}

		for _, locale := range domain.Locales[1:] {
			if got := uses(promptTexts[locale][p.name]); !slices.Equal(got, want) {
				t.Errorf("%s: %s uses %v, %s %v", p.name, locale, got, domain.Locales[0], want)
			}
		}
	}
}
