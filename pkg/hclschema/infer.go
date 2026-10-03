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
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// inferredType is the classification result for a single output expression.
type inferredType struct {
	Type       json.RawMessage
	Confidence string
	Reason     string
}

// inference carries the state of a single module's output classification pass.
type inference struct {
	st       *symbolTable
	resolver SchemaResolver
	// scope holds for-expression iterator bindings, innermost last.
	scope []map[string]cty.Type
}

// result is the intermediate outcome of classifying one expression.
type result struct {
	t      cty.Type
	reason string
}

// dynamicResult returns an unresolvable expression result carrying an explanation.
func dynamicResult(format string, args ...any) result {
	return result{t: cty.DynamicPseudoType, reason: fmt.Sprintf(format, args...)}
}

// inferOutputs classifies every `output` block's value expression in the module,
// returning the resolved cty type JSON and a confidence rating per output name.
func inferOutputs(mod *Module, resolver SchemaResolver) map[string]inferredType {
	out := make(map[string]inferredType, len(mod.Outputs))
	st := buildSymbolTable(mod.Dir)
	inf := &inference{st: st, resolver: resolver}

	for _, o := range mod.Outputs {
		expr, ok := st.outputs[o.Name]
		if !ok {
			out[o.Name] = inferredType{
				Type:       dynamicTypeJSON,
				Confidence: ConfidenceUnknown,
				Reason:     "output value expression could not be read from the module source",
			}

			continue
		}

		res := inf.infer(expr)

		encoded, err := EncodeType(res.t)
		if err != nil {
			encoded = dynamicTypeJSON
		}

		confidence := confidenceFor(res.t)
		reason := res.reason
		if confidence == ConfidenceExact {
			reason = ""
		}

		out[o.Name] = inferredType{
			Type:       encoded,
			Confidence: confidence,
			Reason:     reason,
		}
	}

	return out
}

// confidenceFor grades a resolved type: fully concrete types are exact, types with
// dynamic leaves retain their known shape and are inferred, and a wholly dynamic type
// is unknown.
func confidenceFor(t cty.Type) string {
	if t == cty.NilType || t.Equals(cty.DynamicPseudoType) {
		return ConfidenceUnknown
	}

	if containsDynamic(t) {
		return ConfidenceInferred
	}

	return ConfidenceExact
}

// containsDynamic reports whether t has a dynamic pseudo-type anywhere within it.
func containsDynamic(t cty.Type) bool {
	switch {
	case t == cty.NilType, t.Equals(cty.DynamicPseudoType):
		return true
	case t.IsListType(), t.IsSetType(), t.IsMapType():
		return containsDynamic(t.ElementType())
	case t.IsTupleType():
		if slices.ContainsFunc(t.TupleElementTypes(), containsDynamic) {
			return true
		}
	case t.IsObjectType():
		for _, at := range t.AttributeTypes() {
			if containsDynamic(at) {
				return true
			}
		}
	}

	return false
}

// infer classifies a single expression node.
func (i *inference) infer(expr hclsyntax.Expression) result {
	switch e := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return result{t: e.Val.Type()}
	case *hclsyntax.TemplateExpr:
		return i.inferTemplate(e)
	case *hclsyntax.TemplateWrapExpr:
		return i.infer(e.Wrapped)
	case *hclsyntax.ParenthesesExpr:
		return i.infer(e.Expression)
	case *hclsyntax.ScopeTraversalExpr:
		return i.inferTraversal(e.Traversal)
	case *hclsyntax.RelativeTraversalExpr:
		return i.inferRelativeTraversal(e)
	case *hclsyntax.SplatExpr:
		return i.inferSplat(e)
	case *hclsyntax.IndexExpr:
		return i.inferIndex(e)
	case *hclsyntax.ConditionalExpr:
		return unify(i.infer(e.TrueResult), i.infer(e.FalseResult))
	case *hclsyntax.TupleConsExpr:
		return i.inferTuple(e)
	case *hclsyntax.ObjectConsExpr:
		return i.inferObject(e)
	case *hclsyntax.ObjectConsKeyExpr:
		return i.infer(e.Wrapped)
	case *hclsyntax.ForExpr:
		return i.inferFor(e)
	case *hclsyntax.FunctionCallExpr:
		return i.inferFunctionCall(e)
	case *hclsyntax.BinaryOpExpr:
		return result{t: e.Op.Type}
	case *hclsyntax.UnaryOpExpr:
		return result{t: e.Op.Type}
	}

	return dynamicResult("expression form is not supported by output inference")
}

