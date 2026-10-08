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
	"maps"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// fixedReturnFunctions maps built-in functions whose return type never depends on
// their arguments.
var fixedReturnFunctions = map[string]cty.Type{
	"abs":              cty.Number,
	"base64encode":     cty.String,
	"base64gzip":       cty.String,
	"base64sha256":     cty.String,
	"base64sha512":     cty.String,
	"basename":         cty.String,
	"bcrypt":           cty.String,
	"can":              cty.Bool,
	"ceil":             cty.Number,
	"chomp":            cty.String,
	"cidrhost":         cty.String,
	"cidrnetmask":      cty.String,
	"cidrsubnet":       cty.String,
	"contains":         cty.Bool,
	"dirname":          cty.String,
	"endswith":         cty.Bool,
	"file":             cty.String,
	"filebase64":       cty.String,
	"filebase64sha256": cty.String,
	"fileexists":       cty.Bool,
	"filemd5":          cty.String,
	"filesha1":         cty.String,
	"filesha256":       cty.String,
	"filesha512":       cty.String,
	"floor":            cty.Number,
	"format":           cty.String,
	"indent":           cty.String,
	"index":            cty.Number,
	"join":             cty.String,
	"jsonencode":       cty.String,
	"length":           cty.Number,
	"log":              cty.Number,
	"lower":            cty.String,
	"md5":              cty.String,
	"parseint":         cty.Number,
	"pathexpand":       cty.String,
	"plantimestamp":    cty.String,
	"pow":              cty.Number,
	"replace":          cty.String,
	"sha1":             cty.String,
	"sha256":           cty.String,
	"sha512":           cty.String,
	"signum":           cty.Number,
	"startswith":       cty.Bool,
	"strcontains":      cty.Bool,
	"strrev":           cty.String,
	"substr":           cty.String,
	"templatefile":     cty.String,
	"textdecodebase64": cty.String,
	"textencodebase64": cty.String,
	"timeadd":          cty.String,
	"timecmp":          cty.Number,
	"timestamp":        cty.String,
	"title":            cty.String,
	"tobool":           cty.Bool,
	"tonumber":         cty.Number,
	"tostring":         cty.String,
	"trim":             cty.String,
	"trimprefix":       cty.String,
	"trimspace":        cty.String,
	"trimsuffix":       cty.String,
	"upper":            cty.String,
	"urlencode":        cty.String,
	"uuid":             cty.String,
	"uuidv5":           cty.String,
	"yamlencode":       cty.String,
}

// listOfStringFunctions maps built-in functions that always return a list of strings.
var listOfStringFunctions = map[string]struct{}{
	"cidrsubnets": {},
	"formatlist":  {},
	"regexall":    {},
	"split":       {},
}

// IsKnownFunction reports whether name is a built-in function understood by
// output type inference.
func IsKnownFunction(name string) bool {
	if _, ok := fixedReturnFunctions[name]; ok {
		return true
	}
	if _, ok := listOfStringFunctions[name]; ok {
		return true
	}

	switch name {
	case "try", "coalesce", "coalescelist", "one", "tolist", "toset", "tomap",
		"concat", "setunion", "setintersection", "setsubtract", "flatten", "compact",
		"distinct", "reverse", "slice", "sort", "chunklist", "keys", "values",
		"element", "lookup", "merge", "zipmap", "jsondecode", "yamldecode", "sensitive",
		"nonsensitive":
		return true
	default:
		return false
	}
}

// inferFunctionCall classifies a built-in function call. Functions whose return type
// depends on their arguments are handled individually; everything unrecognized
// degrades to dynamic so the caller reports unknown confidence.
func (i *inference) inferFunctionCall(e *hclsyntax.FunctionCallExpr) result {
	if t, ok := fixedReturnFunctions[e.Name]; ok {
		return result{t: t}
	}

	if _, ok := listOfStringFunctions[e.Name]; ok {
		return result{t: cty.List(cty.String)}
	}

	switch e.Name {
	case "try", "coalesce", "coalescelist":
		return i.unifyArgs(e)
	case "one":
		return i.inferElementOfFirstArg(e)
	case "tolist":
		return i.inferConversion(e, cty.List)
	case "toset":
		return i.inferConversion(e, cty.Set)
	case "tomap":
		return i.inferConversion(e, cty.Map)
	case "concat", "setunion", "setintersection", "setsubtract":
		return i.inferConcat(e)
	case "flatten":
		return i.inferFlatten(e)
	case "compact", "distinct", "reverse", "slice", "sort", "chunklist":
		return i.inferPassthroughList(e)
	case "keys":
		return result{t: cty.List(cty.String)}
	case "values":
		return i.inferValues(e)
	case "element", "lookup":
		return i.inferElementOfFirstArg(e)
	case "merge":
		return i.inferMerge(e)
	case "zipmap":
		return i.inferZipmap(e)
	case "jsondecode", "yamldecode", "sensitive", "nonsensitive":
		return dynamicResult("value derives from %s, whose result type is not statically known", e.Name)
	}

	return dynamicResult("value derives from the %s function, which is not modelled by output inference", e.Name)
}

