package assemblyexport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"

	"github.com/tonedefdev/opendepot/pkg/hclschema"
	"github.com/tonedefdev/opendepot/pkg/hclschema/providerschema"
)

type AuthoritativeDocuments struct {
	Modules   map[string]hclschema.Contract
	Providers map[string]AuthoritativeProvider
}

type AuthoritativeProvider struct {
	Namespace         string
	Name              string
	ProviderNamespace string
	ProviderName      string
	Version           string
	Configuration     providerschema.Block
}

func BuildModel(request Request, registryHost string, documents AuthoritativeDocuments) (RenderModel, []Diagnostic) {
	model := RenderModel{RegistryHost: registryHost}
	var diagnostics []Diagnostic

	if request.SchemaVersion != SchemaVersion {
		diagnostics = append(diagnostics, Diagnostic{Code: "unsupported_schema", Message: "unsupported Assembly export schema", Path: "schemaVersion"})

		return model, diagnostics
	}

	variableNodes := make(map[string]VariableNode, len(request.Variables))
	moduleNodes := make(map[string]ModuleNode, len(request.Modules))
	providerNodes := make(map[string]ProviderNode, len(request.Providers))
	localNames := map[string]string{}
	providerRequirements := map[string]string{}

	for index, variable := range request.Variables {
		path := fmt.Sprintf("variables[%d]", index)
		if !validIdentifier(variable.Name) {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_variable_name", Message: "variable name must be a valid HCL identifier", Path: path + ".name", NodeID: variable.NodeID})
		}
		if previous, exists := localNames["variable:"+variable.Name]; exists {
			diagnostics = append(diagnostics, Diagnostic{Code: "duplicate_variable_name", Message: fmt.Sprintf("variable name is already used by node %s", previous), Path: path + ".name", NodeID: variable.NodeID})
		}
		localNames["variable:"+variable.Name] = variable.NodeID
		variableNodes[variable.NodeID] = variable

		typeExpression, err := renderTypeSpec(variable.Type)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_variable_type", Message: err.Error(), Path: path + ".type", NodeID: variable.NodeID})

			continue
		}

		renderVariable := RenderVariable{Name: variable.Name, TypeExpression: typeExpression, Description: variable.Description}
		for validationIndex, validation := range variable.Validations {
			validationPath := fmt.Sprintf("%s.validations[%d]", path, validationIndex)
			condition := strings.TrimSpace(validation.Condition)
			if condition == "" {
				diagnostics = append(diagnostics, Diagnostic{Code: "invalid_variable_validation", Message: "validation condition is required", Path: validationPath + ".condition", NodeID: variable.NodeID})

				continue
			}

			if _, err := expressionTokens(condition, "condition"); err != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "invalid_variable_validation", Message: err.Error(), Path: validationPath + ".condition", NodeID: variable.NodeID})

				continue
			}

			errorMessage := strings.TrimSpace(validation.ErrorMessage)
			if errorMessage == "" {
				diagnostics = append(diagnostics, Diagnostic{Code: "invalid_variable_validation", Message: "validation error message is required", Path: validationPath + ".errorMessage", NodeID: variable.NodeID})

				continue
			}

			renderVariable.Validations = append(renderVariable.Validations, VariableValidation{
				Condition:    condition,
				ErrorMessage: errorMessage,
			})
		}

		if variable.HasDefault {
			defaultExpression, err := renderValueSpec(variable.Type, variable.Default)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "invalid_variable_default", Message: err.Error(), Path: path + ".default", NodeID: variable.NodeID})
			} else {
				renderVariable.DefaultExpression = &defaultExpression
			}
		}
		model.Variables = append(model.Variables, renderVariable)
	}

	for index, module := range request.Modules {
		path := fmt.Sprintf("modules[%d]", index)
		if !validIdentifier(module.LocalName) {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_module_name", Message: "module local name must be a valid HCL identifier", Path: path + ".localName", NodeID: module.NodeID})
		}
		if previous, exists := localNames["module:"+module.LocalName]; exists {
			diagnostics = append(diagnostics, Diagnostic{Code: "duplicate_module_name", Message: fmt.Sprintf("module local name is already used by node %s", previous), Path: path + ".localName", NodeID: module.NodeID})
		}
		localNames["module:"+module.LocalName] = module.NodeID
		moduleNodes[module.NodeID] = module
	}

	for index, provider := range request.Providers {
		path := fmt.Sprintf("providers[%d]", index)
		if !validIdentifier(provider.LocalName) {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_provider_name", Message: "provider local name must be a valid HCL identifier", Path: path + ".localName", NodeID: provider.NodeID})
		}
		if provider.Alias != "" && !validIdentifier(provider.Alias) {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_provider_alias", Message: "provider alias must be a valid HCL identifier", Path: path + ".alias", NodeID: provider.NodeID})
		}
		key := provider.LocalName + ":" + provider.Alias
		if previous, exists := localNames["provider:"+key]; exists {
			diagnostics = append(diagnostics, Diagnostic{Code: "duplicate_provider_configuration", Message: fmt.Sprintf("provider configuration is already used by node %s", previous), Path: path + ".alias", NodeID: provider.NodeID})
		}
		localNames["provider:"+key] = provider.NodeID
		providerNodes[provider.NodeID] = provider
	}

	resolveField := func(value FieldValue, nodeID, path string) (string, *Diagnostic) {
		return resolveFieldValue(value, variableNodes, moduleNodes, nodeID, path)
	}

	for index, provider := range request.Providers {
		path := fmt.Sprintf("providers[%d]", index)
		document, ok := documents.Providers[provider.NodeID]
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{Code: "provider_unavailable", Message: "provider and exact version could not be resolved", Path: path, NodeID: provider.NodeID})

			continue
		}
		if document.Namespace != provider.Namespace || document.Name != provider.Name || document.Version != normalizeVersion(provider.Version) {
			diagnostics = append(diagnostics, Diagnostic{Code: "provider_identity_mismatch", Message: "provider identity does not match the authorized server document", Path: path, NodeID: provider.NodeID})

			continue
		}
		requirement := document.Namespace + "/" + document.Name + "@" + document.Version
		if existing, exists := providerRequirements[provider.LocalName]; exists && existing != requirement {
			diagnostics = append(diagnostics, Diagnostic{Code: "conflicting_provider_requirement", Message: "provider configurations sharing a local name must use the same OpenDepot provider and exact version", Path: path + ".localName", NodeID: provider.NodeID})

			continue
		}
		providerRequirements[provider.LocalName] = requirement

		configuration, providerDiagnostics := buildProviderConfiguration(provider.Configuration, document.Configuration, provider.NodeID, path+".configuration", resolveField)
		diagnostics = append(diagnostics, providerDiagnostics...)
		model.Providers = append(model.Providers, RenderProvider{
			Namespace:         document.Namespace,
			Name:              document.Name,
			ProviderNamespace: document.ProviderNamespace,
			ProviderName:      document.ProviderName,
			Version:           document.Version,
			LocalName:         provider.LocalName,
			Alias:             provider.Alias,
			Configuration:     configuration,
		})
	}

	for index, module := range request.Modules {
		path := fmt.Sprintf("modules[%d]", index)
		contract, ok := documents.Modules[module.NodeID]
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{Code: "module_unavailable", Message: "module and exact version could not be resolved", Path: path, NodeID: module.NodeID})

			continue
		}
		if contract.SchemaVersion != hclschema.ContractSchemaVersion || contract.Compatibility.Grade == hclschema.GradeUnsupported {
			diagnostics = append(diagnostics, Diagnostic{Code: "module_unsupported", Message: "module contract is unavailable or unsupported", Path: path, NodeID: module.NodeID})

			continue
		}
		if contract.Module.Namespace != module.Namespace || contract.Module.Name != module.Name || normalizeVersion(contract.Module.Version) != normalizeVersion(module.Version) {
			diagnostics = append(diagnostics, Diagnostic{Code: "module_identity_mismatch", Message: "module identity does not match the authorized server contract", Path: path, NodeID: module.NodeID})

			continue
		}

		renderModule := RenderModule{
			LocalName:        module.LocalName,
			Namespace:        contract.Module.Namespace,
			Name:             contract.Module.Name,
			System:           contract.Module.Provider,
			Version:          normalizeVersion(contract.Module.Version),
			Inputs:           map[string]string{},
			ProviderBindings: map[string]ProviderReference{},
		}
		variables := make(map[string]hclschema.ContractVariable, len(contract.Variables))
		for _, variable := range contract.Variables {
			variables[variable.Name] = variable
			value, provided := module.Values[variable.Name]
			provided = provided && inputValueProvided(value)
			if variable.Required && !provided {
				diagnostics = append(diagnostics, Diagnostic{Code: "missing_required_input", Message: fmt.Sprintf("required module input %s is missing", variable.Name), Path: path + ".values." + variable.Name, NodeID: module.NodeID})
			}
		}

		for _, inputName := range sortedKeys(module.Values) {
			variable, exists := variables[inputName]
			if !exists {
				diagnostics = append(diagnostics, Diagnostic{Code: "unknown_module_input", Message: fmt.Sprintf("module input %s is not declared by the authoritative contract", inputName), Path: path + ".values." + inputName, NodeID: module.NodeID})

				continue
			}
			expression, diagnostic := resolveModuleInput(module.Values[inputName], variable, module.NodeID, path+".values."+inputName, resolveField)
			if diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)

				continue
			}
			renderModule.Inputs[inputName] = expression
		}

		switch module.Multiplicity.Kind {
		case "", "none":
		case "count":
			if module.Multiplicity.Expr == nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "invalid_count", Message: "count expression is required", Path: path + ".multiplicity.expr", NodeID: module.NodeID})

				break
			}
			expression, diagnostic := resolveField(*module.Multiplicity.Expr, module.NodeID, path+".multiplicity.expr")
			if diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)

				break
			}
			if module.Multiplicity.Mode == "conditional" {
				expression = expression + " ? 1 : 0"
			} else if module.Multiplicity.Mode != "fixed" {
				diagnostics = append(diagnostics, Diagnostic{Code: "invalid_count", Message: "count mode must be fixed or conditional", Path: path + ".multiplicity.mode", NodeID: module.NodeID})
			}
			renderModule.CountExpression = expression
		case "for_each":
			if module.Multiplicity.VariableNodeID == nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "invalid_for_each", Message: "for_each must reference a canvas variable", Path: path + ".multiplicity.variableNodeId", NodeID: module.NodeID})

				break
			}
			variable, exists := variableNodes[*module.Multiplicity.VariableNodeID]
			if !exists {
				diagnostics = append(diagnostics, Diagnostic{Code: "invalid_for_each", Message: "for_each must reference a canvas variable", Path: path + ".multiplicity.variableNodeId", NodeID: module.NodeID})

				break
			}
			renderModule.ForEachExpression = "var." + variable.Name
		default:
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_multiplicity", Message: "multiplicity kind is invalid", Path: path + ".multiplicity.kind", NodeID: module.NodeID})
		}

		requiredProviders := make(map[string]hclschema.ContractRequiredProvider, len(contract.RequiredProviders))
		for _, required := range contract.RequiredProviders {
			requiredProviders[required.LocalName] = required
		}
		for childName, binding := range module.ProviderBindings {
			required, exists := requiredProviders[childName]
			if !exists {
				diagnostics = append(diagnostics, Diagnostic{Code: "unknown_provider_binding", Message: "provider binding is not declared by the module contract", Path: path + ".providerBindings." + childName, NodeID: module.NodeID})

				continue
			}
			provider, exists := providerNodes[binding.ProviderNodeID]
			document, documentExists := documents.Providers[binding.ProviderNodeID]
			if !exists || !documentExists {
				diagnostics = append(diagnostics, Diagnostic{Code: "dangling_provider_binding", Message: "provider binding references an unavailable provider node", Path: path + ".providerBindings." + childName, NodeID: module.NodeID})

				continue
			}
			if normalizeProviderIdentity(required.Source) != document.ProviderNamespace+"/"+document.ProviderName {
				diagnostics = append(diagnostics, Diagnostic{Code: "provider_binding_mismatch", Message: "provider binding does not match the module's required provider identity", Path: path + ".providerBindings." + childName, NodeID: module.NodeID})

				continue
			}
			renderModule.ProviderBindings[childName] = ProviderReference{LocalName: provider.LocalName, Alias: provider.Alias}
		}
		model.Modules = append(model.Modules, renderModule)
	}

	return model, diagnostics
}

