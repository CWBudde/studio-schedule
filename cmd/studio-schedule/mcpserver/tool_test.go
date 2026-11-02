package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// answer is what the test tool answers: its document, or its error.
type answer struct {
	doc any
	err error
}

// answering is a test tool of access that returns *a and counts its runs
// in *ran; its input has one optional argument, name.
func answering(access httpapi.Access, a *answer, ran *int) tool {
	type input struct {
		Name string `json:"name,omitempty" jsonschema:"A name."`
	}

	return tool{
		name: "probe", description: "a test tool", access: access, annotations: readOnly("Probe"),
		add: handler(func(_ context.Context, _ *Server, _ service.Principal, _ input) (any, error) {
			*ran++

			return a.doc, a.err
		}),
	}
}

// TestCallOutcomes: the outcome of each kind of error, in the result and
// in the audit event. The error's message reaches the client but the log
// only for an unexpected error, which the client sees as "internal error".
func TestCallOutcomes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		answer  answer
		outcome string
		text    string
		logged  bool
	}{
		"ok": {answer: answer{doc: map[string]int{"lessons": 3}}, outcome: "ok", text: `{"lessons":3}`},
		"not found": {
			answer: answer{err: fmt.Errorf("lesson: %w", domain.ErrNotFound)}, outcome: "not-found",
			text: "lesson: not found",
		},
		"field": {
			answer:  answer{err: &domain.FieldError{Field: "from", Msg: "from is not a date"}},
			outcome: "invalid", text: "validation failed: from is not a date",
		},
		"validation": {
			answer:  answer{err: fmt.Errorf("%w: to must be after from", domain.ErrValidation)},
			outcome: "invalid", text: "validation failed: to must be after from",
		},
		"in use": {answer: answer{err: domain.ErrInUse}, outcome: "conflict", text: "conflict: in use"},
		"forbidden": {
			answer: answer{err: fmt.Errorf("%w: not your lesson", domain.ErrForbidden)}, outcome: "forbidden",
			text: "forbidden: not your lesson",
		},
		"unexpected": {
			answer: answer{err: errors.New("disk on fire")}, outcome: "error", text: "internal error", logged: true,
		},
		"no JSON": {answer: answer{doc: make(chan int)}, outcome: "error", text: "internal error", logged: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			office := f.principal(t, demoOffice)

			var ran int

			a := tc.answer
			cs := f.session(t, demoOffice, Options{}, [][]tool{{answering(httpapi.AccessOffice, &a, &ran)}})

			text, isError := callTool(t, cs, "probe", map[string]any{"name": "Zebulon"})
			if text != tc.text || isError != (tc.outcome != "ok") || ran != 1 {
				t.Errorf("result %q (error %t), ran %d; want %q", text, isError, ran, tc.text)
			}

			calls := toolCalls(t, f.st)
			if len(calls) != 1 || calls[0].ActorID != office.AccountID || calls[0].Details != (domain.AuditDetails{
				Tool: "probe", Outcome: tc.outcome, Via: via,
			}) {
				t.Errorf("audit events %+v", calls)
			}

			log := f.log.String()
			if strings.Contains(log, "Zebulon") {
				t.Errorf("the log has the argument: %q", log)
			}

			if logged := strings.Contains(log, "level=ERROR") && strings.Contains(log, "tool=probe"); logged != tc.logged {
				t.Errorf("logged %t, want %t: %q", logged, tc.logged, log)
			}
		})
	}
}

// TestCallArguments: arguments that do not fit the tool's input are an
// invalid call, audited, and the tool does not run; none is the zero
// input.
func TestCallArguments(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	var ran int

	a := answer{doc: map[string]bool{"ok": true}}
	cs := f.session(t, demoMember, Options{}, [][]tool{{answering(httpapi.AccessSignedIn, &a, &ran)}})

	for _, args := range []any{
		map[string]any{"Zebulon": "x"},
		map[string]any{"name": 42},
		[]string{"Zebulon"},
		json.RawMessage("null"),
	} {
		text, isError := callTool(t, cs, "probe", args)
		if !isError || !strings.HasPrefix(text, "validation failed: the arguments do not fit the tool's input schema: ") {
			t.Errorf("%v: result %q (error %t)", args, text, isError)
		}
	}

	if ran != 0 {
		t.Errorf("the tool ran %d times on invalid arguments", ran)
	}

	if doc := document(t, cs, "probe", nil); doc["ok"] != true || ran != 1 {
		t.Errorf("no arguments: %v, ran %d", doc, ran)
	}

	calls := toolCalls(t, f.st)
	if len(calls) != 5 || calls[0].Details.Outcome != "invalid" || calls[3].Details.Outcome != "invalid" ||
		calls[4].Details.Outcome != "ok" {
		t.Errorf("audit events %+v", calls)
	}
}

