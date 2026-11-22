package assemblyexport

import (
	"encoding/json"
	"testing"

	"github.com/tonedefdev/opendepot/pkg/hclschema"
	"github.com/tonedefdev/opendepot/pkg/hclschema/providerschema"
)

func requireDiagnostic(t *testing.T, diagnostics []Diagnostic, code, path string) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Path == path {
			return
		}
	}

	t.Fatalf("diagnostics = %#v, want %s at %s", diagnostics, code, path)
}

func coverageModuleDocument(name string, variables ...hclschema.ContractVariable) hclschema.Contract {
	return hclschema.Contract{
		SchemaVersion: hclschema.ContractSchemaVersion,
		Module:        hclschema.ContractModule{Namespace: "platform", Name: name, Provider: "aws", Version: "1.2.3"},
		Variables:     variables,
		Compatibility: hclschema.Compatibility{Grade: hclschema.GradeFull},
	}
}

func coverageProviderDocument(version string, configuration providerschema.Block) AuthoritativeProvider {
	return AuthoritativeProvider{
		Namespace:         "platform",
		Name:              "aws-provider",
		ProviderNamespace: "hashicorp",
		ProviderName:      "aws",
		Version:           version,
		Configuration:     configuration,
	}
}

func TestBuildModelRejectsUnsupportedSchemaVersion(t *testing.T) {
	model, diagnostics := BuildModel(Request{SchemaVersion: "assembly.export.v0"}, "opendepot.example.com", AuthoritativeDocuments{})
	requireDiagnostic(t, diagnostics, "unsupported_schema", "schemaVersion")
	if len(model.Variables) != 0 || len(model.Modules) != 0 || len(model.Providers) != 0 {
		t.Fatalf("model = %#v, want empty model for unsupported schema", model)
	}
	if model.RegistryHost != "opendepot.example.com" {
		t.Fatalf("RegistryHost = %q, want opendepot.example.com", model.RegistryHost)
	}
}

func TestBuildModelReportsInvalidAndDuplicateLocalNames(t *testing.T) {
	_, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Variables: []VariableNode{
			{NodeID: "variable-bad", Name: "1bad", Type: TypeSpec{Kind: "string"}},
			{NodeID: "variable-one", Name: "region", Type: TypeSpec{Kind: "string"}},
			{NodeID: "variable-two", Name: "region", Type: TypeSpec{Kind: "string"}},
		},
		Modules: []ModuleNode{
			{NodeID: "module-bad", LocalName: "bad name", Multiplicity: Multiplicity{Kind: "none"}},
			{NodeID: "module-one", LocalName: "vpc", Multiplicity: Multiplicity{Kind: "none"}},
			{NodeID: "module-two", LocalName: "vpc", Multiplicity: Multiplicity{Kind: "none"}},
		},
		Providers: []ProviderNode{
			{NodeID: "provider-bad", LocalName: "not valid", Version: "5.0.0"},
			{NodeID: "provider-alias", LocalName: "aws", Alias: "bad alias", Version: "5.0.0"},
			{NodeID: "provider-one", LocalName: "aws", Version: "5.0.0"},
			{NodeID: "provider-two", LocalName: "aws", Version: "5.0.0"},
		},
	}, "opendepot.example.com", AuthoritativeDocuments{})

	requireDiagnostic(t, diagnostics, "invalid_variable_name", "variables[0].name")
	requireDiagnostic(t, diagnostics, "duplicate_variable_name", "variables[2].name")
	requireDiagnostic(t, diagnostics, "invalid_module_name", "modules[0].localName")
	requireDiagnostic(t, diagnostics, "duplicate_module_name", "modules[2].localName")
	requireDiagnostic(t, diagnostics, "invalid_provider_name", "providers[0].localName")
	requireDiagnostic(t, diagnostics, "invalid_provider_alias", "providers[1].alias")
	requireDiagnostic(t, diagnostics, "duplicate_provider_configuration", "providers[3].alias")
}

func TestBuildModelReportsInvalidVariableTypesAndDefaults(t *testing.T) {
	model, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Variables: []VariableNode{
			{NodeID: "variable-kind", Name: "kind", Type: TypeSpec{Kind: "bogus"}},
			{NodeID: "variable-list", Name: "items", Type: TypeSpec{Kind: "list"}},
			{NodeID: "variable-default", Name: "region", Type: TypeSpec{Kind: "string"}, HasDefault: true, Default: ValueSpec{Kind: "list"}},
			{NodeID: "variable-list-default", Name: "names", Type: TypeSpec{Kind: "list", Element: &TypeSpec{Kind: "string"}}, HasDefault: true, Default: ValueSpec{Kind: "scalar", Literal: "x"}},
			{NodeID: "variable-described", Name: "zone", Description: "Availability zone.", Type: TypeSpec{Kind: "string"}},
		},
	}, "opendepot.example.com", AuthoritativeDocuments{})

	requireDiagnostic(t, diagnostics, "invalid_variable_type", "variables[0].type")
	requireDiagnostic(t, diagnostics, "invalid_variable_type", "variables[1].type")
	requireDiagnostic(t, diagnostics, "invalid_variable_default", "variables[2].default")
	requireDiagnostic(t, diagnostics, "invalid_variable_default", "variables[3].default")
	if len(model.Variables) != 3 {
		t.Fatalf("model.Variables = %#v, want the three variables with valid types", model.Variables)
	}
	if zone := model.Variables[2]; zone.Name != "zone" || zone.Description != "Availability zone." {
		t.Fatalf("model.Variables[2] = %#v, want zone with its description", zone)
	}
}

