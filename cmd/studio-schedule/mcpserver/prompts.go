package mcpserver

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// A prompt is a template of messages that tells the model which tools to
// call for a question (ADR 0022). It holds no data of its own: getting one
// reads only the school's language and time zone, so a prompt that leads to
// person data is listed only with --personal-data, like the tools it names.
// Its texts are in prompts/<locale>.json, one per domain.Locales, in the
// school's language (the settings' default locale); the name, title,
// description and arguments the session lists are English, like the tools'.
type prompt struct {
	name        string
	title       string
	description string
	// access is the level of the tools the prompt leads to: the start's
	// role must reach it for the prompt to be listed, the current role for
	// a get to run.
	access httpapi.Access
	// personalData marks a prompt that leads to person data: listed only
	// with Options.PersonalData.
	personalData bool
	args         []*mcp.PromptArgument
	// fill checks the arguments, which are known and given when required,
	// and sets their placeholders in d; today is the school date of now.
	fill func(args map[string]string, today domain.Date, d promptData) error
}

// prompts holds the prompts.
var prompts = []prompt{
	{
		name:  "weekly_overview",
		title: "Weekly overview",
		description: "A summary of one week's lessons from the schedule: cancellations, full lessons and " +
			"free places, day by day.",
		access: httpapi.AccessStaff,
		args: []*mcp.PromptArgument{{
			Name: "week", Title: "Week",
			Description: "A school date (YYYY-MM-DD) in the week, which runs from Monday to Sunday; default this week.",
		}},
		fill: fillWeek,
	},
	{
		name:  "waitlist_invitations",
		title: "Whom to invite from the waiting list",
		description: "Who is next on a lesson's waiting list for its free places, from the lesson's " +
			"participants and waiting list.",
		access:       httpapi.AccessOffice,
		personalData: true,
		args:         []*mcp.PromptArgument{lessonArgument},
		fill:         fillLesson,
	},
	{
		name:  "canceled_lesson_announcement",
		title: "Announcement for a canceled lesson",
		description: "A draft announcement about a canceled lesson, in the school's language, for the " +
			"office to review and publish.",
		access: httpapi.AccessOffice,
		args:   []*mcp.PromptArgument{lessonArgument},
		fill:   fillLesson,
	},
}

// lessonArgument is the argument of a prompt about one lesson.
var lessonArgument = &mcp.PromptArgument{
	Name: "lessonId", Title: "Lesson", Description: "The lesson's ID.", Required: true,
}

// promptData is the data of the prompts' texts: each key stands for the
// placeholder {{key}}.
type promptData map[string]string

// The placeholders of the prompts' texts.
var placeholders = []string{
	"zone", "today", "from", "to", "lessonId", "maxScheduleDays", "maxTitle", "maxBody",
}

// fillWeek sets from and to, the Monday and the Sunday of the argument
// week, a school date in it; this week without one.
func fillWeek(args map[string]string, today domain.Date, d promptData) error {
	day, err := parseDate("week", args["week"])
	if err != nil {
		return err
	}

	if day.IsZero() {
		day = today
	}

	// time.Weekday counts from Sunday; the school's weeks start on Monday.
	from := day.AddDays(-((int(day.Weekday()) + 6) % 7))

	to := from.AddDays(6)
	if !to.Valid() {
		return &domain.FieldError{Field: "week", Msg: "week ends after 9999-12-31"}
	}

	d["from"], d["to"] = from.String(), to.String()

	return nil
}

// fillLesson sets lessonId, the argument's lesson ID. A get reads nothing
// of the lesson: the tool the prompt names does, as the account.
func fillLesson(args map[string]string, _ domain.Date, d promptData) error {
	id, err := parseID[domain.LessonID]("lessonId", args["lessonId"])
	d["lessonId"] = id.String()

	return err
}

//go:embed prompts/*.json
var promptFiles embed.FS

// promptTexts are the prompts' texts by locale and name.
var promptTexts = mustParsePrompts(promptFiles)

// mustParsePrompts reads the texts of files: prompts/<locale>.json for
// each of domain.Locales, a JSON object with a text for every prompt. A
// missing file or prompt, an extra prompt or an unknown placeholder is a
// mistake in the texts, found when the package loads.
func mustParsePrompts(files fs.FS) map[string]map[string]string {
	texts, err := parsePrompts(files)
	if err != nil {
		panic("mcpserver: " + err.Error())
	}

	return texts
}