// TestDecodeNotAnObject: arguments that are JSON but no object, null
// included, do not fit any tool's input, whose schema is an object; null
// would otherwise decode to the zero input and run with the defaults.
func TestDecodeNotAnObject(t *testing.T) {
	t.Parallel()

	for _, args := range []string{"null", " null ", "42", `"Zebulon"`, "true"} {
		if in, err := decode[scheduleInput](json.RawMessage(args)); !errors.Is(err, errArguments) {
			t.Errorf("decode(%s) = %+v, %v; want errArguments", args, in, err)
		}
	}
}

// TestDecodeNothing: a call without arguments, which the SDK's client
// never sends but another may, is the zero input.
func TestDecodeNothing(t *testing.T) {
	t.Parallel()

	if in, err := decode[scheduleInput](nil); err != nil || in != (scheduleInput{}) {
		t.Errorf("decode(nil) = %+v, %v", in, err)
	}
}

// TestCallRefused: before every call the account is resolved again and its
// role checked; a refused call does not run the tool, and its audit event
// names the principal of the start. A failure to resolve it is an
// unexpected error.
func TestCallRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		actor   func(member service.Principal) (service.Principal, error)
		outcome string
		text    string
	}{
		"disabled": {
			actor: func(service.Principal) (service.Principal, error) {
				return service.Principal{}, service.ErrActorInactive
			},
			outcome: "forbidden", text: service.ErrActorInactive.Error(),
		},
		"forbidden": {
			actor: func(service.Principal) (service.Principal, error) {
				return service.Principal{}, fmt.Errorf("--as: %w: the role is too low", domain.ErrForbidden)
			},
			outcome: "forbidden", text: "--as: forbidden: the role is too low",
		},
		"demoted": {
			actor:   func(member service.Principal) (service.Principal, error) { return member, nil },
			outcome: "forbidden",
			text:    "forbidden: the account's role member is below office, which the tool probe needs",
		},
		"store": {
			actor: func(service.Principal) (service.Principal, error) {
				return service.Principal{}, errors.New("actor: connection refused")
			},
			outcome: "error", text: "internal error",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			office, member := f.principal(t, demoOffice), f.principal(t, demoMember)

			var ran int

			a := answer{doc: map[string]bool{"ok": true}}
			cs := f.sessionOf(t, office, func(context.Context) (service.Principal, error) { return tc.actor(member) },
				Options{}, [][]tool{{answering(httpapi.AccessOffice, &a, &ran)}})

			wantError(t, cs, "probe", nil, tc.text)

			if ran != 0 {
				t.Errorf("the tool ran %d times", ran)
			}

			calls := toolCalls(t, f.st)
			if len(calls) != 1 || calls[0].ActorID != office.AccountID ||
				calls[0].Details != (domain.AuditDetails{Tool: "probe", Outcome: tc.outcome, Via: via}) {
				t.Errorf("audit events %+v", calls)
			}

			if logged := strings.Contains(f.log.String(), "connection refused"); logged != (tc.outcome == "error") {
				t.Errorf("log %q", f.log.String())
			}
		})
	}
}

// TestCallNotAudited: a call whose audit event cannot be written fails
// and returns nothing of the tool's document.
func TestCallNotAudited(t *testing.T) {
	t.Parallel()

	st := &faultStore{Store: newFixture(t).st}
	f := fixtureOn(st)

	var ran int

	a := answer{doc: map[string]string{"secret": "the document"}}
	cs := f.session(t, demoOffice, Options{}, [][]tool{{answering(httpapi.AccessOffice, &a, &ran)}})

	st.failing(store.ReadWrite, errors.New("disk full"))

	wantError(t, cs, "probe", nil, "internal error")

	if log := f.log.String(); ran != 1 || !strings.Contains(log, "could not be audited") ||
		!strings.Contains(log, "tool=probe") || !strings.Contains(log, "disk full") {
		t.Errorf("ran %d, log %q", ran, log)
	}
}

// TestInputSchemaOfInvalidType: a tool input without a JSON schema is a
// mistake found when the registry is built.
func TestInputSchemaOfInvalidType(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil || !strings.HasPrefix(fmt.Sprint(r), "mcpserver: ") {
			t.Errorf("recovered %v", r)
		}
	}()

	inputSchema[struct{ C chan int }]()
}
