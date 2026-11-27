/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
)

// ociTestCVE is the vulnerability ID in ociTrivyReport.
const ociTestCVE = "CVE-2026-0001"

// ociTrivyReport is a Trivy JSON report with one HIGH vulnerability.
const ociTrivyReport = `{"SchemaVersion":2,"Results":[{"Target":"alpine","Class":"os-pkgs","Type":"alpine","Vulnerabilities":[{"VulnerabilityID":"CVE-2026-0001","PkgName":"openssl","InstalledVersion":"1.1.1k","FixedVersion":"1.1.1l","Severity":"HIGH","Title":"test vulnerability"}]}]}`

// stubTrivy replaces runTrivy for the rest of the spec. The stub calls inspect with the arguments while the staged scan
// input still exists, and then returns output.
func stubTrivy(output string, inspect func(args []string)) {
	original := runTrivy
	runTrivy = func(_ context.Context, args ...string) ([]byte, error) {
		if inspect != nil {
			inspect(args)
		}

		return []byte(output), nil
	}

	DeferCleanup(func() { runTrivy = original })
}

// argValue returns the value that follows flag in args, or an empty string when the flag is absent.
func argValue(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}

	return ""
}

// tarEntry is one member of a test archive.
type tarEntry struct {
	header tar.Header
	body   string
}

// buildGzipTar returns a gzip-compressed tar archive with the given entries.
func buildGzipTar(entries []tarEntry) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := entry.header
		if header.Typeflag == tar.TypeReg {
			header.Size = int64(len(entry.body))
			header.Mode = 0o644
		}

		Expect(tw.WriteHeader(&header)).To(Succeed())
		if header.Typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(entry.body))
			Expect(err).NotTo(HaveOccurred())
		}
	}

	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())

	return buf.Bytes()
}

// seedGenericArtifact stores a single-layer Generic artifact and tags it as 1.0.0.
func seedGenericArtifact(f *fakeOCIRegistry) ocispec.Descriptor {
	config := f.addBlob(ocispec.MediaTypeEmptyJSON, []byte("{}"))
	layer := f.addBlob("application/vnd.example.asset", []byte("generic asset payload"))
	desc := f.addImage(config, "application/vnd.example.artifact.v1", layer)
	f.tag("1.0.0", desc)

	return desc
}

