package hclschema

import (
	"os"
	"path/filepath"
	"testing"
)

const functionFixtureModule = `variable "items" {
  type = list(string)
}

output "fixed_string" {
  value = lower("ABC")
}

output "fixed_number" {
  value = abs(-3)
}

output "fixed_bool" {
  value = contains(["a"], "a")
}

output "list_of_string" {
  value = split(",", "a,b")
}

output "try_literal" {
  value = try("a", "b")
}

output "coalesce_literal" {
  value = coalesce("", "x")
}

output "one_list" {
  value = one(["only"])
}

output "to_list" {
  value = tolist(["a", "b"])
}

output "to_set" {
  value = toset(["a", "b"])
}

output "to_map" {
  value = tomap({ a = "x" })
}

output "concat_lists" {
  value = concat(["a"], ["b"])
}

output "flatten_lists" {
  value = flatten([["a"], ["b"]])
}

output "compact_list" {
  value = compact(["a", ""])
}

output "keys_map" {
  value = keys({ a = "x" })
}

output "values_map" {
  value = values({ a = "x" })
}

output "element_list" {
  value = element(["a", "b"], 0)
}

output "merge_maps" {
  value = merge({ a = "x" }, { b = "y" })
}

output "zipmap_keys" {
  value = zipmap(["a"], ["x"])
}

output "decoded" {
  value = jsondecode("{}")
}

output "unknown_fn" {
  value = nonexistentfn("x")
}

output "no_arg_try" {
  value = try()
}

output "no_arg_tolist" {
  value = tolist()
}

output "passthrough_list" {
  value = reverse(["a", "b"])
}

output "list_var" {
  value = var.items
}
`

// writeFunctionFixture writes the function fixture into a temporary module directory.
func writeFunctionFixture(t *testing.T, src string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(src), 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	return dir
}

func TestIsKnownFunction(t *testing.T) {
	known := []string{"abs", "lower", "contains", "split", "try", "coalesce", "one", "tolist",
		"toset", "tomap", "concat", "setunion", "flatten", "compact", "keys", "values", "element",
		"lookup", "merge", "zipmap", "jsondecode", "yamldecode", "sensitive", "nonsensitive"}
	for _, name := range known {
		if !IsKnownFunction(name) {
			t.Errorf("IsKnownFunction(%q) = false, want true", name)
		}
	}

	for _, name := range []string{"", "nonexistentfn", "custom_fn"} {
		if IsKnownFunction(name) {
			t.Errorf("IsKnownFunction(%q) = true, want false", name)
		}
	}
}

func TestBuildContractFunctionReturnTypes(t *testing.T) {
	mod, err := ParseModule(writeFunctionFixture(t, functionFixtureModule))
	if err != nil {
		t.Fatalf("ParseModule failed: %v", err)
	}

	c, err := BuildContract(mod, ContractMeta{Namespace: "ns", Name: "fn", Version: "1.0.0"}, nil)
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
		{"fixed_string", `"string"`, ConfidenceExact},
		{"fixed_number", `"number"`, ConfidenceExact},
		{"fixed_bool", `"bool"`, ConfidenceExact},
		{"list_of_string", `["list","string"]`, ConfidenceExact},
		{"try_literal", `"string"`, ConfidenceExact},
		{"coalesce_literal", `"string"`, ConfidenceExact},
		{"to_list", `["list","string"]`, ConfidenceExact},
		{"to_set", `["set","string"]`, ConfidenceExact},
		{"to_map", `["map","string"]`, ConfidenceExact},
		{"concat_lists", `["list","string"]`, ConfidenceExact},
		{"flatten_lists", `["list","string"]`, ConfidenceExact},
		{"compact_list", `["list","string"]`, ConfidenceExact},
		{"keys_map", `["list","string"]`, ConfidenceExact},
		{"merge_maps", `["object",{"a":"string","b":"string"}]`, ConfidenceExact},
		{"passthrough_list", `["list","string"]`, ConfidenceExact},
		{"element_list", `"string"`, ConfidenceExact},
		{"one_list", `"string"`, ConfidenceExact},
		{"decoded", `"dynamic"`, ConfidenceUnknown},
		{"unknown_fn", `"dynamic"`, ConfidenceUnknown},
		{"no_arg_try", `"dynamic"`, ConfidenceUnknown},
		{"no_arg_tolist", `"dynamic"`, ConfidenceUnknown},
	}

	for _, tc := range cases {
		got, ok := byName[tc.output]
		if !ok {
			t.Errorf("output %q missing from contract", tc.output)
			continue
		}

		if string(got.Type) != tc.wantType {
			t.Errorf("output %q type = %s, want %s (reason: %s)", tc.output, got.Type, tc.wantType, got.Reason)
		}

		if got.Confidence != tc.confidence {
			t.Errorf("output %q confidence = %s, want %s (reason: %s)",
				tc.output, got.Confidence, tc.confidence, got.Reason)
		}
	}
}
