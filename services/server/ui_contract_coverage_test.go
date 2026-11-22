package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	covGContractModulePath    = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/vpc"
	covGContractVersionsPath  = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"
	covGContractNamespacePath = "/api/v1/namespaces/team"
	covGContractConfigMapPath = "/api/v1/namespaces/team/configmaps/vpc-contract"
)

// covGGzipBase64 returns the gzip-then-base64 encoding used for stored contract payloads.
func covGGzipBase64(t *testing.T, plain string) string {
	t.Helper()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(plain)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

func covGContractVersion(name, version string, ref *opendepotv1alpha1.ContractConfigMapRef) opendepotv1alpha1.Version {
	v := opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team"}}
	moduleName := "vpc"
	v.Spec.Version = version
	v.Spec.ModuleConfigRef = &opendepotv1alpha1.ModuleConfig{Name: &moduleName}
	v.Status.ContractConfigMapRef = ref

	return v
}

func covGContractRoutes(t *testing.T, versions []opendepotv1alpha1.Version, configMapData map[string]string) map[string]covGResponse {
	t.Helper()

	module := opendepotv1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: "vpc", Namespace: "team"}}
	latest := "1.2.0"
	module.Status.LatestVersion = &latest

	return map[string]covGResponse{
		covGContractModulePath:    {body: module},
		covGContractNamespacePath: {body: map[string]any{"metadata": map[string]any{"name": "team"}}},
		covGContractVersionsPath:  {body: opendepotv1alpha1.VersionList{Items: versions}},
		covGContractConfigMapPath: {body: map[string]any{"metadata": map[string]any{"name": "vpc-contract"}, "data": configMapData}},
	}
}

func TestCovGBrowseContractServesLatestContract(t *testing.T) {
	const contractJSON = `{"module":"vpc","grade":"full"}`
	ref := &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "contract.json.gz", Grade: "full"}
	versions := []opendepotv1alpha1.Version{
		covGContractVersion("vpc-1-1-0", "1.1.0", &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "old", Grade: "full"}),
		covGContractVersion("vpc-1-2-0", "1.2.0", ref),
	}
	api := covGAPIServer(t, covGContractRoutes(t, versions, map[string]string{"contract.json.gz": covGGzipBase64(t, contractJSON)}))
	covGUseCluster(t, api)

	request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract", map[string]string{
		"namespace": "team", "kind": "module", "name": "vpc",
	})
	response := httptest.NewRecorder()
	handleBrowseContract(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("contract status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	if body := response.Body.String(); body != contractJSON {
		t.Fatalf("contract body = %q, want %q", body, contractJSON)
	}
}

func TestCovGBrowseContractSelectsRequestedVersion(t *testing.T) {
	versions := []opendepotv1alpha1.Version{
		covGContractVersion("vpc-1-1-0", "1.1.0", &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "v110", Grade: "full"}),
		covGContractVersion("vpc-1-2-0", "1.2.0", &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "v120", Grade: "full"}),
	}
	data := map[string]string{
		"v110": covGGzipBase64(t, `{"version":"1.1.0"}`),
		"v120": covGGzipBase64(t, `{"version":"1.2.0"}`),
	}
	api := covGAPIServer(t, covGContractRoutes(t, versions, data))
	covGUseCluster(t, api)

	request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract?version=v1.1.0", map[string]string{
		"namespace": "team", "kind": "module", "name": "vpc",
	})
	response := httptest.NewRecorder()
	handleBrowseContract(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("contract status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var got map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("contract body is not JSON: %v", err)
	}

	if got["version"] != "1.1.0" {
		t.Fatalf("contract version = %q, want 1.1.0", got["version"])
	}
}

