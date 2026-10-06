package assemblyexport

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"

	"github.com/tonedefdev/opendepot/pkg/hclschema"
	"github.com/tonedefdev/opendepot/pkg/hclschema/providerschema"
)

func TestBuildModelQuotesTypedStringDefault(t *testing.T) {
	model, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Variables: []VariableNode{{
			NodeID:     "variable-1",
			Name:       "region",
			Type:       TypeSpec{Kind: "string"},
			HasDefault: true,
			Default:    ValueSpec{Kind: "scalar", Literal: "us-east-1"},
		}},
	}, "opendepot.example.com", AuthoritativeDocuments{})
	if len(diagnostics) != 0 {
		t.Fatalf("BuildModel() diagnostics = %#v", diagnostics)
	}
	if model.Variables[0].DefaultExpression == nil || *model.Variables[0].DefaultExpression != `"us-east-1"` {
		t.Fatalf("default expression = %v, want quoted string", model.Variables[0].DefaultExpression)
	}
}

func TestBuildModelValidatesVariableValidations(t *testing.T) {
	request := Request{
		SchemaVersion: SchemaVersion,
		Variables: []VariableNode{
			{
				NodeID: "variable-valid",
				Name:   "region",
				Type:   TypeSpec{Kind: "string"},
				Validations: []VariableValidation{
					{Condition: `contains(["us-east-1", "us-west-2"], var.region)`, ErrorMessage: "Region must be supported."},
					{Condition: `length(var.region) > 0`, ErrorMessage: "Region cannot be empty."},
				},
			},
			{
				NodeID: "variable-invalid",
				Name:   "environment",
				Type:   TypeSpec{Kind: "string"},
				Validations: []VariableValidation{
					{Condition: "", ErrorMessage: "Required."},
					{Condition: "var.environment ==", ErrorMessage: "Malformed."},
					{Condition: `var.environment != ""`, ErrorMessage: ""},
				},
			},
		},
	}

	model, diagnostics := BuildModel(request, "opendepot.example.com", AuthoritativeDocuments{})
	if got := len(model.Variables[0].Validations); got != 2 {
		t.Fatalf("valid validation count = %d, want 2", got)
	}
	if got := len(diagnostics); got != 3 {
		t.Fatalf("BuildModel() diagnostics = %#v, want 3 validation diagnostics", diagnostics)
	}

	wantPaths := []string{
		"variables[1].validations[0].condition",
		"variables[1].validations[1].condition",
		"variables[1].validations[2].errorMessage",
	}
	for index, path := range wantPaths {
		if diagnostics[index].Code != "invalid_variable_validation" || diagnostics[index].Path != path || diagnostics[index].NodeID != "variable-invalid" {
			t.Errorf("diagnostic[%d] = %#v, want invalid_variable_validation at %s", index, diagnostics[index], path)
		}
	}
}

func TestBuildModelAcceptsMultilineMapComprehensionValidation(t *testing.T) {
	condition := `alltrue([
  for _, function in var.lambda_functions :
  length(function.spec.description) <= 100
])`
	model, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Variables: []VariableNode{{
			NodeID: "lambda-functions",
			Name:   "lambda_functions",
			Type: TypeSpec{
				Kind: "map",
				Element: &TypeSpec{
					Kind: "object",
					Attributes: []TypeAttribute{{
						Name: "spec",
						Type: TypeSpec{
							Kind: "object",
							Attributes: []TypeAttribute{
								{Name: "description", Type: TypeSpec{Kind: "string"}},
								{Name: "timeout", Type: TypeSpec{Kind: "number"}, Optional: true, HasDefault: true, Default: &ValueSpec{Kind: "scalar", Literal: "300"}},
							},
						},
					}},
				},
			},
			Description: "A map of `Lambda` function specifications to create",
			Validations: []VariableValidation{{
				Condition:    condition,
				ErrorMessage: "The description must be less than or equal to 100 characters.",
			}},
			HasDefault: true,
			Default:    ValueSpec{Kind: "map"},
		}},
	}, "opendepot.example.com", AuthoritativeDocuments{})
	if len(diagnostics) != 0 {
		t.Fatalf("BuildModel() diagnostics = %#v", diagnostics)
	}

	files, err := Render(model)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	_, parseDiagnostics := hclsyntax.ParseConfig(files.Variables, "variables.tf", hcl.InitialPos)
	if parseDiagnostics.HasErrors() {
		t.Fatalf("rendered variables.tf is invalid HCL: %s\n%s", parseDiagnostics.Error(), files.Variables)
	}
	for _, expected := range []string{
		"for _, function in var.lambda_functions :",
		"length(function.spec.description) <= 100",
	} {
		if !strings.Contains(string(files.Variables), expected) {
			t.Errorf("variables.tf did not contain %q:\n%s", expected, files.Variables)
		}
	}
}

