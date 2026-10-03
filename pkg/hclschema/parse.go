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

// Package hclschema derives machine-readable input/output contracts from OpenTofu
// module source. It is consumed by the Assembly Line composer, which needs to know
// the type of every variable a module accepts and every value a module emits in
// order to generate forms and validate links between modules.
package hclschema

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tfconfig "github.com/hashicorp/terraform-config-inspect/tfconfig"
)

// Variable is a single `variable` block declared by a module.
type Variable struct {
	// Name is the variable's block label.
	Name string
	// TypeExpr is the raw source text of the variable's `type` argument, empty when
	// the variable declares no type constraint.
	TypeExpr string
	// Description is the variable's `description` argument.
	Description string
	// Default is the decoded `default` value, nil when the variable is required.
	Default any
	// Required reports whether the variable has no default and must be supplied.
	Required bool
	// Sensitive mirrors the variable's `sensitive` argument.
	Sensitive bool
}

// Output is a single `output` block declared by a module.
type Output struct {
	// Name is the output's block label.
	Name string
	// Description is the output's `description` argument.
	Description string
	// Sensitive mirrors the output's `sensitive` argument.
	Sensitive bool
}

// RequiredProvider is a single entry in the module's `required_providers` block.
type RequiredProvider struct {
	// LocalName is the key the module uses to refer to the provider, e.g. "aws".
	LocalName string
	// Source is the provider's registry source address, e.g. "hashicorp/aws".
	Source string
	// VersionConstraint is the declared version constraint, e.g. ">= 5.0".
	VersionConstraint string
}

// Module is the inventory of a single OpenTofu module's root directory.
type Module struct {
	// Dir is the filesystem path the module was parsed from.
	Dir string
	// Variables are the module's `variable` blocks, sorted by name.
	Variables []Variable
	// Outputs are the module's `output` blocks, sorted by name.
	Outputs []Output
	// RequiredProviders are the module's `required_providers` entries, sorted by local name.
	RequiredProviders []RequiredProvider
	// HasLocalModules reports whether a `modules/` directory is present at the module root.
	HasLocalModules bool
	// Diagnostics are non-fatal parse warnings surfaced as compatibility warnings.
	Diagnostics []string
}

// ParseModule loads the OpenTofu module rooted at dir and returns its inventory.
// GitHub archives nest all content under a single "<repo>-<sha>/" wrapper directory,
// so when dir contains no .tf files but exactly one subdirectory does, that
// subdirectory is used instead.
//
// Only a total parse failure returns an error; recoverable diagnostics accumulate on
// Module.Diagnostics so callers can surface them as compatibility warnings.
func ParseModule(dir string) (*Module, error) {
	root, err := resolveModuleRoot(dir)
	if err != nil {
		return nil, err
	}

	mod, diags := tfconfig.LoadModule(root)
	if mod == nil {
		return nil, fmt.Errorf("failed to load module at %s: %s", root, diags.Error())
	}

	out := &Module{Dir: root}

	for _, d := range diags {
		if d.Severity == tfconfig.DiagError {
			return nil, fmt.Errorf("module at %s failed to parse: %s", root, d.Detail)
		}

		out.Diagnostics = append(out.Diagnostics, d.Summary)
	}

	for _, v := range mod.Variables {
		out.Variables = append(out.Variables, Variable{
			Name:        v.Name,
			TypeExpr:    strings.TrimSpace(v.Type),
			Description: v.Description,
			Default:     v.Default,
			Required:    v.Required,
			Sensitive:   v.Sensitive,
		})
	}

	sort.Slice(out.Variables, func(i, j int) bool { return out.Variables[i].Name < out.Variables[j].Name })

	for _, o := range mod.Outputs {
		out.Outputs = append(out.Outputs, Output{
			Name:        o.Name,
			Description: o.Description,
			Sensitive:   o.Sensitive,
		})
	}

	sort.Slice(out.Outputs, func(i, j int) bool { return out.Outputs[i].Name < out.Outputs[j].Name })

	for localName, rp := range mod.RequiredProviders {
		out.RequiredProviders = append(out.RequiredProviders, RequiredProvider{
			LocalName:         localName,
			Source:            normalizeProviderSource(localName, rp.Source),
			VersionConstraint: strings.Join(rp.VersionConstraints, ", "),
		})
	}

	sort.Slice(out.RequiredProviders, func(i, j int) bool {
		return out.RequiredProviders[i].LocalName < out.RequiredProviders[j].LocalName
	})

	if info, statErr := os.Stat(filepath.Join(root, "modules")); statErr == nil && info.IsDir() {
		out.HasLocalModules = true
	}

	return out, nil
}

// resolveModuleRoot returns the directory holding the module's root .tf files,
// unwrapping a single-subdirectory archive wrapper when dir itself has no .tf files.
func resolveModuleRoot(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("failed to read module directory %s: %w", dir, err)
	}

	var subDirs []string
	for _, e := range entries {
		if e.IsDir() {
			subDirs = append(subDirs, e.Name())
			continue
		}

		if strings.HasSuffix(e.Name(), ".tf") || strings.HasSuffix(e.Name(), ".tf.json") {
			return dir, nil
		}
	}

	if len(subDirs) == 1 {
		return filepath.Join(dir, subDirs[0]), nil
	}

	return dir, nil
}

// normalizeProviderSource returns a fully qualified "<namespace>/<name>" provider source.
// required_providers entries frequently omit `source` for HashiCorp providers, in which
// case the local name is the provider name under the hashicorp namespace.
func normalizeProviderSource(localName, source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "hashicorp/" + localName
	}

	// Strip an explicit registry hostname: "registry.opentofu.org/hashicorp/aws".
	parts := strings.Split(source, "/")
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}

	if len(parts) == 1 {
		return "hashicorp/" + parts[0]
	}

	return source
}
