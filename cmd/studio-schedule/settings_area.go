package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// settingsArea is an area of the settings: a card of the settings page
// (ADR 0013) and its pair of API operations. Its body is the response body
// of the operations, its data the request body of the update, whose JSON
// field names are the area's keys.
type settingsArea interface {
	// name is the area as the commands take it, the last segment of its
	// API path (/api/v1/admin/settings/<name>); field is its field in a
	// document of all areas.
	name() string
	field() string
	// keys are the area's keys, in the order of the request body.
	keys() []settingKey
	// get reads the area: the response body of its get operation.
	get(ctx context.Context, s app.Services) (any, error)
	// set reads the area, changes the keys of values (JSON values of the
	// request body) and writes it back: the response body of its update
	// operation.
	set(ctx context.Context, env *cli.Env, values map[string]json.RawMessage) (any, error)
	// data is the area's part of settings: the request body.
	data(s domain.Settings) any
	// put adds the area's data in raw (a document's) to the import in.
	put(raw json.RawMessage, in *service.SettingsImport) error
}

// settingKey is a key of an area: a field of its request body.
type settingKey struct {
	name string
	typ  reflect.Type
	// optional: the request body may leave it out (omitempty, omitzero).
	optional bool
}

// area is a settingsArea with the response body B and the request body D.
type area[B, D any] struct {
	areaName, areaField string
	// read and write are the area's operations, through the services.
	read  func(ctx context.Context, s app.Services) (B, error)
	write func(ctx context.Context, env *cli.Env, d D) (B, error)
	// edit is the request body of a response body, of settings the request
	// body; update adds a request body to an import.
	edit   func(B) D
	of     func(domain.Settings) D
	update func(D, *service.SettingsImport)
}

func (a area[B, D]) name() string  { return a.areaName }
func (a area[B, D]) field() string { return a.areaField }

func (a area[B, D]) keys() []settingKey {
	t := reflect.TypeFor[D]()
	out := make([]settingKey, 0, t.NumField())

	for i := range t.NumField() {
		name, opts, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		optional := slices.ContainsFunc(strings.Split(opts, ","), func(o string) bool {
			return o == "omitempty" || o == "omitzero"
		})
		out = append(out, settingKey{name: name, typ: t.Field(i).Type, optional: optional})
	}

	return out
}

func (a area[B, D]) get(ctx context.Context, s app.Services) (any, error) { return a.read(ctx, s) }

func (a area[B, D]) set(ctx context.Context, env *cli.Env, values map[string]json.RawMessage) (any, error) {
	b, err := a.read(ctx, env.Services)
	if err != nil {
		return nil, err
	}

	// The request body as JSON, with the new values. The bodies are plain
	// data (texts, numbers, dates), and every value decodes into its field
	// (settingValue), so neither step fails.
	fields := map[string]json.RawMessage{}

	current, _ := json.Marshal(a.edit(b))
	_ = json.Unmarshal(current, &fields)

	for k, v := range values {
		fields[k] = v
	}

	changed, _ := json.Marshal(fields)

	var d D

	_ = json.Unmarshal(changed, &d)

	return a.write(ctx, env, d)
}

func (a area[B, D]) data(s domain.Settings) any { return a.of(s) }

func (a area[B, D]) put(raw json.RawMessage, in *service.SettingsImport) error {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return cli.Usagef("%s: %w", a.areaField, err)
	}

	for _, k := range a.keys() {
		if _, ok := present[k.name]; !ok && !k.optional {
			return cli.Usagef("%s.%s: missing; an area replaces all of its settings", a.areaField, k.name)
		}
	}

	var d D

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&d); err != nil {
		return cli.Usagef("%s: %w", a.areaField, err)
	}

	a.update(d, in)

	return nil
}

// view is a SettingsView of settings, for the converters of the cards.
func view(s domain.Settings) service.SettingsView { return service.SettingsView{Settings: s} }