func TestBuildModelRejectsReversedMultilineValidationDelimiters(t *testing.T) {
	condition := `alltrue([
  for _, function in var.lambda_functions :
  length(function.spec.description) <= 100
)]`
	_, diagnostics := BuildModel(Request{
		SchemaVersion: SchemaVersion,
		Variables: []VariableNode{{
			NodeID: "lambda-functions",
			Name:   "lambda_functions",
			Type:   TypeSpec{Kind: "string"},
			Validations: []VariableValidation{{
				Condition:    condition,
				ErrorMessage: "The description must be less than or equal to 100 characters.",
			}},
		}},
	}, "opendepot.example.com", AuthoritativeDocuments{})
	if len(diagnostics) != 1 {
		t.Fatalf("BuildModel() diagnostics = %#v, want one invalid-condition diagnostic", diagnostics)
	}
	if diagnostics[0].Path != "variables[0].validations[0].condition" || !strings.Contains(diagnostics[0].Message, "Extra characters after the end of the 'for' expression") {
		t.Fatalf("BuildModel() diagnostic = %#v, want the reversed-delimiter parse error", diagnostics[0])
	}
}

func TestMapComprehensionValidationEvaluates(t *testing.T) {
	expression, diagnostics := hclsyntax.ParseExpression([]byte(`[for _, function in var.lambda_functions : function.spec.description]`), "condition.hcl", hcl.InitialPos)
	if diagnostics.HasErrors() {
		t.Fatalf("ParseExpression() diagnostics = %s", diagnostics.Error())
	}

	lambdaFunctions := cty.MapVal(map[string]cty.Value{
		"example": cty.ObjectVal(map[string]cty.Value{
			"spec": cty.ObjectVal(map[string]cty.Value{"description": cty.StringVal("short description")}),
		}),
	})
	result, diagnostics := expression.Value(&hcl.EvalContext{
		Variables: map[string]cty.Value{"var": cty.ObjectVal(map[string]cty.Value{"lambda_functions": lambdaFunctions})},
	})
	if diagnostics.HasErrors() {
		t.Fatalf("Value() diagnostics = %s", diagnostics.Error())
	}
	want := cty.TupleVal([]cty.Value{cty.StringVal("short description")})
	if !result.RawEquals(want) {
		t.Fatalf("condition result = %s, want %s", result.GoString(), want.GoString())
	}
}

func TestAssemblyRequestExpressionsParse(t *testing.T) {
	for _, expression := range []string{
		`"defdevio/${each.key}"`,
		`each.value.spec.description`,
		`replace(each.key, "_", "-")`,
		`module.lambda_roles[each.key].role_arns[each.key]`,
		`each.value.spec.timeout`,
		`"execution-role-${replace(each.key, "_", "-")}"`,
		`(each.key)`,
	} {
		t.Run(expression, func(t *testing.T) {
			if _, err := expressionTokens(expression, "value"); err != nil {
				t.Fatalf("expressionTokens(%q) error = %v", expression, err)
			}
		})
	}
}