// unifyArgs merges every argument's type, used by try/coalesce style functions.
func (i *inference) unifyArgs(e *hclsyntax.FunctionCallExpr) result {
	if len(e.Args) == 0 {
		return dynamicResult("%s was called with no arguments", e.Name)
	}

	acc := i.infer(e.Args[0])
	for _, arg := range e.Args[1:] {
		acc = unify(acc, i.infer(arg))
	}

	return acc
}

// inferConversion applies an explicit tolist/toset/tomap conversion.
func (i *inference) inferConversion(e *hclsyntax.FunctionCallExpr, wrap func(cty.Type) cty.Type) result {
	if len(e.Args) == 0 {
		return dynamicResult("%s was called with no arguments", e.Name)
	}

	src := i.infer(e.Args[0])
	elem := elementTypeOf(src.t)
	if elem.Equals(cty.DynamicPseudoType) {
		return result{t: cty.DynamicPseudoType, reason: src.reason}
	}

	return result{t: wrap(elem), reason: src.reason}
}

// inferConcat returns a list of the unified element types of every argument.
func (i *inference) inferConcat(e *hclsyntax.FunctionCallExpr) result {
	if len(e.Args) == 0 {
		return dynamicResult("%s was called with no arguments", e.Name)
	}

	elems := make([]cty.Type, 0, len(e.Args))
	reason := ""

	for _, arg := range e.Args {
		r := i.infer(arg)
		elems = append(elems, elementTypeOf(r.t))
		if reason == "" {
			reason = r.reason
		}
	}

	unified := unifyAll(elems, reason)
	if unified.t.Equals(cty.DynamicPseudoType) {
		return unified
	}

	return result{t: cty.List(unified.t), reason: unified.reason}
}

// inferFlatten returns a list of the innermost element type of its argument.
func (i *inference) inferFlatten(e *hclsyntax.FunctionCallExpr) result {
	if len(e.Args) == 0 {
		return dynamicResult("flatten was called with no arguments")
	}

	src := i.infer(e.Args[0])
	elem := elementTypeOf(src.t)
	for elem.IsListType() || elem.IsSetType() || elem.IsTupleType() {
		elem = elementTypeOf(elem)
	}

	if elem.Equals(cty.DynamicPseudoType) {
		return result{t: cty.DynamicPseudoType, reason: src.reason}
	}

	return result{t: cty.List(elem), reason: src.reason}
}

// inferPassthroughList returns a list of the first argument's element type.
func (i *inference) inferPassthroughList(e *hclsyntax.FunctionCallExpr) result {
	if len(e.Args) == 0 {
		return dynamicResult("%s was called with no arguments", e.Name)
	}

	src := i.infer(e.Args[0])
	elem := elementTypeOf(src.t)
	if elem.Equals(cty.DynamicPseudoType) {
		return result{t: cty.DynamicPseudoType, reason: src.reason}
	}

	return result{t: cty.List(elem), reason: src.reason}
}

// inferValues returns a list of the first argument's element type.
func (i *inference) inferValues(e *hclsyntax.FunctionCallExpr) result {
	return i.inferPassthroughList(e)
}

// inferElementOfFirstArg returns the element type of the first argument, unified with
// any remaining arguments so that `lookup(m, k, default)` accounts for the default.
func (i *inference) inferElementOfFirstArg(e *hclsyntax.FunctionCallExpr) result {
	if len(e.Args) == 0 {
		return dynamicResult("%s was called with no arguments", e.Name)
	}

	src := i.infer(e.Args[0])
	acc := result{t: elementTypeOf(src.t), reason: src.reason}

	if e.Name == "lookup" && len(e.Args) == 3 {
		acc = unify(acc, i.infer(e.Args[2]))
	}

	return acc
}

// inferMerge combines object arguments into a single object, degrading to a map of the
// unified element types when any argument is not a static object.
func (i *inference) inferMerge(e *hclsyntax.FunctionCallExpr) result {
	if len(e.Args) == 0 {
		return dynamicResult("merge was called with no arguments")
	}

	attrs := map[string]cty.Type{}
	elems := make([]cty.Type, 0, len(e.Args))
	reason := ""
	allObjects := true

	for _, arg := range e.Args {
		r := i.infer(arg)
		if reason == "" {
			reason = r.reason
		}

		elems = append(elems, elementTypeOf(r.t))

		if !r.t.IsObjectType() {
			allObjects = false
			continue
		}

		maps.Copy(attrs, r.t.AttributeTypes())
	}

	if allObjects {
		return result{t: cty.Object(attrs), reason: reason}
	}

	unified := unifyAll(elems, reason)
	if unified.t.Equals(cty.DynamicPseudoType) {
		return unified
	}

	return result{t: cty.Map(unified.t), reason: unified.reason}
}

// inferZipmap returns a map keyed by strings holding the second argument's element type.
func (i *inference) inferZipmap(e *hclsyntax.FunctionCallExpr) result {
	if len(e.Args) < 2 {
		return dynamicResult("zipmap requires two arguments")
	}

	vals := i.infer(e.Args[1])
	elem := elementTypeOf(vals.t)
	if elem.Equals(cty.DynamicPseudoType) {
		return result{t: cty.DynamicPseudoType, reason: vals.reason}
	}

	return result{t: cty.Map(elem), reason: vals.reason}
}
