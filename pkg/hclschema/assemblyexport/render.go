package assemblyexport

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type RenderModel struct {
	RegistryHost string
	Variables    []RenderVariable
	Modules      []RenderModule
	Providers    []RenderProvider
}

type RenderVariable struct {
	Name              string
	TypeExpression    string
	Description       string
	Validations       []VariableValidation
	DefaultExpression *string
}

type RenderModule struct {
	LocalName         string
	Namespace         string
	Name              string
	System            string
	Version           string
	Inputs            map[string]string
	CountExpression   string
	ForEachExpression string
	ProviderBindings  map[string]ProviderReference
}

type ProviderReference struct {
	LocalName string
	Alias     string
}

type RenderProvider struct {
	Namespace         string
	Name              string
	ProviderNamespace string
	ProviderName      string
	Version           string
	LocalName         string
	Alias             string
	Configuration     RenderBlock
}

type RenderBlock struct {
	Arguments map[string]string
	Blocks    map[string][]RenderBlock
}

type Files struct {
	Main      []byte
	Variables []byte
}

func Render(model RenderModel) (Files, error) {
	if strings.TrimSpace(model.RegistryHost) == "" {
		return Files{}, fmt.Errorf("registry host is required")
	}

	variables, err := renderVariables(model.Variables)
	if err != nil {
		return Files{}, err
	}

	main, err := renderMain(model)
	if err != nil {
		return Files{}, err
	}

	return Files{Main: main, Variables: variables}, nil
}

