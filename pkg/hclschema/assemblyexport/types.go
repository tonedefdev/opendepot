package assemblyexport

import "encoding/json"

const SchemaVersion = "assembly.export.v1"

type Request struct {
	SchemaVersion string         `json:"schemaVersion"`
	Variables     []VariableNode `json:"variables"`
	Modules       []ModuleNode   `json:"modules"`
	Providers     []ProviderNode `json:"providers,omitempty"`
}

type TypeSpec struct {
	Kind       string          `json:"kind"`
	Element    *TypeSpec       `json:"element,omitempty"`
	Elements   []TypeSpec      `json:"elements,omitempty"`
	Attributes []TypeAttribute `json:"attributes,omitempty"`
}

type TypeAttribute struct {
	Name       string     `json:"name"`
	Type       TypeSpec   `json:"type"`
	Optional   bool       `json:"optional,omitempty"`
	HasDefault bool       `json:"hasDefault,omitempty"`
	Default    *ValueSpec `json:"default,omitempty"`
}

type ValueSpec struct {
	Kind    string       `json:"kind"`
	Literal string       `json:"literal,omitempty"`
	Items   []ValueSpec  `json:"items,omitempty"`
	Entries []ValueEntry `json:"entries,omitempty"`
}

type ValueEntry struct {
	Key   string    `json:"key,omitempty"`
	Name  string    `json:"name,omitempty"`
	Value ValueSpec `json:"value"`
}

type VariableNode struct {
	NodeID      string               `json:"nodeId"`
	Name        string               `json:"name"`
	Type        TypeSpec             `json:"type"`
	Description string               `json:"description,omitempty"`
	Validations []VariableValidation `json:"validations,omitempty"`
	HasDefault  bool                 `json:"hasDefault,omitempty"`
	Default     ValueSpec            `json:"default"`
}

type VariableValidation struct {
	Condition    string `json:"condition"`
	ErrorMessage string `json:"errorMessage"`
}

type FieldValue struct {
	Mode              string             `json:"mode"`
	Literal           string             `json:"literal,omitempty"`
	RefNodeID         string             `json:"refNodeId,omitempty"`
	RefOutput         string             `json:"refOutput,omitempty"`
	RefSelector       *ReferenceSelector `json:"refSelector,omitempty"`
	RefOutputSelector *ReferenceSelector `json:"refOutputSelector,omitempty"`
}

type InputValue struct {
	Kind    string            `json:"kind"`
	Value   *FieldValue       `json:"value,omitempty"`
	Items   []InputValue      `json:"items,omitempty"`
	Entries []InputValueEntry `json:"entries,omitempty"`
}

type InputValueEntry struct {
	Key   string     `json:"key,omitempty"`
	Name  string     `json:"name,omitempty"`
	Value InputValue `json:"value"`
}

func (v *InputValue) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind              string             `json:"kind"`
		Mode              string             `json:"mode"`
		Literal           string             `json:"literal"`
		Value             json.RawMessage    `json:"value"`
		Items             []InputValue       `json:"items"`
		Entries           []InputValueEntry  `json:"entries"`
		RefNodeID         string             `json:"refNodeId"`
		RefOutput         string             `json:"refOutput"`
		RefSelector       *ReferenceSelector `json:"refSelector"`
		RefOutputSelector *ReferenceSelector `json:"refOutputSelector"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if raw.Mode != "" {
		v.Kind = "scalar"
		v.Value = &FieldValue{Mode: raw.Mode, Literal: raw.Literal, RefNodeID: raw.RefNodeID, RefOutput: raw.RefOutput, RefSelector: raw.RefSelector, RefOutputSelector: raw.RefOutputSelector}
		return nil
	}

	v.Kind, v.Items, v.Entries = raw.Kind, raw.Items, raw.Entries
	if raw.Kind == "scalar" {
		var field FieldValue
		if err := json.Unmarshal(raw.Value, &field); err != nil {
			return err
		}
		v.Value = &field
	}

	return nil
}

type ReferenceSelector struct {
	Kind string      `json:"kind"`
	Expr *FieldValue `json:"expr,omitempty"`
}

type Multiplicity struct {
	Kind           string      `json:"kind"`
	Mode           string      `json:"mode,omitempty"`
	Expr           *FieldValue `json:"expr,omitempty"`
	VariableNodeID *string     `json:"variableNodeId,omitempty"`
}

type ModuleNode struct {
	NodeID           string                     `json:"nodeId"`
	Namespace        string                     `json:"namespace"`
	Name             string                     `json:"name"`
	Version          string                     `json:"version"`
	LocalName        string                     `json:"localName"`
	Values           map[string]InputValue      `json:"values,omitempty"`
	Multiplicity     Multiplicity               `json:"multiplicity"`
	ProviderBindings map[string]ProviderBinding `json:"providerBindings,omitempty"`
}

type ProviderBinding struct {
	ProviderNodeID string `json:"providerNodeId"`
}

type ProviderNode struct {
	NodeID            string                `json:"nodeId"`
	Namespace         string                `json:"namespace"`
	Name              string                `json:"name"`
	ProviderNamespace string                `json:"providerNamespace"`
	ProviderName      string                `json:"providerName"`
	Version           string                `json:"version"`
	LocalName         string                `json:"localName"`
	Alias             string                `json:"alias,omitempty"`
	Configuration     ProviderConfiguration `json:"configuration"`
}

type ProviderConfiguration struct {
	Arguments map[string]FieldValue              `json:"arguments,omitempty"`
	Blocks    map[string][]ProviderConfiguration `json:"blocks,omitempty"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	NodeID  string `json:"nodeId,omitempty"`
}

type ErrorResponse struct {
	Error       string       `json:"error"`
	Message     string       `json:"message"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Output      string       `json:"output,omitempty"`
}