// inferTemplate resolves a template expression. A single-part template is a passthrough
// wrapper around its only part; anything with interpolation is always a string.
func (i *inference) inferTemplate(e *hclsyntax.TemplateExpr) result {
	if len(e.Parts) == 1 {
		return i.infer(e.Parts[0])
	}

	return result{t: cty.String}
}

// inferTraversal resolves an absolute reference such as `var.x`, `local.y`,
// `aws_s3_bucket.this.arn` or `data.aws_ami.this.id`.
func (i *inference) inferTraversal(trav hcl.Traversal) result {
	root, rest, ok := splitTraversal(trav)
	if !ok {
		return dynamicResult("reference could not be parsed")
	}

	if t, bound := i.lookupScope(root); bound {
		return i.applyPath(t, rest, root)
	}

	switch root {
	case "var":
		if len(rest) == 0 {
			return dynamicResult("bare `var` reference is not a value")
		}

		t, exists := i.st.variables[rest[0]]
		if !exists {
			return dynamicResult("variable %q is not declared in this module", rest[0])
		}

		return i.applyPath(t, rest[1:], "var."+rest[0])
	case "local":
		if len(rest) == 0 {
			return dynamicResult("bare `local` reference is not a value")
		}

		res := i.inferLocal(rest[0])

		return i.applyPathResult(res, rest[1:], "local."+rest[0])
	case "module":
		return dynamicResult("value derives from a child module output, which is not resolved")
	case "each", "count", "self", "terraform", "path":
		return dynamicResult("value derives from the %q meta-argument", root)
	case "data":
		if len(rest) < 2 {
			return dynamicResult("incomplete data source reference")
		}

		return i.inferResource("data."+rest[0]+"."+rest[1], rest[2:])
	}

	if len(rest) == 0 {
		return dynamicResult("unqualified reference %q could not be resolved", root)
	}

	return i.inferResource(root+"."+rest[0], rest[1:])
}

// inferResource resolves an attribute path against a declared resource or data source,
// consulting the provider schema resolver for leaf types.
func (i *inference) inferResource(addr string, path []string) result {
	ref, ok := i.st.resources[addr]
	if !ok {
		return dynamicResult("resource %q is not declared in this module", addr)
	}

	inner := i.resolveResourceAttr(ref, path)
	if ref.Repeated {
		if inner.t.Equals(cty.DynamicPseudoType) {
			return inner
		}

		return result{t: cty.List(inner.t), reason: inner.reason}
	}

	return inner
}

// resolveResourceAttr looks a single attribute path up on a resource type. An empty
// path yields the resource's whole attribute object.
func (i *inference) resolveResourceAttr(ref resourceRef, path []string) result {
	if i.resolver == nil {
		return dynamicResult("provider schema for resource type %q is not available", ref.Type)
	}

	if len(path) == 0 {
		t, ok := i.resolver.ResourceAttributes(ref.Type, ref.IsData)
		if !ok {
			return dynamicResult("provider schema for resource type %q is not available", ref.Type)
		}

		return result{t: t}
	}

	t, ok := i.resolver.Lookup(ref.Type, strings.Join(path, "."), ref.IsData)
	if !ok {
		return dynamicResult("attribute %q is not present in the schema for resource type %q",
			strings.Join(path, "."), ref.Type)
	}

	return result{t: t}
}

// inferRelativeTraversal resolves a traversal applied to an arbitrary sub-expression,
// e.g. `try(x, y).id` or `local.m["k"].arn`.
func (i *inference) inferRelativeTraversal(e *hclsyntax.RelativeTraversalExpr) result {
	src := i.infer(e.Source)
	path, ok := traversalNames(e.Traversal)
	if !ok {
		return dynamicResult("relative reference uses an unsupported accessor")
	}

	return i.applyPathResult(src, path, "expression")
}

// inferSplat resolves `x[*].attr`. The splat source is classified first; when it is a
// repeated resource the element attribute is looked up and re-wrapped as a list.
func (i *inference) inferSplat(e *hclsyntax.SplatExpr) result {
	src := i.infer(e.Source)

	elem := src.t
	if elem.IsListType() || elem.IsSetType() || elem.IsTupleType() {
		elem = elementTypeOf(elem)
	}

	path, ok := splatEachPath(e.Each)
	if !ok {
		return result{t: cty.List(elem), reason: src.reason}
	}

	inner := i.applyPathResult(result{t: elem, reason: src.reason}, path, "splat element")
	if inner.t.Equals(cty.DynamicPseudoType) {
		return inner
	}

	return result{t: cty.List(inner.t), reason: inner.reason}
}

