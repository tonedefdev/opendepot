package main

import (
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	wave7nListDepots   = "/apis/opendepot.defdev.io/v1alpha1/depots"
	wave7nListModules  = "/apis/opendepot.defdev.io/v1alpha1/modules"
	wave7nListProvs    = "/apis/opendepot.defdev.io/v1alpha1/providers"
	wave7nListVersions = "/apis/opendepot.defdev.io/v1alpha1/versions"
)

func wave7nCollectRoutes(overrides map[string]covGResponse) map[string]covGResponse {
	routes := map[string]covGResponse{
		wave7nListDepots:   {body: opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{{ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "team"}}}}},
		wave7nListModules:  {body: opendepotv1alpha1.ModuleList{Items: []opendepotv1alpha1.Module{wave7nMetricsModule()}}},
		wave7nListProvs:    {body: opendepotv1alpha1.ProviderList{Items: []opendepotv1alpha1.Provider{wave7nMetricsProvider()}}},
		wave7nListVersions: {body: opendepotv1alpha1.VersionList{Items: wave7nMetricsVersions()}},
	}

	for path, response := range overrides {
		routes[path] = response
	}

	return routes
}

func wave7nMetricsModule() opendepotv1alpha1.Module {
	return opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "vpc", Namespace: "team"},
		Spec: opendepotv1alpha1.ModuleSpec{
			ModuleConfig: opendepotv1alpha1.ModuleConfig{
				StorageConfig: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: "b"}},
			},
		},
	}
}

func wave7nMetricsProvider() opendepotv1alpha1.Provider {
	return opendepotv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "team"},
		Spec: opendepotv1alpha1.ProviderSpec{
			ProviderConfig: opendepotv1alpha1.ProviderConfig{
				StorageConfig: &opendepotv1alpha1.StorageConfig{FileSystem: &opendepotv1alpha1.FileSystemConfig{}},
			},
		},
	}
}

func wave7nMetricsVersions() []opendepotv1alpha1.Version {
	size := int64(2048)
	moduleName := "vpc"
	providerName := "aws"

	module := opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "vpc-1.0.0", Namespace: "team"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:            "module",
			Version:         "1.0.0",
			ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{Name: &moduleName, StorageConfig: &opendepotv1alpha1.StorageConfig{S3: &opendepotv1alpha1.AmazonS3Config{Bucket: "b"}}},
		},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:           true,
			SyncStatus:       "Synced",
			ArchiveSizeBytes: &size,
			SourceScan:       &opendepotv1alpha1.SourceScan{Findings: []opendepotv1alpha1.SecurityFinding{{Severity: "HIGH"}}},
		},
	}

	provider := opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "aws-5.0.0", Namespace: "team"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:              "provider",
			Version:           "5.0.0",
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{Name: &providerName, StorageConfig: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: "b"}}},
		},
		Status: opendepotv1alpha1.VersionStatus{
			SyncStatus: "Failed to sync",
			BinaryScan: &opendepotv1alpha1.BinaryScan{Findings: []opendepotv1alpha1.SecurityFinding{{Severity: "critical"}}},
		},
	}

	skill := opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "reviewer-1.0.0", Namespace: "team", Labels: map[string]string{"opendepot.defdev.io/skill": "reviewer"}},
		Spec:       opendepotv1alpha1.VersionSpec{Type: "Skill", Version: "1.0.0"},
		Status:     opendepotv1alpha1.VersionStatus{Synced: true},
	}

	return []opendepotv1alpha1.Version{module, provider, skill}
}

func wave7nCollect(t *testing.T) []*dto.MetricFamily {
	t.Helper()

	registry := prometheus.NewPedanticRegistry()
	if err := registry.Register(newRegistryMetricsCollector()); err != nil {
		t.Fatalf("register collector: %v", err)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	return families
}

func wave7nFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}

	return nil
}

func wave7nSuccessValue(t *testing.T, families []*dto.MetricFamily) float64 {
	t.Helper()

	family := wave7nFamily(families, "opendepot_metrics_collection_success")
	if family == nil || len(family.GetMetric()) != 1 {
		t.Fatalf("collection_success metric missing: %v", family)
	}

	return family.GetMetric()[0].GetGauge().GetValue()
}

func TestWave7NRegistryCollectorSuccessEmitsAllFamilies(t *testing.T) {
	covGUseCluster(t, covGAPIServer(t, wave7nCollectRoutes(nil)))

	families := wave7nCollect(t)

	if got := wave7nSuccessValue(t, families); got != 1 {
		t.Fatalf("collection_success = %v, want 1", got)
	}

	for _, name := range []string{
		"opendepot_depot_count",
		"opendepot_module_count",
		"opendepot_provider_count",
		"opendepot_version_count",
		"opendepot_storage_bytes",
		"opendepot_security_finding_count",
		"opendepot_security_affected_resource_count",
	} {
		if wave7nFamily(families, name) == nil {
			t.Fatalf("metric family %s not emitted", name)
		}
	}

	storage := wave7nFamily(families, "opendepot_storage_bytes").GetMetric()
	if len(storage) != 1 || storage[0].GetGauge().GetValue() != 2048 {
		t.Fatalf("storage metrics = %v, want one 2048 byte sample", storage)
	}

	findings := wave7nFamily(families, "opendepot_security_finding_count").GetMetric()
	if len(findings) != 2 {
		t.Fatalf("finding metrics = %d, want 2 (high module, critical provider)", len(findings))
	}
}

func TestWave7NRegistryCollectorListFailuresReportUnsuccessful(t *testing.T) {
	paths := []string{wave7nListDepots, wave7nListModules, wave7nListProvs, wave7nListVersions}

	for _, path := range paths {
		t.Run("failure listing "+path, func(t *testing.T) {
			covGUseCluster(t, covGAPIServer(t, wave7nCollectRoutes(map[string]covGResponse{
				path: {status: http.StatusInternalServerError, raw: `{}`},
			})))

			if got := wave7nSuccessValue(t, wave7nCollect(t)); got != 0 {
				t.Fatalf("collection_success = %v, want 0", got)
			}
		})

		t.Run("malformed list "+path, func(t *testing.T) {
			covGUseCluster(t, covGAPIServer(t, wave7nCollectRoutes(map[string]covGResponse{
				path: {raw: `not-json`},
			})))

			if got := wave7nSuccessValue(t, wave7nCollect(t)); got != 0 {
				t.Fatalf("collection_success = %v, want 0", got)
			}
		})
	}
}

func TestWave7NRegistryCollectorClientFailureReportsUnsuccessful(t *testing.T) {
	covGUseCluster(t, covGAPIServer(t, wave7nCollectRoutes(nil)))

	original := inClusterConfig
	t.Cleanup(func() { inClusterConfig = original })
	inClusterConfig = func() (*rest.Config, error) {
		return nil, errWave7NNoCluster
	}

	if got := wave7nSuccessValue(t, wave7nCollect(t)); got != 0 {
		t.Fatalf("collection_success = %v, want 0", got)
	}
}
