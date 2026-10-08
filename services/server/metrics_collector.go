package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"k8s.io/client-go/kubernetes"
)

type registryMetricsCollector struct {
	depotCount        *prometheus.Desc
	moduleCount       *prometheus.Desc
	providerCount     *prometheus.Desc
	versionCount      *prometheus.Desc
	storageBytes      *prometheus.Desc
	securityFindings  *prometheus.Desc
	securityAffected  *prometheus.Desc
	collectionSuccess *prometheus.Desc
}

func newRegistryMetricsCollector() *registryMetricsCollector {
	return &registryMetricsCollector{
		depotCount:        prometheus.NewDesc("opendepot_depot_count", "Number of Depot resources.", []string{"namespace"}, nil),
		moduleCount:       prometheus.NewDesc("opendepot_module_count", "Number of Module resources.", []string{"namespace", "storage_backend"}, nil),
		providerCount:     prometheus.NewDesc("opendepot_provider_count", "Number of Provider resources.", []string{"namespace", "storage_backend"}, nil),
		versionCount:      prometheus.NewDesc("opendepot_version_count", "Number of Version resources by synchronization state.", []string{"namespace", "kind", "sync_state"}, nil),
		storageBytes:      prometheus.NewDesc("opendepot_storage_bytes", "Stored archive size in bytes.", []string{"namespace", "kind", "name", "version", "storage_backend"}, nil),
		securityFindings:  prometheus.NewDesc("opendepot_security_finding_count", "Number of security findings by severity.", []string{"namespace", "kind", "severity"}, nil),
		securityAffected:  prometheus.NewDesc("opendepot_security_affected_resource_count", "Number of resources with security findings.", []string{"namespace", "kind"}, nil),
		collectionSuccess: prometheus.NewDesc("opendepot_metrics_collection_success", "Whether the latest Kubernetes metrics collection succeeded.", nil, nil),
	}
}

func (c *registryMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.depotCount
	ch <- c.moduleCount
	ch <- c.providerCount
	ch <- c.versionCount
	ch <- c.storageBytes
	ch <- c.securityFindings
	ch <- c.securityAffected
	ch <- c.collectionSuccess
}