func resolveModuleInput(value InputValue, variable hclschema.ContractVariable, nodeID, path string, resolve func(FieldValue, string, string) (string, *Diagnostic)) (string, *Diagnostic) {
	return resolveModuleInputType(value, variable.Type, nodeID, path, 0, resolve)
}

func inputValueProvided(value InputValue) bool {
	if value.Kind == "scalar" {
		return value.Value != nil && (value.Value.Mode != "literal" || strings.TrimSpace(value.Value.Literal) != "")
	}
	if value.Kind == "list" {
		return len(value.Items) > 0
	}

	return len(value.Entries) > 0
}

func resolveModuleInputType(value InputValue, typeData json.RawMessage, nodeID, path string, depth int, resolve func(FieldValue, string, string) (string, *Diagnostic)) (string, *Diagnostic) {
	if value.Kind != "scalar" {
		return renderInputValue(value, typeData, nodeID, path, depth, resolve)
	}
	if value.Value == nil {
		return "", &Diagnostic{Code: "malformed_expression", Message: "scalar input value is missing", Path: path, NodeID: nodeID}
	}

	var kind string
	if err := json.Unmarshal(typeData, &kind); err == nil {
		return resolveModuleScalar(*value.Value, hclschema.ContractVariable{Type: typeData}, nodeID, path, resolve)
	}

	return resolveModuleScalar(*value.Value, hclschema.ContractVariable{Type: json.RawMessage(`"dynamic"`)}, nodeID, path, resolve)
}