var _ = Describe("OCIArtifact Trivy scans", func() {
	var (
		ctx        = context.Background()
		storageDir string
		registry   *fakeOCIRegistry
	)

	BeforeEach(func() {
		storageDir = GinkgoT().TempDir()
		registry = newFakeOCIRegistry()
		ensureOCITestNamespace()
	})

	Context("when scanning an Image", func() {
		It("writes an unpacked OCI layout, runs Trivy offline on it, and removes the layout afterwards", func() {
			index := seedImageIndex(registry, "scan-image")
			reconciler := newOCIReconciler(registry)
			reconciler.TrivyCacheDir = "/cache/trivy"

			version := createOCIVersionObject("scan-image", mirrorConfig("Image", storageDir))
			blobs, err := reconciler.ociBlobStoreFor(ctx, version)
			Expect(err).NotTo(HaveOccurred())

			root, err := registry.FetchManifest(ctx, "registry.example.com/team/app", index.Digest.String(), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(mirrorOCIContent(ctx, registry, "registry.example.com/team/app", blobs, root, maxImageBlobBytes)).To(Succeed())

			var args []string
			var layoutDir string
			stubTrivy(ociTrivyReport, func(a []string) {
				args = a
				layoutDir = argValue(a, "--input")
				Expect(filepath.Join(layoutDir, "oci-layout")).To(BeAnExistingFile())
				Expect(filepath.Join(layoutDir, "index.json")).To(BeAnExistingFile())
				Expect(filepath.Join(layoutDir, "blobs", "sha256", index.Digest.Encoded())).To(BeAnExistingFile())
				amd64Layer := digestOf("amd64 layer scan-image")
				Expect(filepath.Join(layoutDir, "blobs", "sha256", amd64Layer.Encoded())).To(BeAnExistingFile())
			})

			findings, err := reconciler.scanOCIArtifact(ctx, "Image", ociScanInput{root: root, blobs: blobs})
			Expect(err).NotTo(HaveOccurred())
			Expect(args).To(ContainElements(
				"image", "--format", "json", "--skip-db-update", "--skip-java-db-update", "--offline-scan",
				"--cache-dir", "/cache/trivy", "--quiet",
			))
			Expect(argValue(args, "--input")).NotTo(BeEmpty())
			Expect(findings).To(HaveLen(1))
			Expect(findings[0].VulnerabilityID).To(Equal(ociTestCVE))
			Expect(layoutDir).NotTo(BeAnExistingFile())
		})
	})

	Context("when scanning a Helm chart", func() {
		It("unpacks the chart and runs trivy config on the directory", func() {
			chartPath := filepath.Join(storageDir, "mychart-1.0.0.tgz")
			Expect(os.WriteFile(chartPath, buildChartTgz(), 0o600)).To(Succeed())

			var args []string
			stubTrivy(ociTrivyReport, func(a []string) {
				args = a
				target := a[len(a)-1]
				Expect(filepath.Join(target, "mychart", "Chart.yaml")).To(BeAnExistingFile())
			})

			reconciler := newOCIReconciler(registry)
			findings, err := reconciler.scanOCIArtifact(ctx, "Helm", ociScanInput{filePath: chartPath})
			Expect(err).NotTo(HaveOccurred())
			Expect(args[:2]).To(Equal([]string{"config", "--skip-check-update"}))
			Expect(args).NotTo(ContainElement("--offline-scan"))
			Expect(findings).To(HaveLen(1))
		})
	})

	Context("when scanning a Generic artifact", func() {
		It("stages the asset and runs trivy fs offline on the directory", func() {
			assetPath := filepath.Join(storageDir, "tool.bin")
			Expect(os.WriteFile(assetPath, []byte("generic asset payload"), 0o600)).To(Succeed())

			var args []string
			stubTrivy(ociTrivyReport, func(a []string) {
				args = a
				target := a[len(a)-1]
				staged, err := os.ReadFile(filepath.Join(target, "artifact"))
				Expect(err).NotTo(HaveOccurred())
				Expect(string(staged)).To(Equal("generic asset payload"))
			})

			reconciler := newOCIReconciler(registry)
			findings, err := reconciler.scanOCIArtifact(ctx, "Generic", ociScanInput{filePath: assetPath})
			Expect(err).NotTo(HaveOccurred())
			Expect(args[:2]).To(Equal([]string{"fs", "--skip-db-update"}))
			Expect(args).To(ContainElement("--offline-scan"))
			Expect(findings).To(HaveLen(1))
		})
	})

	Context("when Trivy fails or reports nothing", func() {
		It("fails closed when Trivy produces no output", func() {
			stubTrivy("", nil)

			reconciler := newOCIReconciler(registry)
			assetPath := filepath.Join(storageDir, "tool.bin")
			Expect(os.WriteFile(assetPath, []byte("x"), 0o600)).To(Succeed())

			_, err := reconciler.scanOCIArtifact(ctx, "Generic", ociScanInput{filePath: assetPath})
			Expect(err).To(MatchError(ContainSubstring("produced no output")))
		})

		It("refuses a digest that would name a file outside the layout", func() {
			_, err := layoutBlobPath(GinkgoT().TempDir(), digest.Digest("sha256:../../escape"))
			Expect(errors.Is(err, errOCIInvalid)).To(BeTrue())
		})

		It("refuses an artifactType that cannot be scanned", func() {
			reconciler := newOCIReconciler(registry)
			_, err := reconciler.scanOCIArtifact(ctx, "MCP", ociScanInput{})
			Expect(errors.Is(err, errOCIInvalid)).To(BeTrue())
		})
	})

	Context("when extracting a Helm chart archive", func() {
		DescribeTable("rejects entries that would escape the chart directory or are not regular files",
			func(entry tarEntry) {
				dir := GinkgoT().TempDir()
				archive := buildGzipTar([]tarEntry{entry})

				err := extractChartArchive(bytes.NewReader(archive), dir, maxHelmChartBytes)
				Expect(err).To(HaveOccurred())

				_, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt"))
				Expect(os.IsNotExist(statErr)).To(BeTrue())
			},
			Entry("a parent reference", tarEntry{header: tar.Header{Name: "../escape.txt", Typeflag: tar.TypeReg}, body: "x"}),
			Entry("a nested parent reference", tarEntry{header: tar.Header{Name: "mychart/../../escape.txt", Typeflag: tar.TypeReg}, body: "x"}),
			Entry("an absolute path", tarEntry{header: tar.Header{Name: "/etc/escape.txt", Typeflag: tar.TypeReg}, body: "x"}),
			Entry("a symbolic link", tarEntry{header: tar.Header{Name: "mychart/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}}),
		)

		It("extracts a regular chart archive", func() {
			dir := GinkgoT().TempDir()
			archive := buildChartTgz()

			Expect(extractChartArchive(bytes.NewReader(archive), dir, maxHelmChartBytes)).To(Succeed())
			Expect(filepath.Join(dir, "mychart", "Chart.yaml")).To(BeAnExistingFile())
		})

		It("fails when the decompressed size exceeds the limit", func() {
			dir := GinkgoT().TempDir()
			archive := buildGzipTar([]tarEntry{{header: tar.Header{Name: "mychart/big.txt", Typeflag: tar.TypeReg}, body: string(bytes.Repeat([]byte("a"), 4096))}})

			err := extractChartArchive(bytes.NewReader(archive), dir, 1024)
			Expect(errors.Is(err, errHelmChartTooLarge)).To(BeTrue())
		})

		It("bounds the decompressed size of a chart before it is parsed", func() {
			chartPath := filepath.Join(storageDir, "bounded.tgz")
			Expect(os.WriteFile(chartPath, buildChartTgz(), 0o600)).To(Succeed())

			file, err := os.Open(chartPath)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = file.Close() }()

			Expect(checkChartArchiveBounds(file, maxHelmChartBytes)).To(Succeed())
			Expect(checkChartArchiveBounds(file, 16)).To(MatchError(ContainSubstring("exceeds the maximum decompressed size")))
		})
	})

	Context("when a ScanPolicy governs the scan type oci", func() {
		It("exempts a finding that the policy names for oci and blocks one that it does not", func() {
			findings, err := parseTrivyReport([]byte(ociTrivyReport), nil)
			Expect(err).NotTo(HaveOccurred())

			exempting := policy.Resolve(&opendepotv1alpha1.ScanPolicy{
				Spec: opendepotv1alpha1.ScanPolicySpec{
					Exemptions: []opendepotv1alpha1.ScanExemption{{
						VulnerabilityIDs: []string{ociTestCVE},
						ScanTypes:        []string{ociScanType},
						Reason:           "accepted for the test",
					}},
				},
			}, false, true, time.Now())

			annotated, blocking := policy.Apply(exempting, findings, ociScanType)
			Expect(blocking).To(BeNil())
			Expect(annotated[0].Exempted).To(BeTrue())

			wrongType := policy.Resolve(&opendepotv1alpha1.ScanPolicy{
				Spec: opendepotv1alpha1.ScanPolicySpec{
					Exemptions: []opendepotv1alpha1.ScanExemption{{
						VulnerabilityIDs: []string{ociTestCVE},
						ScanTypes:        []string{"module"},
						Reason:           "module only",
					}},
				},
			}, false, true, time.Now())

			annotated, blocking = policy.Apply(wrongType, findings, ociScanType)
			Expect(blocking).NotTo(BeNil())
			Expect(annotated[0].Exempted).To(BeFalse())
		})

		It("blocks the Version on a HIGH finding and records the scan", func() {
			ensureOCITestNamespace()
			registry.tag("1.0.0", seedGenericArtifact(registry))
			stubTrivy(ociTrivyReport, nil)

			config := mirrorConfig("Generic", storageDir)
			createOCIVersion("scan-blocked", config)

			reconciler := newOCIReconciler(registry)
			reconciler.ScanningEnabled = true
			reconciler.BlockOnHigh = true

			_, err := reconcileOCI(reconciler, "scan-blocked")
			Expect(err).To(MatchError(ContainSubstring("Scan policy violation")))

			current := getOCIVersion("scan-blocked")
			Expect(current.Status.Synced).To(BeFalse())
			Expect(current.Status.SyncStatus).To(ContainSubstring(ociTestCVE))
			Expect(current.Status.OCIScan).NotTo(BeNil())
			Expect(current.Status.OCIScan.Findings).To(HaveLen(1))
		})

		It("syncs the Version when a ScanPolicy exempts the finding for oci", func() {
			ensureOCITestNamespace()
			registry.tag("1.0.0", seedGenericArtifact(registry))
			stubTrivy(ociTrivyReport, nil)

			policyObject := &opendepotv1alpha1.ScanPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "oci-exemption", Namespace: ociTestNamespace},
				Spec: opendepotv1alpha1.ScanPolicySpec{
					Exemptions: []opendepotv1alpha1.ScanExemption{{
						VulnerabilityIDs: []string{ociTestCVE},
						ScanTypes:        []string{ociScanType},
						Reason:           "accepted for the test",
					}},
				},
			}
			Expect(k8sClient.Create(ctx, policyObject)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, policyObject) })

			createOCIVersion("scan-exempt", mirrorConfig("Generic", storageDir))

			reconciler := newOCIReconciler(registry)
			reconciler.ScanningEnabled = true
			reconciler.BlockOnHigh = true

			_, err := reconcileOCI(reconciler, "scan-exempt")
			Expect(err).NotTo(HaveOccurred())

			current := getOCIVersion("scan-exempt")
			Expect(current.Status.Synced).To(BeTrue())
			Expect(current.Status.OCIScan).NotTo(BeNil())
			Expect(current.Status.OCIScan.Findings[0].Exempted).To(BeTrue())
			Expect(current.Status.OCIScan.Findings[0].ExemptedBy).To(Equal("oci-exemption"))
		})
	})
})

// createOCIVersionObject returns an in-memory OCIArtifact Version for helpers that need only its storage settings.
func createOCIVersionObject(name string, config *opendepotv1alpha1.OCIArtifactConfig) *opendepotv1alpha1.Version {
	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ociTestNamespace},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:                 opendepotv1alpha1.OpenDepotOCIArtifact,
			Version:              "1.0.0",
			OCIArtifactConfigRef: config,
		},
	}
}

// digestOf returns the digest of content.
func digestOf(content string) digest.Digest {
	return digest.FromBytes([]byte(content))
}
