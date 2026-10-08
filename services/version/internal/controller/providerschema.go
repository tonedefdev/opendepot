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
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
	"github.com/zclconf/go-cty/cty"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema"
	"github.com/tonedefdev/opendepot/pkg/hclschema/providerschema"
	"github.com/tonedefdev/opendepot/pkg/storage"
	"github.com/tonedefdev/opendepot/pkg/storage/types"
	opendepotUtils "github.com/tonedefdev/opendepot/pkg/utils"
)

// maxReducedSchemaBytes caps how much decompressed schema JSON is read back from storage.
// The largest published providers reduce to well under 128 MiB.
const maxReducedSchemaBytes = 128 << 20

// extractProviderSchema runs the provider binary through `tofu providers schema -json`,
// reduces the result and stores it in the configured storage backend. It returns nil, nil
// when the Version's platform does not match the controller's own, since a schema is
// platform independent and one extraction serves every platform of that provider version.
func (r *VersionReconciler) extractProviderSchema(ctx context.Context, version *opendepotv1alpha1.Version, archivePath string) (*opendepotv1alpha1.ProviderSchemaRef, error) {
	if version.Spec.OperatingSystem != runtime.GOOS || version.Spec.Architecture != runtime.GOARCH {
		r.Log.V(5).Info("skipping provider schema extraction: platform does not match the controller",
			"version", version.Name, "os", version.Spec.OperatingSystem, "arch", version.Spec.Architecture)

		return nil, nil
	}

	if version.Spec.ProviderConfigRef == nil || version.Spec.ProviderConfigRef.Name == nil {
		return nil, fmt.Errorf("providerConfigRef.name is required to extract a provider schema")
	}

	providerNamespace := "hashicorp"
	if version.Spec.ProviderConfigRef.Namespace != nil {
		if ns := strings.TrimSpace(*version.Spec.ProviderConfigRef.Namespace); ns != "" {
			providerNamespace = ns
		}
	}

	providerName := strings.TrimSpace(*version.Spec.ProviderConfigRef.Name)
	providerVersion := strings.TrimPrefix(strings.TrimSpace(version.Spec.Version), "v")

	workDir, err := os.MkdirTemp("", "opendepot-schema-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for provider schema extraction: %w", err)
	}
	defer os.RemoveAll(workDir)

	extractCtx, cancel := context.WithTimeout(ctx, r.SchemaExtractionTimeout)
	defer cancel()

	raw, err := providerschema.Extract(extractCtx, providerschema.ExtractInput{
		ProviderZipPath: archivePath,
		RegistryHost:    opendepotv1alpha1.ProviderUpstreamRegistry(version.Spec.ProviderConfigRef),
		Namespace:       providerNamespace,
		Name:            providerName,
		Version:         providerVersion,
		OS:              version.Spec.OperatingSystem,
		Arch:            version.Spec.Architecture,
		WorkDir:         workDir,
		TofuBinPath:     r.TofuBinPath,
	})
	if err != nil {
		return nil, err
	}

	reduced, err := providerschema.Reduce(raw, providerNamespace+"/"+providerName, providerVersion)
	if err != nil {
		return nil, err
	}

	reducedJSON, err := json.Marshal(reduced)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal reduced provider schema: %w", err)
	}

	digest := sha256.Sum256(reducedJSON)

	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	if _, err := gw.Write(reducedJSON); err != nil {
		return nil, fmt.Errorf("failed to gzip reduced provider schema: %w", err)
	}

	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalise gzipped reduced provider schema: %w", err)
	}

	archiveChecksum := sha256.Sum256(compressed.Bytes())
	archiveChecksumBase64 := base64.StdEncoding.EncodeToString(archiveChecksum[:])

	schemaPath, err := getProviderSchemaFilePath(version)
	if err != nil {
		return nil, err
	}

	soi := &types.StorageObjectInput{
		ArchiveChecksum: &archiveChecksumBase64,
		Method:          types.Put,
		FileBytes:       compressed.Bytes(),
		FilePath:        schemaPath,
		Version:         version,
	}

	if err := r.InitStorageFactory(ctx, soi); err != nil {
		return nil, fmt.Errorf("failed to store reduced provider schema: %w", err)
	}

	size := soi.BytesWritten
	if size == 0 {
		size = int64(compressed.Len())
	}

	r.Log.V(5).Info("stored reduced provider schema",
		"version", version.Name, "key", *schemaPath, "sizeBytes", size)

	return &opendepotv1alpha1.ProviderSchemaRef{
		Key:         *schemaPath,
		Digest:      hex.EncodeToString(digest[:]),
		ExtractedAt: time.Now().UTC().Format(time.RFC3339),
		SizeBytes:   size,
	}, nil
}