// inferIndex resolves `x[k]` to the element type of x.
func (i *inference) inferIndex(e *hclsyntax.IndexExpr) result {
	src := i.infer(e.Collection)

	switch {
	case src.t.IsListType(), src.t.IsSetType(), src.t.IsMapType():
		return result{t: src.t.ElementType(), reason: src.reason}
	case src.t.IsTupleType():
		types := src.t.TupleElementTypes()
		if len(types) == 0 {
			return dynamicResult("indexed an empty tuple")
		}

		return unifyAll(types, src.reason)
	case src.t.IsObjectType():
		return unifyAll(objectAttributeTypes(src.t), src.reason)
	}

	return dynamicResult("indexed value type is not known")
}

// inferTuple resolves a `[a, b, c]` literal to a tuple of its element types.
func (i *inference) inferTuple(e *hclsyntax.TupleConsExpr) result {
	types := make([]cty.Type, 0, len(e.Exprs))
	reason := ""

	for _, sub := range e.Exprs {
		r := i.infer(sub)
		types = append(types, r.t)
		if reason == "" {
			reason = r.reason
		}
	}

	return result{t: cty.Tuple(types), reason: reason}
}

// inferObject resolves a `{ k = v }` literal to an object type. Keys that are not
// static strings degrade the whole expression to a map of the unified value types.
func (i *inference) inferObject(e *hclsyntax.ObjectConsExpr) result {
	attrs := make(map[string]cty.Type, len(e.Items))
	valueTypes := make([]cty.Type, 0, len(e.Items))
	reason := ""
	staticKeys := true

	for _, item := range e.Items {
		v := i.infer(item.ValueExpr)
		valueTypes = append(valueTypes, v.t)
		if reason == "" {
			reason = v.reason
		}

		key, ok := staticKeyName(item.KeyExpr)
		if !ok {
			staticKeys = false
			continue
		}

		attrs[key] = v.t
	}

	if !staticKeys {
		unified := unifyAll(valueTypes, reason)
		if unified.t.Equals(cty.DynamicPseudoType) {
			return unified
		}

		return result{t: cty.Map(unified.t), reason: unified.reason}
	}

	return result{t: cty.Object(attrs), reason: reason}
}

// inferFor resolves a `for` expression, binding its iterator symbols so that the
// result expression can reference them.
func (i *inference) inferFor(e *hclsyntax.ForExpr) result {
	coll := i.infer(e.CollExpr)

	bindings := map[string]cty.Type{}
	if e.KeyVar != "" {
		bindings[e.KeyVar] = forKeyType(coll.t)
	}

	if e.ValVar != "" {
		bindings[e.ValVar] = elementTypeOf(coll.t)
	}

	i.scope = append(i.scope, bindings)
	val := i.infer(e.ValExpr)
	i.scope = i.scope[:len(i.scope)-1]

	reason := val.reason
	if reason == "" {
		reason = coll.reason
	}

	if val.t.Equals(cty.DynamicPseudoType) {
		return result{t: cty.DynamicPseudoType, reason: reason}
	}

	if e.KeyExpr != nil {
		return result{t: cty.Map(val.t), reason: reason}
	}

	return result{t: cty.List(val.t), reason: reason}
}

// lookupScope resolves a for-expression iterator binding.
func (i *inference) lookupScope(name string) (cty.Type, bool) {
	for _, v := range slices.Backward(i.scope) {
		if t, ok := v[name]; ok {
			return t, true
		}
	}

	return cty.NilType, false
}

// inferLocal resolves a `locals` entry, memoising the result and breaking reference
// cycles (which are invalid HCL and therefore cannot occur in a loadable module).
func (i *inference) inferLocal(name string) result {
	if t, ok := i.st.localCache[name]; ok {
		return result{t: t}
	}

	if _, cycling := i.st.localsInFlight[name]; cycling {
		return dynamicResult("local value %q participates in a reference cycle", name)
	}

	expr, ok := i.st.locals[name]
	if !ok {
		return dynamicResult("local value %q is not declared in this module", name)
	}

	i.st.localsInFlight[name] = struct{}{}
	res := i.infer(expr)
	delete(i.st.localsInFlight, name)

	i.st.localCache[name] = res.t

	return res
}

// applyPath walks a dotted attribute path into an already-resolved type.
func (i *inference) applyPath(t cty.Type, path []string, addr string) result {
	return i.applyPathResult(result{t: t}, path, addr)
}

// applyPathResult walks a dotted attribute path into an already-resolved result,
// preserving any reason accumulated on the way in.
func (i *inference) applyPathResult(res result, path []string, addr string) result {
	for _, step := range path {
		switch {
		case res.t.IsObjectType():
			if !res.t.HasAttribute(step) {
				return dynamicResult("%s has no attribute %q", addr, step)
			}

			res = result{t: res.t.AttributeType(step), reason: res.reason}
		case res.t.IsMapType():
			res = result{t: res.t.ElementType(), reason: res.reason}
		default:
			return dynamicResult("%s is not an object, so attribute %q cannot be resolved", addr, step)
		}
	}

	return res
}

