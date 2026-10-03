package providerschema

import (
	"strings"
	"testing"
)

func TestExtractInputRejectsPathTraversal(t *testing.T) {
	input := ExtractInput{
		ProviderZipPath: "/tmp/provider.zip",
		Namespace:       "hashicorp",
		Name:            "null",
		Version:         "3.2.3",
		OS:              "darwin",
		Arch:            "arm64",
		WorkDir:         "/tmp/work",
	}

	for field, value := range map[string]string{
		"namespace": "../outside",
		"name":      "../outside",
		"version":   "../outside",
		"os":        "../outside",
		"arch":      "../outside",
	} {
		candidate := input
		switch field {
		case "namespace":
			candidate.Namespace = value
		case "name":
			candidate.Name = value
		case "version":
			candidate.Version = value
		case "os":
			candidate.OS = value
		case "arch":
			candidate.Arch = value
		}

		if err := candidate.validate(); err == nil {
			t.Errorf("validate() with %s traversal = nil, want error", field)
		}
	}

	for _, value := range []string{"bad\"name", "bad\nname", "bad${name}", "bad%{name}"} {
		candidate := input
		candidate.Name = value
		if err := candidate.validate(); err == nil {
			t.Errorf("validate() with unsafe name %q = nil, want error", value)
		}
	}
}

func TestBoundedOutputStopsAtLimit(t *testing.T) {
	output := &boundedOutput{limit: 4}

	if _, err := output.Write([]byte("12345")); err == nil {
		t.Fatal("Write() error = nil, want output limit error")
	}
	if string(output.data) != "1234" {
		t.Fatalf("output = %q, want %q", output.data, "1234")
	}
	if !output.exceeded {
		t.Fatal("exceeded = false, want true")
	}
	if strings.Contains(output.String(), "5") {
		t.Fatalf("output retained bytes beyond limit: %q", output.String())
	}
}
