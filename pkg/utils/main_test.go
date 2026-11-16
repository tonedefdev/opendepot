package utils

import (
	"reflect"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func intPtr(i int) *int {
	return &i
}

func testModule(limit *int, versions ...string) *opendepotv1alpha1.Module {
	module := &opendepotv1alpha1.Module{}
	module.Spec.ModuleConfig.VersionHistoryLimit = limit
	for _, v := range versions {
		module.Spec.Versions = append(module.Spec.Versions, opendepotv1alpha1.ModuleVersion{Version: v})
	}
	return module
}

func testProvider(limit *int, versions ...string) *opendepotv1alpha1.Provider {
	provider := &opendepotv1alpha1.Provider{}
	provider.Spec.ProviderConfig.VersionHistoryLimit = limit
	for _, v := range versions {
		provider.Spec.Versions = append(provider.Spec.Versions, opendepotv1alpha1.ProviderVersion{Version: v})
	}
	return provider
}

func TestVersionsToKeepModule(t *testing.T) {
	versions := []string{"1.0.0", "1.2.0", "1.10.0"}

	tests := []struct {
		name  string
		limit *int
		want  []string
	}{
		{name: "nil limit keeps all", limit: nil, want: nil},
		{name: "zero limit keeps all", limit: intPtr(0), want: nil},
		{name: "negative limit keeps all", limit: intPtr(-1), want: nil},
		{name: "limit below count keeps newest", limit: intPtr(2), want: []string{"v1.2.0", "v1.10.0"}},
		{name: "limit equal to count keeps all", limit: intPtr(3), want: []string{"v1.0.0", "v1.2.0", "v1.10.0"}},
		{name: "limit above count keeps all", limit: intPtr(5), want: []string{"v1.0.0", "v1.2.0", "v1.10.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VersionsToKeep(testModule(tt.limit, versions...), nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("VersionsToKeep() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionsToKeepProvider(t *testing.T) {
	versions := []string{"1.0.0", "1.2.0", "1.10.0"}

	tests := []struct {
		name  string
		limit *int
		want  []string
	}{
		{name: "nil limit keeps all", limit: nil, want: nil},
		{name: "limit below count keeps newest", limit: intPtr(1), want: []string{"v1.10.0"}},
		{name: "limit above count keeps all", limit: intPtr(10), want: []string{"v1.0.0", "v1.2.0", "v1.10.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VersionsToKeep(nil, testProvider(tt.limit, versions...))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("VersionsToKeep() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionsToKeepNilInputs(t *testing.T) {
	if _, err := VersionsToKeep(nil, nil); err == nil {
		t.Error("expected error when both module and provider are nil")
	}
}
