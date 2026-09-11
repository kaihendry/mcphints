package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"slices"
	"strings"
)

// Keep the full definition, including extensions and unknown annotation fields.
func (t *Tool) UnmarshalJSON(data []byte) error {
	type plain Tool
	if err := json.Unmarshal(data, (*plain)(t)); err != nil {
		return err
	}
	t.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (t Tool) MarshalJSON() ([]byte, error) {
	if t.Raw != nil {
		return t.Raw, nil
	}
	type plain Tool
	return json.Marshal(plain(t))
}

func (p payload) MarshalJSON() ([]byte, error) {
	type plain payload
	data, err := json.Marshal(plain(p))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range p.Fields {
		if _, known := fields[key]; !known {
			fields[key] = value
		}
	}
	return json.Marshal(fields)
}

func (p payload) ReportMetadata() map[string]json.RawMessage {
	fields := make(map[string]json.RawMessage)
	for key, value := range p.Fields {
		if key != "tools" && key != "server" && key != "fetched" {
			fields[key] = value
		}
	}
	return fields
}

func (t Tool) DisplayTitle() string {
	if t.Title != nil && strings.TrimSpace(*t.Title) != "" {
		return *t.Title
	}
	if t.Annotations != nil && t.Annotations.Title != nil && strings.TrimSpace(*t.Annotations.Title) != "" {
		return *t.Annotations.Title
	}
	return t.Name
}

type Parameter struct {
	Name, Type, Presence, Description string
	Enum, Default                     json.RawMessage
}

func (t Tool) Parameters() []Parameter {
	var schema struct {
		Properties map[string]json.RawMessage
		Required   []string
	}
	if json.Unmarshal(t.InputSchema, &schema) != nil {
		return nil
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	var parameters []Parameter
	for _, name := range names {
		var property struct {
			Type          json.RawMessage
			Description   string
			Enum, Default json.RawMessage
		}
		// Boolean schemas and composed/reference schemas remain available in full.
		_ = json.Unmarshal(schema.Properties[name], &property)
		kind := "see schema"
		if len(property.Type) > 0 {
			if json.Unmarshal(property.Type, &kind) != nil {
				kind = string(property.Type)
			}
		}
		presence := "optional"
		if slices.Contains(schema.Required, name) {
			presence = "required"
		}
		parameters = append(parameters, Parameter{name, kind, presence, property.Description, property.Enum, property.Default})
	}
	return parameters
}

func prettyJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "JSON unavailable: " + err.Error()
	}
	return string(data)
}

func writeJSONDetails(b *strings.Builder, label string, value any) {
	fmt.Fprintf(b, "\n<details>\n<summary>%s</summary>\n<pre>%s</pre>\n</details>\n\n", label, template.HTMLEscapeString(prettyJSON(value)))
}
