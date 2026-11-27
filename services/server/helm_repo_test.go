package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hashicorp/golang-lru/v2/expirable"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/oci"
)

const (
	ociTestIndexPath       = "/opendepot/helm/v1/" + ociTestNamespace + "/index.yaml"
	ociTestChartsPath      = "/opendepot/helm/v1/" + ociTestNamespace + "/charts/"
	ociTestChartName       = "podinfo"
	ociTestChartTGZ        = "podinfo-1.0.0.tgz"
	ociTestChartProv       = "podinfo-1.0.0.prov"
	ociTestPodinfoArtifact = "podinfo-app"
	ociTestSecretArtifact  = "secret-app"
	ociTestHelmType        = "Helm"
	ociTestYAMLType        = "application/x-yaml"
	ociTestGzipType        = "application/gzip"
	ociTestProvType        = "text/plain; charset=utf-8"
)

// ociTestChartArchive returns a chart .tgz with a Chart.yaml that names the chart, its version, and its description.
func ociTestChartArchive(t *testing.T, name, version, description string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	files := []struct{ name, body string }{
		{
			name: name + "/Chart.yaml",
			body: fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\ndescription: %s\n", name, version, description),
		},
		{name: name + "/values.yaml", body: "replicas: 1\n"},
	}

	for _, file := range files {
		header := &tar.Header{Name: file.name, Mode: 0o600, Size: int64(len(file.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}

		if _, err := tw.Write([]byte(file.body)); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	return buf.Bytes()
}

// publishChart stores a Helm chart packaged by pkg/oci and adds its Version. prov is the optional provenance file.
func (env *ociTestEnv) publishChart(
	t *testing.T,
	artifact, chartName, chartVersion, description string,
	prov []byte,
) (*opendepotv1alpha1.Version, []byte) {
	t.Helper()

	chart := ociTestChartArchive(t, chartName, chartVersion, description)

	var provReader io.ReadSeeker
	if prov != nil {
		provReader = bytes.NewReader(prov)
	}

	packaged, err := oci.PackageHelmChart(bytes.NewReader(chart), provReader)
	if err != nil {
		t.Fatalf("PackageHelmChart() error = %v", err)
	}

	repository := ociTestVersionName(artifact, chartVersion)
	ctx := t.Context()
	if err := env.store.PutBlob(ctx, ociTestNamespace, repository, packaged.Config.Descriptor.Digest,
		packaged.Config.Content); err != nil {
		t.Fatalf("PutBlob(config) error = %v", err)
	}

	for _, layer := range packaged.Layers {
		if err := env.store.PutBlob(ctx, ociTestNamespace, repository, layer.Descriptor.Digest, layer.Content); err != nil {
			t.Fatalf("PutBlob(layer) error = %v", err)
		}
	}

	if _, err := env.store.PutManifest(ctx, ociTestNamespace, repository, packaged.Manifest); err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}

	version := env.newOCITestVersion(artifact, chartVersion, ociTestHelmType)
	version.Status.OCIManifest = &opendepotv1alpha1.OCIManifestStatus{
		Digest:    packaged.Descriptor.Digest.String(),
		MediaType: packaged.Descriptor.MediaType,
	}
	version.Status.HelmChart = &opendepotv1alpha1.HelmChartMetadata{
		Name:        chartName,
		Version:     chartVersion,
		Description: description,
	}
	env.addVersion(version)

	return version, chart
}

// isolateHelmIndexCache gives the test an empty index cache and restores the shared one on cleanup.
func isolateHelmIndexCache(t *testing.T) {
	t.Helper()

	original := helmIndexCache
	helmIndexCache = expirable.NewLRU[string, helmIndexEntry](helmIndexCacheMaxEntries, nil, helmIndexCacheTTL)
	t.Cleanup(func() { helmIndexCache = original })
}

// ociTestHelmRequest sends a request through a router that mounts the Helm repository routes as the server will.
func ociTestHelmRequest(method, target string, headers map[string]string) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	router.Handle("/opendepot/helm/v1/{namespace}/index.yaml", http.HandlerFunc(HelmIndexHandler))
	router.Handle("/opendepot/helm/v1/{namespace}/charts/{file}", http.HandlerFunc(HelmChartHandler))

	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

func TestHelmIndexListsOnlyVisibleSyncedUnblockedCharts(t *testing.T) {
	env := newOCITestEnv(t, false)
	isolateHelmIndexCache(t)
	env.addBinding("podinfo-*")
	env.addArtifact(ociTestPodinfoArtifact, ociTestChartName, ociTestHelmType)
	env.addArtifact(ociTestSecretArtifact, ociTestSecretRepo, ociTestHelmType)

	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagOne, "visible chart", nil)
	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagTwo, "unsynced chart", nil)
	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagThree, "blocked chart", nil)
	env.publishChart(t, ociTestSecretArtifact, ociTestSecretRepo, ociTestTagOne, "hidden chart", nil)
	env.updateVersion(ociTestVersionName(ociTestPodinfoArtifact, ociTestTagTwo), func(v *opendepotv1alpha1.Version) {
		v.Status.Synced = false
	})
	env.updateVersion(ociTestVersionName(ociTestPodinfoArtifact, ociTestTagThree), func(v *opendepotv1alpha1.Version) {
		v.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}
	})

	rec := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, ociTestBearer(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d: %s", rec.Code, rec.Body.String())
	}

	if got := rec.Header().Get(ociTestContentType); got != ociTestYAMLType {
		t.Fatalf("%s = %q", ociTestContentType, got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		ociTestChartName + ":",
		"version: " + ociTestTagOne,
		"- charts/" + ociTestChartTGZ,
		"visible chart",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("index.yaml is missing %q:\n%s", want, body)
		}
	}

	hiddenMarkers := []string{
		ociTestSecretRepo, "hidden chart", ociTestTagTwo, ociTestTagThree, "unsynced chart", "blocked chart",
	}
	for _, hidden := range hiddenMarkers {
		if strings.Contains(body, hidden) {
			t.Fatalf("index.yaml leaks %q:\n%s", hidden, body)
		}
	}
}

