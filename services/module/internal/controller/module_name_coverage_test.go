package controller

import (
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestModuleCovGetModuleVersionName(t *testing.T) {
	module := &opendepotv1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: "vpc-module"}}

	if got := getModuleVersionName(module, "1-2-3"); got != "vpc-module-1-2-3" {
		t.Fatalf("without a ModuleConfig name the Module name must be used, got %q", got)
	}

	name := "vpc"
	module.Spec.ModuleConfig.Name = &name
	if got := getModuleVersionName(module, "1-2-3"); got != "vpc-1-2-3" {
		t.Fatalf("the ModuleConfig name must be used when set, got %q", got)
	}
}

func TestModuleCovSanitizeModuleVersion(t *testing.T) {
	cases := map[string]string{
		"1.2.3":     "1-2-3",
		"v1.2.3":    "1-2-3",
		"V1_0_0":    "v1-0-0",
		"":          "",
		"1.0.0-rc1": "1-0-0-rc1",
	}

	for in, want := range cases {
		if got := sanitizeModuleVersion(in); got != want {
			t.Fatalf("sanitizeModuleVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