func TestBuildModelRendersTypedVariableDefaults(t *testing.T) {
	model, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Variables: []VariableNode{
			{
				NodeID:     "variable-empty",
				Name:       "empty",
				Type:       TypeSpec{Kind: "string"},
				HasDefault: true,
				Default:    ValueSpec{Kind: "scalar", Literal: "   "},
			},
			{
				NodeID:     "variable-list",
				Name:       "names",
				Type:       TypeSpec{Kind: "list", Element: &TypeSpec{Kind: "string"}},
				HasDefault: true,
				Default: ValueSpec{Kind: "list", Items: []ValueSpec{
					{Kind: "scalar", Literal: "a"},
					{Kind: "scalar", Literal: "b"},
				}},
			},
			{
				NodeID:     "variable-map",
				Name:       "limits",
				Type:       TypeSpec{Kind: "map", Element: &TypeSpec{Kind: "number"}},
				HasDefault: true,
				Default: ValueSpec{Kind: "map", Entries: []ValueEntry{
					{Key: "cpu", Value: ValueSpec{Kind: "scalar", Literal: "2"}},
				}},
			},
			{
				NodeID:     "variable-tuple",
				Name:       "pair",
				Type:       TypeSpec{Kind: "tuple", Elements: []TypeSpec{{Kind: "string"}, {Kind: "bool"}}},
				HasDefault: true,
				Default: ValueSpec{Kind: "list", Items: []ValueSpec{
					{Kind: "scalar", Literal: "x"},
					{Kind: "scalar", Literal: "true"},
				}},
			},
			{
				NodeID: "variable-object",
				Name:   "settings",
				Type: TypeSpec{Kind: "object", Attributes: []TypeAttribute{
					{Name: "name", Type: TypeSpec{Kind: "string"}},
					{Name: "replicas", Type: TypeSpec{Kind: "number"}, Optional: true, HasDefault: true, Default: &ValueSpec{Kind: "scalar", Literal: "3"}},
					{Name: "labels", Type: TypeSpec{Kind: "map", Element: &TypeSpec{Kind: "string"}}, Optional: true},
				}},
				HasDefault: true,
				Default: ValueSpec{Kind: "object", Entries: []ValueEntry{
					{Name: "name", Value: ValueSpec{Kind: "scalar", Literal: "web"}},
				}},
			},
		},
	}, "opendepot.example.com", AuthoritativeDocuments{})
	if len(diagnostics) != 0 {
		t.Fatalf("BuildModel() diagnostics = %#v", diagnostics)
	}

	wantDefaults := map[string]string{
		"empty":    "null",
		"names":    `["a", "b"]`,
		"limits":   "{\n  \"cpu\" = 2\n}",
		"pair":     `["x", true]`,
		"settings": "{\n  name = \"web\"\n}",
	}
	for _, variable := range model.Variables {
		want, ok := wantDefaults[variable.Name]
		if !ok {
			t.Fatalf("unexpected variable %q", variable.Name)
		}
		if variable.DefaultExpression == nil || *variable.DefaultExpression != want {
			t.Errorf("default for %s = %v, want %q", variable.Name, variable.DefaultExpression, want)
		}
	}
}

func TestBuildModelReportsInvalidTypeSpecs(t *testing.T) {
	cases := []struct {
		name     string
		spec     TypeSpec
		wantPath string
	}{
		{
			name:     "list without element",
			spec:     TypeSpec{Kind: "list"},
			wantPath: "variables[0].type",
		},
		{
			name:     "tuple with unsupported element",
			spec:     TypeSpec{Kind: "tuple", Elements: []TypeSpec{{Kind: "bogus"}}},
			wantPath: "variables[0].type",
		},
		{
			name:     "map with unsupported element",
			spec:     TypeSpec{Kind: "map", Element: &TypeSpec{Kind: "bogus"}},
			wantPath: "variables[0].type",
		},
		{
			name:     "object attribute with invalid identifier",
			spec:     TypeSpec{Kind: "object", Attributes: []TypeAttribute{{Name: "bad name", Type: TypeSpec{Kind: "string"}}}},
			wantPath: "variables[0].type",
		},
		{
			name:     "object attribute with unsupported type",
			spec:     TypeSpec{Kind: "object", Attributes: []TypeAttribute{{Name: "name", Type: TypeSpec{Kind: "bogus"}}}},
			wantPath: "variables[0].type",
		},
		{
			name:     "optional attribute with default but no default value",
			spec:     TypeSpec{Kind: "object", Attributes: []TypeAttribute{{Name: "replicas", Type: TypeSpec{Kind: "number"}, Optional: true, HasDefault: true}}},
			wantPath: "variables[0].type",
		},
		{
			name:     "optional attribute default with mismatched value",
			spec:     TypeSpec{Kind: "object", Attributes: []TypeAttribute{{Name: "tags", Type: TypeSpec{Kind: "list", Element: &TypeSpec{Kind: "string"}}, Optional: true, HasDefault: true, Default: &ValueSpec{Kind: "scalar", Literal: "x"}}}},
			wantPath: "variables[0].type",
		},
		{
			name:     "unsupported top-level kind",
			spec:     TypeSpec{Kind: "bogus"},
			wantPath: "variables[0].type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diagnostics := BuildModel(Request{
				SchemaVersion: SchemaVersion,
				Variables:     []VariableNode{{NodeID: "variable-type", Name: "value", Type: tc.spec}},
			}, "opendepot.example.com", AuthoritativeDocuments{})
			requireDiagnostic(t, diagnostics, "invalid_variable_type", tc.wantPath)
		})
	}
}

