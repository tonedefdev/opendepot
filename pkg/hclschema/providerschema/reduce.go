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

// Package providerschema extracts and reduces OpenTofu provider schemas so that
// Assembly Line can resolve the type of any resource attribute a module output
// references, without ever loading a provider in a request path.
package providerschema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// SchemaVersion identifies the reduced provider schema wire format.
const SchemaVersion = "assembly.provider.v1"

// Attribute is a single reduced resource attribute: its type and access flags only.
type Attribute struct {
	Type     json.RawMessage `json:"type"`
	Required bool            `json:"required,omitempty"`
	Optional bool            `json:"optional,omitempty"`
	Computed bool            `json:"computed,omitempty"`
}

// Block is a reduced schema block holding attributes and nested blocks.
type Block struct {
	Attributes map[string]Attribute   `json:"attributes,omitempty"`
	Blocks     map[string]NestedBlock `json:"blocks,omitempty"`
}

// NestedBlock is a nested block type along with its nesting mode.
type NestedBlock struct {
	Nesting  string `json:"nesting"`
	MinItems int    `json:"minItems,omitempty"`
	MaxItems int    `json:"maxItems,omitempty"`
	Block    Block  `json:"block"`
}

// ReducedSchema is the stripped provider schema stored in the object storage backend.
// Descriptions, description kinds and deprecation metadata are discarded; only types
// and access flags survive.
type ReducedSchema struct {
	SchemaVersion   string           `json:"schemaVersion"`
	Provider        string           `json:"provider"`
	ProviderVersion string           `json:"providerVersion"`
	ProviderBlock   Block            `json:"providerBlock"`
	Resources       map[string]Block `json:"resources"`
	DataSources     map[string]Block `json:"dataSources"`
}

// rawSchemas is the subset of `tofu providers schema -json` output that is retained.
type rawSchemas struct {
	ProviderSchemas map[string]rawProviderSchema `json:"provider_schemas"`
}

type rawProviderSchema struct {
	Provider          rawSchema            `json:"provider"`
	ResourceSchemas   map[string]rawSchema `json:"resource_schemas"`
	DataSourceSchemas map[string]rawSchema `json:"data_source_schemas"`
}

type rawSchema struct {
	Block rawBlock `json:"block"`
}

type rawBlock struct {
	Attributes map[string]rawAttribute `json:"attributes"`
	BlockTypes map[string]rawBlockType `json:"block_types"`
}

type rawAttribute struct {
	Type     json.RawMessage `json:"type"`
	Required bool            `json:"required"`
	Optional bool            `json:"optional"`
	Computed bool            `json:"computed"`
}

type rawBlockType struct {
	NestingMode string   `json:"nesting_mode"`
	MinItems    int      `json:"min_items"`
	MaxItems    int      `json:"max_items"`
	Block       rawBlock `json:"block"`
}

// Reduce converts raw `tofu providers schema -json` output into a ReducedSchema.
// providerAddr and providerVersion identify the provider the schema belongs to and are
// recorded on the result for provenance.
func Reduce(raw []byte, providerAddr, providerVersion string) (*ReducedSchema, error) {
	var parsed rawSchemas
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse provider schema JSON: %w", err)
	}

	if len(parsed.ProviderSchemas) == 0 {
		return nil, fmt.Errorf("provider schema JSON contains no provider schemas")
	}

	out := &ReducedSchema{
		SchemaVersion:   SchemaVersion,
		Provider:        providerAddr,
		ProviderVersion: providerVersion,
		Resources:       map[string]Block{},
		DataSources:     map[string]Block{},
	}

	for _, ps := range parsed.ProviderSchemas {
		out.ProviderBlock = reduceBlock(ps.Provider.Block)

		for name, rs := range ps.ResourceSchemas {
			out.Resources[name] = reduceBlock(rs.Block)
		}

		for name, ds := range ps.DataSourceSchemas {
			out.DataSources[name] = reduceBlock(ds.Block)
		}
	}

	return out, nil
}

