// Package conformance checks backend responses against the generated OpenAPI
// document without knowing how the backend is implemented.
package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type document struct {
	Version    string                          `json:"openapi"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas map[string]any `json:"schemas"`
	} `json:"components"`
}

type operation struct {
	Responses map[string]response `json:"responses"`
}

type response struct {
	Content map[string]mediaType  `json:"content"`
	Headers map[string]headerSpec `json:"headers"`
}

type mediaType struct {
	Schema map[string]any `json:"schema"`
}

type headerSpec struct {
	Required bool           `json:"required"`
	Schema   map[string]any `json:"schema"`
}

type contract struct {
	paths  map[string]map[string]operation
	schema *jsonschema.Schema
}

func readContract(r io.Reader) (*contract, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// OpenAPI 3.1 uses JSON Schema. Put its shared schemas in $defs so
	// their local references resolve without a dependency on a server.
	data = bytes.ReplaceAll(data, []byte(`"#/components/schemas/`), []byte(`"#/$defs/`))

	var doc document
	reader := json.NewDecoder(bytes.NewReader(data))
	reader.UseNumber()
	if err := reader.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode OpenAPI document: %w", err)
	}
	if !strings.HasPrefix(doc.Version, "3.1.") || len(doc.Paths) == 0 {
		return nil, errors.New("expected an OpenAPI 3.1 document with paths")
	}

	properties := make(map[string]any)

	for path, methods := range doc.Paths {
		for method, operation := range methods {
			for status, response := range operation.Responses {
				key := responseKey(method, path, status)
				for media, body := range response.Content {
					if body.Schema == nil {
						return nil, fmt.Errorf("%s %s has no body schema", key, media)
					}
					properties[key+" body "+media] = body.Schema
				}
				for name, header := range response.Headers {
					if header.Schema == nil {
						return nil, fmt.Errorf("%s header %s has no schema", key, name)
					}
					properties[key+" header "+name] = header.Schema
				}
			}
		}
	}

	const schemaURI = "https://conformance.example.test/schema"
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	compiler.RegisterFormat(&jsonschema.Format{Name: "int64", Validate: validateInt64})
	err = compiler.AddResource(schemaURI, map[string]any{
		"$defs": doc.Components.Schemas, "properties": properties,
	})
	if err != nil {
		return nil, fmt.Errorf("add response schemas: %w", err)
	}

	schema, err := compiler.Compile(schemaURI)
	if err != nil {
		return nil, fmt.Errorf("resolve response schemas: %w", err)
	}

	return &contract{paths: doc.Paths, schema: schema}, nil
}

func responseKey(method, path, status string) string {
	return strings.ToUpper(method) + " " + path + " " + status
}

func (c *contract) validate(method, path string, status int, headers http.Header, body []byte) error {
	op, ok := c.paths[path][strings.ToLower(method)]
	if !ok {
		return fmt.Errorf("no OpenAPI operation for %s %s", method, path)
	}

	code := strconv.Itoa(status)
	rs, ok := op.Responses[code]
	if !ok {
		code = "default"
		rs, ok = op.Responses[code]
	}
	if !ok {
		return fmt.Errorf("no OpenAPI response for %s %s with status %d", method, path, status)
	}

	key := responseKey(method, path, code)
	values := make(map[string]any)

	for name, spec := range rs.Headers {
		actual := headers.Values(name)
		if spec.Required && len(actual) == 0 {
			return fmt.Errorf("%s: required header %s missing", key, name)
		}
		for _, value := range actual {
			field := key + " header " + name
			if err := c.schema.Validate(map[string]any{field: value}); err != nil {
				return err
			}
		}
	}

	if len(rs.Content) == 0 {
		if len(body) != 0 {
			return fmt.Errorf("%s: expected an empty response", key)
		}
		return nil
	}

	media, _, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil {
		return fmt.Errorf("%s: invalid Content-Type: %w", key, err)
	}
	if _, ok := rs.Content[media]; !ok {
		return fmt.Errorf("%s: undocumented Content-Type %s", key, media)
	}

	var value any = string(body)
	if media == "application/json" || strings.HasSuffix(media, "+json") {
		value, err = jsonschema.UnmarshalJSON(bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("%s: invalid JSON: %w", key, err)
		}
	}
	values[key+" body "+media] = value

	return c.schema.Validate(values)
}

// OpenAPI's int64 format limits its integers to the signed 64-bit range.
func validateInt64(value any) error {
	if n, ok := value.(json.Number); ok {
		v, ok := new(big.Rat).SetString(n.String())
		if !ok || !v.IsInt() || !v.Num().IsInt64() {
			return errors.New("value is outside the int64 integer range")
		}
	}

	return nil
}