func TestBuildModelReportsInvalidValueSpecs(t *testing.T) {
	cases := []struct {
		name  string
		spec  TypeSpec
		value ValueSpec
	}{
		{
			name:  "number default is a list",
			spec:  TypeSpec{Kind: "number"},
			value: ValueSpec{Kind: "list"},
		},
		{
			name:  "list default is scalar",
			spec:  TypeSpec{Kind: "list", Element: &TypeSpec{Kind: "string"}},
			value: ValueSpec{Kind: "scalar", Literal: "x"},
		},
		{
			name:  "list element is not a valid value",
			spec:  TypeSpec{Kind: "list", Element: &TypeSpec{Kind: "number"}},
			value: ValueSpec{Kind: "list", Items: []ValueSpec{{Kind: "list"}}},
		},
		{
			name:  "tuple has too many values",
			spec:  TypeSpec{Kind: "tuple", Elements: []TypeSpec{{Kind: "string"}}},
			value: ValueSpec{Kind: "list", Items: []ValueSpec{{Kind: "scalar", Literal: "a"}, {Kind: "scalar", Literal: "b"}}},
		},
		{
			name:  "tuple has too few values",
			spec:  TypeSpec{Kind: "tuple", Elements: []TypeSpec{{Kind: "string"}, {Kind: "string"}}},
			value: ValueSpec{Kind: "list", Items: []ValueSpec{{Kind: "scalar", Literal: "a"}}},
		},
		{
			name:  "map default is a list",
			spec:  TypeSpec{Kind: "map", Element: &TypeSpec{Kind: "string"}},
			value: ValueSpec{Kind: "list"},
		},
		{
			name: "map default has duplicate keys",
			spec: TypeSpec{Kind: "map", Element: &TypeSpec{Kind: "string"}},
			value: ValueSpec{Kind: "map", Entries: []ValueEntry{
				{Key: "a", Value: ValueSpec{Kind: "scalar", Literal: "1"}},
				{Key: "a", Value: ValueSpec{Kind: "scalar", Literal: "2"}},
			}},
		},
		{
			name:  "object default is a scalar",
			spec:  TypeSpec{Kind: "object", Attributes: []TypeAttribute{{Name: "name", Type: TypeSpec{Kind: "string"}}}},
			value: ValueSpec{Kind: "scalar", Literal: "x"},
		},
		{
			name: "object default has duplicate attributes",
			spec: TypeSpec{Kind: "object", Attributes: []TypeAttribute{{Name: "name", Type: TypeSpec{Kind: "string"}}}},
			value: ValueSpec{Kind: "object", Entries: []ValueEntry{
				{Name: "name", Value: ValueSpec{Kind: "scalar", Literal: "a"}},
				{Name: "name", Value: ValueSpec{Kind: "scalar", Literal: "b"}},
			}},
		},
		{
			name:  "object default has unknown attribute",
			spec:  TypeSpec{Kind: "object", Attributes: []TypeAttribute{{Name: "name", Type: TypeSpec{Kind: "string"}}}},
			value: ValueSpec{Kind: "object", Entries: []ValueEntry{{Name: "other", Value: ValueSpec{Kind: "scalar", Literal: "a"}}}},
		},
		{
			name:  "object default is missing a required attribute",
			spec:  TypeSpec{Kind: "object", Attributes: []TypeAttribute{{Name: "name", Type: TypeSpec{Kind: "string"}}}},
			value: ValueSpec{Kind: "object"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diagnostics := BuildModel(Request{
				SchemaVersion: SchemaVersion,
				Variables: []VariableNode{{
					NodeID:     "variable-default",
					Name:       "value",
					Type:       tc.spec,
					HasDefault: true,
					Default:    tc.value,
				}},
			}, "opendepot.example.com", AuthoritativeDocuments{})
			requireDiagnostic(t, diagnostics, "invalid_variable_default", "variables[0].default")
		})
	}
}

