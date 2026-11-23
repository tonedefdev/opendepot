package providerschema

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReduceRejectsInvalidAndEmptyInput(t *testing.T) {
	if _, err := Reduce([]byte(`{not json`), "hashicorp/null", "3.2.3"); err == nil {
		t.Fatal("Reduce accepted malformed JSON")
	}

	if _, err := Reduce([]byte(`{"provider_schemas":{}}`), "hashicorp/null", "3.2.3"); err == nil || !strings.Contains(err.Error(), "no provider schemas") {
		t.Fatalf("Reduce error = %v, want no provider schemas", err)
	}
}

func TestReduceKeepsResourcesAndDataSources(t *testing.T) {
	raw := []byte(`{
  "provider_schemas": {
    "registry.opentofu.org/hashicorp/null": {
      "provider": {"block": {"attributes": {}}},
      "resource_schemas": {
        "null_resource": {"block": {"attributes": {
          "id": {"type": "string", "computed": true},
          "triggers": {"type": ["map","string"], "optional": true}
        }}}
      },
      "data_source_schemas": {
        "null_data_source": {"block": {"attributes": {
          "inputs": {"type": ["map","string"], "optional": true},
          "outputs": {"type": ["map","string"], "computed": true}
        }}}
      }
    }
  }
}`)

	schema, err := Reduce(raw, "hashicorp/null", "3.2.3")
	if err != nil {
		t.Fatalf("Reduce() error = %v", err)
	}

	if schema.Provider != "hashicorp/null" || schema.ProviderVersion != "3.2.3" {
		t.Fatalf("provenance = %q %q", schema.Provider, schema.ProviderVersion)
	}

	if !schema.Resources["null_resource"].Attributes["id"].Computed {
		t.Fatal("resource id attribute not retained as computed")
	}

	if !schema.DataSources["null_data_source"].Attributes["outputs"].Computed {
		t.Fatal("data source outputs attribute not retained as computed")
	}

	if _, ok := schema.Lookup("null_resource", "triggers", false); !ok {
		t.Fatal("Lookup failed for reduced resource attribute")
	}
}

// writeFakeTofu writes a shell stand-in for the tofu binary. initExit and schemaExit control
// the exit status of the init and providers schema subcommands, and schemaJSON is emitted on
// success.
func writeFakeTofu(t *testing.T, dir string, initExit, schemaExit int, schemaJSON string) string {
	t.Helper()

	schemaFile := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaFile, []byte(schemaJSON), 0600); err != nil {
		t.Fatal(err)
	}

	script := `#!/bin/sh
case "$1" in
  init)
    echo "init stderr" >&2
    exit ` + strconv.Itoa(initExit) + `
    ;;
  providers)
    if [ ` + strconv.Itoa(schemaExit) + ` -ne 0 ]; then
      echo "schema failure detail" >&2
      exit ` + strconv.Itoa(schemaExit) + `
    fi
    cat '` + schemaFile + `'
    ;;
esac
`
	bin := filepath.Join(dir, "tofu")
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	return bin
}

func extractInput(t *testing.T, bin string) ExtractInput {
	t.Helper()

	dir := t.TempDir()
	zip := filepath.Join(dir, "provider.zip")
	if err := os.WriteFile(zip, []byte("zip-bytes"), 0600); err != nil {
		t.Fatal(err)
	}

	return ExtractInput{
		ProviderZipPath: zip,
		Namespace:       "hashicorp",
		Name:            "null",
		Version:         "3.2.3",
		OS:              "linux",
		Arch:            "amd64",
		WorkDir:         t.TempDir(),
		TofuBinPath:     bin,
	}
}

