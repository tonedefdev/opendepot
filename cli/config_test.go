package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustLoadProject(t *testing.T, body string) *project {
	t.Helper()

	path := filepath.Join(t.TempDir(), configFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", configFileName, err)
	}

	proj, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	return proj
}

func TestLoadConfigRegistryDefault(t *testing.T) {
	proj := mustLoadProject(t, `
opendepot {
  registry = "registry.example.com"
}

agent_skill "tools" {
  source  = "registry.example.com/platform/tools"
  version = ">= 1.0.0"
}
`)

	if proj.registry != "registry.example.com" {
		t.Fatalf("registry = %q, want registry.example.com", proj.registry)
	}

	if proj.entries[0].source != "registry.example.com/platform/tools" {
		t.Fatalf("source changed: %q", proj.entries[0].source)
	}
}

func TestLoadConfigRegistryOmitted(t *testing.T) {
	proj := mustLoadProject(t, `
agent_skill "tools" {
  source  = "registry.example.com/platform/tools"
  version = ">= 1.0.0"
}
`)

	if proj.registry != "" {
		t.Fatalf("registry = %q, want empty", proj.registry)
	}
}

func TestLoadConfigRegistryInvalid(t *testing.T) {
	tests := map[string]string{
		"empty":          `registry = ""`,
		"with path":      `registry = "registry.example.com/platform"`,
		"leading slash":  `registry = "/registry.example.com"`,
		"trailing slash": `registry = "registry.example.com/"`,
	}

	for name, attr := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), configFileName)
			body := "opendepot {\n  " + attr + "\n}\n"
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatalf("write %s: %v", configFileName, err)
			}

			if _, err := loadConfig(path); err == nil {
				t.Fatalf("loadConfig with %s: expected error", attr)
			}
		})
	}
}

func TestValidateRegistryHost(t *testing.T) {
	if host, err := validateRegistryHost("registry.example.com:8443"); err != nil || host != "registry.example.com:8443" {
		t.Fatalf("validateRegistryHost(valid) = %q, %v", host, err)
	}

	for _, host := range []string{"", "a/b", "/a", "a/"} {
		if _, err := validateRegistryHost(host); err == nil {
			t.Fatalf("validateRegistryHost(%q): expected error", host)
		}
	}
}

func TestResolveListRegistryPrecedence(t *testing.T) {
	proj := &project{
		registry: "default.example.com",
		entries: []entry{
			{source: "inferred.example.com/ns/one"},
		},
	}

	got, err := resolveListRegistry("explicit.example.com", proj)
	if err != nil || got != "explicit.example.com" {
		t.Fatalf("explicit: got %q, %v; want explicit.example.com", got, err)
	}

	got, err = resolveListRegistry("", proj)
	if err != nil || got != "default.example.com" {
		t.Fatalf("project default: got %q, %v; want default.example.com", got, err)
	}

	proj.registry = ""
	got, err = resolveListRegistry("", proj)
	if err != nil || got != "inferred.example.com" {
		t.Fatalf("inferred: got %q, %v; want inferred.example.com", got, err)
	}
}

func TestResolveListRegistryExplicitWithoutProject(t *testing.T) {
	got, err := resolveListRegistry("registry.example.com", nil)
	if err != nil || got != "registry.example.com" {
		t.Fatalf("got %q, %v; want registry.example.com", got, err)
	}
}

func TestResolveListRegistryExplicitValidated(t *testing.T) {
	proj := &project{registry: "default.example.com"}

	if _, err := resolveListRegistry("registry.example.com/ns", proj); err == nil {
		t.Fatal("expected error for invalid explicit registry")
	}
}

func TestResolveListRegistryInfersSingleHost(t *testing.T) {
	proj := mustLoadProject(t, `
agent_skill "one" {
  source  = "registry.example.com/platform/one"
  version = ">= 1.0.0"
}

agent "two" {
  source  = "registry.example.com/platform/two"
  version = ">= 1.0.0"
}
`)

	got, err := resolveListRegistry("", proj)
	if err != nil || got != "registry.example.com" {
		t.Fatalf("got %q, %v; want registry.example.com", got, err)
	}
}

func TestResolveListRegistryAmbiguousHostsNotInferred(t *testing.T) {
	proj := mustLoadProject(t, `
agent_skill "one" {
  source  = "b.example.com/platform/one"
  version = ">= 1.0.0"
}

agent "two" {
  source  = "a.example.com/platform/two"
  version = ">= 1.0.0"
}
`)

	got, err := resolveListRegistry("", proj)
	if err == nil {
		t.Fatalf("expected ambiguity error, got registry %q", got)
	}

	if got != "" {
		t.Fatalf("ambiguous inference returned %q", got)
	}

	if !strings.Contains(err.Error(), "a.example.com, b.example.com") || !strings.Contains(err.Error(), "--registry") {
		t.Fatalf("error = %q, want sorted hosts and --registry hint", err)
	}
}

func TestResolveListRegistryDefaultOverridesAmbiguousSources(t *testing.T) {
	proj := mustLoadProject(t, `
opendepot {
  registry = "a.example.com"
}

agent_skill "one" {
  source  = "a.example.com/platform/one"
  version = ">= 1.0.0"
}

agent "two" {
  source  = "b.example.com/platform/two"
  version = ">= 1.0.0"
}
`)

	got, err := resolveListRegistry("", proj)
	if err != nil || got != "a.example.com" {
		t.Fatalf("got %q, %v; want a.example.com", got, err)
	}
}

func TestResolveListRegistryNoRegistry(t *testing.T) {
	tests := map[string]*project{
		"nil project": nil,
		"no sources":  {},
	}

	for name, proj := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := resolveListRegistry("", proj)
			if err == nil {
				t.Fatalf("expected error, got registry %q", got)
			}

			if !strings.Contains(err.Error(), "--registry") {
				t.Fatalf("error = %q, want --registry instruction", err)
			}
		})
	}
}

func TestLoadOptionalProjectMissingFile(t *testing.T) {
	proj, err := loadOptionalProject(filepath.Join(t.TempDir(), configFileName))
	if err != nil {
		t.Fatalf("loadOptionalProject(missing): %v", err)
	}

	if proj != nil {
		t.Fatal("loadOptionalProject(missing) returned a project")
	}

	got, err := resolveListRegistry("registry.example.com", proj)
	if err != nil || got != "registry.example.com" {
		t.Fatalf("explicit without project file: got %q, %v", got, err)
	}
}
