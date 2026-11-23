package hclschema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInferOutputsClassifiesIndexRelativeTraversalAndForKeys(t *testing.T) {
	dir := t.TempDir()
	source := `
output "list_index" {
  value = ["a", "b"][0]
}

output "object_index" {
  value = {a = "x"}["a"]
}

output "relative_attr" {
  value = {a = "x"}.a
}

output "string_keys" {
  value = [for k, v in {a = "x"} : k]
}

output "number_keys" {
  value = [for k, v in ["a"] : k]
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	mod, err := ParseModule(dir)
	if err != nil {
		t.Fatalf("ParseModule failed: %v", err)
	}

	got := inferOutputs(mod, stubResolver{})

	cases := []struct {
		output     string
		wantType   string
		confidence string
	}{
		{"list_index", `"dynamic"`, ConfidenceUnknown},
		{"object_index", `"string"`, ConfidenceExact},
		{"relative_attr", `"string"`, ConfidenceExact},
		{"string_keys", `["list","string"]`, ConfidenceExact},
		{"number_keys", `["list","number"]`, ConfidenceExact},
	}

	for _, tc := range cases {
		inferred, ok := got[tc.output]
		if !ok {
			t.Errorf("output %q missing from inference result", tc.output)
			continue
		}

		if string(inferred.Type) != tc.wantType {
			t.Errorf("output %q type = %s, want %s (reason: %s)", tc.output, inferred.Type, tc.wantType, inferred.Reason)
		}

		if inferred.Confidence != tc.confidence {
			t.Errorf("output %q confidence = %s, want %s (reason: %s)", tc.output, inferred.Confidence, tc.confidence, inferred.Reason)
		}
	}
}