func TestBuildModelReportsModuleDocumentFailures(t *testing.T) {
	unsupported := coverageModuleDocument("unsupported")
	unsupported.Compatibility.Grade = hclschema.GradeUnsupported

	wrongSchema := coverageModuleDocument("wrong-schema")
	wrongSchema.SchemaVersion = "assembly.module.v0"

	mismatched := coverageModuleDocument("other")

	documents := AuthoritativeDocuments{
		Modules: map[string]hclschema.Contract{
			"module-unsupported": unsupported,
			"module-schema":      wrongSchema,
			"module-mismatch":    mismatched,
		},
	}

	_, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Modules: []ModuleNode{
			{NodeID: "module-missing", LocalName: "missing", Namespace: "platform", Name: "vpc", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}},
			{NodeID: "module-unsupported", LocalName: "unsupported", Namespace: "platform", Name: "unsupported", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}},
			{NodeID: "module-schema", LocalName: "schema", Namespace: "platform", Name: "wrong-schema", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}},
			{NodeID: "module-mismatch", LocalName: "mismatch", Namespace: "platform", Name: "vpc", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}},
		},
	}, "opendepot.example.com", documents)

	requireDiagnostic(t, diagnostics, "module_unavailable", "modules[0]")
	requireDiagnostic(t, diagnostics, "module_unsupported", "modules[1]")
	requireDiagnostic(t, diagnostics, "module_unsupported", "modules[2]")
	requireDiagnostic(t, diagnostics, "module_identity_mismatch", "modules[3]")
}

func TestBuildModelReportsModuleInputFailures(t *testing.T) {
	documents := AuthoritativeDocuments{
		Modules: map[string]hclschema.Contract{
			"module-vpc": coverageModuleDocument("vpc",
				hclschema.ContractVariable{Name: "cidr", Type: json.RawMessage(`"string"`), Required: true},
				hclschema.ContractVariable{Name: "name", Type: json.RawMessage(`"string"`)},
				hclschema.ContractVariable{Name: "count", Type: json.RawMessage(`"number"`)},
			),
		},
	}

	model, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Modules: []ModuleNode{{
			NodeID:       "module-vpc",
			LocalName:    "vpc",
			Namespace:    "platform",
			Name:         "vpc",
			Version:      "v1.2.3",
			Multiplicity: Multiplicity{Kind: "none"},
			Values: map[string]InputValue{
				"name":    {Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: ""}},
				"count":   {Kind: "scalar"},
				"unknown": {Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "x"}},
			},
		}},
	}, "opendepot.example.com", documents)

	requireDiagnostic(t, diagnostics, "missing_required_input", "modules[0].values.cidr")
	requireDiagnostic(t, diagnostics, "malformed_expression", "modules[0].values.name")
	requireDiagnostic(t, diagnostics, "malformed_expression", "modules[0].values.count")
	requireDiagnostic(t, diagnostics, "unknown_module_input", "modules[0].values.unknown")
	if len(model.Modules) != 1 || model.Modules[0].Version != "1.2.3" {
		t.Fatalf("model.Modules = %#v, want one module normalized to 1.2.3", model.Modules)
	}
	if _, ok := model.Modules[0].Inputs["name"]; ok {
		t.Fatalf("Inputs = %#v, want failed input omitted", model.Modules[0].Inputs)
	}
}

func TestBuildModelRendersModuleMultiplicity(t *testing.T) {
	documents := AuthoritativeDocuments{
		Modules: map[string]hclschema.Contract{
			"module-subnet": coverageModuleDocument("subnet"),
		},
	}
	variable := VariableNode{NodeID: "variable-zones", Name: "zones", Type: TypeSpec{Kind: "list", Element: &TypeSpec{Kind: "string"}}}
	countExpression := FieldValue{Mode: "literal", Literal: "var.enabled"}
	missingCount := FieldValue{Mode: "literal", Literal: ""}
	forEachVariable := "variable-zones"
	missingVariable := "variable-missing"

	cases := []struct {
		name         string
		multiplicity Multiplicity
		wantCode     string
		wantPath     string
		wantCount    string
		wantForEach  string
	}{
		{name: "count without expression", multiplicity: Multiplicity{Kind: "count", Mode: "fixed"}, wantCode: "invalid_count", wantPath: "modules[0].multiplicity.expr"},
		{name: "count with unknown mode", multiplicity: Multiplicity{Kind: "count", Mode: "sometimes", Expr: &countExpression}, wantCode: "invalid_count", wantPath: "modules[0].multiplicity.mode"},
		{name: "count with malformed expression", multiplicity: Multiplicity{Kind: "count", Mode: "fixed", Expr: &missingCount}, wantCode: "malformed_expression", wantPath: "modules[0].multiplicity.expr"},
		{name: "conditional count", multiplicity: Multiplicity{Kind: "count", Mode: "conditional", Expr: &countExpression}, wantCount: "var.enabled ? 1 : 0"},
		{name: "fixed count", multiplicity: Multiplicity{Kind: "count", Mode: "fixed", Expr: &countExpression}, wantCount: "var.enabled"},
		{name: "for_each without variable", multiplicity: Multiplicity{Kind: "for_each"}, wantCode: "invalid_for_each", wantPath: "modules[0].multiplicity.variableNodeId"},
		{name: "for_each with dangling variable", multiplicity: Multiplicity{Kind: "for_each", VariableNodeID: &missingVariable}, wantCode: "invalid_for_each", wantPath: "modules[0].multiplicity.variableNodeId"},
		{name: "for_each over canvas variable", multiplicity: Multiplicity{Kind: "for_each", VariableNodeID: &forEachVariable}, wantForEach: "var.zones"},
		{name: "unknown multiplicity kind", multiplicity: Multiplicity{Kind: "sometimes"}, wantCode: "invalid_multiplicity", wantPath: "modules[0].multiplicity.kind"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, diagnostics := BuildModel(Request{
				SchemaVersion: SchemaVersion,
				Variables:     []VariableNode{variable},
				Modules: []ModuleNode{{
					NodeID:       "module-subnet",
					LocalName:    "subnet",
					Namespace:    "platform",
					Name:         "subnet",
					Version:      "1.2.3",
					Multiplicity: tc.multiplicity,
				}},
			}, "opendepot.example.com", documents)

			if tc.wantCode != "" {
				requireDiagnostic(t, diagnostics, tc.wantCode, tc.wantPath)
				return
			}
			if len(diagnostics) != 0 {
				t.Fatalf("BuildModel() diagnostics = %#v", diagnostics)
			}
			if got := model.Modules[0].CountExpression; got != tc.wantCount {
				t.Errorf("CountExpression = %q, want %q", got, tc.wantCount)
			}
			if got := model.Modules[0].ForEachExpression; got != tc.wantForEach {
				t.Errorf("ForEachExpression = %q, want %q", got, tc.wantForEach)
			}
		})
	}
}