func resolveModuleScalar(value FieldValue, variable hclschema.ContractVariable, nodeID, path string, resolve func(FieldValue, string, string) (string, *Diagnostic)) (string, *Diagnostic) {
	if value.Mode != "literal" || isIterationExpression(value.Literal) {
		return resolve(value, nodeID, path)
	}
	if strings.TrimSpace(value.Literal) == "" {
		return "", &Diagnostic{Code: "malformed_expression", Message: "literal expression is empty", Path: path, NodeID: nodeID}
	}
	if contractVariableType(variable) != "string" && !strings.Contains(value.Literal, "${") {
		return resolve(value, nodeID, path)
	}
	if contractVariableType(variable) == "string" && isKnownFunctionExpression(value.Literal) {
		return value.Literal, nil
	}

	if strings.Contains(value.Literal, "${") {
		tokens, err := expressionTokens(`"`+value.Literal+`"`, "value")
		if err != nil {
			return "", &Diagnostic{Code: "malformed_expression", Message: err.Error(), Path: path, NodeID: nodeID}
		}

		return strings.TrimSpace(string(hclwrite.Format(tokens.Bytes()))), nil
	}

	tokens := hclwrite.TokensForValue(cty.StringVal(value.Literal))

	return strings.TrimSpace(string(hclwrite.Format(tokens.Bytes()))), nil
}

