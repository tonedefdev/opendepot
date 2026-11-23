package controller

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema"
)

func versionCovZip(t *testing.T, names ...string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}

		if _, err := w.Write([]byte("content of " + name)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buf.Bytes()
}

func versionCovTarGz(t *testing.T, names ...string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		content := []byte("content of " + name)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}

		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write tar entry %s: %v", name, err)
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

func TestVersionCovSplitProviderSource(t *testing.T) {
	cases := []struct {
		source   string
		wantNS   string
		wantName string
		wantOK   bool
	}{
		{source: "hashicorp/aws", wantNS: "hashicorp", wantName: "aws", wantOK: true},
		{source: "  hashicorp/aws  ", wantNS: "hashicorp", wantName: "aws", wantOK: true},
		{source: "aws"},
		{source: "/aws"},
		{source: "hashicorp/"},
		{source: "a/b/c"},
		{source: ""},
	}

	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			ns, name, ok := splitProviderSource(tc.source)
			if ok != tc.wantOK || ns != tc.wantNS || name != tc.wantName {
				t.Fatalf("splitProviderSource(%q) = (%q, %q, %t), want (%q, %q, %t)",
					tc.source, ns, name, ok, tc.wantNS, tc.wantName, tc.wantOK)
			}
		})
	}
}

func TestVersionCovSelectProviderSchemaVersion(t *testing.T) {
	provider := func(version string, namespace *string, name string, withSchema bool) opendepotv1alpha1.Version {
		v := opendepotv1alpha1.Version{
			Spec: opendepotv1alpha1.VersionSpec{
				Type:    opendepotv1alpha1.OpenDepotProvider,
				Version: version,
				ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{
					Name:      &name,
					Namespace: namespace,
				},
			},
		}
		if withSchema {
			v.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{}
		}

		return v
	}

	ns := "hashicorp"
	other := "acme"
	candidates := []opendepotv1alpha1.Version{
		provider("5.1.0", &ns, "aws", true),
		provider("5.9.0", &ns, "aws", false),    // no extracted schema
		provider("6.0.0", &ns, "aws", true),     // outside constraint
		provider("5.4.0", &ns, "azurerm", true), // different provider
		provider("5.5.0", &other, "aws", true),  // different namespace
		provider("5.3.0", nil, "aws", true),     // default namespace is hashicorp
	}

	rp := hclschema.RequiredProvider{LocalName: "aws", Source: "hashicorp/aws", VersionConstraint: ">= 5.0, < 6.0"}
	got := selectProviderSchemaVersion(candidates, rp)
	// 5.3.0 defaults to the hashicorp namespace and is the highest eligible schema-bearing 5.x aws version.
	if got == nil || got.Spec.Version != "5.3.0" {
		t.Fatalf("selectProviderSchemaVersion() = %+v, want 5.3.0", got)
	}

	noConstraint := selectProviderSchemaVersion(candidates, hclschema.RequiredProvider{Source: "hashicorp/aws"})
	if noConstraint == nil || noConstraint.Spec.Version != "6.0.0" {
		t.Fatalf("selectProviderSchemaVersion() without constraint = %+v, want 6.0.0", noConstraint)
	}

	if selectProviderSchemaVersion(candidates, hclschema.RequiredProvider{Source: "not-a-source"}) != nil {
		t.Fatalf("selectProviderSchemaVersion() with invalid source returned a match")
	}

	if selectProviderSchemaVersion(candidates, hclschema.RequiredProvider{Source: "hashicorp/missing"}) != nil {
		t.Fatalf("selectProviderSchemaVersion() with unknown provider returned a match")
	}
}

func TestVersionCovIsReadmeEntry(t *testing.T) {
	cases := map[string]bool{
		"README.md":                  true,
		"readme":                     true,
		"ReadMe.rst":                 true,
		"docs/README.md":             true,
		"repo-abc123/README.md":      true,
		"repo-abc123/docs/README.md": false,
		"README.md/extra":            false,
		"main.tf":                    false,
		"readme-notes.md":            false,
		"/README.md":                 true,
	}

	for name, want := range cases {
		if got := isReadmeEntry(name); got != want {
			t.Fatalf("isReadmeEntry(%q) = %t, want %t", name, got, want)
		}
	}
}

func TestVersionCovExtractReadmeFromArchive(t *testing.T) {
	t.Run("zip nested readme", func(t *testing.T) {
		archive := versionCovZip(t, "main.tf", "repo-abc/README.md")
		got, err := extractReadmeFromArchive(archive)
		if err != nil {
			t.Fatalf("extractReadmeFromArchive(zip) error = %v", err)
		}

		if string(got) != "content of repo-abc/README.md" {
			t.Fatalf("extractReadmeFromArchive(zip) = %q", got)
		}
	})

	t.Run("zip without readme", func(t *testing.T) {
		got, err := extractReadmeFromArchive(versionCovZip(t, "main.tf"))
		if err != nil || got != nil {
			t.Fatalf("extractReadmeFromArchive(zip without readme) = (%q, %v), want (nil, nil)", got, err)
		}
	})

	t.Run("tar.gz root readme", func(t *testing.T) {
		archive := versionCovTarGz(t, "main.tf", "README")
		got, err := extractReadmeFromArchive(archive)
		if err != nil {
			t.Fatalf("extractReadmeFromArchive(tar.gz) error = %v", err)
		}

		if string(got) != "content of README" {
			t.Fatalf("extractReadmeFromArchive(tar.gz) = %q", got)
		}
	})

	t.Run("tar.gz without readme", func(t *testing.T) {
		got, err := extractReadmeFromArchive(versionCovTarGz(t, "main.tf"))
		if err != nil || got != nil {
			t.Fatalf("extractReadmeFromArchive(tar.gz without readme) = (%q, %v), want (nil, nil)", got, err)
		}
	})
}

func TestVersionCovHighestAgentSpecFinding(t *testing.T) {
	if got := highestAgentSpecFinding(nil); got != nil {
		t.Fatalf("highestAgentSpecFinding(nil) = %+v, want nil", got)
	}

	findings := []opendepotv1alpha1.SecurityFinding{
		{Severity: "LOW", Title: "low"},
		{Severity: "high", Title: "first high"},
		{Severity: "HIGH", Title: "second high"},
	}

	got := highestAgentSpecFinding(findings)
	if got == nil || got.Title != "first high" {
		t.Fatalf("highestAgentSpecFinding() = %+v, want first high finding", got)
	}

	got.Title = "mutated"
	if findings[1].Title != "mutated" {
		t.Fatalf("highestAgentSpecFinding() should return a pointer into the input slice")
	}

	if got := highestAgentSpecFinding([]opendepotv1alpha1.SecurityFinding{{Severity: "MEDIUM"}}); got != nil {
		t.Fatalf("highestAgentSpecFinding(no HIGH) = %+v, want nil", got)
	}
}

func TestVersionCovGenerateProviderFileName(t *testing.T) {
	got, err := generateProviderFileName("terraform-provider-aws_5.1.0_linux_amd64.zip")
	if err != nil {
		t.Fatalf("generateProviderFileName() error = %v", err)
	}

	if got == nil || len(*got) <= len(".zip") || (*got)[len(*got)-4:] != ".zip" {
		t.Fatalf("generateProviderFileName() = %v, want a generated name ending in .zip", got)
	}

	other, err := generateProviderFileName("terraform-provider-aws_5.1.0_linux_amd64.zip")
	if err != nil {
		t.Fatalf("generateProviderFileName() second call error = %v", err)
	}

	if *other == *got {
		t.Fatalf("generateProviderFileName() returned the same name twice: %q", *got)
	}
}