// settingsAreas are the areas, in the order of the settings page: the
// cards of ADR 0013, then the receipt and the pass settings, which live
// with their services.
var settingsAreas = []settingsArea{
	area[httpapi.SchoolSettings, httpapi.SchoolSettings]{
		areaName: "school", areaField: "school",
		read: func(ctx context.Context, s app.Services) (httpapi.SchoolSettings, error) {
			v, err := s.Settings.School(ctx)

			return httpapi.NewSchoolSettings(v), err
		},
		write: func(ctx context.Context, env *cli.Env, d httpapi.SchoolSettings) (httpapi.SchoolSettings, error) {
			v, err := env.Services.Settings.SetSchool(ctx, d.Profile())

			return httpapi.NewSchoolSettings(v), err
		},
		edit:   func(b httpapi.SchoolSettings) httpapi.SchoolSettings { return b },
		of:     func(s domain.Settings) httpapi.SchoolSettings { return httpapi.NewSchoolSettings(view(s)) },
		update: func(d httpapi.SchoolSettings, in *service.SettingsImport) { in.School = new(d.Profile()) },
	},
	area[httpapi.BrandingSettings, httpapi.BrandingSettingsData]{
		areaName: "branding", areaField: "branding",
		read: func(ctx context.Context, s app.Services) (httpapi.BrandingSettings, error) {
			v, err := s.Settings.Branding(ctx)

			return httpapi.NewBrandingSettings(v), err
		},
		write: func(ctx context.Context, env *cli.Env, d httpapi.BrandingSettingsData) (httpapi.BrandingSettings, error) {
			v, err := env.Services.Settings.SetBranding(ctx, d.Branding())

			return httpapi.NewBrandingSettings(v), err
		},
		edit: httpapi.BrandingSettings.Data,
		of: func(s domain.Settings) httpapi.BrandingSettingsData {
			return httpapi.NewBrandingSettings(view(s)).Data()
		},
		update: func(d httpapi.BrandingSettingsData, in *service.SettingsImport) { in.Branding = new(d.Branding()) },
	},
	area[httpapi.RegionalSettings, httpapi.RegionalSettingsData]{
		areaName: "regional", areaField: "regional",
		read: func(ctx context.Context, s app.Services) (httpapi.RegionalSettings, error) {
			v, err := s.Settings.Regional(ctx)

			return httpapi.NewRegionalSettings(v), err
		},
		write: func(ctx context.Context, env *cli.Env, d httpapi.RegionalSettingsData) (httpapi.RegionalSettings, error) {
			v, err := env.Services.Settings.SetRegional(ctx, d.Regional())

			return httpapi.NewRegionalSettings(v), err
		},
		edit: func(b httpapi.RegionalSettings) httpapi.RegionalSettingsData { return b.RegionalSettingsData },
		of: func(s domain.Settings) httpapi.RegionalSettingsData {
			return httpapi.NewRegionalSettings(view(s)).RegionalSettingsData
		},
		update: func(d httpapi.RegionalSettingsData, in *service.SettingsImport) { in.Regional = new(d.Regional()) },
	},
	area[httpapi.BookingRules, httpapi.BookingRulesData]{
		areaName: "booking-rules", areaField: "bookingRules",
		read: func(ctx context.Context, s app.Services) (httpapi.BookingRules, error) {
			v, err := s.Settings.BookingRules(ctx)

			return httpapi.NewBookingRules(v), err
		},
		write: func(ctx context.Context, env *cli.Env, d httpapi.BookingRulesData) (httpapi.BookingRules, error) {
			v, err := env.Services.Settings.SetBookingRules(ctx, d.Rules())
			warnGenerate(env, v.GenerateErr)

			return httpapi.NewBookingRules(v), err
		},
		edit: func(b httpapi.BookingRules) httpapi.BookingRulesData { return b.BookingRulesData },
		of: func(s domain.Settings) httpapi.BookingRulesData {
			return httpapi.NewBookingRules(view(s)).BookingRulesData
		},
		update: func(d httpapi.BookingRulesData, in *service.SettingsImport) { in.BookingRules = new(d.Rules()) },
	},
	area[httpapi.ReceiptSettingsBody, httpapi.ReceiptSettingsData]{
		areaName: "receipts", areaField: "receipts",
		read: func(ctx context.Context, s app.Services) (httpapi.ReceiptSettingsBody, error) {
			v, err := s.Receipts.Settings(ctx)

			return httpapi.NewReceiptSettings(v), err
		},
		write: func(ctx context.Context, env *cli.Env, d httpapi.ReceiptSettingsData) (httpapi.ReceiptSettingsBody, error) {
			u := d.Update()
			v, err := env.Services.Receipts.SetSettings(ctx, u.Settings, u.Prefix)

			return httpapi.NewReceiptSettings(v), err
		},
		edit: func(b httpapi.ReceiptSettingsBody) httpapi.ReceiptSettingsData { return b.ReceiptSettingsData },
		of: func(s domain.Settings) httpapi.ReceiptSettingsData {
			return httpapi.NewReceiptSettings(service.ReceiptSettings{Settings: s.Receipts, Prefix: s.ReceiptPrefix}).
				ReceiptSettingsData
		},
		update: func(d httpapi.ReceiptSettingsData, in *service.SettingsImport) { in.Receipts = new(d.Update()) },
	},
	area[httpapi.PassSettings, httpapi.PassSettings]{
		areaName: "passes", areaField: "passes",
		read: func(ctx context.Context, s app.Services) (httpapi.PassSettings, error) {
			mode, err := s.Passes.RedemptionMode(ctx)

			return httpapi.NewPassSettings(mode), err
		},
		write: func(ctx context.Context, env *cli.Env, d httpapi.PassSettings) (httpapi.PassSettings, error) {
			mode := domain.RedemptionMode(d.RedemptionMode)
			if err := env.Services.Passes.SetRedemptionMode(ctx, mode); err != nil {
				return httpapi.PassSettings{}, err
			}

			return httpapi.NewPassSettings(mode), nil
		},
		edit: func(b httpapi.PassSettings) httpapi.PassSettings { return b },
		of:   func(s domain.Settings) httpapi.PassSettings { return httpapi.NewPassSettings(s.RedemptionMode) },
		update: func(d httpapi.PassSettings, in *service.SettingsImport) {
			in.RedemptionMode = new(domain.RedemptionMode(d.RedemptionMode))
		},
	},
}

