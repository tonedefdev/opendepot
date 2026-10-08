package assemblyexport

import (
	"strings"
	"testing"
)

func TestRenderProducesDeterministicRootModule(t *testing.T) {
	defaultValue := `"us-east-1"`
	files, err := Render(RenderModel{
		RegistryHost: "opendepot.example.com:8443",
		Variables: []RenderVariable{{
			Name:              "region",
			TypeExpression:    "string",
			DefaultExpression: &defaultValue,
		}},
		Providers: []RenderProvider{{
			Namespace:         "platform",
			Name:              "aws",
			ProviderNamespace: "hashicorp",
			ProviderName:      "aws",
			Version:           "v6.0.0",
			LocalName:         "aws",
			Alias:             "primary",
			Configuration: RenderBlock{Arguments: map[string]string{
				"region": "var.region",
			}},
		}},
		Modules: []RenderModule{{
			LocalName: "network",
			Namespace: "platform",
			Name:      "vpc",
			System:    "aws",
			Version:   "v1.2.3",
			Inputs: map[string]string{
				"region": "var.region",
			},
			ProviderBindings: map[string]ProviderReference{
				"aws": {LocalName: "aws", Alias: "primary"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	main := string(files.Main)
	for _, expected := range []string{
		`required_providers {`,
		`source  = "hashicorp/aws"`,
		`version = "6.0.0"`,
		`provider "aws"`,
		`alias  = "primary"`,
		`source  = "opendepot.example.com:8443/platform/vpc/aws"`,
		`version = "~> 1.2.3"`,
		`aws = aws.primary`,
	} {
		if !strings.Contains(main, expected) {
			t.Errorf("main.tf does not contain %q:\n%s", expected, main)
		}
	}

	variables := string(files.Variables)
	if !strings.Contains(variables, `variable "region"`) || !strings.Contains(variables, `default = "us-east-1"`) {
		t.Fatalf("variables.tf did not contain the expected declaration:\n%s", variables)
	}
}

func TestRenderVariableDescriptionAndValidations(t *testing.T) {
	files, err := Render(RenderModel{
		RegistryHost: "opendepot.example.com",
		Variables: []RenderVariable{{
			Name:           "region",
			TypeExpression: "string",
			Description:    `AWS region for the "primary" deployment.`,
			Validations: []VariableValidation{
				{Condition: `contains(["us-east-1", "us-west-2"], var.region)`, ErrorMessage: `Region must be "supported".`},
				{Condition: `length(var.region) > 0`, ErrorMessage: "Region cannot be empty."},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	want := `variable "region" {
  type        = string
  description = "AWS region for the \"primary\" deployment."
  validation {
    condition     = contains(["us-east-1", "us-west-2"], var.region)
    error_message = "Region must be \"supported\"."
  }
  validation {
    condition     = length(var.region) > 0
    error_message = "Region cannot be empty."
  }
}

`
	if got := string(files.Variables); got != want {
		t.Fatalf("variables.tf =\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderOrdersComplexVariableFieldsDefaultDescriptionType(t *testing.T) {
	files, err := Render(RenderModel{RegistryHost: "opendepot.example.com", Variables: []RenderVariable{{
		Name:              "lambda_functions",
		TypeExpression:    "map(object({ spec = object({ description = string }) }))",
		Description:       "Lambda functions.",
		DefaultExpression: func() *string { value := "{}"; return &value }(),
	}}})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	want := `variable "lambda_functions" {
  default     = {}
  description = "Lambda functions."
  type        = map(object({ spec = object({ description = string }) }))
}

`
	if got := string(files.Variables); got != want {
		t.Fatalf("variables.tf =\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderRejectsMalformedExpression(t *testing.T) {
	_, err := Render(RenderModel{
		RegistryHost: "opendepot.example.com",
		Modules: []RenderModule{{
			LocalName: "broken",
			Namespace: "platform",
			Name:      "broken",
			System:    "aws",
			Version:   "1.0.0",
			Inputs:    map[string]string{"value": "["},
		}},
	})
	if err == nil {
		t.Fatal("Render() error = nil, want malformed expression error")
	}
}

func TestRenderOrdersModuleArguments(t *testing.T) {
	files, err := Render(RenderModel{
		RegistryHost: "opendepot.example.com",
		Modules: []RenderModule{
			{
				LocalName:       "kms",
				Namespace:       "platform",
				Name:            "kms",
				System:          "aws",
				Version:         "4.2.1",
				CountExpression: "var.create_kms_key ? 1 : 0",
			},
			{
				LocalName:         "s3_buckets",
				Namespace:         "platform",
				Name:              "s3",
				System:            "aws",
				Version:           "5.15.4",
				ForEachExpression: "var.s3_buckets",
				Inputs: map[string]string{
					"bucket": "each.value.spec.bucket_name",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	want := `module "kms" {
  count   = var.create_kms_key ? 1 : 0
  source  = "opendepot.example.com/platform/kms/aws"
  version = "~> 4.2.1"
}

module "s3_buckets" {
  for_each = var.s3_buckets
  source   = "opendepot.example.com/platform/s3/aws"
  version  = "~> 5.15.4"

  bucket = each.value.spec.bucket_name
}
`
	if got := string(files.Main); got != want {
		t.Fatalf("main.tf =\n%s\nwant:\n%s", got, want)
	}
}
