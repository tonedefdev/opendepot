package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotSigning "github.com/tonedefdev/opendepot/pkg/signing"
)

var errAgentSigningKeyNotConfigured = errors.New("agent signing key not configured")

// agentKind maps the plural kind URL segment to the Version name prefix and the
// GroupBinding resource type used for access control.
type agentKind struct {
	resource    string
	prefix      string
	versionType string
}

// agentParent holds the Skill or Agent Status.VersionRefs written by the controller after each
// Version syncs. Both kinds share the same JSON shape for the fields read here, so one type
// decodes either resource.
type agentParent struct {
	Status struct {
		VersionRefs map[string]struct {
			Version string `json:"version"`
			Synced  bool   `json:"synced"`
		} `json:"versionRefs"`
	} `json:"status"`
}

// versionEntries returns the controller-recorded Versions sorted by version so the response is stable.
func (p agentParent) versionEntries() []struct {
	Version string `json:"version"`
	Synced  bool   `json:"synced"`
} {
	entries := make([]struct {
		Version string `json:"version"`
		Synced  bool   `json:"synced"`
	}, 0, len(p.Status.VersionRefs))
	for _, ref := range p.Status.VersionRefs {
		entries = append(entries, struct {
			Version string `json:"version"`
			Synced  bool   `json:"synced"`
		}{Version: ref.Version, Synced: ref.Synced})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Version < entries[j].Version
	})

	return entries
}

// agentKindFromRequest resolves the kind URL segment, which is either "skills" or "agents".
func agentKindFromRequest(r *http.Request) (agentKind, bool) {
	switch chi.URLParam(r, "kind") {
	case "skills":
		return agentKind{resource: "skills", prefix: "skill", versionType: opendepotv1alpha1.OpenDepotSkill}, true
	case "agents":
		return agentKind{resource: "agents", prefix: "agent", versionType: opendepotv1alpha1.OpenDepotAgent}, true
	}

	return agentKind{}, false
}

// agentVersionResourceName returns the Version resource name for a Skill or Agent,
// such as "skill-my-skill-1-2-0".
func agentVersionResourceName(prefix, name, version string) string {
	return fmt.Sprintf("%s-%s-%s", prefix, name, sanitizeModuleVersionForLookup(version))
}

// agentVersionBlocked reports whether a configured JevPolicy threshold blocks the Version.
// Blocked Versions are never listed or served.
func agentVersionBlocked(version *opendepotv1alpha1.Version) bool {
	return version.Status.JevAssessment != nil && version.Status.JevAssessment.Blocked
}

// agentVersionServable reports whether a Version may be listed or served for the named Skill or Agent.
// It must be synced, unblocked, of the kind's type, and sourced from the named Skill or Agent.
func agentVersionServable(version *opendepotv1alpha1.Version, kind agentKind, name string) bool {
	return version.Status.Synced &&
		!agentVersionBlocked(version) &&
		version.Spec.Type == kind.versionType &&
		version.Spec.AgentSourceRef != nil &&
		version.Spec.AgentSourceRef.Name != nil &&
		*version.Spec.AgentSourceRef.Name == name
}

// buildAgentVersionsResponse lists the servable Versions of a Skill or Agent.
// Yanked Versions are included with Yanked set so clients can skip them on resolution.
func buildAgentVersionsResponse(kind agentKind, name string, entries []struct {
	Version string `json:"version"`
	Synced  bool   `json:"synced"`
}, versions map[string]opendepotv1alpha1.Version) AgentVersionsResponse {
	response := AgentVersionsResponse{Versions: []AgentVersionSummary{}}
	for _, entry := range entries {
		if !entry.Synced {
			continue
		}

		version, ok := versions[agentVersionResourceName(kind.prefix, name, entry.Version)]
		if !ok || !agentVersionServable(&version, kind, name) {
			continue
		}

		response.Versions = append(response.Versions, AgentVersionSummary{
			Version: entry.Version,
			Yanked:  version.Spec.Yanked,
		})
	}

	return response
}

// agentSigningKeys derives the public signing key from the provider GPG private key
// and verifies it against the fingerprint recorded on the Version at sync. A mismatch
// returns an error so that signatures made by a rotated key are never served.
func agentSigningKeys(privateKeyBase64, storedFingerprint string) (*ProviderSigningKeys, error) {
	privateKeyBase64 = strings.TrimSpace(privateKeyBase64)
	if privateKeyBase64 == "" {
		return nil, errAgentSigningKeyNotConfigured
	}

	privateKeyArmor, err := base64.StdEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode gpg private key: %w", err)
	}

	signer, err := opendepotSigning.ParsePrivateKey(privateKeyArmor, "")
	if err != nil {
		return nil, fmt.Errorf("parse gpg private key: %w", err)
	}

	if err = opendepotSigning.VerifyFingerprint(storedFingerprint, signer); err != nil {
		return nil, err
	}

	asciiArmor, err := signer.PublicKeyArmor()
	if err != nil {
		return nil, err
	}

	return &ProviderSigningKeys{
		GPGPublicKeys: []ProviderSigningKey{
			{
				KeyID:      signer.Fingerprint(),
				ASCIIArmor: asciiArmor,
			},
		},
	}, nil
}