func TestBuildModelValidatesModuleProviderBindings(t *testing.T) {
	vpc := coverageModuleDocument("vpc")
	vpc.RequiredProviders = []hclschema.ContractRequiredProvider{
		{LocalName: "aws", Source: "registry.opendepot.example.com/hashicorp/aws"},
	}
	documents := AuthoritativeDocuments{
		Modules: map[string]hclschema.Contract{"module-vpc": vpc},
		Providers: map[string]AuthoritativeProvider{
			"provider-east": coverageProviderDocument("5.0.0", providerschema.Block{}),
			"provider-gcp": {
				Namespace:         "platform",
				Name:              "gcp-provider",
				ProviderNamespace: "hashicorp",
				ProviderName:      "google",
				Version:           "5.0.0",
			},
		},
	}

	providers := []ProviderNode{
		{NodeID: "provider-east", LocalName: "aws", Alias: "east", Namespace: "platform", Name: "aws-provider", ProviderNamespace: "hashicorp", ProviderName: "aws", Version: "5.0.0"},
		{NodeID: "provider-gcp", LocalName: "google", Namespace: "platform", Name: "gcp-provider", ProviderNamespace: "hashicorp", ProviderName: "google", Version: "5.0.0"},
	}

	_, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Providers:     providers,
		Modules: []ModuleNode{{
			NodeID:       "module-vpc",
			LocalName:    "vpc",
			Namespace:    "platform",
			Name:         "vpc",
			Version:      "1.2.3",
			Multiplicity: Multiplicity{Kind: "none"},
			ProviderBindings: map[string]ProviderBinding{
				"unknown": {ProviderNodeID: "provider-east"},
				"aws":     {ProviderNodeID: "provider-missing"},
			},
		}},
	}, "opendepot.example.com", documents)

	requireDiagnostic(t, diagnostics, "unknown_provider_binding", "modules[0].providerBindings.unknown")
	requireDiagnostic(t, diagnostics, "dangling_provider_binding", "modules[0].providerBindings.aws")

	_, mismatchDiagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Providers:     providers,
		Modules: []ModuleNode{{
			NodeID:       "module-vpc",
			LocalName:    "vpc",
			Namespace:    "platform",
			Name:         "vpc",
			Version:      "1.2.3",
			Multiplicity: Multiplicity{Kind: "none"},
			ProviderBindings: map[string]ProviderBinding{
				"aws": {ProviderNodeID: "provider-gcp"},
			},
		}},
	}, "opendepot.example.com", documents)
	requireDiagnostic(t, mismatchDiagnostics, "provider_binding_mismatch", "modules[0].providerBindings.aws")

	bindModel, bindDiagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Providers:     providers,
		Modules: []ModuleNode{{
			NodeID:       "module-vpc",
			LocalName:    "vpc",
			Namespace:    "platform",
			Name:         "vpc",
			Version:      "1.2.3",
			Multiplicity: Multiplicity{Kind: "none"},
			ProviderBindings: map[string]ProviderBinding{
				"aws": {ProviderNodeID: "provider-east"},
			},
		}},
	}, "opendepot.example.com", documents)
	if len(bindDiagnostics) != 0 {
		t.Fatalf("BuildModel() diagnostics = %#v", bindDiagnostics)
	}
	if got := bindModel.Modules[0].ProviderBindings["aws"]; got != (ProviderReference{LocalName: "aws", Alias: "east"}) {
		t.Fatalf("ProviderBindings[aws] = %#v, want aws alias east", got)
	}
}

