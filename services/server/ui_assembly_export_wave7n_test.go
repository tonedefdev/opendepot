package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonedefdev/opendepot/pkg/hclschema/assemblyexport"
)

type wave7nNoFlushWriter struct {
	http.ResponseWriter
}

func TestWave7NStreamAssemblyExportRequiresFlusher(t *testing.T) {
	recorder := httptest.NewRecorder()
	streamAssemblyExport(wave7nNoFlushWriter{ResponseWriter: recorder}, covGRequest(http.MethodPost, "/", nil), assemblyexport.Files{}, nil, "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}

	var response assemblyexport.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if response.Error != "streaming_unavailable" {
		t.Fatalf("error code = %q, want streaming_unavailable", response.Error)
	}
}

func TestWave7NStreamAssemblyExportEmitsErrorEvent(t *testing.T) {
	wave7nAssemblyFlags(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	recorder := httptest.NewRecorder()
	streamAssemblyExport(recorder, covGRequest(http.MethodPost, "/", nil).WithContext(ctx), assemblyexport.Files{}, nil, "")

	if got := recorder.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", got)
	}

	var events []assemblyStreamEvent
	scanner := bufio.NewScanner(bytes.NewReader(recorder.Body.Bytes()))
	for scanner.Scan() {
		var event assemblyStreamEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode event %q: %v", scanner.Text(), err)
		}

		events = append(events, event)
	}

	if len(events) < 2 || events[0].Type != "started" {
		t.Fatalf("events = %+v, want started followed by terminal event", events)
	}

	last := events[len(events)-1]
	if last.Type != "error" || last.Error == nil || last.Error.Error == "" {
		t.Fatalf("terminal event = %+v, want error with code", last)
	}
}

func wave7nAssemblyFlags(t *testing.T) {
	t.Helper()

	workDirectory := t.TempDir()
	tofuPath := filepath.Join(workDirectory, "missing-tofu")
	registryHost := "opendepot.example.com"
	validationRegistryURL := "https://opendepot.example.com"
	validationCACertPath := ""
	registryInsecure := false
	initTimeout := time.Minute
	outputLimit := int64(1024 * 1024)

	previousWorkDirectory := opendepotAssemblyWorkDir
	previousTofuPath := opendepotTofuBinPath
	previousRegistryHost := opendepotRegistryHost
	previousValidationRegistryURL := opendepotAssemblyValidationURL
	previousValidationCACertPath := opendepotAssemblyValidationCACertPath
	previousRegistryInsecure := opendepotAssemblyRegistryInsecure
	previousInitTimeout := opendepotAssemblyInitTimeout
	previousOutputLimit := opendepotAssemblyMaxOutputBytes

	opendepotAssemblyWorkDir = &workDirectory
	opendepotTofuBinPath = &tofuPath
	opendepotRegistryHost = &registryHost
	opendepotAssemblyValidationURL = &validationRegistryURL
	opendepotAssemblyValidationCACertPath = &validationCACertPath
	opendepotAssemblyRegistryInsecure = &registryInsecure
	opendepotAssemblyInitTimeout = &initTimeout
	opendepotAssemblyMaxOutputBytes = &outputLimit

	t.Cleanup(func() {
		opendepotAssemblyWorkDir = previousWorkDirectory
		opendepotTofuBinPath = previousTofuPath
		opendepotRegistryHost = previousRegistryHost
		opendepotAssemblyValidationURL = previousValidationRegistryURL
		opendepotAssemblyValidationCACertPath = previousValidationCACertPath
		opendepotAssemblyRegistryInsecure = previousRegistryInsecure
		opendepotAssemblyInitTimeout = previousInitTimeout
		opendepotAssemblyMaxOutputBytes = previousOutputLimit
	})
}
