package chart

import (
	"encoding/base64"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chartPath(t *testing.T) string {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(filename), "..", "..", "chart", "opendepot")
}

func renderChart(t *testing.T, values map[string]string, stringValues map[string]string) string {
	t.Helper()

	return renderTemplates(t, values, stringValues, nil)
}

func renderTemplate(t *testing.T, values map[string]string, stringValues map[string]string, templateFile string) string {
	t.Helper()

	return renderTemplates(t, values, stringValues, []string{templateFile})
}

func renderTemplates(t *testing.T, values map[string]string, stringValues map[string]string, templateFiles []string) string {
	t.Helper()

	rendered, err := helm.RenderTemplateE(
		t,
		&helm.Options{
			KubectlOptions: &k8s.KubectlOptions{Namespace: "default"},
			Logger:         logger.Discard,
			SetStrValues:   stringValues,
			SetValues:      values,
		},
		chartPath(t),
		"opendepot",
		templateFiles,
	)
	require.NoError(t, err)
	return rendered
}

func unmarshal[T any](t *testing.T, rendered string) T {
	t.Helper()

	var resource T
	require.NoError(t, helm.UnmarshalK8SYamlE(t, rendered, &resource))
	return resource
}

func renderedResources(t *testing.T, rendered string) []unstructured.Unstructured {
	t.Helper()

	var resources []unstructured.Unstructured
	for _, document := range strings.Split(rendered, "\n---\n") {
		if strings.TrimSpace(document) == "" {
			continue
		}
		var resource unstructured.Unstructured
		if helm.UnmarshalK8SYamlE(t, document, &resource) == nil && resource.GetKind() != "" {
			resources = append(resources, resource)
		}
	}
	return resources
}

func hasResource(resources []unstructured.Unstructured, kind, name string) bool {
	for _, resource := range resources {
		if resource.GetKind() == kind && resource.GetName() == name {
			return true
		}
	}
	return false
}

func TestChartPermutations(t *testing.T) {
	tests := map[string]struct {
		values        map[string]string
		stringValues  map[string]string
		templateFiles []string
		contains      []string
		notContains   []string
	}{
		"default monitoring": {
			templateFiles: []string{"charts/monitoring/templates/prometheus/prometheus.yaml"},
			contains:      []string{`retention: "90d"`},
			notContains:   []string{"volumeClaimTemplate:"},
		},
		"persistent monitoring": {
			templateFiles: []string{"charts/monitoring/templates/prometheus/prometheus.yaml"},
			values:        map[string]string{"monitoring.prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage": "10Gi"},
			contains:      []string{"volumeClaimTemplate:"},
			notContains:   []string{},
		},
		"workload identity annotations": {
			templateFiles: []string{"templates/version-serviceaccount.yaml", "templates/server-serviceaccount.yaml"},
			stringValues: map[string]string{
				`version.serviceAccount.annotations.iam\.gke\.io/gcp-service-account`: "version@example.iam.gserviceaccount.com",
				`server.serviceAccount.annotations.iam\.gke\.io/gcp-service-account`:  "server@example.iam.gserviceaccount.com",
			},
			contains: []string{
				"iam.gke.io/gcp-service-account: version@example.iam.gserviceaccount.com",
				"iam.gke.io/gcp-service-account: server@example.iam.gserviceaccount.com",
			},
		},
		"service overrides": {
			templateFiles: []string{"templates/server-service.yaml", "templates/ui-service.yaml"},
			values: map[string]string{
				"ui.enabled":                   "true",
				"ui.sessionPasswordSecretName": "ui-session",
				"ui.service.type":              "LoadBalancer",
				"ui.service.loadBalancerIP":    "203.0.113.10",
				"monitoring.enabled":           "false",
			},
			stringValues: map[string]string{
				`ui.service.annotations.networking\.gke\.io/load-balancer-type`: "External",
			},
			contains: []string{
				"type: ClusterIP",
				"type: LoadBalancer",
				"loadBalancerIP: 203.0.113.10",
				"networking.gke.io/load-balancer-type: External",
			},
		},
		"https ingress with Dex proxy": {
			templateFiles: []string{"templates/server-ingress.yaml"},
			values: map[string]string{
				"ui.enabled":                   "false",
				"server.ingress.enabled":       "true",
				"server.ingress.className":     "gce",
				"server.oidc.enabled":          "true",
				"server.oidc.issuerUrl":        "https://demo.example.com/dex",
				"server.oidc.clientSecretName": "opendepot-dex-client-secret",
				"server.oidc.dexProxy.enabled": "true",
				"dex.enabled":                  "true",
				"monitoring.enabled":           "true",
			},
			stringValues: map[string]string{
				`server.ingress.annotations.kubernetes\.io/ingress\.class`:               "gce",
				`server.ingress.annotations.networking\.gke\.io/managed-certificates`:    "opendepot-demo",
				`server.ingress.annotations.networking\.gke\.io/v1beta1\.FrontendConfig`: "opendepot-demo",
			},
			contains: []string{
				"path: /dex",
				"kubernetes.io/ingress.class: gce",
				"networking.gke.io/managed-certificates: opendepot-demo",
				"networking.gke.io/v1beta1.FrontendConfig: opendepot-demo",
			},
			notContains: []string{},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			rendered := renderTemplates(t, test.values, test.stringValues, test.templateFiles)
			for _, expected := range test.contains {
				assert.Contains(t, rendered, expected)
			}
			for _, unexpected := range test.notContains {
				assert.NotContains(t, rendered, unexpected)
			}
		})
	}
}