// placeholder matches a placeholder of a prompt's text.
var placeholder = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// parsePrompts is mustParsePrompts returning the error.
func parsePrompts(files fs.FS) (map[string]map[string]string, error) {
	names := make([]string, 0, len(prompts))
	for _, p := range prompts {
		names = append(names, p.name)
	}

	slices.Sort(names)

	out := make(map[string]map[string]string, len(domain.Locales))

	for _, locale := range domain.Locales {
		file := "prompts/" + locale + ".json"

		raw, err := fs.ReadFile(files, file)
		if err != nil {
			return nil, err
		}

		var texts map[string]string
		if err := json.Unmarshal(raw, &texts); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}

		if got := slices.Sorted(maps.Keys(texts)); !slices.Equal(got, names) {
			return nil, fmt.Errorf("%s: texts for %v, want %v", file, got, names)
		}

		for _, name := range names {
			for _, m := range placeholder.FindAllStringSubmatch(texts[name], -1) {
				if !slices.Contains(placeholders, m[1]) {
					return nil, fmt.Errorf("%s: %s has the unknown placeholder %s", file, name, m[0])
				}
			}
		}

		out[locale] = texts
	}

	return out, nil
}

// callee is the callee of p.
func (p prompt) callee() callee { return callee{name: p.name, kind: "prompt", access: p.access} }

// addPrompt registers p on s. A get runs through Server.run like a tool
// call: the account is checked again and the get is audited
// (mcp-tool-called with p's name), never with its arguments.
func (s *Server) addPrompt(p prompt) {
	s.mcp.AddPrompt(&mcp.Prompt{Name: p.name, Title: p.title, Description: p.description, Arguments: p.args},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			var res *mcp.GetPromptResult

			_, _, err := s.run(ctx, p.callee(), func(ctx context.Context, _ service.Principal) (any, error) {
				var err error
				res, err = s.render(ctx, p, req.Params.Arguments)

				return nil, err
			})
			if err != nil {
				return nil, promptError(err)
			}

			return res, nil
		})
}

// render is the text of p for args in the school's language. Arguments
// that p does not take or that it requires and lacks are refused before
// anything is read; the errors name an argument, never a value.
func (s *Server) render(ctx context.Context, p prompt, args map[string]string) (*mcp.GetPromptResult, error) {
	for _, name := range slices.Sorted(maps.Keys(args)) {
		if !slices.ContainsFunc(p.args, func(a *mcp.PromptArgument) bool { return a.Name == name }) {
			return nil, &domain.FieldError{Field: name, Msg: "the prompt takes no argument " + name}
		}
	}

	for _, a := range p.args {
		if a.Required && args[a.Name] == "" {
			return nil, &domain.FieldError{Field: a.Name, Msg: a.Name + " is required"}
		}
	}

	locale, loc, err := s.school(ctx)
	if err != nil {
		return nil, err
	}

	today := domain.DateOf(s.clk.Now(), loc)
	d := promptData{
		"zone": loc.String(), "today": today.String(),
		"maxScheduleDays": strconv.Itoa(service.MaxScheduleDays),
		"maxTitle":        strconv.Itoa(domain.MaxAnnouncementTitleLen),
		"maxBody":         strconv.Itoa(domain.MaxAnnouncementBodyLen),
	}

	if err := p.fill(args, today, d); err != nil {
		return nil, err
	}

	pairs := make([]string, 0, 2*len(d))
	for key, value := range d {
		pairs = append(pairs, "{{"+key+"}}", value)
	}

	text := strings.NewReplacer(pairs...).Replace(promptTexts[locale][p.name])

	return &mcp.GetPromptResult{
		Description: p.description,
		Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
	}, nil
}

// school is the school's language, one of domain.Locales, and its time
// zone: English and UTC before the school is set up.
func (s *Server) school(ctx context.Context) (string, *time.Location, error) {
	settings, err := s.svc.Settings.Public(ctx)

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return domain.Locales[0], time.UTC, nil
	case err != nil:
		return "", nil, err
	}

	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return "", nil, fmt.Errorf("the school's time zone: %w", err)
	}

	return settings.DefaultLocale, loc, nil // Public matches the locale to one of domain.Locales
}

// promptError is the JSON-RPC error of a get that failed with err, with
// run's message: invalid arguments are invalid params, as an unknown
// prompt is; a refusal or an unexpected error is an internal error, as
// JSON-RPC has no code for a refusal.
func promptError(err error) error {
	code := int64(jsonrpc.CodeInternalError)
	if errors.Is(err, domain.ErrValidation) {
		code = jsonrpc.CodeInvalidParams
	}

	return &jsonrpc.Error{Code: code, Message: err.Error()}
}
