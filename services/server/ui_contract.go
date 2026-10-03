package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotUtils "github.com/tonedefdev/opendepot/pkg/utils"
)

// maxContractBytes caps how much decompressed contract JSON is served to a client.
const maxContractBytes = 32 << 20

// handleBrowseContract returns the Assembly Line contract for a single module version.
// GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/contract
// Optional query parameter:
//   - ?version=<semver> selects a specific version; defaults to the latest.
func handleBrowseContract(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	namespace := chi.URLParam(r, "namespace")
	kind := strings.ToLower(chi.URLParam(r, "kind"))
	name := chi.URLParam(r, "name")
	requestedVersion := opendepotUtils.SanitizeVersion(r.URL.Query().Get("version"))

	if kind != "module" {
		http.Error(w, "contracts are only available for modules", http.StatusBadRequest)
		return
	}

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

	rawModule, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("modules").
		Name(name).
		DoRaw(r.Context())
	if err != nil {
		if k8sApiErrors.IsNotFound(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		logger.Error("browse: failed to get module for contract", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var m opendepotv1alpha1.Module
	if err := json.Unmarshal(rawModule, &m); err != nil {
		logger.Error("browse: failed to unmarshal module for contract", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsLabels, err := browseGetNamespaceLabels(cs, r, namespace)
	if err != nil {
		logger.Error("browse: failed to get namespace labels", "namespace", namespace, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	pub := isPublicNamespace(nsLabels) && isPublicResource(m.Labels)
	if !isBrowseVisible(pub, false, allAccess, binding, "module", m.Name) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	versions, err := browseListModuleVersions(cs, r, namespace, name)
	if err != nil {
		logger.Error("browse: failed to list module versions for contract", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	targetVersion := requestedVersion
	if targetVersion == "" {
		targetVersion = opendepotUtils.SanitizeVersion(derefString(m.Status.LatestVersion))
	}

	if targetVersion == "" {
		for i := range versions {
			nv := opendepotUtils.SanitizeVersion(versions[i].Spec.Version)
			if targetVersion == "" || compareVersionDesc(nv, targetVersion) {
				targetVersion = nv
			}
		}
	}

	var ref *opendepotv1alpha1.ContractConfigMapRef
	for i := range versions {
		if opendepotUtils.SanitizeVersion(versions[i].Spec.Version) != targetVersion {
			continue
		}

		if versions[i].Status.ContractConfigMapRef != nil {
			ref = versions[i].Status.ContractConfigMapRef
			break
		}
	}

	if ref == nil {
		writeContractUnavailable(w, targetVersion)
		return
	}

	rawCM, err := cs.RESTClient().
		Get().
		AbsPath("/api/v1").
		Namespace(namespace).
		Resource("configmaps").
		Name(ref.Name).
		DoRaw(r.Context())
	if err != nil {
		writeContractUnavailable(w, targetVersion)
		return
	}

	var cm k8sConfigMap
	if err := json.Unmarshal(rawCM, &cm); err != nil {
		writeContractUnavailable(w, targetVersion)
		return
	}

	encoded, found := cm.Data[ref.Key]
	if !found {
		writeContractUnavailable(w, targetVersion)
		return
	}

	contract, err := decodeContract(encoded)
	if err != nil {
		logger.Error("browse: failed to decode module contract", "namespace", namespace, "name", name, "error", err)
		writeContractUnavailable(w, targetVersion)
		return
	}

	w.Write(contract)
}

// decodeContract base64-decodes and gunzips a stored contract payload.
func decodeContract(encoded string) ([]byte, error) {
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	gr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	return io.ReadAll(io.LimitReader(gr, maxContractBytes))
}

// writeContractUnavailable responds with a structured 404 so the UI can distinguish
// "this module has no contract yet" from a genuine server error.
func writeContractUnavailable(w http.ResponseWriter, version string) {
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   "contract_unavailable",
		"message": "no Assembly Line contract has been derived for this module version",
		"version": version,
	})
}
