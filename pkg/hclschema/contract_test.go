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
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

// stubResolver serves a hand-written schema for the resource types used by the fixture.
type stubResolver struct{}

func (stubResolver) Lookup(resourceType, attrPath string, isData bool) (cty.Type, bool) {
	if resourceType == "aws_s3_bucket" && attrPath == "arn" {
		return cty.String, true
	}

	return cty.DynamicPseudoType, false
}

func (stubResolver) ResourceAttributes(resourceType string, isData bool) (cty.Type, bool) {
	if resourceType == "aws_s3_bucket" {
		return cty.Object(map[string]cty.Type{"arn": cty.String}), true
	}

	return cty.DynamicPseudoType, false
}

func (stubResolver) Provenance() []ProvenanceSchema {
	return []ProvenanceSchema{{Provider: "hashicorp/aws", Version: "5.82.0"}}
}

const fixtureModule = `terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

variable "bucket_name" {
  type        = string
  description = "Name of the bucket"
}

variable "lifecycle_rules" {
  type = list(object({
    id      = string
    days    = number
    enabled = optional(bool, true)
  }))
  default = []
}

variable "untyped" {
  default = "x"
}

variable "create_flag" {
  type    = bool
  default = null
}

locals {
  full_name = "${var.bucket_name}-suffix"
  ids       = [for r in var.lifecycle_rules : r.id]
}

resource "aws_s3_bucket" "this" {
  bucket = local.full_name
}

resource "aws_s3_bucket" "replicas" {
  count  = 2
  bucket = "x"
}

output "name" {
  value = local.full_name
}

output "ids" {
  value = local.ids
}

output "rule_map" {
  value = { for r in var.lifecycle_rules : r.id => r.days }
}

output "policy_json" {
  value = jsonencode(local.ids)
}

output "bucket_arn" {
  value = aws_s3_bucket.this.arn
}

output "replica_arns" {
  value = aws_s3_bucket.replicas[*].arn
}

output "first_replica_arn" {
	value = try(aws_s3_bucket.replicas[0].arn, "")
}

output "child" {
  value = module.other.thing
}
`

// writeFixture materialises the fixture module in a temp directory.
func writeFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(fixtureModule), 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	return dir
}

func TestBuildContractVariables(t *testing.T) {
	mod, err := ParseModule(writeFixture(t))
	if err != nil {
		t.Fatalf("ParseModule failed: %v", err)
	}

	c, err := BuildContract(mod, ContractMeta{Namespace: "ns", Name: "s3", Version: "1.0.0"}, stubResolver{})
	if err != nil {
		t.Fatalf("BuildContract failed: %v", err)
	}

	byName := map[string]ContractVariable{}
	for _, v := range c.Variables {
		byName[v.Name] = v
	}

	if got := string(byName["bucket_name"].Type); got != `"string"` {
		t.Errorf("bucket_name type = %s, want \"string\"", got)
	}

	if !byName["bucket_name"].Required {
		t.Error("bucket_name should be required")
	}

	if got := string(byName["untyped"].Type); got != `"dynamic"` {
		t.Errorf("untyped type = %s, want \"dynamic\"", got)
	}

	if !byName["lifecycle_rules"].OptionalAttributes["enabled"] {
		t.Error("lifecycle_rules should report 'enabled' as an optional attribute")
	}

	if !slices.Contains(byName["lifecycle_rules"].OptionalAttributePaths, "enabled") {
		t.Error("lifecycle_rules should report the optional attribute path")
	}

	var rulesType any
	if err := json.Unmarshal(byName["lifecycle_rules"].Type, &rulesType); err != nil {
		t.Fatalf("lifecycle_rules type is not valid JSON: %v", err)
	}

	if c.Compatibility.Grade != GradePartial {
		t.Errorf("grade = %s, want %s (module has an untyped variable)", c.Compatibility.Grade, GradePartial)
	}

	// A required variable (no default at all) must omit "default" entirely...
	if byName["bucket_name"].Default != nil {
		t.Errorf("bucket_name (required, no default) Default = %s, want omitted", byName["bucket_name"].Default)
	}

	// ...whereas an explicit `default = null` must be preserved on the wire
	// as "default": null, since it is NOT the same thing as no default —
	// it tells OpenTofu to pass nothing to the provider for that attribute.
	if byName["create_flag"].Required {
		t.Error("create_flag should not be required despite its default being null")
	}

	if got := string(byName["create_flag"].Default); got != "null" {
		t.Errorf("create_flag Default = %q, want \"null\" (explicit null default must not be omitted)", got)
	}
}

func TestBuildContractOutputs(t *testing.T) {
	mod, err := ParseModule(writeFixture(t))
	if err != nil {
		t.Fatalf("ParseModule failed: %v", err)
	}

	c, err := BuildContract(mod, ContractMeta{Namespace: "ns", Name: "s3", Version: "1.0.0"}, stubResolver{})
	if err != nil {
		t.Fatalf("BuildContract failed: %v", err)
	}

	byName := map[string]ContractOutput{}
	for _, o := range c.Outputs {
		byName[o.Name] = o
	}

	cases := []struct {
		output     string
		wantType   string
		confidence string
	}{
		{"name", `"string"`, ConfidenceExact},
		{"ids", `["list","string"]`, ConfidenceExact},
		{"rule_map", `["map","number"]`, ConfidenceExact},
		{"policy_json", `"string"`, ConfidenceExact},
		{"bucket_arn", `"string"`, ConfidenceExact},
		{"replica_arns", `["list","string"]`, ConfidenceExact},
		{"first_replica_arn", `"string"`, ConfidenceExact},
		{"child", `"dynamic"`, ConfidenceUnknown},
	}

	for _, tc := range cases {
		got, ok := byName[tc.output]
		if !ok {
			t.Errorf("output %q missing from contract", tc.output)
			continue
		}

		if string(got.Type) != tc.wantType {
			t.Errorf("output %q type = %s, want %s", tc.output, got.Type, tc.wantType)
		}

		if got.Confidence != tc.confidence {
			t.Errorf("output %q confidence = %s, want %s (reason: %s)",
				tc.output, got.Confidence, tc.confidence, got.Reason)
		}

		if got.Confidence == ConfidenceExact && got.Reason != "" {
			t.Errorf("output %q reason = %q, want empty for exact confidence", tc.output, got.Reason)
		}
	}
}

func TestBuildContractWithoutResolver(t *testing.T) {
	mod, err := ParseModule(writeFixture(t))
	if err != nil {
		t.Fatalf("ParseModule failed: %v", err)
	}

	c, err := BuildContract(mod, ContractMeta{Namespace: "ns", Name: "s3", Version: "1.0.0"}, nil)
	if err != nil {
		t.Fatalf("BuildContract failed: %v", err)
	}

	for _, o := range c.Outputs {
		if o.Name != "bucket_arn" {
			continue
		}

		if o.Confidence != ConfidenceUnknown {
			t.Errorf("bucket_arn confidence = %s, want %s when no provider schema is available",
				o.Confidence, ConfidenceUnknown)
		}
	}
}