func TestBuildModelQuotesModuleStringInputs(t *testing.T) {
	request := Request{
		SchemaVersion: SchemaVersion,
		Modules: []ModuleNode{{
			NodeID:       "module-1",
			Namespace:    "platform",
			Name:         "sns",
			Version:      "7.1.1",
			LocalName:    "sns",
			Multiplicity: Multiplicity{Kind: "none"},
			Values: map[string]InputValue{
				"display_name": {Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "opendepot-sns-topic"}},
				"enabled":      {Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "true"}},
				"iam_role_arn": {Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "module.lambda_roles[each.key].role_arns[each.key]"}},
			},
		}},
	}
	documents := AuthoritativeDocuments{Modules: map[string]hclschema.Contract{
		"module-1": {
			SchemaVersion: hclschema.ContractSchemaVersion,
			Module:        hclschema.ContractModule{Namespace: "platform", Name: "sns", Provider: "aws", Version: "7.1.1"},
			Variables: []hclschema.ContractVariable{
				{Name: "display_name", Type: json.RawMessage(`"string"`)},
				{Name: "enabled", Type: json.RawMessage(`"bool"`)},
				{Name: "iam_role_arn", Type: json.RawMessage(`"string"`)},
			},
			Compatibility: hclschema.Compatibility{Grade: hclschema.GradeFull},
		},
	}}

	model, diagnostics := BuildModel(request, "opendepot.example.com", documents)
	if len(diagnostics) != 0 {
		t.Fatalf("BuildModel() diagnostics = %#v", diagnostics)
	}
	if got := model.Modules[0].Inputs["display_name"]; got != `"opendepot-sns-topic"` {
		t.Fatalf("display_name expression = %q, want quoted string", got)
	}
	if got := model.Modules[0].Inputs["enabled"]; got != "true" {
		t.Fatalf("enabled expression = %q, want boolean expression", got)
	}
	if got := model.Modules[0].Inputs["iam_role_arn"]; got != "module.lambda_roles[each.key].role_arns[each.key]" {
		t.Fatalf("iam_role_arn expression = %q, want unquoted module traversal", got)
	}
	files, err := Render(model)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	main := string(files.Main)
	if !strings.Contains(main, `display_name = "opendepot-sns-topic"`) {
		t.Fatalf("main.tf did not contain quoted display_name:\n%s", main)
	}
	if !strings.Contains(main, "enabled      = true") {
		t.Fatalf("main.tf did not contain boolean enabled value:\n%s", main)
	}
	if !strings.Contains(main, "iam_role_arn = module.lambda_roles[each.key].role_arns[each.key]") {
		t.Fatalf("main.tf did not contain an unquoted iam_role_arn traversal:\n%s", main)
	}
}

func TestResolveModuleListInputQuotesStringElementsFromCtyType(t *testing.T) {
	value := InputValue{
		Kind: "list",
		Items: []InputValue{{
			Kind:  "scalar",
			Value: &FieldValue{Mode: "literal", Literal: "hello world!"},
		}},
	}
	got, diagnostic := resolveModuleInputType(
		value,
		json.RawMessage(`["list","string"]`),
		"module-lambda",
		"values.command",
		0,
		func(value FieldValue, nodeID, path string) (string, *Diagnostic) {
			return resolveFieldValue(value, nil, nil, nodeID, path)
		},
	)
	if diagnostic != nil {
		t.Fatalf("resolveModuleInputType() diagnostic = %#v", diagnostic)
	}
	if got != `["hello world!"]` {
		t.Fatalf("resolveModuleInputType() = %q, want [\"hello world!\"]", got)
	}
}

func TestBuildModelPreservesStringIterationExpression(t *testing.T) {
	request := Request{
		SchemaVersion: SchemaVersion,
		Modules: []ModuleNode{{
			NodeID:       "module-1",
			Namespace:    "platform",
			Name:         "sns",
			Version:      "7.1.1",
			LocalName:    "sns",
			Multiplicity: Multiplicity{Kind: "for_each"},
			Values:       map[string]InputValue{"name": {Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "each.key"}}},
		}},
	}
	documents := AuthoritativeDocuments{Modules: map[string]hclschema.Contract{
		"module-1": {
			SchemaVersion: hclschema.ContractSchemaVersion,
			Module:        hclschema.ContractModule{Namespace: "platform", Name: "sns", Provider: "aws", Version: "7.1.1"},
			Variables:     []hclschema.ContractVariable{{Name: "name", Type: json.RawMessage(`"string"`)}},
			Compatibility: hclschema.Compatibility{Grade: hclschema.GradeFull},
		},
	}}

	model, diagnostics := BuildModel(request, "opendepot.example.com", documents)
	if !hasDiagnosticCode(diagnostics, "invalid_for_each") {
		t.Fatalf("BuildModel() diagnostics = %#v, want invalid_for_each for missing canvas variable", diagnostics)
	}
	if got := model.Modules[0].Inputs["name"]; got != "each.key" {
		t.Fatalf("name expression = %q, want each.key", got)
	}
}

func TestResolveModuleScalarQuotesStringTemplate(t *testing.T) {
	got, diagnostic := resolveModuleScalar(
		FieldValue{Mode: "literal", Literal: `lambda-execution-${replace(each.key, "-", "_")}`},
		hclschema.ContractVariable{Type: json.RawMessage(`"dynamic"`)},
		"module-1",
		"values.roles.entries[0].value.name",
		func(value FieldValue, _ string, _ string) (string, *Diagnostic) {
			return value.Literal, nil
		},
	)
	if diagnostic != nil {
		t.Fatalf("resolveModuleScalar() diagnostic = %#v", diagnostic)
	}
	if got != "\"lambda-execution-${replace(each.key, \"-\", \"_\")}\"" {
		t.Fatalf("resolveModuleScalar() = %q", got)
	}
}