// unify merges two alternative branch results into the most specific common type.
func unify(a, b result) result {
	reason := a.reason
	if reason == "" {
		reason = b.reason
	}

	if a.t.Equals(b.t) {
		return result{t: a.t, reason: reason}
	}

	if a.t.Equals(cty.DynamicPseudoType) {
		return result{t: b.t, reason: reason}
	}

	if b.t.Equals(cty.DynamicPseudoType) {
		return result{t: a.t, reason: reason}
	}

	return result{t: cty.DynamicPseudoType, reason: "conditional branches produce differing types"}
}

// unifyAll merges a slice of alternative types.
func unifyAll(types []cty.Type, reason string) result {
	if len(types) == 0 {
		return dynamicResult("no candidate types to unify")
	}

	acc := result{t: types[0], reason: reason}
	for _, t := range types[1:] {
		acc = unify(acc, result{t: t, reason: reason})
	}

	return acc
}

// elementTypeOf returns the element type of a collection, or dynamic for anything else.
func elementTypeOf(t cty.Type) cty.Type {
	switch {
	case t.IsListType(), t.IsSetType(), t.IsMapType():
		return t.ElementType()
	case t.IsTupleType():
		return unifyAll(t.TupleElementTypes(), "").t
	case t.IsObjectType():
		return unifyAll(objectAttributeTypes(t), "").t
	}

	return cty.DynamicPseudoType
}

// forKeyType returns the key type produced by iterating a collection: a number for
// ordered collections and a string for maps and objects.
func forKeyType(t cty.Type) cty.Type {
	switch {
	case t.IsListType(), t.IsTupleType():
		return cty.Number
	case t.IsMapType(), t.IsObjectType(), t.IsSetType():
		return cty.String
	}

	return cty.DynamicPseudoType
}

// objectAttributeTypes returns an object's attribute types as a slice.
func objectAttributeTypes(t cty.Type) []cty.Type {
	attrs := t.AttributeTypes()
	types := make([]cty.Type, 0, len(attrs))
	for _, at := range attrs {
		types = append(types, at)
	}

	return types
}

// staticKeyName returns the literal attribute name of an object construction key.
func staticKeyName(expr hclsyntax.Expression) (string, bool) {
	if keyExpr, ok := expr.(*hclsyntax.ObjectConsKeyExpr); ok {
		if name := hcl.ExprAsKeyword(keyExpr); name != "" {
			return name, true
		}

		expr = keyExpr.Wrapped
	}

	tmpl, ok := expr.(*hclsyntax.TemplateExpr)
	if !ok || !tmpl.IsStringLiteral() {
		return "", false
	}

	val, diags := tmpl.Value(nil)
	if diags.HasErrors() || val.Type() != cty.String || val.IsNull() {
		return "", false
	}

	return val.AsString(), true
}

// splitTraversal converts an hcl.Traversal into a root name and dotted attribute path.
// Index steps terminate the path since they select into a collection rather than name
// an attribute; the caller handles those via IndexExpr.
func splitTraversal(trav hcl.Traversal) (string, []string, bool) {
	if len(trav) == 0 {
		return "", nil, false
	}

	root, ok := trav[0].(hcl.TraverseRoot)
	if !ok {
		return "", nil, false
	}

	path, ok := traversalNames(trav[1:])

	return root.Name, path, ok
}

// traversalNames converts the attribute steps of a traversal into plain names.
// A literal string index is treated as an attribute name so that `local.m["k"]`
// resolves through object types; any other index form is unsupported.
func traversalNames(steps hcl.Traversal) ([]string, bool) {
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		switch s := step.(type) {
		case hcl.TraverseAttr:
			names = append(names, s.Name)
		case hcl.TraverseIndex:
			if s.Key.Type() != cty.String || s.Key.IsNull() {
				return names, false
			}

			names = append(names, s.Key.AsString())
		default:
			return names, false
		}
	}

	return names, true
}

// splatEachPath extracts the attribute path applied to each element of a splat.
func splatEachPath(each hclsyntax.Expression) ([]string, bool) {
	rel, ok := each.(*hclsyntax.RelativeTraversalExpr)
	if !ok {
		return nil, false
	}

	if _, isAnon := rel.Source.(*hclsyntax.AnonSymbolExpr); !isAnon {
		return nil, false
	}

	return traversalNames(rel.Traversal)
}