// warnGenerate warns on stderr that the lessons of a wider window could
// not be generated at once: the change is stored, the hourly job
// generates them.
func warnGenerate(env *cli.Env, err error) {
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr(), "warning: %v; the hourly job generates them\n", err)
	}
}

// areaNames are the names of the areas, for messages.
func areaNames() string {
	names := make([]string, len(settingsAreas))
	for i, a := range settingsAreas {
		names[i] = a.name()
	}

	return strings.Join(names, ", ")
}

// areaNamed returns the area of name, or a usage error listing the areas.
func areaNamed(name string) (settingsArea, error) {
	for _, a := range settingsAreas {
		if a.name() == name {
			return a, nil
		}
	}

	return nil, cli.Usagef("unknown settings area %q: want one of %s", name, areaNames())
}

// keyNames are the names of keys, for messages.
func keyNames(keys []settingKey) string {
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.name
	}

	return strings.Join(names, ", ")
}

// settingValues parses the key=value arguments of `settings set` into the
// JSON values of a's request body. An argument without "=", a key the area
// does not have or names twice, or a value its field cannot take is a
// usage error naming it.
func settingValues(a settingsArea, args []string) (map[string]json.RawMessage, error) {
	keys := a.keys()
	values := make(map[string]json.RawMessage, len(args))

	for _, arg := range args {
		name, value, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, cli.Usagef("%q: want key=value", arg)
		}

		i := slices.IndexFunc(keys, func(k settingKey) bool { return k.name == name })
		if i < 0 {
			return nil, cli.Usagef("%s: unknown key of the %s settings; keys: %s", arg, a.name(), keyNames(keys))
		}

		if _, dup := values[name]; dup {
			return nil, cli.Usagef("%s: the key %s is given twice", arg, name)
		}

		raw, err := settingValue(keys[i].typ, value)
		if err != nil {
			return nil, cli.Usagef("%s: %w", arg, err)
		}

		values[name] = raw
	}

	return values, nil
}

// settingValue is value as the JSON value of a field of type t: a number
// for a number, else a string (a text, a time of day, a date; an empty
// date is none), checked by decoding it into t.
func settingValue(t reflect.Type, value string) (json.RawMessage, error) {
	var raw json.RawMessage

	if t.Kind() == reflect.Int {
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, errors.New("not a whole number")
		}

		raw = strconv.AppendInt(nil, int64(n), 10)
	} else {
		raw, _ = json.Marshal(value)
	}

	if err := json.Unmarshal(raw, reflect.New(t).Interface()); err != nil {
		return nil, err
	}

	return raw, nil
}