func TestHelmIndexRequiresAuthentication(t *testing.T) {
	newOCITestEnv(t, false)
	isolateHelmIndexCache(t)

	rec := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	if got := rec.Header().Get("WWW-Authenticate"); got != ociTestChallenge {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
}

func TestHelmIndexIsDeterministic(t *testing.T) {
	env := newOCITestEnv(t, true)
	isolateHelmIndexCache(t)
	env.addArtifact(ociTestPodinfoArtifact, ociTestChartName, ociTestHelmType)
	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagOne, "one", nil)
	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, "1.1.0", "two", nil)

	first := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, nil)
	isolateHelmIndexCache(t)
	second := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, nil)

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d", first.Code, second.Code)
	}

	if first.Body.String() != second.Body.String() {
		t.Fatalf("index.yaml differs between renders:\n%s\n---\n%s", first.Body.String(), second.Body.String())
	}

	if !strings.Contains(first.Body.String(), "version: 1.1.0") {
		t.Fatalf("index.yaml is missing the newer version:\n%s", first.Body.String())
	}
}

func TestHelmIndexCacheHitAndResourceVersionInvalidation(t *testing.T) {
	env := newOCITestEnv(t, true)
	isolateHelmIndexCache(t)
	env.addArtifact(ociTestPodinfoArtifact, ociTestChartName, ociTestHelmType)
	version, _ := env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagOne, "first", nil)

	rec := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, nil)
	if !strings.Contains(rec.Body.String(), "first") {
		t.Fatalf("index.yaml missing the first description:\n%s", rec.Body.String())
	}

	// Changing content without changing the resourceVersion must still return the cached document. The created time is
	// the field of the Version that the index renders, so it is the one that changes here.
	env.updateVersion(version.Name, func(v *opendepotv1alpha1.Version) {
		v.CreationTimestamp = metav1.NewTime(time.Date(2027, 5, 6, 12, 0, 0, 0, time.UTC))
	})

	cached := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, nil)
	if cached.Body.String() != rec.Body.String() || strings.Contains(cached.Body.String(), "2027-05-06") {
		t.Fatalf("cache hit was not served from the cache:\n%s", cached.Body.String())
	}

	env.updateVersion(version.Name, func(v *opendepotv1alpha1.Version) { v.ResourceVersion = "2" })

	fresh := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, nil)
	if !strings.Contains(fresh.Body.String(), "2027-05-06") {
		t.Fatalf("resourceVersion change did not invalidate the cache:\n%s", fresh.Body.String())
	}
}

