package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema/assemblyexport"
	"github.com/tonedefdev/opendepot/pkg/hclschema/providerschema"
)

func TestPackageAssemblyContainsOnlyGeneratedFiles(t *testing.T) {
	files := assemblyexport.Files{Main: []byte("main"), Variables: []byte("variables")}
	archiveBytes, err := packageAssembly(files)
	if err != nil {
		t.Fatalf("packageAssembly() error = %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	if len(reader.File) != 2 || reader.File[0].Name != "main.tf" || reader.File[1].Name != "variables.tf" {
		t.Fatalf("ZIP entries = %#v, want main.tf and variables.tf", reader.File)
	}
	if err := validateAssemblyArchive(archiveBytes, files); err != nil {
		t.Fatalf("validateAssemblyArchive() error = %v", err)
	}
}

func TestValidateAssemblyArchiveRejectsTruncatedArchive(t *testing.T) {
	files := assemblyexport.Files{Main: []byte("main"), Variables: []byte("variables")}
	archiveBytes, err := packageAssembly(files)
	if err != nil {
		t.Fatalf("packageAssembly() error = %v", err)
	}

	if err := validateAssemblyArchive(archiveBytes[:len(archiveBytes)-1], files); err == nil {
		t.Fatal("validateAssemblyArchive() error = nil, want truncated archive error")
	}
}

func TestWriteAssemblyCLIConfigUsesHTTPSValidationMirror(t *testing.T) {
	registryHost := opendepotRegistryHost
	validationRegistryURL := opendepotAssemblyValidationURL
	t.Cleanup(func() {
		opendepotRegistryHost = registryHost
		opendepotAssemblyValidationURL = validationRegistryURL
	})

	host := "opendepot.localtest.me:8443"
	validationURL := "https://server.opendepot-system.svc.cluster.local"
	opendepotRegistryHost = &host
	opendepotAssemblyValidationURL = &validationURL

	path := filepath.Join(t.TempDir(), "tofurc")
	providers := []assemblyexport.RenderProvider{
		{Namespace: "team-b", ProviderNamespace: "opentofu", ProviderName: "random"},
		{Namespace: "team-a", ProviderNamespace: "hashicorp", ProviderName: "aws"},
		{Namespace: "team-a", ProviderNamespace: "hashicorp", ProviderName: "aws"},
	}

	if err := writeAssemblyCLIConfig(path, providers, "secret-token"); err != nil {
		t.Fatalf("writeAssemblyCLIConfig() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	for _, expected := range []string{
		`credentials "opendepot.localtest.me:8443"`,
		`credentials "server.opendepot-system.svc.cluster.local"`,
		`token = "secret-token"`,
		`host "opendepot.localtest.me:8443"`,
		`"modules.v1"`,
		`"https://server.opendepot-system.svc.cluster.local/opendepot/modules/v1/"`,
		`provider_installation`,
		`url     = "https://server.opendepot-system.svc.cluster.local/opendepot/providers/mirror/v1/team-a/"`,
		`"registry.opentofu.org/hashicorp/aws"`,
		`url     = "https://server.opendepot-system.svc.cluster.local/opendepot/providers/mirror/v1/team-b/"`,
		`"registry.opentofu.org/opentofu/random"`,
		`exclude = ["registry.opentofu.org/*/*"]`,
	} {
		if !strings.Contains(string(content), expected) {
			t.Errorf("CLI config does not contain %q:\n%s", expected, content)
		}
	}
	if strings.Contains(string(content), "providers.v1") {
		t.Fatalf("CLI config overrides providers.v1 instead of using the network mirror:\n%s", content)
	}
}

func TestSanitizeAssemblyOutputRedactsToken(t *testing.T) {
	const token = "secret-token"
	output := sanitizeAssemblyOutput("request failed with secret-token", token)
	if bytes.Contains([]byte(output), []byte(token)) {
		t.Fatalf("sanitizeAssemblyOutput() leaked token: %q", output)
	}
}

func TestAssemblyProviderSupported(t *testing.T) {
	terraformRegistry := opendepotv1alpha1.TerraformRegistryHost

	if !assemblyProviderSupported(nil) {
		t.Fatal("assemblyProviderSupported(nil) = false, want default OpenTofu provider support")
	}
	if assemblyProviderSupported(&opendepotv1alpha1.ProviderConfig{UpstreamRegistry: &terraformRegistry}) {
		t.Fatal("assemblyProviderSupported(terraform provider) = true, want false")
	}
}

func TestBoundedCommandOutputReportsCompleteLines(t *testing.T) {
	var lines []string
	output := &boundedCommandOutput{
		limit: 1024,
		phase: "init",
		progress: func(phase, line string) {
			lines = append(lines, phase+":"+line)
		},
	}

	_, _ = output.Write([]byte("Initializing "))
	_, _ = output.Write([]byte("modules...\nDownloading module"))
	if got := output.String(); got != "Initializing modules...\nDownloading module" {
		t.Fatalf("String() = %q", got)
	}
	want := []string{"init:Initializing modules...", "init:Downloading module"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("progress lines = %#v, want %#v", lines, want)
	}
}

func TestStreamAssemblyExportReportsProgressAndArchive(t *testing.T) {
	workDirectory := t.TempDir()
	tofuPath := filepath.Join(workDirectory, "tofu")
	if err := os.WriteFile(tofuPath, []byte("#!/bin/sh\necho running $1\n"), 0700); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	previousWorkDirectory := opendepotAssemblyWorkDir
	previousTofuPath := opendepotTofuBinPath
	previousRegistryHost := opendepotRegistryHost
	previousValidationRegistryURL := opendepotAssemblyValidationURL
	previousValidationCACertPath := opendepotAssemblyValidationCACertPath
	previousRegistryInsecure := opendepotAssemblyRegistryInsecure
	previousInitTimeout := opendepotAssemblyInitTimeout
	previousValidateTimeout := opendepotAssemblyValidateTimeout
	previousOutputLimit := opendepotAssemblyMaxOutputBytes
	registryHost := "opendepot.example.com"
	validationRegistryURL := "https://opendepot.example.com"
	validationCACertPath := ""
	registryInsecure := false
	initTimeout := time.Minute
	validateTimeout := time.Minute
	outputLimit := int64(1024 * 1024)
	opendepotAssemblyWorkDir = &workDirectory
	opendepotTofuBinPath = &tofuPath
	opendepotRegistryHost = &registryHost
	opendepotAssemblyValidationURL = &validationRegistryURL
	opendepotAssemblyValidationCACertPath = &validationCACertPath
	opendepotAssemblyRegistryInsecure = &registryInsecure
	opendepotAssemblyInitTimeout = &initTimeout
	opendepotAssemblyValidateTimeout = &validateTimeout
	opendepotAssemblyMaxOutputBytes = &outputLimit

	t.Cleanup(func() {
		opendepotAssemblyWorkDir = previousWorkDirectory
		opendepotTofuBinPath = previousTofuPath
		opendepotRegistryHost = previousRegistryHost
		opendepotAssemblyValidationURL = previousValidationRegistryURL
		opendepotAssemblyValidationCACertPath = previousValidationCACertPath
		opendepotAssemblyRegistryInsecure = previousRegistryInsecure
		opendepotAssemblyInitTimeout = previousInitTimeout
		opendepotAssemblyValidateTimeout = previousValidateTimeout
		opendepotAssemblyMaxOutputBytes = previousOutputLimit
	})

	files := assemblyexport.Files{Main: []byte("module {}\n"), Variables: []byte("variable {}\n")}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/opendepot/ui/v1/assembly/export", nil)
	streamAssemblyExport(response, request, files, nil, "")

	if got := response.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q", got)
	}

	var events []assemblyStreamEvent
	for _, line := range strings.Split(strings.TrimSpace(response.Body.String()), "\n") {
		var event assemblyStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("json.Unmarshal() error = %v for %q", err, line)
		}
		events = append(events, event)
	}
	if len(events) != 6 {
		t.Fatalf("event count = %d, want 6: %#v", len(events), events)
	}
	if events[1].Output != "$ tofu init -input=false -backend=false -no-color" || events[2].Output != "running init" {
		t.Fatalf("init events = %#v", events[1:3])
	}
	if events[3].Output != "$ tofu validate -no-color" || events[4].Output != "running validate" {
		t.Fatalf("validate events = %#v", events[3:5])
	}
	archive, err := base64.StdEncoding.DecodeString(events[5].Archive)
	if err != nil {
		t.Fatalf("base64.DecodeString() error = %v", err)
	}
	if err := validateAssemblyArchive(archive, files); err != nil {
		t.Fatalf("validateAssemblyArchive() error = %v", err)
	}
}

func TestDecodeProviderSchemaVerifiesDigest(t *testing.T) {
	raw, err := json.Marshal(providerschema.ReducedSchema{SchemaVersion: providerschema.SchemaVersion})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		t.Fatalf("gzip.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}

	digest := sha256.Sum256(raw)
	if _, err := decodeProviderSchema(bytes.NewReader(compressed.Bytes()), hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("decodeProviderSchema() error = %v", err)
	}
	if _, err := decodeProviderSchema(bytes.NewReader(compressed.Bytes()), "incorrect"); err == nil {
		t.Fatal("decodeProviderSchema() error = nil, want digest mismatch")
	}
}
