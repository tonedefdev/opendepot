package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/oci"
	"github.com/tonedefdev/opendepot/pkg/storage"
)

const (
	// helmIndexCacheTTL bounds how long a rendered index.yaml is reused.
	helmIndexCacheTTL        = 30 * time.Second
	helmIndexCacheMaxEntries = 64
	helmArtifactType         = "Helm"
	helmIndexContentType     = "application/x-yaml"
	helmChartContentType     = "application/gzip"
	helmProvenanceType       = "text/plain; charset=utf-8"
)

// helmIndexCache holds rendered index.yaml documents. Each key covers the namespace, the caller's visibility, and the
// resourceVersion of every OCI Version in the namespace, so a change to any of them misses the cache.
var helmIndexCache = newTTLCache[[]byte](helmIndexCacheMaxEntries)

// helmChartFilePattern matches a chart file name, <name>-<version>.tgz or .prov. Group 1 is the name and version.
var helmChartFilePattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._+-]{0,254})\.(tgz|prov)$`)

// HelmIndexHandler serves the classic Helm repository index.yaml for a namespace. It lists the Helm OCIArtifact
// Versions that the caller may see, and only the synced, unblocked ones. The output is deterministic and is cached for
// a short time.
func HelmIndexHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	namespace := chi.URLParam(r, "namespace")
	if !ociNamespacePattern.MatchString(namespace) {
		http.Error(w, "invalid namespace", http.StatusBadRequest)

		return
	}

	principal, status := authenticateOCIRequest(r)
	if status != 0 {
		writeHelmAuthFailure(w, status)

		return
	}

	versions, err := listOCIVersions(r.Context(), principal.clientset, namespace)
	if err != nil {
		writeHelmKubeError(w, err)

		return
	}

	key := helmIndexCacheKey(namespace, principal, versions)

	body, ok := helmIndexCache.get(key, ociNow())
	if !ok {
		body, err = buildHelmIndex(r.Context(), principal, versions)
		if err != nil {
			logger.Error("helm: unable to build index.yaml", "error", err, "namespace", namespace)
			http.Error(w, "internal server error", http.StatusInternalServerError)

			return
		}

		now := ociNow()
		helmIndexCache.put(key, body, now.Add(helmIndexCacheTTL), now)
	}

	w.Header().Set("Content-Type", helmIndexContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// HelmChartHandler serves the chart archive (.tgz) or the provenance file (.prov) of a Helm OCIArtifact Version. It
// answers 404 when the Version is unknown, blocked, unsynced, or hidden from the caller, and when the provenance file
// does not exist.
func HelmChartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	namespace := chi.URLParam(r, "namespace")
	if !ociNamespacePattern.MatchString(namespace) {
		http.Error(w, "invalid namespace", http.StatusBadRequest)

		return
	}

	matches := helmChartFilePattern.FindStringSubmatch(chi.URLParam(r, "file"))
	if matches == nil {
		http.Error(w, "chart not found", http.StatusNotFound)

		return
	}

	principal, status := authenticateOCIRequest(r)
	if status != 0 {
		writeHelmAuthFailure(w, status)

		return
	}

	versions, err := listOCIVersions(r.Context(), principal.clientset, namespace)
	if err != nil {
		writeHelmKubeError(w, err)

		return
	}

	chart := findVisibleHelmChart(principal, versions, matches[1])
	if chart == nil {
		http.Error(w, "chart not found", http.StatusNotFound)

		return
	}

	vs, err := (&ociRequestStorage{}).forVersion(r.Context(), chart)
	if err != nil {
		writeHelmContentError(w, err)

		return
	}

	content, err := vs.loadContent(r.Context())
	if errors.Is(err, storage.ErrNotFound) {
		http.Error(w, "chart not found", http.StatusNotFound)

		return
	}

	if err != nil {
		writeHelmContentError(w, err)

		return
	}

	mediaType, contentType := oci.HelmChartLayerMediaType, helmChartContentType
	if matches[2] == "prov" {
		mediaType, contentType = oci.HelmProvenanceLayerMediaType, helmProvenanceType
	}

	desc, ok := content.blobByMediaType(mediaType)
	if !ok {
		http.Error(w, "chart not found", http.StatusNotFound)

		return
	}

	streamOCIBlob(w, r, vs, desc, contentType)
}

// visibleHelmVersions returns the synced, unblocked Helm OCIArtifact Versions that the principal may see, in name
// order.
func visibleHelmVersions(principal *ociPrincipal, versions []opendepotv1alpha1.Version) []opendepotv1alpha1.Version {
	visible := make([]opendepotv1alpha1.Version, 0, len(versions))
	for i := range versions {
		version := &versions[i]
		if version.Spec.Type != opendepotv1alpha1.OpenDepotOCIArtifact || !ociVersionServable(version) {
			continue
		}

		if version.Status.HelmChart == nil || version.Spec.OCIArtifactConfigRef.ArtifactType != helmArtifactType {
			continue
		}

		artifactName := version.Labels[ociArtifactLabelKey]
		if artifactName == "" || !ociArtifactVisible(principal, artifactName) {
			continue
		}

		visible = append(visible, *version)
	}

	return visible
}

// findVisibleHelmChart returns the visible Helm Version whose chart name and version make base, as <name>-<version>.
func findVisibleHelmChart(
	principal *ociPrincipal,
	versions []opendepotv1alpha1.Version,
	base string,
) *opendepotv1alpha1.Version {
	charts := visibleHelmVersions(principal, versions)
	for i := range charts {
		md := charts[i].Status.HelmChart
		if md.Name+"-"+md.Version == base {
			return &charts[i]
		}
	}

	return nil
}

// buildHelmIndex renders index.yaml for the Helm Versions that the principal may see. Each chart archive is spooled
// from the store, so pkg/oci reads its metadata and digest from the archive itself.
func buildHelmIndex(
	ctx context.Context,
	principal *ociPrincipal,
	versions []opendepotv1alpha1.Version,
) ([]byte, error) {
	charts := visibleHelmVersions(principal, versions)
	rs := &ociRequestStorage{}
	archives := make([]*os.File, 0, len(charts))

	defer func() {
		for _, archive := range archives {
			removeOCITempFile(archive)
		}
	}()

	indexCharts := make([]oci.IndexChart, 0, len(charts))

	var generated time.Time
	for i := range charts {
		vs, err := rs.forVersion(ctx, &charts[i])
		if err != nil {
			return nil, err
		}

		content, err := vs.loadContent(ctx)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, err
		}

		desc, ok := content.blobByMediaType(oci.HelmChartLayerMediaType)
		if !ok {
			continue
		}

		archive, err := spoolOCIBlob(ctx, vs, desc)
		if err != nil {
			return nil, err
		}

		archives = append(archives, archive)

		created := charts[i].CreationTimestamp.Time
		indexCharts = append(indexCharts, oci.IndexChart{Archive: archive, Created: created})
		if created.After(generated) {
			generated = created
		}
	}

	return oci.RenderIndex(indexCharts, generated)
}

// helmIndexCacheKey builds the cache key of a rendered index. It covers the namespace, the caller's binding, and the
// resourceVersion of every OCI Version in the namespace.
func helmIndexCacheKey(namespace string, principal *ociPrincipal, versions []opendepotv1alpha1.Version) string {
	keys := make([]string, 0, len(versions))
	for i := range versions {
		if versions[i].Spec.Type == opendepotv1alpha1.OpenDepotOCIArtifact {
			keys = append(keys, versions[i].Name+"@"+versions[i].ResourceVersion)
		}
	}

	return namespace + "\x00" + ociBindingKey(principal) + "\x00" + strings.Join(keys, ",")
}

// writeHelmAuthFailure writes a failed authentication for the Helm repository routes. A 401 carries the Basic
// challenge.
func writeHelmAuthFailure(w http.ResponseWriter, status int) {
	if status == http.StatusUnauthorized {
		setOCIChallenge(w)
	}

	http.Error(w, http.StatusText(status), status)
}

// writeHelmKubeError maps a cluster read error to a Helm repository response.
func writeHelmKubeError(w http.ResponseWriter, err error) {
	switch {
	case k8sApiErrors.IsNotFound(err):
		http.Error(w, "namespace not found", http.StatusNotFound)
	case k8sApiErrors.IsForbidden(err):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		logger.Error("helm: unable to read cluster resources", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// writeHelmContentError maps a storage error to a Helm repository response.
func writeHelmContentError(w http.ResponseWriter, err error) {
	if errors.Is(err, errOCIStorageForbidden) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	logger.Error("helm: unable to read stored chart", "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
