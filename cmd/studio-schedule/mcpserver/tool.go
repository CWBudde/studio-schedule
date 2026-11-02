package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// tool is one entry of the registry.
type tool struct {
	name        string
	description string
	// access is the level of the operation the tool mirrors (ADR 0005):
	// the start's role must reach it for the tool to be listed, the
	// current role for a call to run.
	access httpapi.Access
	// personalData marks a tool that returns person data: listed only with
	// Options.PersonalData.
	personalData bool
	// write is the group of a write tool, "" for a read: listed only when
	// Options.AllowWrites has the group.
	write       WriteGroup
	annotations mcp.ToolAnnotations
	// add registers the tool on s with its typed input (handler).
	add func(s *Server, t tool)
}

// registry holds the tools, one list per file.
var registry = [][]tool{scheduleTools(), statsTools(), checkTools()}

// readOnly are the annotations of a tool that only reads the school's own
// data: repeating it changes nothing, and it reaches nothing outside.
func readOnly(title string) mcp.ToolAnnotations {
	closed := false

	return mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
}

// handler returns the add of a tool whose arguments are In: a struct of
// plain string fields, whose JSON schema (with the descriptions of their
// jsonschema tags) the session lists. A call runs through Server.call: the
// arguments are decoded only after the account was checked, so a refused
// call reads nothing of them, and arguments that do not fit In are an
// invalid call, audited like any other.
func handler[In any](run func(ctx context.Context, s *Server, p service.Principal, in In) (any, error)) func(*Server, tool) {
	schema := inputSchema[In]()

	return func(s *Server, t tool) {
		annotations := t.annotations

		s.mcp.AddTool(&mcp.Tool{
			Name: t.name, Title: t.annotations.Title, Description: t.description, InputSchema: schema,
			Annotations: &annotations,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return s.call(ctx, t, func(ctx context.Context, p service.Principal) (any, error) {
				in, err := decode[In](req.Params.Arguments)
				if err != nil {
					return nil, err
				}

				return run(ctx, s, p, in)
			})
		})
	}
}

// inputSchema is the JSON schema of In. A type that has none (a channel, a
// func) is a mistake in the tool's code, found when the registry is built.
func inputSchema[In any]() *jsonschema.Schema {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic("mcpserver: " + err.Error())
	}

	return schema
}

// errArguments wraps the errors of arguments that do not fit a tool's
// input.
var errArguments = fmt.Errorf("%w: the arguments do not fit the tool's input schema", domain.ErrValidation)

// errNotAnObject is the error of arguments that are no JSON object.
var errNotAnObject = fmt.Errorf("%w: they are not an object", errArguments)

// decode decodes the arguments of a call into In; no arguments are In's
// zero value. Arguments that are no object, null included, are refused:
// every input schema is an object, and null would decode to the zero
// input. Unknown properties are refused, as the schema says. The
// decoder's message names a property or a type, never a value.
func decode[In any](args json.RawMessage) (In, error) {
	var in In

	if len(args) == 0 {
		return in, nil
	}

	if trimmed := bytes.TrimSpace(args); len(trimmed) == 0 || trimmed[0] != '{' {
		return in, errNotAnObject
	}

	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&in); err != nil {
		return in, fmt.Errorf("%w: %w", errArguments, err)
	}

	return in, nil
}

// The outcomes of a call, recorded in its audit event (AuditDetails.Outcome).
const (
	outcomeOK        = "ok"
	outcomeForbidden = "forbidden"
	outcomeNotFound  = "not-found"
	outcomeInvalid   = "invalid"
	outcomeConflict  = "conflict"
	outcomeError     = "error"
)

// errInternal is what the client learns of an unexpected error; the log
// has the error.
var errInternal = errors.New("internal error")

// call runs fn as the current principal of the session and returns its
// document as the result:
//
//  1. It resolves the account of --as again (Actor), and
//  2. checks its current role against t's access level, so an account
//     disabled or demoted since the start is refused and fn does not run.
//  3. fn runs with the fresh principal, so the services' own rules see
//     the account as it is now.
//  4. The call is audited (mcp-tool-called) with t's name and the outcome:
//     ok, or the error's kind. A refused call is recorded as the start's
//     principal, whose Via names the MCP channel too.
//
// Neither the audit event nor the log ever gets the call's arguments: the
// log records only unexpected errors, by the tool's name, and no error of
// the caller's input, whose message may repeat it. If the audit event
// cannot be written, the call fails and returns nothing.
func (s *Server) call(
	ctx context.Context, t tool, fn func(ctx context.Context, p service.Principal) (any, error),
) (*mcp.CallToolResult, error) {
	p, err := s.authorize(ctx, t)

	var (
		text    []byte
		outcome string
	)

	if err != nil {
		p, outcome = s.start, refusal(err)
	} else {
		var doc any
		if doc, err = fn(ctx, p); err == nil {
			text, err = json.Marshal(doc) // a document that does not encode is the tool's error
		}

		outcome = outcomeOf(err)
	}

	if outcome == outcomeError {
		s.log.ErrorContext(ctx, "mcp: a tool call failed", "tool", t.name, "error", err)
		err = errInternal
	}

	if auditErr := s.svc.Auth.RecordToolCall(ctx, p, t.name, outcome); auditErr != nil {
		s.log.ErrorContext(ctx, "mcp: a tool call could not be audited", "tool", t.name, "error", auditErr)

		return errorResult(errInternal), nil
	}

	if err != nil {
		return errorResult(err), nil
	}

	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil
}

// authorize resolves the current principal and checks its role against
// t's access level.
func (s *Server) authorize(ctx context.Context, t tool) (service.Principal, error) {
	p, err := s.actor(ctx)
	if err != nil {
		return service.Principal{}, err
	}

	minimum, _ := t.access.MinRole()
	if !p.Role.AtLeast(minimum) {
		return service.Principal{}, fmt.Errorf("%w: the account's role %s is below %s, which the tool %s needs",
			domain.ErrForbidden, p.Role, minimum, t.name)
	}

	return p, nil
}

// refusal is the outcome of a call whose account was refused: an account
// that cannot act any more (not found: unknown, unconfirmed or disabled)
// or whose role is too low is forbidden; anything else, the store failing,
// is an error.
func refusal(err error) string {
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrForbidden) {
		return outcomeForbidden
	}

	return outcomeError
}

// outcomeOf is the outcome of a call whose tool returned err.
func outcomeOf(err error) string {
	switch {
	case err == nil:
		return outcomeOK
	case errors.Is(err, domain.ErrNotFound):
		return outcomeNotFound
	case errors.Is(err, domain.ErrValidation): // a *domain.FieldError too
		return outcomeInvalid
	case errors.Is(err, domain.ErrConflict): // ErrInUse and ErrDuplicate wrap it
		return outcomeConflict
	case errors.Is(err, domain.ErrForbidden):
		return outcomeForbidden
	default:
		return outcomeError
	}
}

// errorResult is the result of a failed call: the error's message for the
// assistant, which the caller's own input may be part of.
func errorResult(err error) *mcp.CallToolResult {
	var r mcp.CallToolResult

	r.SetError(err)

	return &r
}