func TestChartResourceEnablement(t *testing.T) {
	tests := map[string]struct {
		values  map[string]string
		present [][2]string
		absent  [][2]string
	}{
		"optional resources disabled": {
			values: map[string]string{
				"dex.enabled":                "false",
				"monitoring.bundled.enabled": "false",
				"monitoring.enabled":         "false",
				"provider.enabled":           "false",
				"ui.enabled":                 "false",
			},
			absent: [][2]string{
				{"Deployment", "provider-controller"},
				{"Deployment", "ui"},
				{"Service", "ui"},
				{"Deployment", "opendepot-dex"},
				{"Prometheus", "opendepot-monitoring-prometheus"},
			},
		},
		"optional resources enabled": {
			values: map[string]string{
				"dex.enabled":                  "true",
				"monitoring.bundled.enabled":   "true",
				"monitoring.enabled":           "true",
				"provider.enabled":             "true",
				"ui.enabled":                   "true",
				"ui.sessionPasswordSecretName": "ui-session",
			},
			present: [][2]string{
				{"Deployment", "provider-controller"},
				{"Deployment", "ui"},
				{"Service", "ui"},
				{"Deployment", "opendepot-dex"},
				{"Prometheus", "opendepot-monitoring-prometheus"},
			},
		},
		"monitoring and ServiceMonitor independently disabled": {
			values: map[string]string{
				"monitoring.bundled.enabled":            "true",
				"monitoring.enabled":                    "true",
				"server.metrics.serviceMonitor.enabled": "false",
			},
			present: [][2]string{{"Prometheus", "opendepot-monitoring-prometheus"}},
			absent:  [][2]string{{"ServiceMonitor", "server"}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resources := renderedResources(t, renderChart(t, test.values, nil))
			for _, source := range test.present {
				assert.True(t, hasResource(resources, source[0], source[1]), "expected %s/%s to render", source[0], source[1])
			}
			for _, source := range test.absent {
				assert.False(t, hasResource(resources, source[0], source[1]), "expected %s/%s not to render", source[0], source[1])
			}
		})
	}
}

