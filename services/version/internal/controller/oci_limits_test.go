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
	"bytes"
	"context"
	"errors"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/oci"
)

// countingBlobRegistry counts the blob fetches that reach the upstream and serves every blob from payload.
type countingBlobRegistry struct {
	ociRegistry
	payload []byte
	fetches int
}

func (r *countingBlobRegistry) FetchBlob(_ context.Context, _ string, _ ocispec.Descriptor) (io.ReadCloser, error) {
	r.fetches++
	return io.NopCloser(bytes.NewReader(r.payload)), nil
}

var _ = Describe("OCIArtifact storage and size limits", func() {
	ctx := context.Background()

	Describe("checkOCIFilesystemRoot", func() {
		DescribeTable("keeps the filesystem directory under the mount path",
			func(directoryPath string, wantErr bool) {
				cfg := &opendepotv1alpha1.StorageConfig{
					FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directoryPath},
				}

				err := checkOCIFilesystemRoot(cfg, "/data/modules")
				if wantErr {
					Expect(errors.Is(err, errOCIInvalid)).To(BeTrue())
					return
				}

				Expect(err).NotTo(HaveOccurred())
			},
			Entry("a directory inside the mount path is allowed", "/data/modules/oci", false),
			Entry("the mount path itself is allowed", "/data/modules", false),
			Entry("a .. that stays inside the mount path is allowed", "/data/modules/a/../oci", false),
			Entry("a sibling that shares the prefix is rejected", "/data/modules-evil", true),
			Entry("a path outside the mount path is rejected", "/etc", true),
			Entry("a .. traversal out of the mount path is rejected", "/data/modules/../../etc", true),
			Entry("a relative path is rejected", "modules/oci", true),
		)

		It("skips the check when no mount path is configured", func() {
			directoryPath := "/etc"
			cfg := &opendepotv1alpha1.StorageConfig{
				FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directoryPath},
			}

			Expect(checkOCIFilesystemRoot(cfg, "")).To(Succeed())
		})

		It("skips the check for storage that is not the filesystem", func() {
			Expect(checkOCIFilesystemRoot(&opendepotv1alpha1.StorageConfig{}, "/data/modules")).To(Succeed())
		})
	})

	DescribeTable("ociBlobLimit caps each artifact type",
		func(artifactType string, want int64) {
			Expect(ociBlobLimit(artifactType)).To(Equal(want))
		},
		Entry("a Helm chart layer is capped at 64 MiB", ociArtifactHelm, int64(64<<20)),
		Entry("a Generic layer is capped at 1 GiB", ociArtifactGeneric, int64(1<<30)),
		Entry("an image blob is capped at 4 GiB", ociArtifactImage, int64(4<<30)),
	)

	Describe("mirrorOCIBlob", func() {
		const repository = "registry.example.com/team/chart"

		It("rejects a descriptor larger than its limit before the blob is fetched", func() {
			registry := &countingBlobRegistry{payload: []byte("unused")}
			desc := ocispec.Descriptor{Digest: digest.FromString("unused"), Size: maxHelmLayerBytes + 1}

			err := mirrorOCIBlob(ctx, registry, repository, ociBlobStore{}, desc, maxHelmLayerBytes)
			Expect(errors.Is(err, errOCIBlobTooLarge)).To(BeTrue())
			Expect(errors.Is(err, errOCIInvalid)).To(BeTrue())
			Expect(registry.fetches).To(BeZero())
		})

		It("rejects a stream that overruns its descriptor during spool", func() {
			payload := []byte("0123456789abcdef")
			registry := &countingBlobRegistry{payload: payload}
			desc := ocispec.Descriptor{Digest: digest.FromBytes(payload), Size: 10}

			err := mirrorOCIBlob(ctx, registry, repository, ociBlobStore{}, desc, maxImageBlobBytes)
			Expect(errors.Is(err, errOCIBlobTooLarge)).To(BeTrue())
			Expect(registry.fetches).To(Equal(1))
		})

		It("rejects a stream that ends before its descriptor size", func() {
			payload := []byte("01234")
			registry := &countingBlobRegistry{payload: payload}
			desc := ocispec.Descriptor{Digest: digest.FromBytes(payload), Size: 10}

			err := mirrorOCIBlob(ctx, registry, repository, ociBlobStore{}, desc, maxImageBlobBytes)
			Expect(errors.Is(err, errOCIInvalid)).To(BeTrue())
			Expect(errors.Is(err, errOCIBlobTooLarge)).To(BeFalse())
		})
	})

	Describe("spoolOCIPrimary", func() {
		It("rejects an oversized Helm chart layer before the layer is read", func() {
			root := &oci.ResolvedManifest{
				Descriptor: ocispec.Descriptor{MediaType: ocispec.MediaTypeImageManifest},
				Manifests: []oci.PlatformManifest{{
					Manifest: ocispec.Manifest{
						Layers: []ocispec.Descriptor{{
							MediaType: oci.HelmChartLayerMediaType,
							Digest:    digest.FromString("chart"),
							Size:      maxHelmLayerBytes + 1,
						}},
					},
				}},
			}

			_, err := spoolOCIPrimary(ctx, ociArtifactHelm, root, ociBlobStore{})
			Expect(errors.Is(err, errOCIBlobTooLarge)).To(BeTrue())
		})
	})
})