func TestCovGBrowseContractRejectsNonModuleKind(t *testing.T) {
	request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/team/provider/aws/contract", map[string]string{
		"namespace": "team", "kind": "provider", "name": "aws",
	})
	response := httptest.NewRecorder()
	handleBrowseContract(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("contract status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestCovGBrowseContractErrorPaths(t *testing.T) {
	tests := []struct {
		name       string
		routes     func(t *testing.T) map[string]covGResponse
		target     string
		wantStatus int
		wantError  string
	}{
		{
			name: "module not found",
			routes: func(t *testing.T) map[string]covGResponse {
				return map[string]covGResponse{}
			},
			target:     "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "module payload is not JSON",
			routes: func(t *testing.T) map[string]covGResponse {
				return map[string]covGResponse{covGContractModulePath: {raw: "not-json"}}
			},
			target:     "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "module server error",
			routes: func(t *testing.T) map[string]covGResponse {
				return map[string]covGResponse{covGContractModulePath: {status: http.StatusInternalServerError, raw: `{"kind":"Status","code":500}`}}
			},
			target:     "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "no version carries a contract reference",
			routes: func(t *testing.T) map[string]covGResponse {
				return covGContractRoutes(t, []opendepotv1alpha1.Version{covGContractVersion("vpc-1-2-0", "1.2.0", nil)}, nil)
			},
			target:     "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract",
			wantStatus: http.StatusNotFound,
			wantError:  "contract_unavailable",
		},
		{
			name: "configmap is missing",
			routes: func(t *testing.T) map[string]covGResponse {
				ref := &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "contract.json.gz", Grade: "full"}
				routes := covGContractRoutes(t, []opendepotv1alpha1.Version{covGContractVersion("vpc-1-2-0", "1.2.0", ref)}, nil)
				delete(routes, covGContractConfigMapPath)

				return routes
			},
			target:     "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract",
			wantStatus: http.StatusNotFound,
			wantError:  "contract_unavailable",
		},
		{
			name: "configmap has no contract key",
			routes: func(t *testing.T) map[string]covGResponse {
				ref := &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "contract.json.gz", Grade: "full"}

				return covGContractRoutes(t, []opendepotv1alpha1.Version{covGContractVersion("vpc-1-2-0", "1.2.0", ref)}, map[string]string{"other": "x"})
			},
			target:     "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract",
			wantStatus: http.StatusNotFound,
			wantError:  "contract_unavailable",
		},
		{
			name: "configmap payload is not base64 gzip",
			routes: func(t *testing.T) map[string]covGResponse {
				ref := &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "contract.json.gz", Grade: "full"}

				return covGContractRoutes(t, []opendepotv1alpha1.Version{covGContractVersion("vpc-1-2-0", "1.2.0", ref)}, map[string]string{"contract.json.gz": "!!!not-base64!!!"})
			},
			target:     "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract",
			wantStatus: http.StatusNotFound,
			wantError:  "contract_unavailable",
		},
		{
			name: "configmap read fails",
			routes: func(t *testing.T) map[string]covGResponse {
				ref := &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "contract.json.gz", Grade: "full"}
				routes := covGContractRoutes(t, []opendepotv1alpha1.Version{covGContractVersion("vpc-1-2-0", "1.2.0", ref)}, nil)
				routes[covGContractConfigMapPath] = covGResponse{status: http.StatusInternalServerError, raw: `{"kind":"Status","code":500}`}

				return routes
			},
			target:     "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract",
			wantStatus: http.StatusNotFound,
			wantError:  "contract_unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := covGAPIServer(t, tc.routes(t))
			covGUseCluster(t, api)

			request := covGRequest(http.MethodGet, tc.target, map[string]string{"namespace": "team", "kind": "module", "name": "vpc"})
			response := httptest.NewRecorder()
			handleBrowseContract(response, request)

			if response.Code != tc.wantStatus {
				t.Fatalf("contract status = %d, want %d: %s", response.Code, tc.wantStatus, response.Body.String())
			}

			if tc.wantError == "" {
				return
			}

			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("error body is not JSON: %v", err)
			}

			if body["error"] != tc.wantError {
				t.Fatalf("error = %q, want %q", body["error"], tc.wantError)
			}
		})
	}
}

func TestCovGDecodeContractRoundTrip(t *testing.T) {
	got, err := decodeContract(covGGzipBase64(t, "hello"))
	if err != nil {
		t.Fatalf("decodeContract() error = %v", err)
	}

	if string(got) != "hello" {
		t.Fatalf("decodeContract() = %q, want hello", got)
	}

	if _, err := decodeContract("%%%"); err == nil {
		t.Fatal("decodeContract() accepted invalid base64")
	}

	if _, err := decodeContract(base64.StdEncoding.EncodeToString([]byte("plain text"))); err == nil {
		t.Fatal("decodeContract() accepted payload that is not gzip")
	}
}

func TestCovGWriteContractUnavailable(t *testing.T) {
	response := httptest.NewRecorder()
	writeContractUnavailable(response, "1.2.0")

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}

	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}

	if body["error"] != "contract_unavailable" || body["version"] != "1.2.0" {
		t.Fatalf("body = %#v, want error contract_unavailable and version 1.2.0", body)
	}
}