func isKnownFunctionExpression(value string) bool {
	expression := strings.TrimSpace(value)
	parsed, diagnostics := hclsyntax.ParseExpression([]byte(expression), "function.hcl", hcl.InitialPos)
	if diagnostics.HasErrors() {
		return false
	}

	call, ok := parsed.(*hclsyntax.FunctionCallExpr)
	return ok && hclschema.IsKnownFunction(call.Name)
}

func renderInputValue(value InputValue, typeData json.RawMessage, nodeID, path string, depth int, resolve func(FieldValue, string, string) (string, *Diagnostic)) (string, *Diagnostic) {
	if value.Kind == "list" {
		parts := make([]string, 0, len(value.Items))
		for index, item := range value.Items {
			part, diagnostic := resolveModuleInputType(item, nestedInputType(typeData, "element", ""), nodeID, fmt.Sprintf("%s.items[%d]", path, index), depth+1, resolve)
			if diagnostic != nil {
				return "", diagnostic
			}
			parts = append(parts, part)
		}

		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	if value.Kind == "map" || value.Kind == "object" {
		parts := make([]string, 0, len(value.Entries))
		indent := strings.Repeat("  ", depth+1)
		for index, entry := range value.Entries {
			name := entry.Key
			if value.Kind == "object" {
				name = entry.Name
			}
			keyTokens, err := renderStructuredKeyTokens(value.Kind, name)
			if err != nil {
				return "", &Diagnostic{Code: "malformed_expression", Message: err.Error(), Path: fmt.Sprintf("%s.entries[%d].key", path, index), NodeID: nodeID}
			}
			item, diagnostic := resolveModuleInputType(entry.Value, nestedInputType(typeData, value.Kind, name), nodeID, fmt.Sprintf("%s.entries[%d].value", path, index), depth+1, resolve)
			if diagnostic != nil {
				return "", diagnostic
			}
			parts = append(parts, indent+strings.TrimSpace(string(hclwrite.Format(keyTokens.Bytes())))+" = "+item)
		}

		return "{\n" + strings.Join(parts, "\n") + "\n" + strings.Repeat("  ", depth) + "}", nil
	}

	return "", &Diagnostic{Code: "malformed_expression", Message: "unsupported structured module input value", Path: path, NodeID: nodeID}
}

func renderStructuredKeyTokens(kind, key string) (hclwrite.Tokens, error) {
	if kind == "object" && validIdentifier(key) {
		return hclwrite.TokensForIdentifier(key), nil
	}

	return renderMapKeyTokens(key)
}

func renderMapKeyTokens(key string) (hclwrite.Tokens, error) {
	key = strings.TrimSpace(key)
	if isExpressionKey(key) {
		return expressionTokens("("+key+")", "key")
	}

	return hclwrite.TokensForValue(cty.StringVal(key)), nil
}

func isExpressionKey(key string) bool {
	for _, prefix := range []string{"count.", "data.", "each.", "module.", "var."} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}

func nestedInputType(typeData json.RawMessage, kind, name string) json.RawMessage {
	var shape map[string]json.RawMessage
	if json.Unmarshal(typeData, &shape) != nil {
		return json.RawMessage(`"dynamic"`)
	}
	if kind == "object" {
		var attributes []map[string]json.RawMessage
		if json.Unmarshal(shape["object"], &attributes) == nil {
			for _, attribute := range attributes {
				if attributeType, ok := attribute[name]; ok {
					return attributeType
				}
			}
		}
		return json.RawMessage(`"dynamic"`)
	}
	var elements []json.RawMessage
	if json.Unmarshal(shape[kind], &elements) == nil && len(elements) > 0 {
		return elements[0]
	}
	return json.RawMessage(`"dynamic"`)
}

func contractVariableType(variable hclschema.ContractVariable) string {
	var kind string
	if err := json.Unmarshal(variable.Type, &kind); err != nil {
		return ""
	}

	return kind
}

func isIterationExpression(value string) bool {
	value = strings.TrimSpace(value)

	return value == "count.index" || value == "each.key" || value == "each.value" || strings.HasPrefix(value, "each.key.") || strings.HasPrefix(value, "each.value.")
}

func buildProviderConfiguration(configuration ProviderConfiguration, schema providerschema.Block, nodeID, path string, resolve func(FieldValue, string, string) (string, *Diagnostic)) (RenderBlock, []Diagnostic) {
	rendered := RenderBlock{Arguments: map[string]string{}, Blocks: map[string][]RenderBlock{}}
	var diagnostics []Diagnostic

	for name, attribute := range schema.Attributes {
		_, provided := configuration.Arguments[name]
		if attribute.Required && !provided {
			diagnostics = append(diagnostics, Diagnostic{Code: "missing_required_provider_argument", Message: fmt.Sprintf("required provider argument %s is missing", name), Path: path + ".arguments." + name, NodeID: nodeID})
		}
	}

	for _, name := range sortedKeys(configuration.Arguments) {
		attribute, exists := schema.Attributes[name]
		if !exists {
			diagnostics = append(diagnostics, Diagnostic{Code: "unknown_provider_argument", Message: fmt.Sprintf("provider argument %s is not declared by the authoritative schema", name), Path: path + ".arguments." + name, NodeID: nodeID})

			continue
		}
		expression, diagnostic := resolveProviderArgument(configuration.Arguments[name], attribute, nodeID, path+".arguments."+name, resolve)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)

			continue
		}
		rendered.Arguments[name] = expression
	}

	for _, name := range sortedKeys(configuration.Blocks) {
		nested, exists := schema.Blocks[name]
		if !exists {
			diagnostics = append(diagnostics, Diagnostic{Code: "unknown_provider_block", Message: fmt.Sprintf("provider block %s is not declared by the authoritative schema", name), Path: path + ".blocks." + name, NodeID: nodeID})

			continue
		}
		for index, child := range configuration.Blocks[name] {
			childBlock, childDiagnostics := buildProviderConfiguration(child, nested.Block, nodeID, fmt.Sprintf("%s.blocks.%s[%d]", path, name, index), resolve)
			diagnostics = append(diagnostics, childDiagnostics...)
			rendered.Blocks[name] = append(rendered.Blocks[name], childBlock)
		}
	}

	for name, nested := range schema.Blocks {
		count := len(configuration.Blocks[name])
		if count < nested.MinItems {
			diagnostics = append(diagnostics, Diagnostic{Code: "missing_required_provider_block", Message: fmt.Sprintf("provider block %s requires at least %d instance(s)", name, nested.MinItems), Path: path + ".blocks." + name, NodeID: nodeID})
		}
		if nested.MaxItems > 0 && count > nested.MaxItems {
			diagnostics = append(diagnostics, Diagnostic{Code: "too_many_provider_blocks", Message: fmt.Sprintf("provider block %s permits at most %d instance(s)", name, nested.MaxItems), Path: path + ".blocks." + name, NodeID: nodeID})
		}
	}

	return rendered, diagnostics
}