// writeAgentSigningKeyError maps a signing key check failure to an HTTP response.
func writeAgentSigningKeyError(w http.ResponseWriter, err error) {
	if errors.Is(err, errAgentSigningKeyNotConfigured) {
		http.Error(w, "agent signing key not configured", http.StatusNotImplemented)
		return
	}

	logger.Error("agent signing key does not match the Version fingerprint; possible key rotation, refusing to serve signing material", "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// listAgentVersionsByName lists the Version resources in the namespace keyed by resource name.
func listAgentVersionsByName(clientset *kubernetes.Clientset, r *http.Request, namespace string) (map[string]opendepotv1alpha1.Version, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		return nil, err
	}

	var versionList opendepotv1alpha1.VersionList
	if err = json.Unmarshal(result, &versionList); err != nil {
		return nil, fmt.Errorf("unable to unmarshal versions list: %w", err)
	}

	versions := make(map[string]opendepotv1alpha1.Version, len(versionList.Items))
	for _, item := range versionList.Items {
		versions[item.Name] = item
	}

	return versions, nil
}

// getAgentVersionForRequest fetches the Version for the Skill or Agent in the request URL.
// Blocked Versions are reported as not found. It writes the error response and returns false on failure.
func getAgentVersionForRequest(w http.ResponseWriter, r *http.Request, clientset *kubernetes.Clientset, kind agentKind) (*opendepotv1alpha1.Version, bool) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	requestedVersion := chi.URLParam(r, "version")

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		Name(agentVersionResourceName(kind.prefix, name, requestedVersion)).
		DoRaw(r.Context())
	if err != nil {
		switch {
		case k8sApiErrors.IsNotFound(err):
			http.Error(w, "version not found", http.StatusNotFound)
		case k8sApiErrors.IsForbidden(err):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			logger.Error("unable to get agent version", "error", err, "namespace", namespace, "kind", kind.prefix, "name", name, "version", requestedVersion)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}

		return nil, false
	}

	var version opendepotv1alpha1.Version
	if err = json.Unmarshal(result, &version); err != nil {
		logger.Error("unable to unmarshal agent version", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return nil, false
	}

	if !agentVersionServable(&version, kind, name) {
		http.Error(w, "version not found", http.StatusNotFound)
		return nil, false
	}

	return &version, true
}

// getAgentVersions returns the synced, unblocked versions of a Skill or Agent. Yanked
// versions are listed with yanked set to true.
func getAgentVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	kind, ok := agentKindFromRequest(r)
	if !ok {
		http.Error(w, "kind not found", http.StatusNotFound)
		return
	}

	clientset, binding, _, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if binding != nil {
		if !isResourceAllowed(binding, kind.prefix, name) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", kind.prefix, "resource_name", name, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", kind.prefix, "resource_name", name, "namespace", namespace)
	}

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource(kind.resource).
		Name(name).
		DoRaw(r.Context())
	if err != nil {
		statusCode := http.StatusInternalServerError
		var apiStatus k8sApiErrors.APIStatus
		if errors.As(err, &apiStatus) {
			statusCode = int(apiStatus.Status().Code)
		}

		message := err.Error()
		if len(message) > 256 {
			message = message[:256]
		}

		logger.Error("unable to get agent resource", "statusCode", statusCode, "message", message, "namespace", namespace, "kind", kind.resource, "name", name)

		switch {
		case k8sApiErrors.IsNotFound(err):
			http.Error(w, kind.prefix+" not found", http.StatusNotFound)
		case k8sApiErrors.IsForbidden(err):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}

		return
	}

	var parent agentParent
	if err = json.Unmarshal(result, &parent); err != nil {
		logger.Error("unable to unmarshal agent resource", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	versions, err := listAgentVersionsByName(clientset, r, namespace)
	if err != nil {
		logger.Error("unable to list agent versions", "error", err, "namespace", namespace)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(buildAgentVersionsResponse(kind, name, parent.versionEntries(), versions))
}

// getAgentDownloadMetadata returns the download metadata for a single Skill or Agent Version,
// including the public signing key and the URLs of the signed SHA256SUMS. The token is never included.
func getAgentDownloadMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	kind, ok := agentKindFromRequest(r)
	if !ok {
		http.Error(w, "kind not found", http.StatusNotFound)
		return
	}

	clientset, binding, _, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if binding != nil {
		if !isResourceAllowed(binding, kind.prefix, name) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", kind.prefix, "resource_name", name, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", kind.prefix, "resource_name", name, "namespace", namespace)
	}

	version, ok := getAgentVersionForRequest(w, r, clientset, kind)
	if !ok {
		return
	}

	if version.Spec.FileName == nil || version.Status.Checksum == nil || version.Status.ShaSums == "" || version.Status.ShaSumsSignature == "" {
		http.Error(w, "version artifact not yet available", http.StatusServiceUnavailable)
		return
	}

	checksumHex, err := decodeSHA256Checksum(*version.Status.Checksum)
	if err != nil {
		logger.Error("unable to decode agent checksum", "error", err, "version", version.Name)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	signingKeys, err := agentSigningKeys(os.Getenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64"), version.Status.SigningKeyFingerprint)
	if err != nil {
		writeAgentSigningKeyError(w, err)
		return
	}

	base := fmt.Sprintf("%s/opendepot/agents/v1/%s/%s/%s/%s", requestBaseURL(r), namespace, chi.URLParam(r, "kind"), name, chi.URLParam(r, "version"))
	response := AgentDownloadResponse{
		Protocols:           []string{"1.0"},
		Kind:                kind.prefix,
		Name:                name,
		Version:             version.Spec.Version,
		Yanked:              version.Spec.Yanked,
		Filename:            *version.Spec.FileName,
		DownloadURL:         base + "/archive",
		Shasum:              checksumHex,
		ShasumsURL:          base + "/SHA256SUMS",
		ShasumsSignatureURL: base + "/SHA256SUMS.sig",
		SigningKeys:         signingKeys,
		Assessment:          version.Status.JevAssessment,
	}

	json.NewEncoder(w).Encode(response)
}

// authorizeAgentRequest returns a kube client for the caller after checking GroupBinding access
// to the Skill or Agent in the request. It writes the error response and returns false on failure.
func authorizeAgentRequest(w http.ResponseWriter, r *http.Request, kind agentKind) (*kubernetes.Clientset, bool) {
	clientset, binding, _, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return nil, false
	}

	name := chi.URLParam(r, "name")
	if binding != nil && !isResourceAllowed(binding, kind.prefix, name) {
		logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", kind.prefix, "resource_name", name, "namespace", chi.URLParam(r, "namespace"))
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, false
	}

	return clientset, true
}

// serveAgentArchive redirects to the stored archive of a Skill or Agent Version. The redirect is
// not cacheable by shared caches. The caller must pass the same GroupBinding check as the metadata
// endpoint, since the redirect target reveals the archive location and checksum.
func serveAgentArchive(w http.ResponseWriter, r *http.Request) {
	kind, ok := agentKindFromRequest(r)
	if !ok {
		http.Error(w, "kind not found", http.StatusNotFound)
		return
	}

	clientset, ok := authorizeAgentRequest(w, r, kind)
	if !ok {
		return
	}

	version, ok := getAgentVersionForRequest(w, r, clientset, kind)
	if !ok {
		return
	}

	if version.Status.Checksum == nil {
		http.Error(w, "version artifact not yet available", http.StatusServiceUnavailable)
		return
	}

	downloadPath, err := buildDownloadPathFromVersion(version)
	if err != nil {
		logger.Error("unable to build agent download path", "error", err, "version", version.Name)
		http.Error(w, "version artifact not available", http.StatusServiceUnavailable)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	requestedVersion := chi.URLParam(r, "version")
	err = recordDownload(r.Context(), namespace, kind.prefix, name, requestedVersion)
	if err != nil {
		logger.Error("failed to record download", "error", err, "namespace", namespace, "kind", kind.prefix, "name", name, "version", requestedVersion)
	}

	query := url.Values{}
	query.Set("fileChecksum", *version.Status.Checksum)
	w.Header().Set("Cache-Control", "private, no-store")
	http.Redirect(w, r, "/opendepot/modules/v1/download/"+downloadPath+"?"+query.Encode(), http.StatusFound)
}

// serveAgentSHA256SUMS serves the SHA256SUMS file stored at sync. The server never signs on read.
func serveAgentSHA256SUMS(w http.ResponseWriter, r *http.Request) {
	kind, ok := agentKindFromRequest(r)
	if !ok {
		http.Error(w, "kind not found", http.StatusNotFound)
		return
	}

	clientset, ok := authorizeAgentRequest(w, r, kind)
	if !ok {
		return
	}

	version, ok := getAgentVersionForRequest(w, r, clientset, kind)
	if !ok {
		return
	}

	if version.Status.ShaSums == "" {
		http.Error(w, "agent shasums not available", http.StatusNotImplemented)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, version.Status.ShaSums)
}

// serveAgentSHA256SUMSSignature serves the detached signature stored at sync. The signature
// is only served when the server's signing key matches the fingerprint recorded on the Version.
func serveAgentSHA256SUMSSignature(w http.ResponseWriter, r *http.Request) {
	kind, ok := agentKindFromRequest(r)
	if !ok {
		http.Error(w, "kind not found", http.StatusNotFound)
		return
	}

	clientset, ok := authorizeAgentRequest(w, r, kind)
	if !ok {
		return
	}

	version, ok := getAgentVersionForRequest(w, r, clientset, kind)
	if !ok {
		return
	}

	if version.Status.ShaSumsSignature == "" {
		http.Error(w, "agent shasums signature not available", http.StatusNotImplemented)
		return
	}

	if _, err := agentSigningKeys(os.Getenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64"), version.Status.SigningKeyFingerprint); err != nil {
		writeAgentSigningKeyError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.WriteString(w, version.Status.ShaSumsSignature)
}
