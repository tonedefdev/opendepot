package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	covGAssemblyModuleBody   = `{"schemaVersion":"assembly.v1","modules":[{"nodeId":"m1","namespace":"team","name":"kms","version":"1.0.0","localName":"kms"}]}`
	covGAssemblyProviderBody = `{"schemaVersion":"assembly.v1","providers":[{"nodeId":"p1","namespace":"team","name":"aws","version":"5.1.0","localName":"aws"}]}`
)

// covGAssemblyBaseRoutes serves the namespace, a public-enough module, and an OpenTofu provider named aws.
func covGAssemblyBaseRoutes(providerConfig opendepotv1alpha1.ProviderConfig) map[string]covGResponse {
	return map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/kms": {body: opendepotv1alpha1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "kms", Namespace: "team"},
		}},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers/aws": {body: opendepotv1alpha1.Provider{
			ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "team"},
			Spec: opendepotv1alpha1.ProviderSpec{
				ProviderConfig: providerConfig,
			},
		}},
		"/api/v1/namespaces/team": {body: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata":   map[string]any{"name": "team"},
		}},
	}
}

func covGAssemblyOpenTofuConfig() opendepotv1alpha1.ProviderConfig {
	return opendepotv1alpha1.ProviderConfig{UpstreamRegistry: covGStringPtr(opendepotv1alpha1.OpenTofuRegistryHost)}
}

func covGAssemblyModuleVersion(synced bool, contractKey string) opendepotv1alpha1.Version {
	return opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "kms-1-0-0", Namespace: "team"},
		Spec: opendepotv1alpha1.VersionSpec{
			Version:         "1.0.0",
			ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{Name: covGStringPtr("kms")},
		},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:               synced,
			ContractConfigMapRef: &opendepotv1alpha1.ContractConfigMapRef{Name: "kms-contract", Key: contractKey},
		},
	}
}

func covGAssemblyProviderVersion(configName string, storage *opendepotv1alpha1.StorageConfig, schemaRef *opendepotv1alpha1.ProviderSchemaRef) opendepotv1alpha1.Version {
	return opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "aws-5-1-0", Namespace: "team"},
		Spec: opendepotv1alpha1.VersionSpec{
			Version: "5.1.0",
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{
				Name:          covGStringPtr(configName),
				StorageConfig: storage,
			},
		},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:            true,
			ProviderSchemaRef: schemaRef,
		},
	}
}

func covGAssemblyRun(t *testing.T, routes map[string]covGResponse, body string) *httptest.ResponseRecorder {
	t.Helper()

	previousMaxBytes := opendepotAssemblyMaxRequestBytes
	previousMaxNodes := opendepotAssemblyMaxNodes
	previousAnonymous := opendepotAnonymousAuth
	previousRegistryHost := opendepotRegistryHost
	t.Cleanup(func() {
		opendepotAssemblyMaxRequestBytes = previousMaxBytes
		opendepotAssemblyMaxNodes = previousMaxNodes
		opendepotAnonymousAuth = previousAnonymous
		opendepotRegistryHost = previousRegistryHost
	})

	maxBytes := int64(2 << 20)
	maxNodes := 100
	anonymous := true
	registryHost := "registry.example.com"
	opendepotAssemblyMaxRequestBytes = &maxBytes
	opendepotAssemblyMaxNodes = &maxNodes
	opendepotAnonymousAuth = &anonymous
	opendepotRegistryHost = &registryHost

	api := covGAPIServer(t, routes)
	covGUseCluster(t, api)

	response := httptest.NewRecorder()
	handleAssemblyExport(response, covGAssemblyExportRequest(t, body, nil))

	return response
}

// covGAssemblyExpectDiagnostic asserts an invalid_canvas response carrying the given diagnostic code.
func covGAssemblyExpectDiagnostic(t *testing.T, response *httptest.ResponseRecorder, code, nodeID string) {
	t.Helper()

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}

	decoded := covGDecodeAssemblyError(t, response)
	if decoded.Error != "invalid_canvas" {
		t.Fatalf("error = %q, want invalid_canvas", decoded.Error)
	}

	for _, diagnostic := range decoded.Diagnostics {
		if diagnostic.Code == code && diagnostic.NodeID == nodeID {
			return
		}
	}

	t.Fatalf("diagnostics = %+v, want %s for %s", decoded.Diagnostics, code, nodeID)
}