func TestExtractReturnsSchemaFromTofu(t *testing.T) {
	const schemaJSON = `{"provider_schemas":{}}`
	bin := writeFakeTofu(t, t.TempDir(), 0, 0, schemaJSON)
	in := extractInput(t, bin)

	out, err := Extract(context.Background(), in)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if string(out) != schemaJSON {
		t.Fatalf("Extract() = %q, want %q", out, schemaJSON)
	}

	staged := filepath.Join(in.WorkDir, "mirror", "registry.opentofu.org", "hashicorp", "null",
		"terraform-provider-null_3.2.3_linux_amd64.zip")
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("provider archive not staged into mirror: %v", err)
	}

	main, err := os.ReadFile(filepath.Join(in.WorkDir, "config", "main.tf"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(main), `"registry.opentofu.org/hashicorp/null"`) {
		t.Fatalf("throwaway module missing source:\n%s", main)
	}
}

func TestExtractUsesCustomRegistryHost(t *testing.T) {
	bin := writeFakeTofu(t, t.TempDir(), 0, 0, `{}`)
	in := extractInput(t, bin)
	in.RegistryHost = "registry.example.com"

	if _, err := Extract(context.Background(), in); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	staged := filepath.Join(in.WorkDir, "mirror", "registry.example.com", "hashicorp", "null")
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("mirror not created under custom registry host: %v", err)
	}
}

func TestExtractReportsInitFailure(t *testing.T) {
	bin := writeFakeTofu(t, t.TempDir(), 1, 0, `{}`)

	_, err := Extract(context.Background(), extractInput(t, bin))
	if err == nil || !strings.Contains(err.Error(), "tofu init failed") || !strings.Contains(err.Error(), "init stderr") {
		t.Fatalf("Extract() error = %v, want init failure with stderr", err)
	}
}

func TestExtractReportsSchemaFailure(t *testing.T) {
	bin := writeFakeTofu(t, t.TempDir(), 0, 2, `{}`)

	_, err := Extract(context.Background(), extractInput(t, bin))
	if err == nil || !strings.Contains(err.Error(), "tofu providers schema failed") || !strings.Contains(err.Error(), "schema failure detail") {
		t.Fatalf("Extract() error = %v, want schema failure with stderr", err)
	}
}

func TestExtractRejectsIncompleteInputBeforeRunning(t *testing.T) {
	in := ExtractInput{Namespace: "hashicorp", Name: "null"}

	_, err := Extract(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "missing required input") {
		t.Fatalf("Extract() error = %v, want missing input error", err)
	}
}

func TestExtractFailsWhenProviderArchiveMissing(t *testing.T) {
	bin := writeFakeTofu(t, t.TempDir(), 0, 0, `{}`)
	in := extractInput(t, bin)
	in.ProviderZipPath = filepath.Join(t.TempDir(), "absent.zip")

	_, err := Extract(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "stage provider archive") {
		t.Fatalf("Extract() error = %v, want staging error", err)
	}
}

func TestValidateRejectsInvalidIdentifiers(t *testing.T) {
	base := extractInput(t, "tofu")

	cases := map[string]func(*ExtractInput){
		"name starting with digit": func(in *ExtractInput) { in.Name = "1null" },
		"name with dot":            func(in *ExtractInput) { in.Name = "nu.ll" },
		"registry host traversal":  func(in *ExtractInput) { in.RegistryHost = ".." },
		"blank arch":               func(in *ExtractInput) { in.Arch = " " },
		"blank work dir":           func(in *ExtractInput) { in.WorkDir = "" },
		"blank version":            func(in *ExtractInput) { in.Version = "" },
		"blank provider zip":       func(in *ExtractInput) { in.ProviderZipPath = "" },
		"blank namespace":          func(in *ExtractInput) { in.Namespace = "" },
		"blank name":               func(in *ExtractInput) { in.Name = "" },
		"padded os":                func(in *ExtractInput) { in.OS = " linux" },
	}

	for label, mutate := range cases {
		t.Run(label, func(t *testing.T) {
			in := base
			mutate(&in)
			if err := in.validate(); err == nil {
				t.Fatalf("validate() accepted %s", label)
			}
		})
	}

	if err := base.validate(); err != nil {
		t.Fatalf("validate() rejected a valid input: %v", err)
	}
}