// reduceBlock strips a raw schema block down to types and access flags.
func reduceBlock(in rawBlock) Block {
	out := Block{}

	if len(in.Attributes) > 0 {
		out.Attributes = make(map[string]Attribute, len(in.Attributes))
		for name, attr := range in.Attributes {
			out.Attributes[name] = Attribute{
				Type:     attr.Type,
				Required: attr.Required,
				Optional: attr.Optional,
				Computed: attr.Computed,
			}
		}
	}

	if len(in.BlockTypes) > 0 {
		out.Blocks = make(map[string]NestedBlock, len(in.BlockTypes))
		for name, bt := range in.BlockTypes {
			out.Blocks[name] = NestedBlock{
				Nesting:  bt.NestingMode,
				MinItems: bt.MinItems,
				MaxItems: bt.MaxItems,
				Block:    reduceBlock(bt.Block),
			}
		}
	}

	return out
}

// Lookup returns the cty type of attrPath on resourceType. attrPath is a dotted path
// that may traverse nested blocks as well as attributes; nested block types are wrapped
// according to their nesting mode so that a `list` nested block resolves to a list of
// objects.
func (s *ReducedSchema) Lookup(resourceType, attrPath string, isData bool) (cty.Type, bool) {
	block, ok := s.block(resourceType, isData)
	if !ok {
		return cty.DynamicPseudoType, false
	}

	steps := strings.Split(attrPath, ".")
	current := block

	for idx, step := range steps {
		if attr, exists := current.Attributes[step]; exists {
			t, err := decodeType(attr.Type)
			if err != nil {
				return cty.DynamicPseudoType, false
			}

			if idx == len(steps)-1 {
				return t, true
			}

			return walkIntoType(t, steps[idx+1:])
		}

		nested, exists := current.Blocks[step]
		if !exists {
			return cty.DynamicPseudoType, false
		}

		if idx == len(steps)-1 {
			return wrapNesting(nested.Nesting, blockObjectType(nested.Block)), true
		}

		current = nested.Block
	}

	return cty.DynamicPseudoType, false
}

// ResourceAttributes returns the whole schema of resourceType as an object type.
func (s *ReducedSchema) ResourceAttributes(resourceType string, isData bool) (cty.Type, bool) {
	block, ok := s.block(resourceType, isData)
	if !ok {
		return cty.DynamicPseudoType, false
	}

	return blockObjectType(block), true
}

// block selects the resource or data source schema for a type name.
func (s *ReducedSchema) block(resourceType string, isData bool) (Block, bool) {
	if isData {
		b, ok := s.DataSources[resourceType]
		return b, ok
	}

	b, ok := s.Resources[resourceType]

	return b, ok
}

// blockObjectType renders a block as an object type covering its attributes and nested
// blocks.
func blockObjectType(b Block) cty.Type {
	attrs := make(map[string]cty.Type, len(b.Attributes)+len(b.Blocks))

	for name, attr := range b.Attributes {
		t, err := decodeType(attr.Type)
		if err != nil {
			t = cty.DynamicPseudoType
		}

		attrs[name] = t
	}

	for name, nested := range b.Blocks {
		attrs[name] = wrapNesting(nested.Nesting, blockObjectType(nested.Block))
	}

	return cty.Object(attrs)
}

// wrapNesting applies a nested block's nesting mode to its object type.
func wrapNesting(mode string, inner cty.Type) cty.Type {
	switch mode {
	case "list":
		return cty.List(inner)
	case "set":
		return cty.Set(inner)
	case "map":
		return cty.Map(inner)
	}

	return inner
}

// walkIntoType resolves a remaining attribute path within an already-decoded type.
func walkIntoType(t cty.Type, steps []string) (cty.Type, bool) {
	for _, step := range steps {
		switch {
		case t.IsObjectType():
			if !t.HasAttribute(step) {
				return cty.DynamicPseudoType, false
			}

			t = t.AttributeType(step)
		case t.IsMapType():
			t = t.ElementType()
		default:
			return cty.DynamicPseudoType, false
		}
	}

	return t, true
}

// decodeType parses a cty type JSON value from a provider schema.
func decodeType(raw json.RawMessage) (cty.Type, error) {
	if len(raw) == 0 {
		return cty.DynamicPseudoType, nil
	}

	return ctyjson.UnmarshalType(raw)
}