func TestResolveModuleScalarPreservesKnownFunction(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "replace", value: `replace(each.key, "_", "-")`, want: `replace(each.key, "_", "-")`},
		{name: "indexed module traversal", value: "module.lambda_roles[each.key].role_arns[each.key]", want: "module.lambda_roles[each.key].role_arns[each.key]"},
		{name: "parenthesized conditional", value: `(var.use_private ? "private" : "public")`, want: `(var.use_private ? "private" : "public")`},
		{name: "unknown function in conditional", value: `var.enabled ? custom(var.name) : "fallback"`, want: `"var.enabled ? custom(var.name) : \"fallback\""`},
		{name: "literal text", value: "replace-this", want: `"replace-this"`},
		{name: "unknown call", value: "custom(each.key)", want: `"custom(each.key)"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, diagnostic := resolveModuleScalar(
				FieldValue{Mode: "literal", Literal: test.value},
				hclschema.ContractVariable{Type: json.RawMessage(`"string"`)},
				"module-1",
				"values.name",
				func(value FieldValue, _ string, _ string) (string, *Diagnostic) {
					return value.Literal, nil
				},
			)
			if diagnostic != nil {
				t.Fatalf("resolveModuleScalar() diagnostic = %#v", diagnostic)
			}
			if got != test.want {
				t.Fatalf("resolveModuleScalar() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveProviderArgumentPreservesKnownStringExpressions(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "literal", value: "us-west-2", want: `"us-west-2"`},
		{name: "function", value: `replace(var.region, "_", "-")`, want: `replace(var.region, "_", "-")`},
		{name: "conditional", value: `var.private ? "private" : "public"`, want: `var.private ? "private" : "public"`},
		{name: "unknown function", value: `custom(var.region)`, want: `"custom(var.region)"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, diagnostic := resolveProviderArgument(
				FieldValue{Mode: "literal", Literal: test.value},
				providerschema.Attribute{Type: json.RawMessage(`"string"`)},
				"provider-1",
				"providers[0].configuration.arguments.region",
				func(value FieldValue, _ string, _ string) (string, *Diagnostic) {
					return value.Literal, nil
				},
			)
			if diagnostic != nil {
				t.Fatalf("resolveProviderArgument() diagnostic = %#v", diagnostic)
			}
			if got != test.want {
				t.Fatalf("resolveProviderArgument() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildModelRendersStructuredModuleInput(t *testing.T) {
	request := Request{
		SchemaVersion: SchemaVersion,
		Modules: []ModuleNode{{
			NodeID:    "module-1",
			Namespace: "platform",
			Name:      "network",
			Version:   "1.0.0",
			LocalName: "network",
			Values: map[string]InputValue{
				"settings": {
					Kind: "map",
					Entries: []InputValueEntry{{
						Key: "primary",
						Value: InputValue{Kind: "object", Entries: []InputValueEntry{{
							Name:  "region",
							Value: InputValue{Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "us-east-1"}},
						}}},
					}},
				},
			},
		}},
	}
	documents := AuthoritativeDocuments{Modules: map[string]hclschema.Contract{
		"module-1": {
			SchemaVersion: hclschema.ContractSchemaVersion,
			Module:        hclschema.ContractModule{Namespace: "platform", Name: "network", Provider: "aws", Version: "1.0.0"},
			Variables:     []hclschema.ContractVariable{{Name: "settings", Type: json.RawMessage(`["map",["object",{"region":"string"}]]`)}},
			Compatibility: hclschema.Compatibility{Grade: hclschema.GradeFull},
		},
	}}

	model, diagnostics := BuildModel(request, "opendepot.example.com", documents)
	if len(diagnostics) != 0 {
		t.Fatalf("BuildModel() diagnostics = %#v", diagnostics)
	}
	if got := model.Modules[0].Inputs["settings"]; got != "{\n  \"primary\" = {\n    region = \"us-east-1\"\n  }\n}" {
		t.Fatalf("settings expression = %q", got)
	}
}

func TestRenderMapKeyTokens(t *testing.T) {
	for _, test := range []struct {
		name string
		key  string
		want string
	}{
		{name: "iteration key", key: "each.key", want: "(each.key)"},
		{name: "variable", key: "var.name", want: "(var.name)"},
		{name: "module", key: "module.network.name", want: "(module.network.name)"},
		{name: "data source", key: "data.aws_region.current.name", want: "(data.aws_region.current.name)"},
		{name: "literal", key: "primary", want: `"primary"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tokens, err := renderMapKeyTokens(test.key)
			if err != nil {
				t.Fatalf("renderMapKeyTokens() error = %v", err)
			}
			got := strings.TrimSpace(string(hclwrite.Format(tokens.Bytes())))
			if got != test.want {
				t.Fatalf("renderMapKeyTokens() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRenderInputValueAcceptsExpressionMapKey(t *testing.T) {
	value, diagnostic := renderInputValue(
		InputValue{
			Kind: "map",
			Entries: []InputValueEntry{{
				Key:   "each.key",
				Value: InputValue{Kind: "scalar", Value: &FieldValue{Mode: "literal", Literal: "each.value"}},
			}},
		},
		json.RawMessage(`["map","string"]`),
		"module-1",
		"values.roles",
		0,
		func(value FieldValue, _ string, _ string) (string, *Diagnostic) {
			return value.Literal, nil
		},
	)
	if diagnostic != nil {
		t.Fatalf("renderInputValue() diagnostic = %#v", diagnostic)
	}
	if value != "{\n  (each.key) = each.value\n}" {
		t.Fatalf("renderInputValue() = %q, want {\\n  (each.key) = each.value\\n}", value)
	}
	if _, err := expressionTokens(value, "roles"); err != nil {
		t.Fatalf("rendered map expression is invalid HCL: %v", err)
	}
}

func TestRenderInputValueAcceptsPayloadRoleMap(t *testing.T) {
	value, diagnostic := renderInputValue(
		InputValue{
			Kind: "map",
			Entries: []InputValueEntry{{
				Key: "each.key",
				Value: InputValue{Kind: "object", Entries: []InputValueEntry{{
					Name: "name",
					Value: InputValue{Kind: "scalar", Value: &FieldValue{
						Mode:    "literal",
						Literal: `execution-role-${replace(each.key, "_", "-")}`,
					}},
				}}},
			}},
		},
		json.RawMessage(`["map",["object",{"name":"string"}]]`),
		"module-20",
		"values.roles",
		0,
		func(value FieldValue, nodeID, path string) (string, *Diagnostic) {
			return resolveModuleScalar(value, hclschema.ContractVariable{Type: json.RawMessage(`"string"`)}, nodeID, path, func(value FieldValue, _ string, _ string) (string, *Diagnostic) {
				return resolveFieldValue(value, nil, nil, nodeID, path)
			})
		},
	)
	if diagnostic != nil {
		t.Fatalf("renderInputValue() diagnostic = %#v", diagnostic)
	}
	if _, err := expressionTokens(value, "roles"); err != nil {
		t.Fatalf("rendered roles expression is invalid HCL: %v\n%s", err, value)
	}
}

func TestResolveFieldValueAppliesOutputSelector(t *testing.T) {
	key := FieldValue{Mode: "literal", Literal: `"primary"`}
	index := FieldValue{Mode: "literal", Literal: "0"}
	got, diagnostic := resolveFieldValue(FieldValue{
		Mode:              "reference",
		RefNodeID:         "module-1",
		RefOutput:         "subnets",
		RefSelector:       &ReferenceSelector{Kind: "key", Expr: &key},
		RefOutputSelector: &ReferenceSelector{Kind: "index", Expr: &index},
	}, nil, map[string]ModuleNode{
		"module-1": {LocalName: "network", Multiplicity: Multiplicity{Kind: "for_each"}},
	}, "module-2", "values.subnet")
	if diagnostic != nil {
		t.Fatalf("resolveFieldValue() diagnostic = %#v", diagnostic)
	}
	if got != `module.network["primary"].subnets[0]` {
		t.Fatalf("resolveFieldValue() = %q", got)
	}
}

func TestResolveFieldValueQuotesKnownForEachKeys(t *testing.T) {
	variableID := "variable-1"
	variables := map[string]VariableNode{
		variableID: {
			NodeID:     variableID,
			Name:       "lambda_functions",
			Type:       TypeSpec{Kind: "map", Element: &TypeSpec{Kind: "string"}},
			HasDefault: true,
			Default:    ValueSpec{Kind: "map", Entries: []ValueEntry{{Key: "primary"}}},
		},
	}
	modules := map[string]ModuleNode{
		"module-1": {
			LocalName:    "roles",
			Multiplicity: Multiplicity{Kind: "for_each", VariableNodeID: &variableID},
		},
	}

	for _, test := range []struct {
		name     string
		selector string
		want     string
	}{
		{name: "known key", selector: "primary", want: `module.roles["primary"].role_arn`},
		{name: "expression", selector: "each.key", want: "module.roles[each.key].role_arn"},
	} {
		t.Run(test.name, func(t *testing.T) {
			selector := FieldValue{Mode: "literal", Literal: test.selector}
			got, diagnostic := resolveFieldValue(FieldValue{
				Mode:        "reference",
				RefNodeID:   "module-1",
				RefOutput:   "role_arn",
				RefSelector: &ReferenceSelector{Kind: "key", Expr: &selector},
			}, variables, modules, "module-2", "values.role_arn")
			if diagnostic != nil {
				t.Fatalf("resolveFieldValue() diagnostic = %#v", diagnostic)
			}
			if got != test.want {
				t.Fatalf("resolveFieldValue() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildModelRejectsMissingRequiredInput(t *testing.T) {
	request := Request{
		SchemaVersion: SchemaVersion,
		Modules: []ModuleNode{{
			NodeID:       "module-1",
			Namespace:    "platform",
			Name:         "vpc",
			Version:      "v1.2.3",
			LocalName:    "network",
			Multiplicity: Multiplicity{Kind: "none"},
		}},
	}
	documents := AuthoritativeDocuments{Modules: map[string]hclschema.Contract{
		"module-1": {
			SchemaVersion: hclschema.ContractSchemaVersion,
			Module:        hclschema.ContractModule{Namespace: "platform", Name: "vpc", Provider: "aws", Version: "1.2.3"},
			Variables:     []hclschema.ContractVariable{{Name: "region", Required: true}},
			Compatibility: hclschema.Compatibility{Grade: hclschema.GradeFull},
		},
	}}

	_, diagnostics := BuildModel(request, "opendepot.example.com", documents)
	if !hasDiagnosticCode(diagnostics, "missing_required_input") {
		t.Fatalf("BuildModel() diagnostics = %#v, want missing_required_input", diagnostics)
	}
}

func TestBuildModelRejectsMismatchedProviderBinding(t *testing.T) {
	request := Request{
		SchemaVersion: SchemaVersion,
		Modules: []ModuleNode{{
			NodeID:       "module-1",
			Namespace:    "platform",
			Name:         "vpc",
			Version:      "1.2.3",
			LocalName:    "network",
			Multiplicity: Multiplicity{Kind: "none"},
			ProviderBindings: map[string]ProviderBinding{
				"aws": {ProviderNodeID: "provider-1"},
			},
		}},
		Providers: []ProviderNode{{
			NodeID:            "provider-1",
			Namespace:         "platform",
			Name:              "google",
			ProviderNamespace: "hashicorp",
			ProviderName:      "google",
			Version:           "6.0.0",
			LocalName:         "google",
		}},
	}
	documents := AuthoritativeDocuments{
		Modules: map[string]hclschema.Contract{
			"module-1": {
				SchemaVersion:     hclschema.ContractSchemaVersion,
				Module:            hclschema.ContractModule{Namespace: "platform", Name: "vpc", Provider: "aws", Version: "1.2.3"},
				RequiredProviders: []hclschema.ContractRequiredProvider{{LocalName: "aws", Source: "hashicorp/aws"}},
				Compatibility:     hclschema.Compatibility{Grade: hclschema.GradeFull},
			},
		},
		Providers: map[string]AuthoritativeProvider{
			"provider-1": {
				Namespace:         "platform",
				Name:              "google",
				ProviderNamespace: "hashicorp",
				ProviderName:      "google",
				Version:           "6.0.0",
			},
		},
	}

	_, diagnostics := BuildModel(request, "opendepot.example.com", documents)
	if !hasDiagnosticCode(diagnostics, "provider_binding_mismatch") {
		t.Fatalf("BuildModel() diagnostics = %#v, want provider_binding_mismatch", diagnostics)
	}
}

func hasDiagnosticCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if strings.EqualFold(diagnostic.Code, code) {
			return true
		}
	}

	return false
}
