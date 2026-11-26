package chart

import (
	"encoding/base64"
	"io/fs"
	"os"
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
			values:      map[string]string{"monitoring.bundled.enabled": "true"},
			contains:    []string{`retention: "90d"`},
			notContains: []string{"volumeClaimTemplate:"},
		},
		"persistent monitoring": {
			values: map[string]string{
				"monitoring.bundled.enabled": "true",
				"monitoring.prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage": "10Gi",
			},
			contains:    []string{"volumeClaimTemplate:"},
			notContains: []string{},
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

func TestUIUpstreamTLSVerification(t *testing.T) {
	tests := map[string]struct {
		values      map[string]string
		contains    []string
		notContains []string
		exactCounts map[string]int
	}{
		"public CA and default service name": {
			values: map[string]string{
				"ui.enabled":                   "true",
				"ui.sessionPasswordSecretName": "ui-session",
				"server.tls.enabled":           "true",
			},
			contains: []string{
				`proxy_ssl_verify                 on;`,
				`proxy_ssl_trusted_certificate   "/etc/ssl/cert.pem";`,
				`proxy_ssl_server_name           on;`,
				`proxy_ssl_name                  "server.opendepot-system.svc.cluster.local";`,
				`proxy_ssl_verify_depth           3;`,
			},
			exactCounts: map[string]int{
				`proxy_ssl_verify                 on;`:                                         3,
				`proxy_ssl_trusted_certificate   "/etc/ssl/cert.pem";`:                         3,
				`proxy_ssl_server_name           on;`:                                          3,
				`proxy_ssl_name                  "server.opendepot-system.svc.cluster.local";`: 3,
				`proxy_ssl_verify_depth           3;`:                                          3,
			},
		},
		"private CA and custom certificate name": {
			values: map[string]string{
				"ui.enabled":                   "true",
				"ui.sessionPasswordSecretName": "ui-session",
				"server.tls.enabled":           "true",
				"ui.serverCACertPath":          "/etc/tls/ca.crt",
				"ui.serverTLSName":             "server.internal.example.com",
			},
			contains: []string{
				`proxy_ssl_verify                 on;`,
				`proxy_ssl_trusted_certificate   "/etc/tls/ca.crt";`,
				`proxy_ssl_server_name           on;`,
				`proxy_ssl_name                  "server.internal.example.com";`,
				`proxy_ssl_verify_depth           3;`,
			},
			exactCounts: map[string]int{
				`proxy_ssl_verify                 on;`:                           3,
				`proxy_ssl_trusted_certificate   "/etc/tls/ca.crt"`:              3,
				`proxy_ssl_server_name           on;`:                            3,
				`proxy_ssl_name                  "server.internal.example.com";`: 3,
				`proxy_ssl_verify_depth           3;`:                            3,
			},
		},
		"plain HTTP has no upstream TLS directives": {
			values: map[string]string{
				"ui.enabled":                   "true",
				"ui.sessionPasswordSecretName": "ui-session",
				"server.tls.enabled":           "false",
			},
			notContains: []string{"proxy_ssl_"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			rendered := renderTemplate(t, test.values, nil, "templates/ui-configmap.yaml")
			for _, expected := range test.contains {
				assert.Contains(t, rendered, expected)
			}
			for _, unexpected := range test.notContains {
				assert.NotContains(t, rendered, unexpected)
			}
			for directive, expectedCount := range test.exactCounts {
				assert.Equal(t, expectedCount, strings.Count(rendered, directive), "unexpected count for %q", directive)
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
				"iac.enabled":                "false",
				"monitoring.bundled.enabled": "false",
				"monitoring.enabled":         "false",
				"ui.enabled":                 "false",
			},
			absent: [][2]string{
				{"Deployment", "iac-controller"},
				{"ServiceAccount", "iac-controller"},
				{"ClusterRole", "iac-controller-role"},
				{"Deployment", "ui"},
				{"Service", "ui"},
				{"Deployment", "opendepot-dex"},
				{"Prometheus", "opendepot-monitoring-prometheus"},
			},
		},
		"optional resources enabled": {
			values: map[string]string{
				"dex.enabled":                  "true",
				"iac.enabled":                  "true",
				"monitoring.bundled.enabled":   "true",
				"monitoring.enabled":           "true",
				"ui.enabled":                   "true",
				"ui.sessionPasswordSecretName": "ui-session",
			},
			present: [][2]string{
				{"Deployment", "iac-controller"},
				{"ServiceAccount", "iac-controller"},
				{"ClusterRole", "iac-controller-role"},
				{"Deployment", "ui"},
				{"Service", "ui"},
				{"Deployment", "opendepot-dex"},
				{"Prometheus", "opendepot-monitoring-prometheus"},
			},
		},
		"legacy module values disable iac": {
			values: map[string]string{
				"module.enabled":   "false",
				"provider.enabled": "false",
			},
			absent: [][2]string{
				{"Deployment", "iac-controller"},
				{"ServiceAccount", "iac-controller"},
			},
		},
		"legacy module and provider values render iac": {
			values: map[string]string{
				"module.enabled":   "true",
				"provider.enabled": "true",
			},
			present: [][2]string{{"Deployment", "iac-controller"}},
			absent: [][2]string{
				{"Deployment", "module-controller"},
				{"Deployment", "provider-controller"},
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
				"server.oidc.clientCredentialsAudiences":  "{ci-pipeline,svc-other}",
			},
			contains: []string{
				"--anonymous-auth",
				"--oidc-issuer-url=https://issuer.example.com",
				"--oidc-groups-claim=roles",
				"--oidc-allow-sa-fallback",
				"--oidc-allow-client-credentials",
				"--oidc-client-credentials-audiences=ci-pipeline,svc-other",
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
	require.Len(t, version.Spec.Template.Spec.Containers, 2)
	args := strings.Join(version.Spec.Template.Spec.Containers[1].Args, " ")
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

func TestServerClientCredentialsAudiencesRequired(t *testing.T) {
	tests := map[string]struct {
		audiences string
		wantErr   string
		contains  []string
	}{
		"empty audiences fail": {
			audiences: "",
			wantErr:   "server.oidc.clientCredentialsAudiences must list the Dex client IDs",
		},
		"non-empty audiences render": {
			audiences: "{ci-pipeline,svc-other}",
			contains: []string{
				"--oidc-allow-client-credentials",
				"--oidc-client-credentials-audiences=ci-pipeline,svc-other",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			options := &helm.Options{
				KubectlOptions: &k8s.KubectlOptions{Namespace: "default"},
				Logger:         logger.Discard,
				SetValues: map[string]string{
					"server.oidc.enabled":                    "true",
					"server.oidc.dexProxy.enabled":           "false",
					"server.oidc.issuerUrl":                  "https://issuer.example.com",
					"server.oidc.allowClientCredentials":     "true",
					"server.oidc.clientCredentialsAudiences": test.audiences,
				},
			}

			rendered, err := helm.RenderTemplateE(t, options, chartPath(t), "opendepot", []string{"templates/server-deployment.yaml"})
			if test.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErr)
				return
			}

			require.NoError(t, err)
			for _, expected := range test.contains {
				assert.Contains(t, rendered, expected)
			}
		})
	}
}

func TestIaCControllerValueFallback(t *testing.T) {
	tests := map[string]struct {
		values      map[string]string
		contains    []string
		notContains []string
	}{
		"defaults": {
			contains: []string{
				"name: iac-controller",
				"replicas: 1",
				`image: "ghcr.io/tonedefdev/opendepot/iac-controller:`,
				"cpu: 1000m",
			},
			notContains: []string{"module-controller", "provider-controller"},
		},
		"iac values": {
			values: map[string]string{
				"iac.replicaCount":     "2",
				"iac.image.repository": "registry.example.com/iac",
				"iac.image.tag":        "v1.2.3",
				"iac.zapLogLevel":      "5",
			},
			contains: []string{
				"replicas: 2",
				`image: "registry.example.com/iac:v1.2.3"`,
				"--zap-log-level=5",
			},
		},
		"legacy module values fall back": {
			values: map[string]string{
				"module.replicaCount":     "2",
				"module.image.repository": "registry.example.com/module",
				"module.zapLogLevel":      "4",
			},
			contains: []string{
				"replicas: 2",
				`image: "registry.example.com/module:`,
				"--zap-log-level=4",
			},
		},
		"legacy provider values fall back": {
			values: map[string]string{
				"provider.image.tag": "v9.9.9",
			},
			contains: []string{`iac-controller:v9.9.9"`},
		},
		"iac takes precedence over legacy values": {
			values: map[string]string{
				"module.replicaCount":   "2",
				"provider.replicaCount": "4",
				"iac.replicaCount":      "3",
			},
			contains:    []string{"replicas: 3"},
			notContains: []string{"replicas: 2", "replicas: 4"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			rendered := renderTemplate(t, test.values, nil, "templates/iac-deployment.yaml")
			for _, expected := range test.contains {
				assert.Contains(t, rendered, expected)
			}
			for _, unexpected := range test.notContains {
				assert.NotContains(t, rendered, unexpected)
			}
		})
	}
}

func TestOCIControllerEnablement(t *testing.T) {
	ociResources := [][2]string{
		{"Deployment", "oci-controller"},
		{"ServiceAccount", "oci-controller"},
		{"ClusterRole", "oci-controller-role"},
		{"ClusterRoleBinding", "oci-controller-role-binding"},
	}

	tests := map[string]struct {
		values   map[string]string
		present  bool
		flagHits int
		flag     string
	}{
		"disabled by default": {
			values:   map[string]string{},
			present:  false,
			flagHits: 0,
		},
		"enabled with an empty allow-list": {
			values:   map[string]string{"oci.enabled": "true"},
			present:  true,
			flagHits: 4,
			flag:     `"--oci-allowed-registries="`,
		},
		"enabled with one registry": {
			values: map[string]string{
				"oci.enabled":              "true",
				"oci.allowedRegistries[0]": "ghcr.io",
			},
			present:  true,
			flagHits: 4,
			flag:     `"--oci-allowed-registries=ghcr.io"`,
		},
		"enabled with several registries": {
			values: map[string]string{
				"oci.enabled":              "true",
				"oci.allowedRegistries[0]": "ghcr.io",
				"oci.allowedRegistries[1]": "docker.io",
			},
			present:  true,
			flagHits: 4,
			flag:     `"--oci-allowed-registries=ghcr.io,docker.io"`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			rendered := renderChart(t, test.values, nil)
			resources := renderedResources(t, rendered)
			for _, source := range ociResources {
				assert.Equal(t, test.present, hasResource(resources, source[0], source[1]), "unexpected render state for %s/%s", source[0], source[1])
			}

			if test.flag == "" {
				assert.NotContains(t, rendered, "--oci-allowed-registries")
				return
			}

			assert.Equal(t, test.flagHits, strings.Count(rendered, test.flag), "unexpected count for %q", test.flag)
		})
	}
}

func TestNotesDeprecationWarning(t *testing.T) {
	tests := map[string]struct {
		values   map[string]string
		warning  bool
		contains []string
	}{
		"default install is silent": {
			values:  map[string]string{},
			warning: false,
		},
		"module values warn": {
			values:   map[string]string{"module.replicaCount": "2"},
			warning:  true,
			contains: []string{"module.* -> iac.*"},
		},
		"provider values warn": {
			values:   map[string]string{"provider.enabled": "true"},
			warning:  true,
			contains: []string{"provider.* -> iac.*"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			notes := renderNotes(t, test.values)
			if !test.warning {
				assert.NotContains(t, notes, "deprecated")
				return
			}

			assert.Contains(t, notes, "the module.* and provider.* values are deprecated")
			for _, expected := range test.contains {
				assert.Contains(t, notes, expected)
			}
		})
	}
}

// renderNotes renders templates/NOTES.txt. helm template skips NOTES, so the chart is copied to a temporary
// directory and the NOTES body is wrapped in a ConfigMap. The template logic that runs is the same.
func renderNotes(t *testing.T, values map[string]string) string {
	t.Helper()

	chartCopy := t.TempDir()
	copyChart(t, chartPath(t), chartCopy)

	templates := filepath.Join(chartCopy, "templates")
	notes, err := os.ReadFile(filepath.Join(templates, "NOTES.txt"))
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(templates, "NOTES.txt")))

	notesDefinition := "{{- define \"opendepot.notesBody\" -}}\n" + string(notes) + "\n{{- end -}}\n"
	require.NoError(t, os.WriteFile(filepath.Join(templates, "_notes.tpl"), []byte(notesDefinition), 0o600))

	wrapper := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: notes-check\ndata:\n  notes: |\n" +
		"{{ include \"opendepot.notesBody\" . | indent 4 }}\n"
	require.NoError(t, os.WriteFile(filepath.Join(templates, "notes-check.yaml"), []byte(wrapper), 0o600))

	return renderedChart(t, chartCopy, values, []string{"templates/notes-check.yaml"})
}

func renderedChart(t *testing.T, chart string, values map[string]string, templateFiles []string) string {
	t.Helper()

	rendered, err := helm.RenderTemplateE(
		t,
		&helm.Options{
			KubectlOptions: &k8s.KubectlOptions{Namespace: "default"},
			Logger:         logger.Discard,
			SetValues:      values,
		},
		chart,
		"opendepot",
		templateFiles,
	)
	require.NoError(t, err)
	return rendered
}

func copyChart(t *testing.T, source, destination string) {
	t.Helper()

	require.NoError(t, filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}

		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, content, 0o600)
	}))
}