// getProviderSchemaFilePath computes the storage key for a provider Version's reduced
// schema. It mirrors getVersionFilePath's prefix handling so the schema lands alongside
// the provider artifacts rather than in a separate container.
func getProviderSchemaFilePath(version *opendepotv1alpha1.Version) (*string, error) {
	storageConfig, err := getVersionStorageConfig(version)
	if err != nil {
		return nil, err
	}

	name, err := getVersionName(version)
	if err != nil {
		return nil, err
	}

	suffix := fmt.Sprintf("%s/schemas/%s/schema.json.gz", *name, opendepotUtils.SanitizeVersion(version.Spec.Version))

	if storageConfig.S3 != nil && storageConfig.S3.Key != nil {
		sanitized, err := storage.RemoveTrailingSlash(storageConfig.S3.Key)
		if err != nil {
			return nil, err
		}

		filePath := fmt.Sprintf("%s/%s", *sanitized, suffix)

		return &filePath, nil
	}

	if storageConfig.FileSystem != nil && storageConfig.FileSystem.DirectoryPath != nil {
		sanitized, err := storage.RemoveTrailingSlash(storageConfig.FileSystem.DirectoryPath)
		if err != nil {
			return nil, err
		}

		filePath := fmt.Sprintf("%s/%s", *sanitized, suffix)

		return &filePath, nil
	}

	return &suffix, nil
}

// schemaResolver implements hclschema.SchemaResolver over the reduced provider schemas
// that were loaded for a module's required providers.
type schemaResolver struct {
	schemas    []*providerschema.ReducedSchema
	provenance []hclschema.ProvenanceSchema
}

// Lookup resolves a resource attribute against the first provider schema that declares
// the resource type.
func (s *schemaResolver) Lookup(resourceType, attrPath string, isData bool) (cty.Type, bool) {
	for _, schema := range s.schemas {
		if t, ok := schema.Lookup(resourceType, attrPath, isData); ok {
			return t, true
		}
	}

	return cty.DynamicPseudoType, false
}

// ResourceAttributes returns a resource type's full attribute object.
func (s *schemaResolver) ResourceAttributes(resourceType string, isData bool) (cty.Type, bool) {
	for _, schema := range s.schemas {
		if t, ok := schema.ResourceAttributes(resourceType, isData); ok {
			return t, true
		}
	}

	return cty.DynamicPseudoType, false
}

// Provenance reports which provider schemas backed this resolver.
func (s *schemaResolver) Provenance() []hclschema.ProvenanceSchema {
	return s.provenance
}

// buildSchemaResolver loads the reduced schema for every provider a module requires.
// Providers that are not onboarded, or whose onboarded versions do not satisfy the
// module's constraint, are skipped and reported as compatibility warnings. Returns a nil
// resolver when no schema at all could be loaded.
func (r *VersionReconciler) buildSchemaResolver(ctx context.Context, version *opendepotv1alpha1.Version, required []hclschema.RequiredProvider) (*schemaResolver, []string) {
	if len(required) == 0 {
		return nil, nil
	}

	providerVersions := &opendepotv1alpha1.VersionList{}
	if err := r.List(ctx, providerVersions, client.InNamespace(version.Namespace)); err != nil {
		return nil, []string{fmt.Sprintf("failed to list provider versions for schema resolution: %v", err)}
	}

	resolver := &schemaResolver{}
	var warnings []string

	for _, rp := range required {
		match := selectProviderSchemaVersion(providerVersions.Items, rp)
		if match == nil {
			warnings = append(warnings, fmt.Sprintf(
				"provider %s is not onboarded with an extracted schema; output types for its resources are best-effort", rp.Source))

			continue
		}

		schema, err := r.loadReducedSchema(ctx, match)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to load the schema for provider %s: %v", rp.Source, err))
			continue
		}

		resolver.schemas = append(resolver.schemas, schema)
		resolver.provenance = append(resolver.provenance, hclschema.ProvenanceSchema{
			Provider: rp.Source,
			Version:  opendepotUtils.SanitizeVersion(match.Spec.Version),
			Digest:   match.Status.ProviderSchemaRef.Digest,
		})
	}

	if len(resolver.schemas) == 0 {
		return nil, warnings
	}

	return resolver, warnings
}