func TestBuildModelReportsProviderFailures(t *testing.T) {
	documents := AuthoritativeDocuments{
		Providers: map[string]AuthoritativeProvider{
			"provider-aws":    coverageProviderDocument("5.0.0", providerschema.Block{}),
			"provider-second": coverageProviderDocument("4.0.0", providerschema.Block{}),
			"provider-mirror": coverageProviderDocument("5.0.0", providerschema.Block{}),
		},
	}

	_, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Providers: []ProviderNode{
			{NodeID: "provider-missing", LocalName: "missing", Namespace: "platform", Name: "aws-provider", ProviderNamespace: "hashicorp", ProviderName: "aws", Version: "5.0.0"},
			{NodeID: "provider-aws", LocalName: "aws", Namespace: "other", Name: "aws-provider", ProviderNamespace: "hashicorp", ProviderName: "aws", Version: "5.0.0"},
			{NodeID: "provider-aws", LocalName: "shared", Namespace: "platform", Name: "aws-provider", ProviderNamespace: "hashicorp", ProviderName: "aws", Version: "5.0.0"},
			{NodeID: "provider-second", LocalName: "shared", Namespace: "platform", Name: "aws-provider", ProviderNamespace: "hashicorp", ProviderName: "aws", Version: "4.0.0"},
			{NodeID: "provider-mirror", LocalName: "shared", Namespace: "platform", Name: "aws-provider", ProviderNamespace: "hashicorp", ProviderName: "aws", Version: "5.0.0", Alias: "mirror"},
		},
	}, "opendepot.example.com", documents)

	requireDiagnostic(t, diagnostics, "provider_unavailable", "providers[0]")
	requireDiagnostic(t, diagnostics, "provider_identity_mismatch", "providers[1]")
	requireDiagnostic(t, diagnostics, "conflicting_provider_requirement", "providers[3].localName")
}

func TestBuildModelValidatesProviderConfiguration(t *testing.T) {
	stringType := json.RawMessage(`"string"`)
	numberType := json.RawMessage(`"number"`)
	schema := providerschema.Block{
		Attributes: map[string]providerschema.Attribute{
			"region": {Type: stringType, Required: true},
			"retry":  {Type: numberType, Optional: true},
		},
		Blocks: map[string]providerschema.NestedBlock{
			"assume_role": {Nesting: "single", MinItems: 1, MaxItems: 1, Block: providerschema.Block{
				Attributes: map[string]providerschema.Attribute{"arn": {Type: stringType, Required: true}},
			}},
			"endpoints": {Nesting: "list", MaxItems: 1, Block: providerschema.Block{
				Attributes: map[string]providerschema.Attribute{"url": {Type: stringType}},
			}},
		},
	}
	documents := AuthoritativeDocuments{
		Providers: map[string]AuthoritativeProvider{
			"provider-aws": coverageProviderDocument("5.0.0", schema),
		},
	}

	_, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Variables:     []VariableNode{{NodeID: "variable-region", Name: "region", Type: TypeSpec{Kind: "string"}}},
		Providers: []ProviderNode{{
			NodeID:            "provider-aws",
			LocalName:         "aws",
			Namespace:         "platform",
			Name:              "aws-provider",
			ProviderNamespace: "hashicorp",
			ProviderName:      "aws",
			Version:           "5.0.0",
			Configuration: ProviderConfiguration{
				Arguments: map[string]FieldValue{
					"retry":   {Mode: "literal", Literal: ""},
					"unknown": {Mode: "literal", Literal: "x"},
				},
				Blocks: map[string][]ProviderConfiguration{
					"unknown_block": {{}},
					"endpoints": {
						{Arguments: map[string]FieldValue{"url": {Mode: "literal", Literal: "https://a.example.com"}}},
						{Arguments: map[string]FieldValue{"url": {Mode: "literal", Literal: "https://b.example.com"}}},
					},
				},
			},
		}},
	}, "opendepot.example.com", documents)

	requireDiagnostic(t, diagnostics, "missing_required_provider_argument", "providers[0].configuration.arguments.region")
	requireDiagnostic(t, diagnostics, "malformed_expression", "providers[0].configuration.arguments.retry")
	requireDiagnostic(t, diagnostics, "unknown_provider_argument", "providers[0].configuration.arguments.unknown")
	requireDiagnostic(t, diagnostics, "unknown_provider_block", "providers[0].configuration.blocks.unknown_block")
	requireDiagnostic(t, diagnostics, "missing_required_provider_block", "providers[0].configuration.blocks.assume_role")
	requireDiagnostic(t, diagnostics, "too_many_provider_blocks", "providers[0].configuration.blocks.endpoints")
}