func TestHelmIndexEmptyNamespaceIsEmptyIndex(t *testing.T) {
	newOCITestEnv(t, true)
	isolateHelmIndexCache(t)

	rec := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "entries: {}") {
		t.Fatalf("empty index status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
}

func TestHelmChartDownloads(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestPodinfoArtifact, ociTestChartName, ociTestHelmType)
	prov := []byte("-----BEGIN PGP SIGNED MESSAGE-----\nprovenance\n")
	_, chart := env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagOne, "chart", prov)

	t.Run("tgz streams the chart layer", func(t *testing.T) {
		rec := ociTestHelmRequest(http.MethodGet, ociTestChartsPath+ociTestChartTGZ, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}

		if !bytes.Equal(rec.Body.Bytes(), chart) {
			t.Fatal("chart body does not match the packaged archive")
		}

		if got := rec.Header().Get(ociTestContentType); got != ociTestGzipType {
			t.Fatalf("%s = %q", ociTestContentType, got)
		}
	})

	t.Run("prov streams the provenance layer", func(t *testing.T) {
		rec := ociTestHelmRequest(http.MethodGet, ociTestChartsPath+ociTestChartProv, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}

		if !bytes.Equal(rec.Body.Bytes(), prov) {
			t.Fatalf("provenance body = %q, want %q", rec.Body.String(), prov)
		}

		if got := rec.Header().Get(ociTestContentType); got != ociTestProvType {
			t.Fatalf("%s = %q", ociTestContentType, got)
		}
	})

	t.Run("HEAD returns the chart length without a body", func(t *testing.T) {
		rec := ociTestHelmRequest(http.MethodHead, ociTestChartsPath+ociTestChartTGZ, nil)
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Fatalf("HEAD status = %d, body length = %d", rec.Code, rec.Body.Len())
		}

		if got := rec.Header().Get(ociTestContentLength); got != fmt.Sprint(len(chart)) {
			t.Fatalf("%s = %q, want %d", ociTestContentLength, got, len(chart))
		}
	})
}

func TestHelmChartNotFoundCases(t *testing.T) {
	env := newOCITestEnv(t, false)
	isolateHelmIndexCache(t)
	env.addBinding("podinfo-*")
	env.addArtifact(ociTestPodinfoArtifact, ociTestChartName, ociTestHelmType)
	env.addArtifact(ociTestSecretArtifact, ociTestSecretRepo, ociTestHelmType)

	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagOne, "no provenance", nil)
	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagTwo, "unsynced", nil)
	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagThree, "blocked", nil)
	env.publishChart(t, ociTestSecretArtifact, ociTestSecretRepo, ociTestTagOne, "hidden", nil)
	env.updateVersion(ociTestVersionName(ociTestPodinfoArtifact, ociTestTagTwo), func(v *opendepotv1alpha1.Version) {
		v.Status.Synced = false
	})
	env.updateVersion(ociTestVersionName(ociTestPodinfoArtifact, ociTestTagThree), func(v *opendepotv1alpha1.Version) {
		v.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}
	})

	cases := []struct {
		name   string
		target string
	}{
		{name: "prov is absent upstream", target: ociTestChartsPath + ociTestChartProv},
		{name: "unsynced version", target: ociTestChartsPath + "podinfo-" + ociTestTagTwo + ".tgz"},
		{name: "blocked version", target: ociTestChartsPath + "podinfo-" + ociTestTagThree + ".tgz"},
		{name: "hidden artifact", target: ociTestChartsPath + "secret-" + ociTestTagOne + ".tgz"},
		{name: "unknown version", target: ociTestChartsPath + "podinfo-9.9.9.tgz"},
		{name: "unsupported extension", target: ociTestChartsPath + "podinfo-" + ociTestTagOne + ".txt"},
		{name: "path traversal", target: ociTestChartsPath + "..%2Fsecret.tgz"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := ociTestHelmRequest(http.MethodGet, tc.target, ociTestBearer(t))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHelmChartDuplicateVersionIsAmbiguous(t *testing.T) {
	env := newOCITestEnv(t, true)
	isolateHelmIndexCache(t)
	env.addArtifact("podinfo-first", ociTestChartName, ociTestHelmType)
	env.addArtifact("podinfo-second", ociTestChartName, ociTestHelmType)
	env.publishChart(t, "podinfo-first", ociTestChartName, ociTestTagOne, "first", nil)
	env.publishChart(t, "podinfo-second", ociTestChartName, ociTestTagOne, "second", nil)

	rec := ociTestHelmRequest(http.MethodGet, ociTestChartsPath+ociTestChartTGZ, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a duplicated chart version: %s", rec.Code, rec.Body.String())
	}

	if bytes.Contains(rec.Body.Bytes(), []byte("first")) || bytes.Contains(rec.Body.Bytes(), []byte("second")) {
		t.Fatalf("duplicate chart error leaked a chart: %q", rec.Body.String())
	}
}

func TestHelmBasicBearerPasswordAuthenticates(t *testing.T) {
	env := newOCITestEnv(t, false)
	isolateHelmIndexCache(t)
	env.addBinding("*")
	env.addArtifact(ociTestPodinfoArtifact, ociTestChartName, ociTestHelmType)
	env.publishChart(t, ociTestPodinfoArtifact, ociTestChartName, ociTestTagOne, "basic", nil)

	bearer := ociTestBearer(t)[ociTestAuthorization][len(ociTestBearerPrefix):]
	rec := ociTestHelmRequest(http.MethodGet, ociTestIndexPath, ociTestBasic("helm", bearer))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "basic") {
		t.Fatalf("Basic index status = %d: %s", rec.Code, rec.Body.String())
	}
}
