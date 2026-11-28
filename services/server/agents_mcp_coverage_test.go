package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"k8s.io/client-go/rest"
)

func TestAgentMCPServerImageDownloadUnavailableWithoutArtifact(t *testing.T) {
	version := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, mcpImageSourceType)
	noDigest := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, mcpImageSourceType)
	noDigest.Status.MCPMetadata.PackageDigest = ""
	noMetadata := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, mcpImageSourceType)
	noMetadata.Status.MCPMetadata = nil

	tests := []struct {
		name    string
		version *opendepotv1alpha1.Version
	}{
		{name: "artifact not created yet", version: version},
		{name: "package digest not yet published", version: noDigest},
		{name: "metadata not yet synced", version: noMetadata},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api, _ := agentTestAPIServer(t, map[string][]byte{
				agentMCPTestVersionURL(): agentTestJSON(t, tc.version),
			})
			agentTestUseKubeconfigAuth(t)

			originalInClusterConfig := inClusterConfig
			inClusterConfig = func() (*rest.Config, error) {
				return &rest.Config{Host: api.URL}, nil
			}
			t.Cleanup(func() { inClusterConfig = originalInClusterConfig })

			response := agentTestMCPDownload(t, api, agentMCPTestDownload)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("download status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
			}
		})
	}
}

func TestAgentMCPServerImageDownloadArtifactReadFailure(t *testing.T) {
	version := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, mcpImageSourceType)
	versionBody := agentTestJSON(t, version)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == agentMCPTestVersionURL() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(versionBody)

			return
		}

		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(api.Close)
	agentTestUseKubeconfigAuth(t)

	originalInClusterConfig := inClusterConfig
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}
	t.Cleanup(func() { inClusterConfig = originalInClusterConfig })

	response := agentTestMCPDownload(t, api, agentMCPTestDownload)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("download status = %d, want %d: %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
}

func TestAgentVersionSourceNameAndArchiveGuards(t *testing.T) {
	mcp := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, agentMCPTestPyPI)
	mcpNoRef := mcp.DeepCopy()
	mcpNoRef.Spec.MCPServerSourceRef = nil

	agent := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotAgent},
	}
	agentName := "skill-agent"
	agentWithRef := agent.DeepCopy()
	agentWithRef.Spec.AgentSourceRef = &opendepotv1alpha1.AgentSourceConfig{Name: &agentName}

	if name := agentVersionSourceName(mcpNoRef); name != nil {
		t.Fatalf("MCPServer without a source ref name = %q, want nil", *name)
	}

	if name := agentVersionSourceName(mcp); name == nil || *name != agentMCPTestWeather {
		t.Fatalf("MCPServer source name = %v, want %q", name, agentMCPTestWeather)
	}

	if name := agentVersionSourceName(agent); name != nil {
		t.Fatalf("Agent without a source ref name = %q, want nil", *name)
	}

	if name := agentVersionSourceName(agentWithRef); name == nil || *name != agentName {
		t.Fatalf("Agent source name = %v, want %q", name, agentName)
	}

	if got := agentArchiveVersion(agent); got != agent {
		t.Fatal("agentArchiveVersion must return non-MCPServer versions unchanged")
	}

	if got := agentArchiveVersion(mcpNoRef); got != mcpNoRef {
		t.Fatal("agentArchiveVersion must return MCPServer versions without a source ref unchanged")
	}

	if isMCPImageVersion(mcpNoRef) || isMCPImageVersion(agent) {
		t.Fatal("isMCPImageVersion must be false without an MCPServer image source")
	}
}

func TestApplyAgentCatalogMCPFieldsSelectsVersion(t *testing.T) {
	latest := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, agentMCPTestPyPI)
	latest.Status.MCPMetadata.Transport = ""
	newest := agentTestMCPVersion(agentMCPTestVersionNewer, agentMCPTestWeather, agentMCPTestSource)
	noRef := latest.DeepCopy()
	noRef.Spec.MCPServerSourceRef = nil

	t.Run("no versions leaves the item unchanged", func(t *testing.T) {
		item := AgentCatalogItem{}
		applyAgentCatalogMCPFields(&item, nil, nil)

		if item.SourceType != "" || item.Transport != "" || item.Runnable != nil {
			t.Fatalf("item = %+v, want unchanged", item)
		}
	})

	t.Run("newest version is used when none is installable", func(t *testing.T) {
		item := AgentCatalogItem{}
		applyAgentCatalogMCPFields(&item, nil, newest)

		if item.SourceType != agentMCPTestSource {
			t.Fatalf("SourceType = %q, want %q", item.SourceType, agentMCPTestSource)
		}

		if item.Runnable == nil || *item.Runnable {
			t.Fatalf("Runnable = %v, want false", item.Runnable)
		}
	})

	t.Run("version without an MCPServer source ref leaves the item unchanged", func(t *testing.T) {
		item := AgentCatalogItem{}
		applyAgentCatalogMCPFields(&item, noRef, nil)

		if item.SourceType != "" || item.Runnable != nil {
			t.Fatalf("item = %+v, want unchanged", item)
		}
	})

	t.Run("empty synced transport keeps the source transport", func(t *testing.T) {
		item := AgentCatalogItem{}
		applyAgentCatalogMCPFields(&item, latest, nil)

		if item.Transport != agentMCPTestTransport {
			t.Fatalf("Transport = %q, want %q", item.Transport, agentMCPTestTransport)
		}
	})

	t.Run("version without synced metadata leaves runnable unset", func(t *testing.T) {
		noMetadata := latest.DeepCopy()
		noMetadata.Status.MCPMetadata = nil
		item := AgentCatalogItem{}
		applyAgentCatalogMCPFields(&item, noMetadata, nil)

		if item.Runnable != nil {
			t.Fatalf("Runnable = %v, want nil", item.Runnable)
		}
	})
}
