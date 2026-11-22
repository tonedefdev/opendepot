package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type wave7nReply struct {
	status int
	body   string
}

// wave7nModuleCluster serves the given paths from a fake API server, answers the GroupBinding list from bindings,
// and points inClusterConfig at it. OIDC mode is enabled when oidc is true, anonymous mode otherwise.
func wave7nModuleCluster(t *testing.T, oidc bool, routes map[string]wave7nReply, bindings []opendepotv1alpha1.GroupBinding) {
	t.Helper()

	bindingPayload := agentTestJSON(t, opendepotv1alpha1.GroupBindingList{Items: bindings})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/groupbindings") {
			_, _ = w.Write(bindingPayload)
			return
		}

		reply, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`))
			return
		}

		status := reply.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(api.Close)

	original := inClusterConfig
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}
	t.Cleanup(func() { inClusterConfig = original })

	if oidc {
		agentTestOIDCAuth(t)
		return
	}

	covGUseCluster(t, api)
}

func wave7nModuleStatus(reason string, code int) string {
	payload, _ := json.Marshal(map[string]any{
		"kind":       "Status",
		"apiVersion": "v1",
		"status":     "Failure",
		"reason":     reason,
		"code":       code,
	})

	return string(payload)
}

func wave7nModuleBinding(name, expression string, moduleResources []string) opendepotv1alpha1.GroupBinding {
	return opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:      expression,
			ModuleResources: moduleResources,
		},
	}
}

func TestWave7NModuleVersionAndDownloadFailures(t *testing.T) {
	directory := t.TempDir()
	covGModuleArchive(t, directory, "kms", "module.zip", "terraform-aws-kms-abc")
	version := covGModuleVersion(t, directory, "kms", "1.2.0", "module.zip", "Y2hlY2tzdW0=")
	versionBody := agentTestJSON(t, version)

	noConfigName := version
	noConfigName.Name = "kms-1-6-0"
	noConfigName.Spec.ModuleConfigRef = &opendepotv1alpha1.ModuleConfig{
		StorageConfig: version.Spec.ModuleConfigRef.StorageConfig,
	}
	noConfigNameBody := agentTestJSON(t, noConfigName)

	wave7nModuleCluster(t, false, map[string]wave7nReply{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-2-0": {body: string(versionBody)},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-5-0": {body: "not-json"},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-6-0": {body: string(noConfigNameBody)},
	}, nil)

	params := func(version string) map[string]string {
		return map[string]string{"namespace": "team", "name": "kms", "version": version}
	}

	t.Run("malformed version body is internal error", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, covGRequest(http.MethodGet, "http://registry.example.com/", params("1.5.0")))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("unresolvable download path is unavailable", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, covGRequest(http.MethodGet, "http://registry.example.com/", params("1.6.0")))

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
	})

	t.Run("missing version leaves no redirect", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, covGRequest(http.MethodGet, "http://registry.example.com/", params("9.9.9")))

		if header := response.Header().Get("X-Terraform-Get"); header != "" {
			t.Fatalf("X-Terraform-Get = %q, want none for a missing version", header)
		}
	})
}

func TestWave7NModuleDownloadGroupBindingDecisions(t *testing.T) {
	directory := t.TempDir()
	covGModuleArchive(t, directory, "kms", "module.zip", "terraform-aws-kms-abc")
	version := covGModuleVersion(t, directory, "kms", "1.2.0", "module.zip", covGChecksumFor([]byte("module")))
	routes := map[string]wave7nReply{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-2-0": {body: string(agentTestJSON(t, version))},
	}

	bindings := []opendepotv1alpha1.GroupBinding{
		wave7nModuleBinding("platform", `"platform" in groups`, []string{"other-*"}),
	}
	wave7nModuleCluster(t, true, routes, bindings)

	request := func(token string) *http.Request {
		req := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "name": "kms", "version": "1.2.0"})
		req.Header.Set("Authorization", "Bearer "+token)

		return req
	}

	t.Run("binding without module access is forbidden", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, request(agentTestBearerToken(t, []string{"platform"})))

		if response.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
		}
	})

	t.Run("binding with module access redirects", func(t *testing.T) {
		allowed := []opendepotv1alpha1.GroupBinding{
			wave7nModuleBinding("platform", `"platform" in groups`, []string{"kms*"}),
		}
		wave7nModuleCluster(t, true, routes, allowed)

		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, request(agentTestBearerToken(t, []string{"platform"})))

		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
		}
	})
}

func TestWave7NGetModuleVersionsResponses(t *testing.T) {
	module := opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "kms", Namespace: "team"},
		Spec: opendepotv1alpha1.ModuleSpec{
			Versions: []opendepotv1alpha1.ModuleVersion{{Version: "1.2.0"}},
		},
	}

	wave7nModuleCluster(t, false, map[string]wave7nReply{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/kms":        {body: string(agentTestJSON(t, module))},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/malformed":  {body: "not-json"},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/forbidden":  {status: http.StatusForbidden, body: wave7nModuleStatus("Forbidden", http.StatusForbidden)},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/unexpected": {status: http.StatusInternalServerError, body: wave7nModuleStatus("InternalError", http.StatusInternalServerError)},
	}, nil)

	versionsFor := func(name string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		getModuleVersions(response, covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "name": name, "system": "aws"}))

		return response
	}

	t.Run("lists module versions", func(t *testing.T) {
		response := versionsFor("kms")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		var body ModuleVersionsResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}

		if len(body.Modules) != 1 || len(body.Modules[0].Versions) != 1 || body.Modules[0].Versions[0].Version != "1.2.0" {
			t.Fatalf("versions body = %s, want one 1.2.0 version", response.Body.String())
		}
	})

	t.Run("missing module is not found", func(t *testing.T) {
		response := versionsFor("absent")
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("forbidden module is forbidden", func(t *testing.T) {
		response := versionsFor("forbidden")
		if response.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
		}
	})

	t.Run("other api failure is internal error", func(t *testing.T) {
		response := versionsFor("unexpected")
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("malformed module body is internal error", func(t *testing.T) {
		response := versionsFor("malformed")
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NGetModuleVersionsGroupBindingDecisions(t *testing.T) {
	module := opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "kms", Namespace: "team"},
		Spec: opendepotv1alpha1.ModuleSpec{
			Versions: []opendepotv1alpha1.ModuleVersion{{Version: "1.2.0"}},
		},
	}
	routes := map[string]wave7nReply{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/kms": {body: string(agentTestJSON(t, module))},
	}

	request := func(token string) *httptest.ResponseRecorder {
		req := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "name": "kms", "system": "aws"})
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		getModuleVersions(response, req)

		return response
	}

	t.Run("binding without module access is forbidden", func(t *testing.T) {
		wave7nModuleCluster(t, true, routes, []opendepotv1alpha1.GroupBinding{
			wave7nModuleBinding("platform", `"platform" in groups`, []string{"other-*"}),
		})

		response := request(agentTestBearerToken(t, []string{"platform"}))
		if response.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
		}
	})

	t.Run("binding with module access lists versions", func(t *testing.T) {
		wave7nModuleCluster(t, true, routes, []opendepotv1alpha1.GroupBinding{
			wave7nModuleBinding("platform", `"platform" in groups`, []string{"kms"}),
		})

		response := request(agentTestBearerToken(t, []string{"platform"}))
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
	})
}

func TestWave7NModuleArchiveWrapperErrors(t *testing.T) {
	zipWithEntry := func(t *testing.T, entryName string) []byte {
		t.Helper()

		var buf bytes.Buffer
		writer := zip.NewWriter(&buf)
		entry, err := writer.Create(entryName)
		if err != nil {
			t.Fatalf("zip.Create() error = %v", err)
		}
		if _, err = entry.Write([]byte("x")); err != nil {
			t.Fatalf("zip entry write error = %v", err)
		}
		if err = writer.Close(); err != nil {
			t.Fatalf("zip.Close() error = %v", err)
		}

		return buf.Bytes()
	}

	tarWithEntry := func(t *testing.T, entryName string) []byte {
		t.Helper()

		var archive bytes.Buffer
		gzipWriter := gzip.NewWriter(&archive)
		tarWriter := tar.NewWriter(gzipWriter)
		if err := tarWriter.WriteHeader(&tar.Header{Name: entryName, Typeflag: tar.TypeReg, Mode: 0600, Size: 0}); err != nil {
			t.Fatalf("tar.WriteHeader() error = %v", err)
		}
		if err := tarWriter.Close(); err != nil {
			t.Fatalf("tar.Close() error = %v", err)
		}
		if err := gzipWriter.Close(); err != nil {
			t.Fatalf("gzip.Close() error = %v", err)
		}

		return archive.Bytes()
	}

	shortHeader := make([]byte, 10)

	badSignature := make([]byte, 30)
	binary.LittleEndian.PutUint32(badSignature[:4], 0x12345678)

	truncatedName := make([]byte, 30)
	binary.LittleEndian.PutUint32(truncatedName[:4], 0x04034b50)
	binary.LittleEndian.PutUint16(truncatedName[26:28], 16)

	var emptyTar bytes.Buffer
	emptyGzip := gzip.NewWriter(&emptyTar)
	if err := tar.NewWriter(emptyGzip).Close(); err != nil {
		t.Fatalf("tar.Close() error = %v", err)
	}
	if err := emptyGzip.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}

	tests := []struct {
		name        string
		archiveType string
		archive     []byte
	}{
		{name: "zip header too short", archiveType: "zip", archive: shortHeader},
		{name: "zip signature invalid", archiveType: "zip", archive: badSignature},
		{name: "zip entry name truncated", archiveType: "zip", archive: truncatedName},
		{name: "zip entry without wrapper directory", archiveType: "zip", archive: zipWithEntry(t, "main.tf")},
		{name: "zip entry escaping root", archiveType: "zip", archive: zipWithEntry(t, "../escape/main.tf")},
		{name: "tar.gz not gzip", archiveType: "tar.gz", archive: []byte("plain")},
		{name: "tar.gz empty archive", archiveType: "tar.gz", archive: emptyTar.Bytes()},
		{name: "tar.gz entry without wrapper directory", archiveType: "tar.gz", archive: tarWithEntry(t, "main.tf")},
		{name: "unsupported archive type", archiveType: "rar", archive: []byte("rar")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := moduleArchiveWrapper(bytes.NewReader(tt.archive), tt.archiveType); err == nil {
				t.Fatal("moduleArchiveWrapper() error = nil, want error")
			}
		})
	}
}
