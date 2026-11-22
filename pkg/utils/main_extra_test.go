package utils

import (
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func moduleWithVersions(name string, configName *string, versions ...string) *opendepotv1alpha1.Module {
	module := &opendepotv1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: name}}
	module.Spec.ModuleConfig.Name = configName

	for _, v := range versions {
		module.Spec.Versions = append(module.Spec.Versions, opendepotv1alpha1.ModuleVersion{Version: v})
	}

	return module
}

func providerWithVersions(name string, configName *string, versions ...string) *opendepotv1alpha1.Provider {
	provider := &opendepotv1alpha1.Provider{ObjectMeta: metav1.ObjectMeta{Name: name}}
	provider.Spec.ProviderConfig.Name = configName

	for _, v := range versions {
		provider.Spec.Versions = append(provider.Spec.Versions, opendepotv1alpha1.ProviderVersion{Version: v})
	}

	return provider
}

func TestGetLatestVersionModule(t *testing.T) {
	t.Run("returns the highest canonical semver", func(t *testing.T) {
		module := moduleWithVersions("m", nil, "1.2.0", "v1.10.0", "1.9.3")

		got, err := GetLatestVersion(module, nil)
		if err != nil {
			t.Fatalf("GetLatestVersion returned error: %v", err)
		}

		if got == nil || *got != "v1.10.0" {
			t.Fatalf("GetLatestVersion = %v, want v1.10.0", got)
		}
	})

	t.Run("returns nil when there are no versions", func(t *testing.T) {
		got, err := GetLatestVersion(moduleWithVersions("m", nil), nil)
		if err != nil || got != nil {
			t.Fatalf("GetLatestVersion = %v, %v; want nil, nil", got, err)
		}
	})
}

func TestGetLatestVersionProvider(t *testing.T) {
	t.Run("returns the highest version and skips duplicates", func(t *testing.T) {
		provider := providerWithVersions("p", nil, "3.5.0", "3.6.0", "3.6.0", "v3.5.1")

		got, err := GetLatestVersion(nil, provider)
		if err != nil {
			t.Fatalf("GetLatestVersion returned error: %v", err)
		}

		if got == nil || *got != "v3.6.0" {
			t.Fatalf("GetLatestVersion = %v, want v3.6.0", got)
		}
	})

	t.Run("returns nil when there are no versions", func(t *testing.T) {
		got, err := GetLatestVersion(nil, providerWithVersions("p", nil))
		if err != nil || got != nil {
			t.Fatalf("GetLatestVersion = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("requires a module or provider", func(t *testing.T) {
		if _, err := GetLatestVersion(nil, nil); err == nil {
			t.Fatal("GetLatestVersion returned no error when both arguments were nil")
		}
	})
}

func TestVersionsToKeepKeepsNewestWhenLimitExceedsCount(t *testing.T) {
	limit := 10
	module := moduleWithVersions("m", nil, "1.0.0", "2.0.0")
	module.Spec.ModuleConfig.VersionHistoryLimit = &limit

	got, err := VersionsToKeep(module, nil)
	if err != nil {
		t.Fatalf("VersionsToKeep returned error: %v", err)
	}

	if len(got) != 2 || got[0] != "v1.0.0" || got[1] != "v2.0.0" {
		t.Fatalf("VersionsToKeep = %v, want all versions in ascending order", got)
	}
}

func TestGetName(t *testing.T) {
	configured := "configured"

	t.Run("module uses the object name without config name", func(t *testing.T) {
		got, err := GetName(moduleWithVersions("object-name", nil), nil)
		if err != nil || *got != "object-name" {
			t.Fatalf("GetName = %v, %v; want object-name", got, err)
		}
	})

	t.Run("module prefers config name", func(t *testing.T) {
		got, err := GetName(moduleWithVersions("object-name", &configured), nil)
		if err != nil || *got != configured {
			t.Fatalf("GetName = %v, %v; want %s", got, err, configured)
		}
	})

	t.Run("provider uses the object name without config name", func(t *testing.T) {
		got, err := GetName(nil, providerWithVersions("provider-name", nil))
		if err != nil || *got != "provider-name" {
			t.Fatalf("GetName = %v, %v; want provider-name", got, err)
		}
	})

	t.Run("provider prefers config name", func(t *testing.T) {
		got, err := GetName(nil, providerWithVersions("provider-name", &configured))
		if err != nil || *got != configured {
			t.Fatalf("GetName = %v, %v; want %s", got, err, configured)
		}
	})

	t.Run("requires a module or provider", func(t *testing.T) {
		if _, err := GetName(nil, nil); err == nil {
			t.Fatal("GetName returned no error when both arguments were nil")
		}
	})
}

func TestGetVersionName(t *testing.T) {
	configured := "configured"

	tests := []struct {
		name     string
		module   *opendepotv1alpha1.Module
		provider *opendepotv1alpha1.Provider
		want     string
	}{
		{name: "module object name", module: moduleWithVersions("mod", nil), want: "mod-1.0.0"},
		{name: "module config name", module: moduleWithVersions("mod", &configured), want: "configured-1.0.0"},
		{name: "provider object name", provider: providerWithVersions("prov", nil), want: "prov-1.0.0"},
		{name: "provider config name", provider: providerWithVersions("prov", &configured), want: "configured-1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetVersionName(tt.module, tt.provider, "1.0.0")
			if err != nil {
				t.Fatalf("GetVersionName returned error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("GetVersionName = %q, want %q", got, tt.want)
			}
		})
	}

	if _, err := GetVersionName(nil, nil, "1.0.0"); err == nil {
		t.Fatal("GetVersionName returned no error when both arguments were nil")
	}
}

func TestSanitizeVersion(t *testing.T) {
	tests := map[string]string{
		"v1.2.3":     "1.2.3",
		"  v1.2.3 ":  "1.2.3",
		"1.2.3":      "1.2.3",
		"":           "",
		"v":          "",
		"  ":         "",
		"vv1.0.0":    "v1.0.0",
		" 2.0.0-rc1": "2.0.0-rc1",
	}

	for in, want := range tests {
		if got := SanitizeVersion(in); got != want {
			t.Errorf("SanitizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
