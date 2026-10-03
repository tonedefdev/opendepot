package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema/providerschema"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
	opendepotUtils "github.com/tonedefdev/opendepot/pkg/utils"
)

const maxProviderSchemaBytes = 128 << 20

type providerConfigurationSchemaResponse struct {
	Namespace         string               `json:"namespace"`
	Name              string               `json:"name"`
	ProviderNamespace string               `json:"providerNamespace"`
	ProviderName      string               `json:"providerName"`
	Version           string               `json:"version"`
	SchemaVersion     string               `json:"schemaVersion"`
	Configuration     providerschema.Block `json:"configuration"`
}

// handleBrowseProviderSchema returns the configuration schema for one exact provider version.
// GET /opendepot/ui/v1/resources/{namespace}/provider/{name}/provider-schema?version=<semver>
func handleBrowseProviderSchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	requestedVersion := opendepotUtils.SanitizeVersion(r.URL.Query().Get("version"))

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	rawProvider, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("providers").
		Name(name).
		DoRaw(r.Context())
	if err != nil {
		if k8sApiErrors.IsNotFound(err) {
			http.Error(w, "not found", http.StatusNotFound)

			return
		}

		logger.Error("browse: failed to get provider for schema", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	var provider opendepotv1alpha1.Provider
	if err := json.Unmarshal(rawProvider, &provider); err != nil {
		logger.Error("browse: failed to unmarshal provider for schema", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	nsLabels, err := browseGetNamespaceLabels(cs, r, namespace)
	if err != nil {
		logger.Error("browse: failed to get namespace labels for provider schema", "namespace", namespace, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	public := isPublicNamespace(nsLabels) && isPublicResource(provider.Labels)
	if !isBrowseVisible(public, false, allAccess, binding, "provider", provider.Name) {
		http.Error(w, "not found", http.StatusNotFound)

		return
	}

	versions, err := browseListProviderVersions(cs, r, namespace)
	if err != nil {
		logger.Error("browse: failed to list provider versions for schema", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	providerName := name
	if provider.Spec.ProviderConfig.Name != nil && strings.TrimSpace(*provider.Spec.ProviderConfig.Name) != "" {
		providerName = strings.TrimSpace(*provider.Spec.ProviderConfig.Name)
	}

	versions = providerVersionsFor(versions, providerName)
	targetVersion := requestedVersion
	if targetVersion == "" {
		targetVersion = opendepotUtils.SanitizeVersion(derefString(provider.Status.LatestVersion))
	}

	if targetVersion == "" {
		for i := range versions {
			candidate := opendepotUtils.SanitizeVersion(versions[i].Spec.Version)
			if targetVersion == "" || compareVersionDesc(candidate, targetVersion) {
				targetVersion = candidate
			}
		}
	}

	var selected *opendepotv1alpha1.Version
	for i := range versions {
		version := &versions[i]
		if opendepotUtils.SanitizeVersion(version.Spec.Version) != targetVersion ||
			!version.Status.Synced || version.Status.ProviderSchemaRef == nil {
			continue
		}

		selected = version

		break
	}

	if selected == nil {
		writeProviderSchemaUnavailable(w, targetVersion, "no provider configuration schema is available for this version")

		return
	}

	storageConfig := selected.Spec.ProviderConfigRef.StorageConfig
	if storageConfig == nil || selected.Spec.ProviderConfigRef.Name == nil {
		writeProviderSchemaUnavailable(w, targetVersion, "provider schema storage is unavailable")

		return
	}

	storageBackend, err := initStorageBackend(r.Context(), storageConfig)
	if err != nil {
		logger.Error("browse: failed to initialize provider schema storage", "namespace", namespace, "name", name, "version", targetVersion, "error", err)
		writeProviderSchemaUnavailable(w, targetVersion, "provider schema storage is unavailable")

		return
	}

	ref := selected.Status.ProviderSchemaRef
	key := ref.Key
	soi := &storageTypes.StorageObjectInput{
		Method:        storageTypes.Get,
		FilePath:      &key,
		StorageConfig: storageConfig,
		ContainerName: selected.Spec.ProviderConfigRef.Name,
		Version:       selected,
	}

	reader, err := storageBackend.GetObject(r.Context(), soi)
	if err != nil {
		logger.Error("browse: failed to read provider schema", "namespace", namespace, "name", name, "version", targetVersion, "error", err)
		writeProviderSchemaUnavailable(w, targetVersion, "provider configuration schema could not be read")

		return
	}
	if reader == nil {
		writeProviderSchemaUnavailable(w, targetVersion, "provider configuration schema could not be read")

		return
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	schema, err := decodeProviderSchema(reader, ref.Digest)
	if err != nil {
		logger.Error("browse: failed to decode provider schema", "namespace", namespace, "name", name, "version", targetVersion, "error", err)
		writeProviderSchemaUnavailable(w, targetVersion, "provider configuration schema is invalid")

		return
	}

	if schema.SchemaVersion != providerschema.SchemaVersion {
		writeProviderSchemaUnavailable(w, targetVersion, "provider configuration schema uses an unsupported format")

		return
	}

	providerNamespace := "hashicorp"
	if selected.Spec.ProviderConfigRef.Namespace != nil && strings.TrimSpace(*selected.Spec.ProviderConfigRef.Namespace) != "" {
		providerNamespace = strings.TrimSpace(*selected.Spec.ProviderConfigRef.Namespace)
	}

	json.NewEncoder(w).Encode(providerConfigurationSchemaResponse{
		Namespace:         namespace,
		Name:              name,
		ProviderNamespace: providerNamespace,
		ProviderName:      providerName,
		Version:           targetVersion,
		SchemaVersion:     schema.SchemaVersion,
		Configuration:     schema.ProviderBlock,
	})
}

func decodeProviderSchema(reader io.Reader, expectedDigest string) (*providerschema.ReducedSchema, error) {
	gr, err := gzip.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to gunzip provider schema: %w", err)
	}
	defer gr.Close()

	raw, err := io.ReadAll(io.LimitReader(gr, maxProviderSchemaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read provider schema: %w", err)
	}

	if len(raw) > maxProviderSchemaBytes {
		return nil, fmt.Errorf("provider schema exceeds the %d byte limit", maxProviderSchemaBytes)
	}

	digest := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), expectedDigest) {
		return nil, fmt.Errorf("provider schema digest does not match its status reference")
	}

	var schema providerschema.ReducedSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("failed to decode provider schema: %w", err)
	}

	return &schema, nil
}

func writeProviderSchemaUnavailable(w http.ResponseWriter, version, message string) {
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   "provider_schema_unavailable",
		"message": message,
		"version": version,
	})
}