func resolveProviderArgument(value FieldValue, attribute providerschema.Attribute, nodeID, path string, resolve func(FieldValue, string, string) (string, *Diagnostic)) (string, *Diagnostic) {
	var kind string
	if value.Mode != "literal" || json.Unmarshal(attribute.Type, &kind) != nil || kind != "string" {
		return resolve(value, nodeID, path)
	}
	if strings.TrimSpace(value.Literal) == "" {
		return "", &Diagnostic{Code: "malformed_expression", Message: "literal expression is empty", Path: path, NodeID: nodeID}
	}

	tokens := hclwrite.TokensForValue(cty.StringVal(value.Literal))

	return strings.TrimSpace(string(hclwrite.Format(tokens.Bytes()))), nil
}

func resolveFieldValue(value FieldValue, variables map[string]VariableNode, modules map[string]ModuleNode, nodeID, path string) (string, *Diagnostic) {
	if value.Mode == "literal" {
		if strings.TrimSpace(value.Literal) == "" {
			return "", &Diagnostic{Code: "malformed_expression", Message: "literal expression is empty", Path: path, NodeID: nodeID}
		}

		if _, err := expressionTokens(value.Literal, "value"); err != nil {
			return "", &Diagnostic{Code: "malformed_expression", Message: err.Error(), Path: path, NodeID: nodeID}
		}

		return value.Literal, nil
	}

	if value.Mode != "reference" {
		return "", &Diagnostic{Code: "invalid_value_mode", Message: "field value mode must be literal or reference", Path: path + ".mode", NodeID: nodeID}
	}

	if variable, exists := variables[value.RefNodeID]; exists {
		expression := "var." + variable.Name
		if value.RefOutput != "" {
			expression += "." + value.RefOutput
		}

		return applyOutputSelector(expression, value.RefOutputSelector, variables, modules, nodeID, path)
	}

	module, exists := modules[value.RefNodeID]
	if !exists || value.RefOutput == "" {
		return "", &Diagnostic{Code: "dangling_reference", Message: "reference target does not exist", Path: path, NodeID: nodeID}
	}

	base := "module." + module.LocalName
	var expression string
	switch module.Multiplicity.Kind {
	case "", "none":
		if value.RefSelector != nil {
			return "", &Diagnostic{Code: "invalid_reference_selector", Message: "non-repeated module references cannot use a selector", Path: path + ".refSelector", NodeID: nodeID}
		}

		expression = base + "." + value.RefOutput
	case "count":
		if value.RefSelector == nil {
			return "", &Diagnostic{Code: "missing_reference_selector", Message: "counted module reference requires a selector", Path: path + ".refSelector", NodeID: nodeID}
		}
		if value.RefSelector.Kind == "all" {
			expression = base + "[*]." + value.RefOutput

			break
		}
		if value.RefSelector.Kind != "index" || value.RefSelector.Expr == nil {
			return "", &Diagnostic{Code: "invalid_reference_selector", Message: "counted module selector must be all or index", Path: path + ".refSelector", NodeID: nodeID}
		}
		selector, diagnostic := resolveFieldValue(*value.RefSelector.Expr, variables, modules, nodeID, path+".refSelector.expr")
		if diagnostic != nil {
			return "", diagnostic
		}

		expression = base + "[" + selector + "]." + value.RefOutput
	case "for_each":
		if value.RefSelector == nil {
			return "", &Diagnostic{Code: "missing_reference_selector", Message: "for_each module reference requires a selector", Path: path + ".refSelector", NodeID: nodeID}
		}
		if value.RefSelector.Kind == "all" {
			expression = fmt.Sprintf("[for instance in %s : instance.%s]", base, value.RefOutput)

			break
		}
		if value.RefSelector.Kind != "key" || value.RefSelector.Expr == nil {
			return "", &Diagnostic{Code: "invalid_reference_selector", Message: "for_each module selector must be all or key", Path: path + ".refSelector", NodeID: nodeID}
		}
		selector, diagnostic := resolveFieldValue(*value.RefSelector.Expr, variables, modules, nodeID, path+".refSelector.expr")
		if diagnostic != nil {
			return "", diagnostic
		}

		expression = base + "[" + selector + "]." + value.RefOutput
	default:
		return "", &Diagnostic{Code: "invalid_reference_source", Message: "referenced module has invalid multiplicity", Path: path, NodeID: nodeID}
	}

	return applyOutputSelector(expression, value.RefOutputSelector, variables, modules, nodeID, path)
}

