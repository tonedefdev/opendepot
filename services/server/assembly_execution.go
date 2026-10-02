package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema/assemblyexport"
)

type assemblyCommandError struct {
	Phase   string
	Output  string
	Timeout bool
	Err     error
}

func (e *assemblyCommandError) Error() string {
	return fmt.Sprintf("tofu %s failed: %v", e.Phase, e.Err)
}

type assemblyProgressFunc func(phase, output string)

type assemblyProviderMirror struct {
	Namespace string
	Sources   []string
}

func validateAndPackageAssembly(ctx context.Context, files assemblyexport.Files, providers []assemblyexport.RenderProvider, token string, progress assemblyProgressFunc) ([]byte, error) {
	workspace, err := os.MkdirTemp(*opendepotAssemblyWorkDir, "request-*")
	if err != nil {
		return nil, fmt.Errorf("create validation workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	if err := os.WriteFile(filepath.Join(workspace, "main.tf"), files.Main, 0600); err != nil {
		return nil, fmt.Errorf("write main.tf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "variables.tf"), files.Variables, 0600); err != nil {
		return nil, fmt.Errorf("write variables.tf: %w", err)
	}

	home := filepath.Join(workspace, "home")
	temp := filepath.Join(workspace, "tmp")
	data := filepath.Join(workspace, ".terraform-data")
	for _, directory := range []string{home, temp, data} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, fmt.Errorf("create validation directory: %w", err)
		}
	}

	cliConfig := filepath.Join(workspace, "tofurc")
	if err := writeAssemblyCLIConfig(cliConfig, providers, token); err != nil {
		return nil, err
	}

	environment := assemblyEnvironment(home, temp, data, cliConfig)
	if err := runAssemblyCommand(ctx, workspace, environment, "init", *opendepotAssemblyInitTimeout,
		progress,
		"init", "-input=false", "-backend=false", "-no-color"); err != nil {
		return nil, err
	}

	return packageAssembly(files)
}

func writeAssemblyCLIConfig(path string, providers []assemblyexport.RenderProvider, token string) error {
	file := hclwrite.NewEmptyFile()
	if token != "" {
		credentialHosts := []string{*opendepotRegistryHost}
		registryURL, err := url.Parse(*opendepotAssemblyValidationURL)
		if err != nil {
			return fmt.Errorf("parse Assembly Line registry URL: %w", err)
		}
		if registryURL.Host != *opendepotRegistryHost {
			credentialHosts = append(credentialHosts, registryURL.Host)
		}

		for _, credentialHost := range credentialHosts {
			credentials := file.Body().AppendNewBlock("credentials", []string{credentialHost})
			credentials.Body().SetAttributeValue("token", cty.StringVal(token))
			file.Body().AppendNewline()
		}
	}

	baseURL := *opendepotAssemblyValidationURL
	host := file.Body().AppendNewBlock("host", []string{*opendepotRegistryHost})
	host.Body().SetAttributeRaw("services", hclwrite.TokensForObject([]hclwrite.ObjectAttrTokens{
		{Name: hclwrite.TokensForValue(cty.StringVal("modules.v1")), Value: hclwrite.TokensForValue(cty.StringVal(baseURL + "/opendepot/modules/v1/"))},
	}))
	file.Body().AppendNewline()

	mirrors := assemblyProviderMirrors(providers)
	if len(mirrors) > 0 {
		installation := file.Body().AppendNewBlock("provider_installation", nil)
		for _, mirror := range mirrors {
			networkMirror := installation.Body().AppendNewBlock("network_mirror", nil)
			networkMirror.Body().SetAttributeValue("url", cty.StringVal(fmt.Sprintf("%s/opendepot/providers/mirror/v1/%s/", baseURL, mirror.Namespace)))
			networkMirror.Body().SetAttributeValue("include", stringListValue(mirror.Sources))
		}

		direct := installation.Body().AppendNewBlock("direct", nil)
		direct.Body().SetAttributeValue("exclude", stringListValue([]string{opendepotv1alpha1.OpenTofuRegistryHost + "/*/*"}))
	}

	if err := os.WriteFile(path, file.Bytes(), 0600); err != nil {
		return fmt.Errorf("write OpenTofu CLI config: %w", err)
	}

	return nil
}