func TestCovGAssemblyModuleContractFailures(t *testing.T) {
	versions := opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{covGAssemblyModuleVersion(true, "contract.json")}}

	t.Run("contract configmap is missing", func(t *testing.T) {
		routes := covGAssemblyBaseRoutes(covGAssemblyOpenTofuConfig())
		routes["/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"] = covGResponse{body: versions}

		covGAssemblyExpectDiagnostic(t, covGAssemblyRun(t, routes, covGAssemblyModuleBody), "module_contract_unavailable", "m1")
	})

	t.Run("contract key is absent from the configmap", func(t *testing.T) {
		routes := covGAssemblyBaseRoutes(covGAssemblyOpenTofuConfig())
		routes["/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"] = covGResponse{body: versions}
		routes["/api/v1/namespaces/team/configmaps/kms-contract"] = covGResponse{body: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"data":       map[string]string{"other.json": covGGzipBase64(t, `{}`)},
		}}

		covGAssemblyExpectDiagnostic(t, covGAssemblyRun(t, routes, covGAssemblyModuleBody), "module_contract_unavailable", "m1")
	})

	t.Run("contract value is not base64 gzip", func(t *testing.T) {
		routes := covGAssemblyBaseRoutes(covGAssemblyOpenTofuConfig())
		routes["/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"] = covGResponse{body: versions}
		routes["/api/v1/namespaces/team/configmaps/kms-contract"] = covGResponse{body: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"data":       map[string]string{"contract.json": "%%%not-base64%%%"},
		}}

		covGAssemblyExpectDiagnostic(t, covGAssemblyRun(t, routes, covGAssemblyModuleBody), "module_contract_unavailable", "m1")
	})

	t.Run("decoded contract is not JSON", func(t *testing.T) {
		routes := covGAssemblyBaseRoutes(covGAssemblyOpenTofuConfig())
		routes["/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"] = covGResponse{body: versions}
		routes["/api/v1/namespaces/team/configmaps/kms-contract"] = covGResponse{body: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"data":       map[string]string{"contract.json": covGGzipBase64(t, "not json")},
		}}

		covGAssemblyExpectDiagnostic(t, covGAssemblyRun(t, routes, covGAssemblyModuleBody), "module_contract_unavailable", "m1")
	})

	t.Run("version list failure is a module_unavailable diagnostic", func(t *testing.T) {
		routes := covGAssemblyBaseRoutes(covGAssemblyOpenTofuConfig())

		covGAssemblyExpectDiagnostic(t, covGAssemblyRun(t, routes, covGAssemblyModuleBody), "module_unavailable", "m1")
	})
}

func TestCovGAssemblyProviderSchemaFailures(t *testing.T) {
	t.Run("upstream name and namespace overrides select the matching versions", func(t *testing.T) {
		config := covGAssemblyOpenTofuConfig()
		config.Name = covGStringPtr("aws-upstream")
		config.Namespace = covGStringPtr("hashicorp-labs")

		// The version carries no schema reference, so it is found but cannot be used.
		versions := opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{
			covGAssemblyProviderVersion("aws-upstream", nil, nil),
		}}

		routes := covGAssemblyBaseRoutes(config)
		routes["/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"] = covGResponse{body: versions}

		covGAssemblyExpectDiagnostic(t, covGAssemblyRun(t, routes, covGAssemblyProviderBody), "provider_schema_unavailable", "p1")
	})

	t.Run("version without a provider config reference is not selected", func(t *testing.T) {
		versions := opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "aws-5-1-0", Namespace: "team"},
				Spec:       opendepotv1alpha1.VersionSpec{Version: "5.1.0"},
				Status:     opendepotv1alpha1.VersionStatus{Synced: true},
			},
		}}

		routes := covGAssemblyBaseRoutes(covGAssemblyOpenTofuConfig())
		routes["/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"] = covGResponse{body: versions}

		covGAssemblyExpectDiagnostic(t, covGAssemblyRun(t, routes, covGAssemblyProviderBody), "provider_schema_unavailable", "p1")
	})

	t.Run("unsupported storage configuration is reported as unavailable", func(t *testing.T) {
		versions := opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{
			covGAssemblyProviderVersion("aws", &opendepotv1alpha1.StorageConfig{}, &opendepotv1alpha1.ProviderSchemaRef{Key: "schema.json.gz", Digest: "sha256:abc"}),
		}}

		routes := covGAssemblyBaseRoutes(covGAssemblyOpenTofuConfig())
		routes["/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"] = covGResponse{body: versions}

		covGAssemblyExpectDiagnostic(t, covGAssemblyRun(t, routes, covGAssemblyProviderBody), "provider_schema_unavailable", "p1")
	})
}