// fieldOfSetting names a settings field error by the key of the API's
// request body (httpapi.SettingsField): the domain counts the cutoffs in
// durations, the API in minutes.
func fieldOfSetting(err error) error {
	var fe *domain.FieldError
	if errors.As(err, &fe) {
		return &domain.FieldError{Field: httpapi.SettingsField(fe.Field), Msg: fe.Msg}
	}

	return err
}

// setting is a key of an area and its JSON value, as the text output
// shows it.
type setting struct {
	key   string
	value json.RawMessage
}

// settingRows flattens a body (a JSON object) into its keys and values in
// their order; an object inside (the booking rules' window) gives keys
// like window.now.
func settingRows(body any) []setting {
	raw := plainJSON(body) // the API's bodies

	var out []setting

	flatten(json.NewDecoder(bytes.NewReader(raw)), "", &out)

	return out
}

// areaRows are the settings of a body of the area a as the text output
// lists them: every key of a, a key the body leaves out as null, then the
// fields only shown.
func areaRows(a settingsArea, body any) []setting {
	all := settingRows(body)
	out := make([]setting, 0, len(all))

	for _, k := range a.keys() {
		out = append(out, setting{key: k.name, value: valueOf(all, k.name)})
	}

	for _, s := range all {
		if !slices.ContainsFunc(out, func(o setting) bool { return o.key == s.key }) {
			out = append(out, s)
		}
	}

	return out
}

// valueOf is the value of key among settings, null for none (a key the
// request body leaves out).
func valueOf(settings []setting, key string) json.RawMessage {
	for _, s := range settings {
		if s.key == key {
			return s.value
		}
	}

	return json.RawMessage("null")
}

// flatten reads the object that dec is at the start of, adding its keys
// under prefix.
func flatten(dec *json.Decoder, prefix string, out *[]setting) {
	_, _ = dec.Token() // {

	for dec.More() {
		tok, _ := dec.Token()
		key := prefix + tok.(string) //nolint:forcetypeassert // an object's keys are strings

		var raw json.RawMessage

		_ = dec.Decode(&raw)

		if raw[0] == '{' {
			flatten(json.NewDecoder(bytes.NewReader(raw)), key+".", out)

			continue
		}

		*out = append(*out, setting{key: key, value: raw})
	}
}

// settingText is the value of a setting as the text output shows it: a
// duration in days, hours and minutes, the release weekday by name, an
// amount in the school's currency, an instant in the school's time zone,
// yes or no, and a text with its line breaks as \n.
func settingText(t *cli.Text, currency, key string, raw json.RawMessage) string {
	var v any

	_ = json.Unmarshal(raw, &v)

	n, _ := v.(float64)

	switch {
	case v == nil:
		return ""
	case strings.HasSuffix(key, "Minutes"):
		return minutesText(int(n))
	case key == "releaseWeekday":
		return time.Weekday(n).String()
	case key == "addressRequiredAbove":
		return cli.Money(domain.Money(n), currency)
	case key == "logoMaxBytes":
		return strconv.Itoa(int(n)/1024) + " KiB"
	case strings.HasPrefix(key, "window."):
		at, _ := time.Parse(time.RFC3339, v.(string)) //nolint:forcetypeassert // the window's instants

		return t.Time(at)
	}

	switch v := v.(type) {
	case bool:
		if v {
			return "yes"
		}

		return "no"
	case string:
		return strings.ReplaceAll(v, "\n", `\n`)
	default:
		return string(raw)
	}
}

// minutesText is a number of minutes in days, hours and minutes: 90 is
// "1h 30m", 2880 "2d", 0 "0m".
func minutesText(minutes int) string {
	if minutes == 0 {
		return "0m"
	}

	var parts []string

	for _, unit := range []struct {
		size   int
		suffix string
	}{{24 * 60, "d"}, {60, "h"}, {1, "m"}} {
		if n := minutes / unit.size; n > 0 {
			parts = append(parts, strconv.Itoa(n)+unit.suffix)
			minutes -= n * unit.size
		}
	}

	return strings.Join(parts, " ")
}
