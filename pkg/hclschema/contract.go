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
	"time"

	"github.com/zclconf/go-cty/cty"
)

const (
	// ContractSchemaVersion identifies the module contract wire format.
	ContractSchemaVersion = "assembly.module.v1"

	// GradeFull means every variable is explicitly typed and every output resolved exactly.
	GradeFull = "full"
	// GradePartial means the module parsed but some detail could not be resolved.
	GradePartial = "partial"
	// GradeUnsupported means the module could not be used by Assembly Line at all.
	GradeUnsupported = "unsupported"

	// ConfidenceExact means an output type was resolved against a provider schema.
	ConfidenceExact = "exact"
	// ConfidenceInferred means an output's cardinality is known but its leaf type is not.
	ConfidenceInferred = "inferred"
	// ConfidenceUnknown means an output's type could not be classified at all.
	ConfidenceUnknown = "unknown"
)

// ContractModule identifies the module a contract was derived from.
type ContractModule struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Provider  string `json:"provider,omitempty"`
	Version   string `json:"version"`
}

// ContractValidation is a single `validation` block on a variable.
type ContractValidation struct {
	Condition    string `json:"condition"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ContractVariable is a module input rendered for the Assembly Line form generator.
type ContractVariable struct {
	Name                   string          `json:"name"`
	Type                   json.RawMessage `json:"type"`
	OptionalAttributes     map[string]bool `json:"optionalAttributes,omitempty"`
	OptionalAttributePaths []string        `json:"optionalAttributePaths,omitempty"`
	Required               bool            `json:"required"`
	Sensitive              bool            `json:"sensitive,omitempty"`
	Description            string          `json:"description,omitempty"`
	// Default is only present when Required is false, but is NOT omitted just
	// because the default itself is `null` — `default = null` is a deliberate
	// OpenTofu idiom meaning "pass nothing to the provider", distinct from
	// having no default at all (Required=true). Encoding this as raw JSON
	// (rather than `any` with omitempty) lets an explicit null default survive
	// as `"default": null` on the wire instead of being indistinguishable from
	// an omitted key.
	Default     json.RawMessage      `json:"default,omitempty"`
	Validations []ContractValidation `json:"validations,omitempty"`
}

// ContractOutput is a module output with its resolved type and confidence rating.
type ContractOutput struct {
	Name        string          `json:"name"`
	Type        json.RawMessage `json:"type"`
	Confidence  string          `json:"confidence"`
	Sensitive   bool            `json:"sensitive,omitempty"`
	Description string          `json:"description,omitempty"`
	Reason      string          `json:"reason,omitempty"`
}

// ContractRequiredProvider mirrors a module `required_providers` entry.
type ContractRequiredProvider struct {
	LocalName         string `json:"localName"`
	Source            string `json:"source"`
	VersionConstraint string `json:"versionConstraint,omitempty"`
}

// Compatibility is the Assembly Line grade for a module plus the reasons behind it.
type Compatibility struct {
	Grade    string   `json:"grade"`
	Warnings []string `json:"warnings,omitempty"`
}

// ProvenanceSchema records which provider schema was consulted while resolving outputs.
type ProvenanceSchema struct {
	Provider string `json:"provider"`
	Version  string `json:"version"`
	Digest   string `json:"digest,omitempty"`
}

// Provenance records when and against what a contract was derived.
type Provenance struct {
	DerivedAt string             `json:"derivedAt"`
	Schemas   []ProvenanceSchema `json:"schemas,omitempty"`
}

// Contract is the full Assembly Line contract for one module Version.
type Contract struct {
	SchemaVersion     string                     `json:"schemaVersion"`
	Module            ContractModule             `json:"module"`
	Source            string                     `json:"source,omitempty"`
	RequiredProviders []ContractRequiredProvider `json:"requiredProviders,omitempty"`
	Variables         []ContractVariable         `json:"variables"`
	Outputs           []ContractOutput           `json:"outputs"`
	Compatibility     Compatibility              `json:"compatibility"`
	Provenance        Provenance                 `json:"provenance"`
}

// ContractMeta is the identity a contract is derived for, supplied by the caller since
// it comes from the Version CR rather than the module source.
type ContractMeta struct {
	Namespace string
	Name      string
	Provider  string
	Version   string
	Source    string
}

// SchemaResolver resolves a resource or data source attribute to its declared type.
// A nil resolver disables provider-backed inference entirely; outputs that depend on a
// resource attribute are then reported at ConfidenceUnknown.
type SchemaResolver interface {
	// Lookup returns the type of attrPath on resourceType. isData selects the data
	// source schema instead of the managed resource schema.
	Lookup(resourceType, attrPath string, isData bool) (cty.Type, bool)
	// ResourceAttributes returns every attribute of resourceType as an object type.
	ResourceAttributes(resourceType string, isData bool) (cty.Type, bool)
	// Provenance describes the provider schemas this resolver was built from.
	Provenance() []ProvenanceSchema
}

// BuildContract assembles the Assembly Line contract for a parsed module. When resolver
// is non-nil, output expressions are walked and their leaf types resolved against the
// provider schemas it holds; otherwise every output is emitted as dynamic/unknown.
func BuildContract(mod *Module, meta ContractMeta, resolver SchemaResolver) (*Contract, error) {
	if mod == nil {
		return nil, fmt.Errorf("cannot build a contract from a nil module")
	}

	c := &Contract{
		SchemaVersion: ContractSchemaVersion,
		Module: ContractModule{
			Namespace: meta.Namespace,
			Name:      meta.Name,
			Provider:  meta.Provider,
			Version:   meta.Version,
		},
		Source:     meta.Source,
		Variables:  make([]ContractVariable, 0, len(mod.Variables)),
		Outputs:    make([]ContractOutput, 0, len(mod.Outputs)),
		Provenance: Provenance{DerivedAt: time.Now().UTC().Format(time.RFC3339)},
	}

	warnings := append([]string(nil), mod.Diagnostics...)

	for _, rp := range mod.RequiredProviders {
		c.RequiredProviders = append(c.RequiredProviders, ContractRequiredProvider{
			LocalName:         rp.LocalName,
			Source:            rp.Source,
			VersionConstraint: rp.VersionConstraint,
		})
	}

	untyped := false
	for _, v := range mod.Variables {
		t, optionalAttrs, err := ParseTypeConstraint(v.TypeExpr)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("variable %q has an unparseable type constraint: %v", v.Name, err))
			untyped = true
		}

		if v.TypeExpr == "" {
			warnings = append(warnings, fmt.Sprintf("variable %q has no explicit type", v.Name))
			untyped = true
		}

		encoded, err := EncodeType(t)
		if err != nil {
			return nil, err
		}
		optionalPaths, err := OptionalAttributePaths(v.TypeExpr)
		if err != nil {
			return nil, err
		}

		// Only optional variables carry a default at all — but when they do,
		// encode it even if the underlying value is nil (i.e. `default =
		// null`), so the wire form distinguishes "no default" (key absent,
		// Required=true) from "default is explicitly null" (key present with
		// a null value).
		var defaultJSON json.RawMessage
		if !v.Required {
			encodedDefault, err := json.Marshal(v.Default)
			if err != nil {
				return nil, err
			}

			defaultJSON = encodedDefault
		}

		c.Variables = append(c.Variables, ContractVariable{
			Name:                   v.Name,
			Type:                   encoded,
			OptionalAttributes:     optionalAttrs,
			OptionalAttributePaths: optionalPaths,
			Required:               v.Required,
			Sensitive:              v.Sensitive,
			Description:            v.Description,
			Default:                defaultJSON,
		})
	}

	outputTypes := inferOutputs(mod, resolver)
	allExact := true

	for _, o := range mod.Outputs {
		inferred, ok := outputTypes[o.Name]
		if !ok {
			inferred = inferredType{
				Type:       dynamicTypeJSON,
				Confidence: ConfidenceUnknown,
				Reason:     "output expression could not be located in the module source",
			}
		}

		if inferred.Confidence != ConfidenceExact {
			allExact = false
		}

		c.Outputs = append(c.Outputs, ContractOutput{
			Name:        o.Name,
			Type:        inferred.Type,
			Confidence:  inferred.Confidence,
			Sensitive:   o.Sensitive,
			Description: o.Description,
			Reason:      inferred.Reason,
		})
	}

	if resolver != nil {
		c.Provenance.Schemas = resolver.Provenance()
	}

	if mod.HasLocalModules {
		warnings = append(warnings, "module contains a local modules/ directory; child module outputs are not resolved")
	}

	c.Compatibility = grade(c, mod, untyped, allExact, warnings)

	return c, nil
}

