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
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// resourceRef is a managed resource or data source declared by the module.
type resourceRef struct {
	// Type is the provider resource type, e.g. "aws_s3_bucket".
	Type string
	// IsData distinguishes a data source from a managed resource.
	IsData bool
	// Repeated reports whether the block declares `count` or `for_each`, which makes
	// unindexed references to it resolve to a collection rather than a single object.
	Repeated bool
}

// symbolTable is the module-wide index of addressable symbols needed to classify
// output expressions.
type symbolTable struct {
	// variables maps a variable name to its declared type constraint.
	variables map[string]cty.Type
	// locals maps a local value name to its defining expression.
	locals map[string]hclsyntax.Expression
	// resources maps "<type>.<name>" (and "data.<type>.<name>") to its declaration.
	resources map[string]resourceRef
	// modules holds the names of child module calls, whose outputs are unresolvable.
	modules map[string]struct{}
	// outputs maps an output name to its `value` expression.
	outputs map[string]hclsyntax.Expression
	// localCache memoises resolved local types; localsInFlight breaks reference cycles.
	localCache     map[string]cty.Type
	localsInFlight map[string]struct{}
	// skippedJSON records .tf.json files that hclsyntax cannot parse.
	skippedJSON []string
}

// buildSymbolTable parses every .tf file at dir and indexes the symbols an output
// expression may reference. Files that fail to parse are skipped rather than fatal:
// the inventory pass in ParseModule has already established the module is loadable.
func buildSymbolTable(dir string) *symbolTable {
	st := &symbolTable{
		variables:      map[string]cty.Type{},
		locals:         map[string]hclsyntax.Expression{},
		resources:      map[string]resourceRef{},
		modules:        map[string]struct{}{},
		outputs:        map[string]hclsyntax.Expression{},
		localCache:     map[string]cty.Type{},
		localsInFlight: map[string]struct{}{},
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return st
	}

	parser := hclparse.NewParser()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if strings.HasSuffix(name, ".tf.json") {
			st.skippedJSON = append(st.skippedJSON, name)
			continue
		}

		if !strings.HasSuffix(name, ".tf") {
			continue
		}

		src, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			continue
		}

		file, diags := parser.ParseHCL(src, filepath.Join(dir, name))
		if file == nil || diags.HasErrors() {
			continue
		}

		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}

		st.indexBody(body)
	}

	return st
}

// indexBody records every top-level block in a single .tf file body.
func (st *symbolTable) indexBody(body *hclsyntax.Body) {
	for _, block := range body.Blocks {
		switch block.Type {
		case "variable":
			st.indexVariable(block)
		case "output":
			st.indexOutput(block)
		case "locals":
			for name, attr := range block.Body.Attributes {
				st.locals[name] = attr.Expr
			}
		case "resource":
			st.indexResource(block, false)
		case "data":
			st.indexResource(block, true)
		case "module":
			if len(block.Labels) == 1 {
				st.modules[block.Labels[0]] = struct{}{}
			}
		}
	}
}

// indexVariable records a variable's declared type constraint, defaulting to dynamic
// when the block declares no `type`.
func (st *symbolTable) indexVariable(block *hclsyntax.Block) {
	if len(block.Labels) != 1 {
		return
	}

	t := cty.DynamicPseudoType
	if attr, ok := block.Body.Attributes["type"]; ok {
		if parsed, _, err := ParseTypeConstraintExpr(attr.Expr); err == nil {
			t = parsed
		}
	}

	st.variables[block.Labels[0]] = t
}

// indexOutput records an output's `value` expression.
func (st *symbolTable) indexOutput(block *hclsyntax.Block) {
	if len(block.Labels) != 1 {
		return
	}

	if attr, ok := block.Body.Attributes["value"]; ok {
		st.outputs[block.Labels[0]] = attr.Expr
	}
}

// indexResource records a resource or data source block and whether it is repeated.
func (st *symbolTable) indexResource(block *hclsyntax.Block, isData bool) {
	if len(block.Labels) != 2 {
		return
	}

	_, hasCount := block.Body.Attributes["count"]
	_, hasForEach := block.Body.Attributes["for_each"]

	key := block.Labels[0] + "." + block.Labels[1]
	if isData {
		key = "data." + key
	}

	st.resources[key] = resourceRef{
		Type:     block.Labels[0],
		IsData:   isData,
		Repeated: hasCount || hasForEach,
	}
}
