package assemblyexport

import (
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclwrite"
)

func TestRenderRejectsBlankRegistryHost(t *testing.T) {
	_, err := Render(RenderModel{RegistryHost: "   "})
	if err == nil || !strings.Contains(err.Error(), "registry host is required") {
		t.Fatalf("Render() error = %v, want registry host is required", err)
	}
}

func TestIsComplexVariableType(t *testing.T) {
	cases := []struct {
		expression string
		want       bool
	}{
		{"object({name=string})", true},
		{"map(object({name=string}))", true},
		{"map(string)", false},
		{"list(object({name=string}))", false},
		{"string", false},
		{"object({", false},
	}

	for _, tc := range cases {
		if got := isComplexVariableType(tc.expression); got != tc.want {
			t.Errorf("isComplexVariableType(%q) = %t, want %t", tc.expression, got, tc.want)
		}
	}
}

func TestAppendRenderBlockRendersNestedBlocks(t *testing.T) {
	file := hclwrite.NewEmptyFile()
	err := appendRenderBlock(file.Body(), RenderBlock{
		Arguments: map[string]string{"enabled": "true"},
		Blocks: map[string][]RenderBlock{
			"rule": {{Arguments: map[string]string{"name": `"a"`}}},
		},
	})
	if err != nil {
		t.Fatalf("appendRenderBlock() error = %v", err)
	}

	got := string(file.Bytes())
	for _, want := range []string{"enabled = true", "rule {", `name = "a"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered block = %q, want it to contain %q", got, want)
		}
	}
}

func TestAppendRenderBlockRejectsInvalidExpressions(t *testing.T) {
	cases := []struct {
		name  string
		block RenderBlock
		want  string
	}{
		{
			name:  "top-level argument",
			block: RenderBlock{Arguments: map[string]string{"bad": "1 +"}},
			want:  "argument bad",
		},
		{
			name: "nested argument",
			block: RenderBlock{Blocks: map[string][]RenderBlock{
				"rule": {{Arguments: map[string]string{"bad": "(("}}},
			}},
			want: "argument bad",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := appendRenderBlock(hclwrite.NewEmptyFile().Body(), tc.block)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("appendRenderBlock() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