// grade applies the Assembly Line compatibility rules to a contract.
func grade(c *Contract, mod *Module, untyped, allExact bool, warnings []string) Compatibility {
	sort.Strings(warnings)
	warnings = dedupe(warnings)

	if len(mod.Variables) == 0 && len(mod.Outputs) == 0 {
		return Compatibility{
			Grade:    GradeUnsupported,
			Warnings: append(warnings, "module declares no variable or output blocks at its root"),
		}
	}

	if untyped || mod.HasLocalModules || !allExact {
		return Compatibility{Grade: GradePartial, Warnings: warnings}
	}

	_ = c

	return Compatibility{Grade: GradeFull, Warnings: warnings}
}

// dedupe removes consecutive duplicates from a sorted slice.
func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}

	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}

	return out
}

// UnsupportedContract returns a minimal contract for a module that could not be parsed,
// so that the UI can explain why a module is unavailable rather than showing nothing.
func UnsupportedContract(meta ContractMeta, reason string) *Contract {
	return &Contract{
		SchemaVersion: ContractSchemaVersion,
		Module: ContractModule{
			Namespace: meta.Namespace,
			Name:      meta.Name,
			Provider:  meta.Provider,
			Version:   meta.Version,
		},
		Source:    meta.Source,
		Variables: []ContractVariable{},
		Outputs:   []ContractOutput{},
		Compatibility: Compatibility{
			Grade:    GradeUnsupported,
			Warnings: []string{reason},
		},
		Provenance: Provenance{DerivedAt: time.Now().UTC().Format(time.RFC3339)},
	}
}