func TestBuildModelProviderConfigurationStringArguments(t *testing.T) {
	stringType := json.RawMessage(`"string"`)
	schema := providerschema.Block{
		Attributes: map[string]providerschema.Attribute{
			"region":  {Type: stringType, Optional: true},
			"profile": {Type: stringType, Optional: true},
			"zone":    {Type: stringType, Optional: true},
			"ratio":   {Type: json.RawMessage(`"number"`), Optional: true},
		},
	}
	documents := AuthoritativeDocuments{
		Providers: map[string]AuthoritativeProvider{
			"provider-aws": coverageProviderDocument("5.0.0", schema),
		},
	}

	model, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Variables:     []VariableNode{{NodeID: "variable-region", Name: "region", Type: TypeSpec{Kind: "string"}}},
		Providers: []ProviderNode{{
			NodeID:            "provider-aws",
			LocalName:         "aws",
			Namespace:         "platform",
			Name:              "aws-provider",
			ProviderNamespace: "hashicorp",
			ProviderName:      "aws",
			Version:           "5.0.0",
			Configuration: ProviderConfiguration{
				Arguments: map[string]FieldValue{
					"region":  {Mode: "reference", RefNodeID: "variable-region"},
					"profile": {Mode: "literal", Literal: "default"},
					"zone":    {Mode: "literal", Literal: `upper("us-east-1")`},
					"ratio":   {Mode: "literal", Literal: "0.5"},
				},
			},
		}},
	}, "opendepot.example.com", documents)
	if len(diagnostics) != 0 {
		t.Fatalf("BuildModel() diagnostics = %#v", diagnostics)
	}

	wantArguments := map[string]string{
		"region":  "var.region",
		"profile": `"default"`,
		"zone":    `upper("us-east-1")`,
		"ratio":   "0.5",
	}
	for name, want := range wantArguments {
		if got := model.Providers[0].Configuration.Arguments[name]; got != want {
			t.Errorf("argument %s = %q, want %q", name, got, want)
		}
	}
}