// selectProviderSchemaVersion picks the highest onboarded provider Version that has an
// extracted schema and satisfies the module's declared version constraint.
func selectProviderSchemaVersion(candidates []opendepotv1alpha1.Version, rp hclschema.RequiredProvider) *opendepotv1alpha1.Version {
	wantNamespace, wantName, ok := splitProviderSource(rp.Source)
	if !ok {
		return nil
	}

	var constraints goversion.Constraints
	if trimmed := strings.TrimSpace(rp.VersionConstraint); trimmed != "" {
		parsed, err := goversion.NewConstraint(trimmed)
		if err == nil {
			constraints = parsed
		}
	}

	var best *opendepotv1alpha1.Version
	var bestVersion *goversion.Version

	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Spec.Type != opendepotv1alpha1.OpenDepotProvider ||
			candidate.Status.ProviderSchemaRef == nil ||
			candidate.Spec.ProviderConfigRef == nil ||
			candidate.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		candidateNamespace := "hashicorp"
		if candidate.Spec.ProviderConfigRef.Namespace != nil {
			if ns := strings.TrimSpace(*candidate.Spec.ProviderConfigRef.Namespace); ns != "" {
				candidateNamespace = ns
			}
		}

		if candidateNamespace != wantNamespace || *candidate.Spec.ProviderConfigRef.Name != wantName {
			continue
		}

		parsed, err := goversion.NewVersion(opendepotUtils.SanitizeVersion(candidate.Spec.Version))
		if err != nil {
			continue
		}

		if constraints != nil && !constraints.Check(parsed) {
			continue
		}

		if bestVersion == nil || parsed.GreaterThan(bestVersion) {
			best = candidate
			bestVersion = parsed
		}
	}

	return best
}

// splitProviderSource splits a "<namespace>/<name>" provider source address.
func splitProviderSource(source string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}

	return parts[0], parts[1], true
}

// loadReducedSchema streams a provider Version's reduced schema out of the storage
// backend and decodes it.
func (r *VersionReconciler) loadReducedSchema(ctx context.Context, version *opendepotv1alpha1.Version) (*providerschema.ReducedSchema, error) {
	key := version.Status.ProviderSchemaRef.Key
	soi := &types.StorageObjectInput{
		Method:   types.Get,
		FilePath: &key,
		Version:  version,
	}

	storageInterface, err := r.resolveStorageInterface(ctx, soi)
	if err != nil {
		return nil, err
	}

	reader, err := storageInterface.GetObject(ctx, soi)
	if err != nil {
		return nil, fmt.Errorf("failed to read provider schema %s: %w", key, err)
	}

	if reader == nil {
		return nil, fmt.Errorf("failed to read provider schema %s: storage backend returned no reader", key)
	}

	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	gr, err := gzip.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to gunzip provider schema %s: %w", key, err)
	}
	defer gr.Close()

	raw, err := io.ReadAll(io.LimitReader(gr, maxReducedSchemaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read provider schema %s: %w", key, err)
	}

	if len(raw) > maxReducedSchemaBytes {
		return nil, fmt.Errorf("provider schema %s exceeds the %d byte limit", key, maxReducedSchemaBytes)
	}

	digest := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), version.Status.ProviderSchemaRef.Digest) {
		return nil, fmt.Errorf("provider schema %s digest does not match its status reference", key)
	}

	var schema providerschema.ReducedSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("failed to decode provider schema %s: %w", key, err)
	}

	return &schema, nil
}
