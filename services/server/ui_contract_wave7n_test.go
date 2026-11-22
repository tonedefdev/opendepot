package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func wave7nContractRequest() *http.Request {
	return covGRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/team/module/vpc/contract", map[string]string{
		"namespace": "team", "kind": "module", "name": "vpc",
	})
}

func wave7nContractStatus(t *testing.T, request *http.Request, want int) *httptest.ResponseRecorder {
	t.Helper()

	response := httptest.NewRecorder()
	handleBrowseContract(response, request)

	if response.Code != want {
		t.Fatalf("contract status = %d, want %d: %s", response.Code, want, response.Body.String())
	}

	return response
}

func TestWave7NBrowseContractFailurePaths(t *testing.T) {
	ref := &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "contract.json.gz", Grade: "full"}
	versions := []opendepotv1alpha1.Version{covGContractVersion("vpc-1-2-0", "1.2.0", ref)}
	data := map[string]string{"contract.json.gz": covGGzipBase64(t, `{"module":"vpc"}`)}

	t.Run("OIDC without bearer token is 401", func(t *testing.T) {
		api := covGAPIServer(t, covGContractRoutes(t, versions, data))
		covGUseCluster(t, api)
		agentTestOIDCAuth(t)

		wave7nContractStatus(t, wave7nContractRequest(), http.StatusUnauthorized)
	})

	t.Run("service account client failure is 500", func(t *testing.T) {
		api := covGAPIServer(t, covGContractRoutes(t, versions, data))
		covGUseCluster(t, api)
		inClusterConfig = func() (*rest.Config, error) {
			return nil, http.ErrNotSupported
		}

		wave7nContractStatus(t, wave7nContractRequest(), http.StatusInternalServerError)
	})

	t.Run("namespace read failure is 500", func(t *testing.T) {
		routes := covGContractRoutes(t, versions, data)
		routes[covGContractNamespacePath] = covGResponse{status: http.StatusInternalServerError, raw: `{"kind":"Status","code":500}`}
		covGUseCluster(t, covGAPIServer(t, routes))

		wave7nContractStatus(t, wave7nContractRequest(), http.StatusInternalServerError)
	})

	t.Run("version list failure is 500", func(t *testing.T) {
		routes := covGContractRoutes(t, versions, data)
		routes[covGContractVersionsPath] = covGResponse{status: http.StatusInternalServerError, raw: `{"kind":"Status","code":500}`}
		covGUseCluster(t, covGAPIServer(t, routes))

		wave7nContractStatus(t, wave7nContractRequest(), http.StatusInternalServerError)
	})
}

func TestWave7NBrowseContractDerivesLatestVersion(t *testing.T) {
	publicLabels := map[string]string{labelPublic: "true"}
	module := opendepotv1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: "vpc", Namespace: "team", Labels: publicLabels}}
	namespace := map[string]any{"metadata": map[string]any{"name": "team", "labels": publicLabels}}

	routes := covGContractRoutes(t, []opendepotv1alpha1.Version{
		covGContractVersion("vpc-1-1-0", "1.1.0", &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "v110", Grade: "full"}),
		covGContractVersion("vpc-1-2-0", "1.2.0", &opendepotv1alpha1.ContractConfigMapRef{Name: "vpc-contract", Key: "v120", Grade: "full"}),
	}, map[string]string{
		"v110": covGGzipBase64(t, `{"version":"1.1.0"}`),
		"v120": covGGzipBase64(t, `{"version":"1.2.0"}`),
	})
	routes[covGContractModulePath] = covGResponse{body: module}
	routes[covGContractNamespacePath] = covGResponse{body: namespace}
	covGUseCluster(t, covGAPIServer(t, routes))

	response := wave7nContractStatus(t, wave7nContractRequest(), http.StatusOK)
	if body := response.Body.String(); body != `{"version":"1.2.0"}` {
		t.Fatalf("contract body = %q, want 1.2.0 contract", body)
	}
}