func TestBuildModelResolvesFieldReferences(t *testing.T) {
	regionMap := VariableNode{
		NodeID:     "variable-regions",
		Name:       "regions",
		Type:       TypeSpec{Kind: "map", Element: &TypeSpec{Kind: "string"}},
		HasDefault: true,
		Default:    ValueSpec{Kind: "map", Entries: []ValueEntry{{Key: "east", Value: ValueSpec{Kind: "scalar", Literal: "us-east-1"}}}},
	}
	zoneList := VariableNode{
		NodeID:     "variable-zones",
		Name:       "zones",
		Type:       TypeSpec{Kind: "list", Element: &TypeSpec{Kind: "string"}},
		HasDefault: true,
		Default:    ValueSpec{Kind: "list", Items: []ValueSpec{{Kind: "scalar", Literal: "a"}, {Kind: "list"}}},
	}
	countVariable := "variable-regions"

	countModule := ModuleNode{NodeID: "module-count", LocalName: "count_vpc", Namespace: "platform", Name: "vpc", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "count", Mode: "fixed", Expr: &FieldValue{Mode: "literal", Literal: "2"}}}
	forEachModule := ModuleNode{NodeID: "module-each", LocalName: "each_vpc", Namespace: "platform", Name: "vpc", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "for_each", VariableNodeID: &countVariable}}
	plainModule := ModuleNode{NodeID: "module-plain", LocalName: "plain", Namespace: "platform", Name: "vpc", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}}
	badModule := ModuleNode{NodeID: "module-bad", LocalName: "bad", Namespace: "platform", Name: "vpc", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "sometimes"}}

	referenceModule := func(node string, selector *ReferenceSelector, output string) ModuleNode {
		return ModuleNode{
			NodeID:       "module-consumer-" + node,
			LocalName:    "consumer_" + node,
			Namespace:    "platform",
			Name:         "consumer",
			Version:      "1.2.3",
			Multiplicity: Multiplicity{Kind: "none"},
			Values: map[string]InputValue{
				"value": {Kind: "scalar", Value: &FieldValue{Mode: "reference", RefNodeID: node, RefOutput: output, RefSelector: selector}},
			},
		}
	}
	indexOf := func(literal string) *FieldValue {
		return &FieldValue{Mode: "literal", Literal: literal}
	}

	cases := []struct {
		name     string
		consumer ModuleNode
		want     string
		wantCode string
	}{
		{name: "plain module output", consumer: referenceModule("module-plain", nil, "id"), want: "module.plain.id"},
		{name: "variable output with selector", consumer: ModuleNode{NodeID: "module-var", LocalName: "var_consumer", Namespace: "platform", Name: "consumer", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}, Values: map[string]InputValue{"value": {Kind: "scalar", Value: &FieldValue{Mode: "reference", RefNodeID: "variable-zones", RefOutputSelector: &ReferenceSelector{Kind: "index", Expr: indexOf("0")}}}}}, want: "var.zones[0]"},
		{name: "variable output selector kind is invalid", consumer: ModuleNode{NodeID: "module-var", LocalName: "var_consumer", Namespace: "platform", Name: "consumer", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}, Values: map[string]InputValue{"value": {Kind: "scalar", Value: &FieldValue{Mode: "reference", RefNodeID: "variable-zones", RefOutputSelector: &ReferenceSelector{Kind: "slice"}}}}}, wantCode: "invalid_output_selector"},
		{name: "variable output selector without expression", consumer: ModuleNode{NodeID: "module-var", LocalName: "var_consumer", Namespace: "platform", Name: "consumer", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}, Values: map[string]InputValue{"value": {Kind: "scalar", Value: &FieldValue{Mode: "reference", RefNodeID: "variable-zones", RefOutputSelector: &ReferenceSelector{Kind: "key"}}}}}, wantCode: "invalid_output_selector"},
		{name: "plain module with selector", consumer: referenceModule("module-plain", &ReferenceSelector{Kind: "all"}, "id"), wantCode: "invalid_reference_selector"},
		{name: "plain module without output", consumer: referenceModule("module-plain", nil, ""), wantCode: "dangling_reference"},
		{name: "dangling reference", consumer: referenceModule("module-missing", nil, "id"), wantCode: "dangling_reference"},
		{name: "counted module requires selector", consumer: referenceModule("module-count", nil, "id"), wantCode: "missing_reference_selector"},
		{name: "counted module all selector", consumer: referenceModule("module-count", &ReferenceSelector{Kind: "all"}, "id"), want: "module.count_vpc[*].id"},
		{name: "counted module index selector", consumer: referenceModule("module-count", &ReferenceSelector{Kind: "index", Expr: indexOf("1")}, "id"), want: "module.count_vpc[1].id"},
		{name: "counted module index without expression", consumer: referenceModule("module-count", &ReferenceSelector{Kind: "index"}, "id"), wantCode: "invalid_reference_selector"},
		{name: "counted module key selector rejected", consumer: referenceModule("module-count", &ReferenceSelector{Kind: "key", Expr: indexOf("x")}, "id"), wantCode: "invalid_reference_selector"},
		{name: "for_each module requires selector", consumer: referenceModule("module-each", nil, "id"), wantCode: "missing_reference_selector"},
		{name: "for_each module all selector", consumer: referenceModule("module-each", &ReferenceSelector{Kind: "all"}, "id"), want: "[for instance in module.each_vpc : instance.id]"},
		{name: "for_each module known key is quoted", consumer: referenceModule("module-each", &ReferenceSelector{Kind: "key", Expr: indexOf("east")}, "id"), want: `module.each_vpc["east"].id`},
		{name: "for_each module unknown key is an expression", consumer: referenceModule("module-each", &ReferenceSelector{Kind: "key", Expr: &FieldValue{Mode: "reference", RefNodeID: "variable-regions"}}, "id"), want: "module.each_vpc[var.regions].id"},
		{name: "for_each module index selector rejected", consumer: referenceModule("module-each", &ReferenceSelector{Kind: "index", Expr: indexOf("0")}, "id"), wantCode: "invalid_reference_selector"},
		{name: "module with unknown multiplicity", consumer: referenceModule("module-bad", nil, "id"), wantCode: "invalid_reference_source"},
		{name: "reference with unknown mode", consumer: ModuleNode{NodeID: "module-mode", LocalName: "mode_consumer", Namespace: "platform", Name: "consumer", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}, Values: map[string]InputValue{"value": {Kind: "scalar", Value: &FieldValue{Mode: "sideways"}}}}, wantCode: "invalid_value_mode"},
		{name: "literal list element that fails to parse", consumer: ModuleNode{NodeID: "module-literal", LocalName: "literal_consumer", Namespace: "platform", Name: "consumer", Version: "1.2.3", Multiplicity: Multiplicity{Kind: "none"}, Values: map[string]InputValue{"value": {Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "var.region =="}}}}, wantCode: "malformed_expression"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			modules := []ModuleNode{countModule, forEachModule, plainModule, badModule, tc.consumer}
			documents := AuthoritativeDocuments{Modules: map[string]hclschema.Contract{}}
			for _, module := range modules {
				documents.Modules[module.NodeID] = coverageModuleDocument(module.Name, hclschema.ContractVariable{Name: "value", Type: json.RawMessage(`"dynamic"`)})
			}

			model, diagnostics := BuildModel(Request{
				SchemaVersion: SchemaVersion,
				Variables:     []VariableNode{regionMap, zoneList},
				Modules:       modules,
			}, "opendepot.example.com", documents)

			if tc.wantCode != "" {
				if !hasDiagnosticCode(diagnostics, tc.wantCode) {
					t.Fatalf("diagnostics = %#v, want %s", diagnostics, tc.wantCode)
				}
				return
			}

			consumer := model.Modules[len(model.Modules)-1]
			if got := consumer.Inputs["value"]; got != tc.want {
				t.Fatalf("value input = %q, want %q (diagnostics %#v)", got, tc.want, diagnostics)
			}
		})
	}
}

func TestBuildModelReportsMalformedLiteralFields(t *testing.T) {
	_, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Modules: []ModuleNode{{
			NodeID:       "module-vpc",
			LocalName:    "vpc",
			Namespace:    "platform",
			Name:         "vpc",
			Version:      "1.2.3",
			Multiplicity: Multiplicity{Kind: "none"},
			Values: map[string]InputValue{
				"blank": {Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "  "}},
			},
		}},
	}, "opendepot.example.com", AuthoritativeDocuments{
		Modules: map[string]hclschema.Contract{"module-vpc": coverageModuleDocument("vpc", hclschema.ContractVariable{Name: "blank", Type: json.RawMessage(`"string"`)})},
	})

	requireDiagnostic(t, diagnostics, "malformed_expression", "modules[0].values.blank")
}
