/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package hclschema

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// dynamicTypeJSON is the cty type JSON encoding of cty.DynamicPseudoType.
var dynamicTypeJSON = json.RawMessage(`"dynamic"`)

// ParseTypeConstraint converts the raw source text of a variable's `type` argument into
// a cty type. It also returns the set of object attribute names declared with
// `optional(...)`, which the Assembly Line form renderer uses to decide which fields it
// may omit. An empty constraint yields cty.DynamicPseudoType.
func ParseTypeConstraint(src string) (cty.Type, map[string]bool, error) {
	if src == "" {
		return cty.DynamicPseudoType, nil, nil
	}

	expr, diags := hclsyntax.ParseExpression([]byte(src), "type.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return cty.DynamicPseudoType, nil, fmt.Errorf("failed to parse type constraint %q: %s", src, diags.Error())
	}

	return ParseTypeConstraintExpr(expr)
}

// ParseTypeConstraintExpr converts an already-parsed `type` argument expression into a
// cty type and the set of attribute names declared with `optional(...)`.
func ParseTypeConstraintExpr(expr hcl.Expression) (cty.Type, map[string]bool, error) {
	t, defaults, diags := typeexpr.TypeConstraintWithDefaults(expr)
	if diags.HasErrors() {
		return cty.DynamicPseudoType, nil, fmt.Errorf("invalid type constraint: %s", diags.Error())
	}

	return t, collectOptionalAttributes(t, defaults), nil
}

func OptionalAttributePaths(src string) ([]string, error) {
	if src == "" {
		return nil, nil
	}

	expr, diags := hclsyntax.ParseExpression([]byte(src), "type.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse type constraint %q: %s", src, diags.Error())
	}

	t, defaults, diags := typeexpr.TypeConstraintWithDefaults(expr)
	if diags.HasErrors() {
		return nil, fmt.Errorf("invalid type constraint: %s", diags.Error())
	}

	paths := make(map[string]bool)
	walkOptionalAttributePaths(t, paths, "")
	if defaults != nil {
		collectDefaultAttributePaths(defaults, paths, "")
	}

	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)

	return result, nil
}

// collectOptionalAttributes flattens a typeexpr.Defaults tree into the set of attribute
// names that were declared optional anywhere within the type constraint.
func collectOptionalAttributes(t cty.Type, defaults *typeexpr.Defaults) map[string]bool {
	optional := make(map[string]bool)
	walkOptionalAttributes(t, optional)

	if defaults != nil {
		collectDefaultAttributes(defaults, optional)
	}

	if len(optional) == 0 {
		return nil
	}

	return optional
}

// walkOptionalAttributes records every optional object attribute reachable from t.
func walkOptionalAttributes(t cty.Type, out map[string]bool) {
	switch {
	case t.IsObjectType():
		for name := range t.AttributeTypes() {
			if t.AttributeOptional(name) {
				out[name] = true
			}
		}

		for _, at := range t.AttributeTypes() {
			walkOptionalAttributes(at, out)
		}
	case t.IsListType(), t.IsSetType(), t.IsMapType():
		walkOptionalAttributes(t.ElementType(), out)
	case t.IsTupleType():
		for _, et := range t.TupleElementTypes() {
			walkOptionalAttributes(et, out)
		}
	}
}

func walkOptionalAttributePaths(t cty.Type, out map[string]bool, prefix string) {
	switch {
	case t.IsObjectType():
		for name, at := range t.AttributeTypes() {
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			if t.AttributeOptional(name) {
				out[path] = true
			}
			walkOptionalAttributePaths(at, out, path)
		}
	case t.IsListType(), t.IsSetType(), t.IsMapType():
		walkOptionalAttributePaths(t.ElementType(), out, prefix)
	case t.IsTupleType():
		for _, et := range t.TupleElementTypes() {
			walkOptionalAttributePaths(et, out, prefix)
		}
	}
}

// collectDefaultAttributes records attribute names carrying an `optional(t, default)` value.
func collectDefaultAttributes(defaults *typeexpr.Defaults, out map[string]bool) {
	for name := range defaults.DefaultValues {
		out[name] = true
	}

	for _, child := range defaults.Children {
		collectDefaultAttributes(child, out)
	}
}

func collectDefaultAttributePaths(defaults *typeexpr.Defaults, out map[string]bool, prefix string) {
	for name := range defaults.DefaultValues {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		out[path] = true
	}

	for name, child := range defaults.Children {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		collectDefaultAttributePaths(child, out, path)
	}
}

// EncodeType renders a cty type as cty type JSON — the same wire format used by
// `tofu providers schema -json`. cty.DynamicPseudoType encodes as the literal
// string "dynamic".
func EncodeType(t cty.Type) (json.RawMessage, error) {
	if t == cty.NilType || t.Equals(cty.DynamicPseudoType) {
		return dynamicTypeJSON, nil
	}

	raw, err := ctyjson.MarshalType(t)
	if err != nil {
		return nil, fmt.Errorf("failed to encode cty type: %w", err)
	}

	return json.RawMessage(raw), nil
}

// DecodeType parses cty type JSON back into a cty type. It is the inverse of EncodeType.
func DecodeType(raw json.RawMessage) (cty.Type, error) {
	if len(raw) == 0 {
		return cty.DynamicPseudoType, nil
	}

	t, err := ctyjson.UnmarshalType(raw)
	if err != nil {
		return cty.DynamicPseudoType, fmt.Errorf("failed to decode cty type: %w", err)
	}

	return t, nil
}
