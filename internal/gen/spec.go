// Gen is golagram's code generator (codegen stage 2): it reads
// scripts/api.json (produced by scripts/parse_botapi.py, stage 1) and emits
// types.gen.go and methods.gen.go at the repository root.
//
// Run it with `go generate ./...` from the repo root, or directly:
//
//	go run ./internal/gen
package main

import "encoding/json"

// Spec mirrors the top-level shape of scripts/api.json.
type Spec struct {
	APIVersion string `json:"api_version"`
	Items      []Item `json:"items"`
}

// Item is one entry from api.json: a type, a method, a union, or prose
// (section headers etc., which the generator ignores).
type Item struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"` // "type", "method", "union", "prose"
	Description string   `json:"description"`
	Fields      []Field  `json:"fields,omitempty"`  // kind == "type"
	Params      []Field  `json:"params,omitempty"`  // kind == "method"
	Returns     string   `json:"returns,omitempty"` // kind == "method"
	Members     []string `json:"members,omitempty"` // kind == "union"
}

// Field is one field of a type or one param of a method. Required is only
// populated for method params ("Yes" / "Optional"); type fields instead
// signal optionality with an "Optional. " prefix in Description.
type Field struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    string `json:"required,omitempty"`
	Description string `json:"description"`
}

// parseSpec unmarshals api.json's top-level JSON into a Spec.
func parseSpec(data []byte) (*Spec, error) {
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