func (c *registryMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	cs, err := browseSAClient()
	if err != nil {
		logger.Error("metrics: failed to create Kubernetes client", "error", err)
		ch <- prometheus.MustNewConstMetric(c.collectionSuccess, prometheus.GaugeValue, 0)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	metrics, err := collectRegistryMetrics(ctx, cs)
	if err != nil {
		logger.Error("metrics: failed to collect registry metrics", "error", err)
		ch <- prometheus.MustNewConstMetric(c.collectionSuccess, prometheus.GaugeValue, 0)
		return
	}

	for key, value := range metrics.depots {
		ch <- prometheus.MustNewConstMetric(c.depotCount, prometheus.GaugeValue, float64(value), key)
	}
	for key, value := range metrics.modules {
		ch <- prometheus.MustNewConstMetric(c.moduleCount, prometheus.GaugeValue, float64(value), key.namespace, key.backend)
	}
	for key, value := range metrics.providers {
		ch <- prometheus.MustNewConstMetric(c.providerCount, prometheus.GaugeValue, float64(value), key.namespace, key.backend)
	}
	for key, value := range metrics.versions {
		ch <- prometheus.MustNewConstMetric(c.versionCount, prometheus.GaugeValue, float64(value), key.namespace, key.kind, key.state)
	}
	for key, value := range metrics.storage {
		ch <- prometheus.MustNewConstMetric(c.storageBytes, prometheus.GaugeValue, float64(value), key.namespace, key.kind, key.name, key.version, key.backend)
	}
	for key, value := range metrics.findings {
		ch <- prometheus.MustNewConstMetric(c.securityFindings, prometheus.GaugeValue, float64(value), key.namespace, key.kind, key.severity)
	}
	for key, value := range metrics.affected {
		ch <- prometheus.MustNewConstMetric(c.securityAffected, prometheus.GaugeValue, float64(value), key.namespace, key.kind)
	}
	ch <- prometheus.MustNewConstMetric(c.collectionSuccess, prometheus.GaugeValue, 1)
}

type metricKey struct {
	namespace string
	kind      string
	name      string
	version   string
	backend   string
	state     string
	severity  string
}

type registryMetrics struct {
	depots    map[string]int
	modules   map[metricKey]int
	providers map[metricKey]int
	versions  map[metricKey]int
	storage   map[metricKey]int64
	findings  map[metricKey]int
	affected  map[metricKey]int
}

func collectRegistryMetrics(ctx context.Context, cs *kubernetes.Clientset) (registryMetrics, error) {
	metrics := registryMetrics{
		depots:    make(map[string]int),
		modules:   make(map[metricKey]int),
		providers: make(map[metricKey]int),
		versions:  make(map[metricKey]int),
		storage:   make(map[metricKey]int64),
		findings:  make(map[metricKey]int),
		affected:  make(map[metricKey]int),
	}

	var depots opendepotv1alpha1.DepotList
	if err := listCustomResources(ctx, cs, "depots", &depots); err != nil {
		return metrics, err
	}
	for _, depot := range depots.Items {
		metrics.depots[depot.Namespace]++
	}

	var modules opendepotv1alpha1.ModuleList
	if err := listCustomResources(ctx, cs, "modules", &modules); err != nil {
		return metrics, err
	}
	for _, module := range modules.Items {
		backend := storageBackendName(module.Spec.ModuleConfig.StorageConfig)
		metrics.modules[metricKey{namespace: module.Namespace, backend: backend}]++
	}

	var providers opendepotv1alpha1.ProviderList
	if err := listCustomResources(ctx, cs, "providers", &providers); err != nil {
		return metrics, err
	}
	for _, provider := range providers.Items {
		backend := storageBackendName(provider.Spec.ProviderConfig.StorageConfig)
		metrics.providers[metricKey{namespace: provider.Namespace, backend: backend}]++
	}

	var versions opendepotv1alpha1.VersionList
	if err := listCustomResources(ctx, cs, "versions", &versions); err != nil {
		return metrics, err
	}
	for _, version := range versions.Items {
		kind := strings.ToLower(version.Spec.Type)
		state := versionSyncState(version.Status.Synced, version.Status.SyncStatus)
		metrics.versions[metricKey{namespace: version.Namespace, kind: kind, state: state}]++

		backend := "unknown"
		if version.Spec.ModuleConfigRef != nil {
			backend = storageBackendName(version.Spec.ModuleConfigRef.StorageConfig)
			downloadsTotal.WithLabelValues(version.Namespace, kind, *version.Spec.ModuleConfigRef.Name, version.Spec.Version, serverInstance).Add(0)
		}
		if version.Spec.ProviderConfigRef != nil {
			backend = storageBackendName(version.Spec.ProviderConfigRef.StorageConfig)
			downloadsTotal.WithLabelValues(version.Namespace, kind, *version.Spec.ProviderConfigRef.Name, version.Spec.Version, serverInstance).Add(0)
		}
		if version.Status.ArchiveSizeBytes != nil {
			metrics.storage[metricKey{namespace: version.Namespace, kind: kind, name: version.Name, version: version.Spec.Version, backend: backend}] = *version.Status.ArchiveSizeBytes
		}

		affected := false
		for _, scan := range []*opendepotv1alpha1.SourceScan{version.Status.SourceScan} {
			if scan != nil {
				for _, finding := range scan.Findings {
					metrics.findings[metricKey{namespace: version.Namespace, kind: kind, severity: normalizedSeverity(finding.Severity)}]++
					affected = true
				}
			}
		}

		if version.Status.BinaryScan != nil {
			for _, finding := range version.Status.BinaryScan.Findings {
				metrics.findings[metricKey{namespace: version.Namespace, kind: kind, severity: normalizedSeverity(finding.Severity)}]++
				affected = true
			}
		}

		if affected {
			metrics.affected[metricKey{namespace: version.Namespace, kind: kind}]++
		}
	}

	return metrics, nil
}

func listCustomResources(ctx context.Context, cs *kubernetes.Clientset, resource string, target any) error {
	raw, err := cs.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Resource(resource).DoRaw(ctx)
	if err != nil {
		return fmt.Errorf("list %s: %w", resource, err)
	}

	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode %s: %w", resource, err)
	}

	return nil
}

func versionSyncState(synced bool, status string) string {
	status = strings.ToLower(status)
	if strings.Contains(status, "failed") || strings.Contains(status, "error") {
		return "failed"
	}

	if synced {
		return "synced"
	}

	return "unsynced"
}

func normalizedSeverity(severity string) string {
	switch strings.ToLower(severity) {
	case "critical", "high", "medium", "low":
		return strings.ToLower(severity)
	default:
		return "unknown"
	}
}