func applyOutputSelector(expression string, selector *ReferenceSelector, variables map[string]VariableNode, modules map[string]ModuleNode, nodeID, path string) (string, *Diagnostic) {
	if selector == nil || selector.Kind == "all" {
		return expression, nil
	}
	if (selector.Kind != "index" && selector.Kind != "key") || selector.Expr == nil {
		return "", &Diagnostic{Code: "invalid_output_selector", Message: "output selector must be all, index, or key", Path: path + ".refOutputSelector", NodeID: nodeID}
	}

	index, diagnostic := resolveFieldValue(*selector.Expr, variables, modules, nodeID, path+".refOutputSelector.expr")
	if diagnostic != nil {
		return "", diagnostic
	}

	return expression + "[" + index + "]", nil
}

func renderTypeSpec(spec TypeSpec) (string, error) {
	var tokens hclwrite.Tokens

	switch spec.Kind {
	case "string", "number", "bool", "any":
		tokens = hclwrite.TokensForIdentifier(spec.Kind)
	case "list", "set", "map":
		if spec.Element == nil {
			return "", fmt.Errorf("%s type requires an element type", spec.Kind)
		}
		element, err := renderTypeSpecTokens(*spec.Element)
		if err != nil {
			return "", err
		}
		tokens = hclwrite.TokensForFunctionCall(spec.Kind, element)
	case "tuple":
		elements := make([]hclwrite.Tokens, 0, len(spec.Elements))
		for _, element := range spec.Elements {
			elementTokens, err := renderTypeSpecTokens(element)
			if err != nil {
				return "", err
			}
			elements = append(elements, elementTokens)
		}
		tokens = hclwrite.TokensForFunctionCall("tuple", hclwrite.TokensForTuple(elements))
	case "object":
		attributes := append([]TypeAttribute(nil), spec.Attributes...)
		sort.Slice(attributes, func(i, j int) bool { return attributes[i].Name < attributes[j].Name })
		object := make([]hclwrite.ObjectAttrTokens, 0, len(attributes))
		for _, attribute := range attributes {
			if !validIdentifier(attribute.Name) {
				return "", fmt.Errorf("object attribute %q is not a valid identifier", attribute.Name)
			}
			attributeType, err := renderTypeSpecTokens(attribute.Type)
			if err != nil {
				return "", err
			}
			if attribute.Optional {
				arguments := []hclwrite.Tokens{attributeType}
				if attribute.HasDefault {
					if attribute.Default == nil {
						return "", fmt.Errorf("optional object attribute %q has no default value", attribute.Name)
					}
					defaultTokens, err := renderValueSpecTokens(attribute.Type, *attribute.Default)
					if err != nil {
						return "", err
					}
					arguments = append(arguments, defaultTokens)
				}
				attributeType = hclwrite.TokensForFunctionCall("optional", arguments...)
			}
			object = append(object, hclwrite.ObjectAttrTokens{Name: hclwrite.TokensForIdentifier(attribute.Name), Value: attributeType})
		}
		tokens = hclwrite.TokensForFunctionCall("object", hclwrite.TokensForObject(object))
	default:
		return "", fmt.Errorf("unsupported type kind %q", spec.Kind)
	}

	return strings.TrimSpace(string(hclwrite.Format(tokens.Bytes()))), nil
}