func assemblyProviderMirrors(providers []assemblyexport.RenderProvider) []assemblyProviderMirror {
	grouped := make(map[string]map[string]struct{})
	for _, provider := range providers {
		if _, exists := grouped[provider.Namespace]; !exists {
			grouped[provider.Namespace] = make(map[string]struct{})
		}

		source := fmt.Sprintf("%s/%s/%s", opendepotv1alpha1.OpenTofuRegistryHost, provider.ProviderNamespace, provider.ProviderName)
		grouped[provider.Namespace][source] = struct{}{}
	}

	namespaces := make([]string, 0, len(grouped))
	for namespace := range grouped {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)

	mirrors := make([]assemblyProviderMirror, 0, len(namespaces))
	for _, namespace := range namespaces {
		sources := make([]string, 0, len(grouped[namespace]))
		for source := range grouped[namespace] {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		mirrors = append(mirrors, assemblyProviderMirror{Namespace: namespace, Sources: sources})
	}

	return mirrors
}

func stringListValue(values []string) cty.Value {
	items := make([]cty.Value, 0, len(values))
	for _, value := range values {
		items = append(items, cty.StringVal(value))
	}

	return cty.ListVal(items)
}

func assemblyEnvironment(home, temp, data, cliConfig string) []string {
	environment := []string{
		"HOME=" + home,
		"TMPDIR=" + temp,
		"TF_DATA_DIR=" + data,
		"TF_CLI_CONFIG_FILE=" + cliConfig,
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
		"CHECKPOINT_DISABLE=1",
	}

	for _, name := range []string{"PATH", "SSL_CERT_DIR"} {
		if value, exists := os.LookupEnv(name); exists {
			environment = append(environment, name+"="+value)
		}
	}

	if *opendepotAssemblyValidationCACertPath != "" {
		environment = append(environment, "SSL_CERT_FILE="+*opendepotAssemblyValidationCACertPath)
	} else if value, exists := os.LookupEnv("SSL_CERT_FILE"); exists {
		environment = append(environment, "SSL_CERT_FILE="+value)
	}

	return environment
}

func runAssemblyCommand(parent context.Context, directory string, environment []string, phase string, timeout time.Duration, progress assemblyProgressFunc, arguments ...string) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	command := exec.CommandContext(ctx, *opendepotTofuBinPath, arguments...)
	command.Dir = directory
	command.Env = environment

	output := &boundedCommandOutput{limit: *opendepotAssemblyMaxOutputBytes, lineLimit: int(*opendepotAssemblyMaxOutputBytes), phase: phase, progress: progress}
	command.Stdout = output
	command.Stderr = output
	if progress != nil {
		progress(phase, "$ tofu "+strings.Join(arguments, " "))
	}

	err := command.Run()
	output.Finish()
	if err != nil {
		return &assemblyCommandError{
			Phase:   phase,
			Output:  output.String(),
			Timeout: ctx.Err() == context.DeadlineExceeded,
			Err:     err,
		}
	}

	return nil
}

type boundedCommandOutput struct {
	buffer        bytes.Buffer
	limit         int64
	phase         string
	line          strings.Builder
	lineLimit     int
	lineTruncated bool
	progressBytes int64
	progress      assemblyProgressFunc
	mutex         sync.Mutex
}

func (w *boundedCommandOutput) Write(value []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	originalLength := len(value)
	remaining := w.limit - int64(w.buffer.Len())
	if remaining > 0 {
		if int64(len(value)) > remaining {
			value = value[:remaining]
		}
		_, _ = w.buffer.Write(value)
	}
	w.writeProgress(value)

	return originalLength, nil
}

func (w *boundedCommandOutput) String() string {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	w.flushProgress()
	value := strings.TrimSpace(w.buffer.String())
	if int64(w.buffer.Len()) >= w.limit {
		value += "\n[output truncated]"
	}

	return value
}

func (w *boundedCommandOutput) Finish() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	w.flushProgress()
}

func (w *boundedCommandOutput) writeProgress(value []byte) {
	if w.progress == nil {
		return
	}
	remaining := w.limit - w.progressBytes
	if remaining <= 0 {
		return
	}
	if int64(len(value)) > remaining {
		value = value[:remaining]
	}
	w.progressBytes += int64(len(value))
	if w.lineLimit == 0 {
		w.lineLimit = int(w.limit)
	}

	for _, character := range string(value) {
		if character == '\n' {
			w.flushProgress()

			continue
		}
		if w.lineTruncated {
			continue
		}
		if w.line.Len() >= w.lineLimit {
			w.lineTruncated = true
			w.line.WriteString("[output line truncated]")

			continue
		}
		w.line.WriteRune(character)
	}
}

func (w *boundedCommandOutput) flushProgress() {
	if w.progress == nil || w.line.Len() == 0 {
		return
	}

	w.progress(w.phase, strings.TrimSuffix(w.line.String(), "\r"))
	w.line.Reset()
	w.lineTruncated = false
}

func packageAssembly(files assemblyexport.Files) ([]byte, error) {
	var output bytes.Buffer
	archive := zip.NewWriter(&output)

	for _, file := range []struct {
		name string
		body []byte
	}{
		{name: "main.tf", body: files.Main},
		{name: "variables.tf", body: files.Variables},
	} {
		writer, err := archive.CreateHeader(&zip.FileHeader{Name: file.name, Method: zip.Deflate})
		if err != nil {
			return nil, fmt.Errorf("create ZIP entry %s: %w", file.name, err)
		}
		if _, err := io.Copy(writer, bytes.NewReader(file.body)); err != nil {
			return nil, fmt.Errorf("write ZIP entry %s: %w", file.name, err)
		}
	}

	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("close Assembly ZIP: %w", err)
	}

	archiveBytes := output.Bytes()
	if err := validateAssemblyArchive(archiveBytes, files); err != nil {
		return nil, fmt.Errorf("validate Assembly ZIP: %w", err)
	}

	return archiveBytes, nil
}

func validateAssemblyArchive(archiveBytes []byte, files assemblyexport.Files) error {
	expected := []struct {
		name string
		body []byte
	}{
		{name: "main.tf", body: files.Main},
		{name: "variables.tf", body: files.Variables},
	}

	reader, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	if len(reader.File) != len(expected) {
		return fmt.Errorf("archive contains %d entries, want %d", len(reader.File), len(expected))
	}

	for index, file := range reader.File {
		if file.Name != expected[index].name {
			return fmt.Errorf("archive entry %d is %q, want %q", index, file.Name, expected[index].name)
		}

		entry, err := file.Open()
		if err != nil {
			return fmt.Errorf("open archive entry %s: %w", file.Name, err)
		}
		body, readErr := io.ReadAll(entry)
		closeErr := entry.Close()
		if readErr != nil {
			return fmt.Errorf("read archive entry %s: %w", file.Name, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close archive entry %s: %w", file.Name, closeErr)
		}
		if !bytes.Equal(body, expected[index].body) {
			return fmt.Errorf("archive entry %s content does not match generated file", file.Name)
		}
	}

	return nil
}