func TestServerFlagConfiguration(t *testing.T) {
	tests := map[string]struct {
		values      map[string]string
		contains    []string
		notContains []string
	}{
		"secure defaults": {
			contains: []string{
				"--use-bearer-token",
				"--stats-lookback=90d",
				"--stats-query-timeout=5s",
				"--stats-prometheus-url=http://opendepot-monitoring-prometheus.default.svc.cluster.local:9090",
			},
			notContains: []string{"--anonymous-auth", "--assembly-enabled"},
		},
		"authentication and TLS flags": {
			values: map[string]string{
				"server.anonymousAuth":                    "true",
				"server.useBearerToken":                   "false",
				"server.tls.enabled":                      "true",
				"server.tls.certPath":                     "/custom/tls.crt",
				"server.tls.keyPath":                      "/custom/tls.key",
				"server.oidc.enabled":                     "true",
				"server.oidc.dexProxy.enabled":            "false",
				"server.oidc.issuerUrl":                   "https://issuer.example.com",
				"server.oidc.groupsClaim":                 "roles",
				"server.oidc.allowServiceAccountFallback": "true",
				"server.oidc.allowClientCredentials":      "true",
			},
			contains: []string{
				"--anonymous-auth",
				"--oidc-issuer-url=https://issuer.example.com",
				"--oidc-groups-claim=roles",
				"--oidc-allow-sa-fallback",
				"--oidc-allow-client-credentials",
				"--tls-cert-path=/custom/tls.crt",
				"--tls-cert-key=/custom/tls.key",
			},
			notContains: []string{"--use-bearer-token"},
		},
		"assembly and custom metrics configuration": {
			values: map[string]string{
				"assembly.enabled":               "true",
				"assembly.validationRegistryUrl": "http://registry.example.com",
				"assembly.validationCACertPath":  "/etc/tls/ca.crt",
				"ui.baseUrl":                     "https://registry.example.com",
				"assembly.maxNodes":              "25",
				"server.stats.lookback":          "30d",
				"server.stats.queryTimeout":      "10s",
				"server.stats.prometheusURL":     "https://prometheus.example.com",
			},
			contains: []string{
				"--assembly-enabled",
				"--assembly-validation-registry-url=http://registry.example.com",
				"--assembly-validation-ca-cert-path=/etc/tls/ca.crt",
				"--assembly-registry-insecure",
				"--assembly-max-nodes=25",
				"--stats-lookback=30d",
				"--stats-query-timeout=10s",
				"--stats-prometheus-url=https://prometheus.example.com",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			deployment := unmarshal[appsv1.Deployment](t, renderTemplate(t, test.values, nil, "templates/server-deployment.yaml"))
			require.NotEmpty(t, deployment.Spec.Template.Spec.Containers)
			args := strings.Join(deployment.Spec.Template.Spec.Containers[0].Args, " ")
			for _, expected := range test.contains {
				assert.Contains(t, args, expected)
			}
			for _, unexpected := range test.notContains {
				assert.NotContains(t, args, unexpected)
			}
		})
	}
}

func TestVersionControllerFlagConfiguration(t *testing.T) {
	rendered := renderTemplate(t, map[string]string{
		"assembly.enabled":           "true",
		"ui.baseUrl":                 "https://registry.example.com",
		"assembly.extractionTimeout": "10m",
		"scanning.enabled":           "true",
		"scanning.providerScanning":  "true",
		"scanning.offline":           "false",
		"version.zapLogLevel":        "5",
	}, nil, "templates/version-deployment.yaml")

	version := unmarshal[appsv1.Deployment](t, rendered)
	require.NotEmpty(t, version.Spec.Template.Spec.Containers)
	args := strings.Join(version.Spec.Template.Spec.Containers[0].Args, " ")
	assert.Contains(t, args, "--assembly-enabled=true")
	assert.Contains(t, args, "--schema-extraction-timeout=10m")
	assert.Contains(t, args, "--scanning-enabled=true")
	assert.Contains(t, args, "--scan-offline=false")
	assert.Contains(t, args, "--zap-log-level=5")
}

func TestChartRendersValidDexPassword(t *testing.T) {
	rendered := renderChart(t, map[string]string{
		"dex.enabled":                            "true",
		"server.oidc.enabled":                    "true",
		"server.oidc.issuerUrl":                  "https://demo.example.com/dex",
		"server.oidc.clientSecretName":           "opendepot-dex-client-secret",
		"dex.config.enablePasswordDB":            "true",
		"dex.config.staticPasswords[0].username": "demo",
		"dex.config.staticPasswords[0].email":    "demo@example.com",
		"monitoring.enabled":                     "false",
	}, map[string]string{
		"dex.config.staticPasswords[0].hash": `$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy`,
	})

	configMatch := regexp.MustCompile(`config.yaml: "([^"]+)"`).FindStringSubmatch(rendered)
	require.Len(t, configMatch, 2)
	config, err := base64.StdEncoding.DecodeString(configMatch[1])
	require.NoError(t, err)
	assert.Contains(t, string(config), "username: demo")
	assert.Contains(t, string(config), "email: demo@example.com")
}