func renderTypeSpecTokens(spec TypeSpec) (hclwrite.Tokens, error) {
	expression, err := renderTypeSpec(spec)
	if err != nil {
		return nil, err
	}

	return expressionTokens(expression, "type")
}

func renderValueSpec(spec TypeSpec, value ValueSpec) (string, error) {
	tokens, err := renderValueSpecTokens(spec, value)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(hclwrite.Format(tokens.Bytes()))), nil
}

func renderValueSpecTokens(spec TypeSpec, value ValueSpec) (hclwrite.Tokens, error) {
	switch spec.Kind {
	case "string":
		if value.Kind != "scalar" {
			return nil, fmt.Errorf("string value must be scalar")
		}
		if strings.TrimSpace(value.Literal) == "" {
			return hclwrite.TokensForIdentifier("null"), nil
		}

		return hclwrite.TokensForValue(cty.StringVal(value.Literal)), nil
	case "number", "bool", "any":
		if value.Kind != "scalar" {
			return nil, fmt.Errorf("%s value must be scalar", spec.Kind)
		}

		return expressionTokens(value.Literal, "value")
	case "list", "set", "tuple":
		if value.Kind != "list" {
			return nil, fmt.Errorf("%s value must be a list", spec.Kind)
		}
		items := make([]hclwrite.Tokens, 0, len(value.Items))
		for index, item := range value.Items {
			var element TypeSpec
			if spec.Kind == "tuple" {
				if index >= len(spec.Elements) {
					return nil, fmt.Errorf("tuple has too many values")
				}
				element = spec.Elements[index]
			} else if spec.Element != nil {
				element = *spec.Element
			} else {
				return nil, fmt.Errorf("%s type requires an element type", spec.Kind)
			}
			tokens, err := renderValueSpecTokens(element, item)
			if err != nil {
				return nil, err
			}
			items = append(items, tokens)
		}
		if spec.Kind == "tuple" && len(items) != len(spec.Elements) {
			return nil, fmt.Errorf("tuple value count does not match its type")
		}

		return hclwrite.TokensForTuple(items), nil
	case "map":
		if value.Kind != "map" || spec.Element == nil {
			return nil, fmt.Errorf("map value does not match its type")
		}
		entries := append([]ValueEntry(nil), value.Entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
		object := make([]hclwrite.ObjectAttrTokens, 0, len(entries))
		seen := map[string]bool{}
		for _, entry := range entries {
			if seen[entry.Key] {
				return nil, fmt.Errorf("map value contains duplicate key %q", entry.Key)
			}
			seen[entry.Key] = true
			tokens, err := renderValueSpecTokens(*spec.Element, entry.Value)
			if err != nil {
				return nil, err
			}
			object = append(object, hclwrite.ObjectAttrTokens{Name: hclwrite.TokensForValue(cty.StringVal(entry.Key)), Value: tokens})
		}

		return hclwrite.TokensForObject(object), nil
	case "object":
		if value.Kind != "object" {
			return nil, fmt.Errorf("object value does not match its type")
		}
		attributeTypes := make(map[string]TypeSpec, len(spec.Attributes))
		optionalAttributes := make(map[string]bool, len(spec.Attributes))
		for _, attribute := range spec.Attributes {
			attributeTypes[attribute.Name] = attribute.Type
			optionalAttributes[attribute.Name] = attribute.Optional
		}
		entries := append([]ValueEntry(nil), value.Entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		object := make([]hclwrite.ObjectAttrTokens, 0, len(entries))
		seen := map[string]bool{}
		for _, entry := range entries {
			if seen[entry.Name] {
				return nil, fmt.Errorf("object value contains duplicate attribute %q", entry.Name)
			}
			seen[entry.Name] = true
			attributeType, exists := attributeTypes[entry.Name]
			if !exists {
				return nil, fmt.Errorf("object value contains unknown attribute %q", entry.Name)
			}
			tokens, err := renderValueSpecTokens(attributeType, entry.Value)
			if err != nil {
				return nil, err
			}
			object = append(object, hclwrite.ObjectAttrTokens{Name: hclwrite.TokensForIdentifier(entry.Name), Value: tokens})
		}
		for name := range attributeTypes {
			if !optionalAttributes[name] && !seen[name] {
				return nil, fmt.Errorf("object value is missing required attribute %q", name)
			}
		}

		return hclwrite.TokensForObject(object), nil
	default:
		return nil, fmt.Errorf("unsupported value type %q", spec.Kind)
	}
}

func validIdentifier(value string) bool {
	return value != "" && hclsyntax.ValidIdentifier(value)
}

func normalizeProviderIdentity(source string) string {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}

	return source
}