func renderVariables(variables []RenderVariable) ([]byte, error) {
	file := hclwrite.NewEmptyFile()
	body := file.Body()
	sorted := append([]RenderVariable(nil), variables...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	for _, variable := range sorted {
		block := body.AppendNewBlock("variable", []string{variable.Name})
		blockBody := block.Body()
		complexType := isComplexVariableType(variable.TypeExpression)

		if complexType {
			if err := setVariableDefault(blockBody, variable); err != nil {
				return nil, err
			}
			setVariableDescription(blockBody, variable)
			if err := setVariableType(blockBody, variable); err != nil {
				return nil, err
			}
		} else {
			if err := setVariableType(blockBody, variable); err != nil {
				return nil, err
			}
			if err := setVariableDefault(blockBody, variable); err != nil {
				return nil, err
			}
			setVariableDescription(blockBody, variable)
		}

		for index, validation := range variable.Validations {
			validationBlock := blockBody.AppendNewBlock("validation", nil)
			validationBody := validationBlock.Body()

			tokens, err := expressionTokens(validation.Condition, "condition")
			if err != nil {
				return nil, fmt.Errorf("variable %s validation %d condition: %w", variable.Name, index, err)
			}

			validationBody.SetAttributeRaw("condition", tokens)
			validationBody.SetAttributeValue("error_message", cty.StringVal(validation.ErrorMessage))
		}

		body.AppendNewline()
	}

	return file.Bytes(), nil
}

func setVariableType(body *hclwrite.Body, variable RenderVariable) error {
	tokens, err := expressionTokens(variable.TypeExpression, "type")
	if err != nil {
		return fmt.Errorf("variable %s type: %w", variable.Name, err)
	}
	body.SetAttributeRaw("type", tokens)

	return nil
}

func setVariableDefault(body *hclwrite.Body, variable RenderVariable) error {
	if variable.DefaultExpression == nil {
		return nil
	}

	tokens, err := expressionTokens(*variable.DefaultExpression, "default")
	if err != nil {
		return fmt.Errorf("variable %s default: %w", variable.Name, err)
	}
	body.SetAttributeRaw("default", tokens)

	return nil
}

func setVariableDescription(body *hclwrite.Body, variable RenderVariable) {
	if variable.Description != "" {
		body.SetAttributeValue("description", cty.StringVal(variable.Description))
	}
}

func isComplexVariableType(typeExpression string) bool {
	expression, diagnostics := hclsyntax.ParseExpression([]byte(typeExpression), "type.hcl", hcl.InitialPos)
	if diagnostics.HasErrors() {
		return false
	}

	call, ok := expression.(*hclsyntax.FunctionCallExpr)
	if !ok {
		return false
	}
	if call.Name == "object" {
		return true
	}
	if call.Name != "map" || len(call.Args) != 1 {
		return false
	}

	_, ok = call.Args[0].(*hclsyntax.FunctionCallExpr)
	return ok && call.Args[0].(*hclsyntax.FunctionCallExpr).Name == "object"
}

func renderMain(model RenderModel) ([]byte, error) {
	file := hclwrite.NewEmptyFile()
	body := file.Body()

	providers := append([]RenderProvider(nil), model.Providers...)
	sort.Slice(providers, func(i, j int) bool {
		if providers[i].LocalName == providers[j].LocalName {
			return providers[i].Alias < providers[j].Alias
		}

		return providers[i].LocalName < providers[j].LocalName
	})

	if len(providers) > 0 {
		terraform := body.AppendNewBlock("terraform", nil)
		required := terraform.Body().AppendNewBlock("required_providers", nil)
		seen := map[string]bool{}

		for _, provider := range providers {
			if seen[provider.LocalName] {
				continue
			}
			seen[provider.LocalName] = true
			required.Body().SetAttributeRaw(provider.LocalName,
				hclwrite.TokensForObject([]hclwrite.ObjectAttrTokens{
					{Name: hclwrite.TokensForIdentifier("source"), Value: hclwrite.TokensForValue(cty.StringVal(providerSource(provider)))},
					{Name: hclwrite.TokensForIdentifier("version"), Value: hclwrite.TokensForValue(cty.StringVal(normalizeVersion(provider.Version)))},
				}),
			)
		}

		body.AppendNewline()
	}

	for _, provider := range providers {
		block := body.AppendNewBlock("provider", []string{provider.LocalName})
		if provider.Alias != "" {
			block.Body().SetAttributeValue("alias", cty.StringVal(provider.Alias))
		}

		if err := appendRenderBlock(block.Body(), provider.Configuration); err != nil {
			return nil, fmt.Errorf("provider %s configuration: %w", provider.LocalName, err)
		}
		body.AppendNewline()
	}

	modules := append([]RenderModule(nil), model.Modules...)
	sort.Slice(modules, func(i, j int) bool { return modules[i].LocalName < modules[j].LocalName })

	for moduleIndex, module := range modules {
		block := body.AppendNewBlock("module", []string{module.LocalName})
		blockBody := block.Body()

		if module.CountExpression != "" {
			tokens, err := expressionTokens(module.CountExpression, "count")
			if err != nil {
				return nil, fmt.Errorf("module %s count: %w", module.LocalName, err)
			}
			blockBody.SetAttributeRaw("count", tokens)
		}

		if module.ForEachExpression != "" {
			tokens, err := expressionTokens(module.ForEachExpression, "for_each")
			if err != nil {
				return nil, fmt.Errorf("module %s for_each: %w", module.LocalName, err)
			}
			blockBody.SetAttributeRaw("for_each", tokens)
		}

		blockBody.SetAttributeValue("source", cty.StringVal(moduleSource(model.RegistryHost, module)))
		blockBody.SetAttributeValue("version", cty.StringVal("~> "+normalizeVersion(module.Version)))

		if len(module.Inputs) > 0 || len(module.ProviderBindings) > 0 {
			blockBody.AppendNewline()
		}

		inputNames := sortedKeys(module.Inputs)
		for _, name := range inputNames {
			tokens, err := expressionTokens(module.Inputs[name], name)
			if err != nil {
				return nil, fmt.Errorf("module %s input %s: %w", module.LocalName, name, err)
			}
			blockBody.SetAttributeRaw(name, tokens)
		}

		if len(module.ProviderBindings) > 0 {
			bindings := make([]hclwrite.ObjectAttrTokens, 0, len(module.ProviderBindings))
			for _, childName := range sortedKeys(module.ProviderBindings) {
				reference := module.ProviderBindings[childName]
				traversal := hcl.Traversal{hcl.TraverseRoot{Name: reference.LocalName}}
				if reference.Alias != "" {
					traversal = append(traversal, hcl.TraverseAttr{Name: reference.Alias})
				}
				bindings = append(bindings, hclwrite.ObjectAttrTokens{
					Name:  hclwrite.TokensForIdentifier(childName),
					Value: hclwrite.TokensForTraversal(traversal),
				})
			}
			blockBody.SetAttributeRaw("providers", hclwrite.TokensForObject(bindings))
		}

		if moduleIndex < len(modules)-1 {
			body.AppendNewline()
		}
	}

	return file.Bytes(), nil
}

func appendRenderBlock(body *hclwrite.Body, block RenderBlock) error {
	for _, name := range sortedKeys(block.Arguments) {
		tokens, err := expressionTokens(block.Arguments[name], name)
		if err != nil {
			return fmt.Errorf("argument %s: %w", name, err)
		}
		body.SetAttributeRaw(name, tokens)
	}

	for _, name := range sortedKeys(block.Blocks) {
		for _, child := range block.Blocks[name] {
			childBlock := body.AppendNewBlock(name, nil)
			if err := appendRenderBlock(childBlock.Body(), child); err != nil {
				return err
			}
		}
	}

	return nil
}

func expressionTokens(expression, name string) (hclwrite.Tokens, error) {
	source := []byte(name + " = " + expression + "\n")
	file, diagnostics := hclwrite.ParseConfig(source, "expression.tf", hcl.InitialPos)
	if diagnostics.HasErrors() {
		return nil, fmt.Errorf("invalid HCL expression: %s", diagnostics.Error())
	}

	attribute := file.Body().GetAttribute(name)
	if attribute == nil {
		return nil, fmt.Errorf("invalid HCL expression")
	}

	return attribute.Expr().BuildTokens(nil), nil
}

func moduleSource(host string, module RenderModule) string {
	return fmt.Sprintf("%s/%s/%s/%s", host, module.Namespace, module.Name, module.System)
}

func providerSource(provider RenderProvider) string {
	return fmt.Sprintf("%s/%s", provider.ProviderNamespace, provider.ProviderName)
}

func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}
