package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/oci"
	"github.com/tonedefdev/opendepot/pkg/storage"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

const (
	// ociArtifactLabelKey names the OCIArtifact that a Version belongs to. The OCI controller sets it.
	ociArtifactLabelKey = "opendepot.defdev.io/ociartifact"
	// ociMaxIndexManifests bounds the platform manifests that one index may list.
	ociMaxIndexManifests  = 64
	ociIndexSchemaVersion = 2
	ociBlobContentType    = "application/octet-stream"
	ociDefaultPresignTTL  = 15 * time.Minute
)

var (
	ociNamespacePattern    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	ociRepositoryPattern   = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	ociTagPattern          = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	ociSHA256DigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

// errOCIStorageForbidden is returned when a Version's filesystem storage root is outside the configured mount path.
var errOCIStorageForbidden = errors.New("storage location is outside the allowed root")

// OCIDistributionHandler serves the OCI Distribution read API under /v2/. It serves GET and HEAD for the API version
// check, manifests by tag or digest, blobs, tag lists, and referrers. Only synced, unblocked Versions are served. Every
// other method returns 405 UNSUPPORTED. The route is the path after /v2, so it can be mounted on /v2 and /v2/*.
func OCIDistributionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(ociAPIVersionHeader, ociAPIVersion)

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeOCIError(w, http.StatusMethodNotAllowed, ociCodeUnsupported, "the operation is unsupported")

		return
	}

	route, routeErr := parseOCIRoute(r.URL.Path)
	if routeErr != nil {
		writeOCIError(w, routeErr.status, routeErr.code, routeErr.message)

		return
	}

	if route.kind == ociRouteAPIVersion {
		serveOCIAPIVersion(w, r)

		return
	}

	principal, status := authenticateOCIRequest(r)
	if status != 0 {
		writeOCIAuthFailure(w, status)

		return
	}

	switch route.kind {
	case ociRouteManifest:
		serveOCIManifest(w, r, principal, route)
	case ociRouteBlob:
		serveOCIBlob(w, r, principal, route)
	case ociRouteTags:
		serveOCITags(w, r, principal, route)
	case ociRouteReferrers:
		serveOCIReferrers(w, r, principal, route)
	case ociRouteAPIVersion:
	}
}

// serveOCIAPIVersion answers the API version check. Authentication is required unless anonymous mode is on.
func serveOCIAPIVersion(w http.ResponseWriter, r *http.Request) {
	if !*opendepotAnonymousAuth {
		if _, status := authenticateOCIRequest(r); status != 0 {
			writeOCIAuthFailure(w, status)

			return
		}
	}

	writeOCIBytes(w, r, ociContentTypeJSON, []byte("{}"))
}

type ociRouteKind int

const (
	ociRouteAPIVersion ociRouteKind = iota
	ociRouteManifest
	ociRouteBlob
	ociRouteTags
	ociRouteReferrers
)

// ociRoute is a parsed and validated /v2/ request path.
type ociRoute struct {
	kind      ociRouteKind
	namespace string
	// name is the repository path under the namespace. It names an OCIArtifact.
	name string
	// tag is the tag of a manifest route that is addressed by tag.
	tag string
	// contentDigest is the digest of a manifest route that is addressed by digest, and of blob and referrers routes.
	contentDigest digest.Digest
}

// parseOCIRoute splits a /v2/ path into its namespace, repository name, and reference. It validates every part
// against a strict pattern before any lookup, so a path can never reach a storage key that it did not validate.
func parseOCIRoute(requestPath string) (ociRoute, *ociRouteError) {
	rest := strings.TrimPrefix(strings.TrimPrefix(requestPath, "/v2"), "/")
	if rest == "" {
		return ociRoute{kind: ociRouteAPIVersion}, nil
	}

	unknown := &ociRouteError{status: http.StatusNotFound, code: ociCodeNameUnknown, message: "unknown route"}
	segments := strings.Split(rest, "/")
	if len(segments) < 4 {
		return ociRoute{}, unknown
	}

	route := ociRoute{namespace: segments[0], name: strings.Join(segments[1:len(segments)-2], "/")}
	verb := segments[len(segments)-2]
	reference := segments[len(segments)-1]

	switch {
	case verb == "tags" && reference == "list":
		route.kind = ociRouteTags
	case verb == "manifests":
		route.kind = ociRouteManifest
	case verb == "blobs":
		route.kind = ociRouteBlob
	case verb == "referrers":
		route.kind = ociRouteReferrers
	default:
		return ociRoute{}, unknown
	}

	if !ociNamespacePattern.MatchString(route.namespace) {
		return ociRoute{}, &ociRouteError{
			status:  http.StatusBadRequest,
			code:    ociCodeNameInvalid,
			message: "invalid namespace",
		}
	}

	if len(route.name) > 255 || !ociRepositoryPattern.MatchString(route.name) {
		return ociRoute{}, &ociRouteError{
			status:  http.StatusBadRequest,
			code:    ociCodeNameInvalid,
			message: "invalid repository name",
		}
	}

	switch route.kind {
	case ociRouteManifest:
		if strings.Contains(reference, ":") {
			if !ociSHA256DigestPattern.MatchString(reference) {
				return ociRoute{}, &ociRouteError{
					status:  http.StatusBadRequest,
					code:    ociCodeDigestInvalid,
					message: "invalid digest",
				}
			}

			route.contentDigest = digest.Digest(reference)

			break
		}

		if !ociTagPattern.MatchString(reference) {
			return ociRoute{}, &ociRouteError{status: http.StatusBadRequest, code: ociCodeTagInvalid, message: "invalid tag"}
		}

		route.tag = reference
	case ociRouteBlob, ociRouteReferrers:
		if !ociSHA256DigestPattern.MatchString(reference) {
			return ociRoute{}, &ociRouteError{
				status:  http.StatusBadRequest,
				code:    ociCodeDigestInvalid,
				message: "invalid digest",
			}
		}

		route.contentDigest = digest.Digest(reference)
	case ociRouteTags, ociRouteAPIVersion:
	}

	return route, nil
}

// ociRepositoryName returns the repository path of an OCIArtifact. It is its config name, or its resource name when the
// config name is omitted.
func ociRepositoryName(artifact *opendepotv1alpha1.OCIArtifact) string {
	if artifact.Spec.OCIArtifactConfig.Name != nil {
		return *artifact.Spec.OCIArtifactConfig.Name
	}

	return artifact.Name
}

// ociVersionServable reports whether a Version may be served. It must be synced, must not be blocked by Jev, and must
// have both a manifest and a config.
func ociVersionServable(version *opendepotv1alpha1.Version) bool {
	return version.Status.Synced &&
		(version.Status.JevAssessment == nil || !version.Status.JevAssessment.Blocked) &&
		version.Status.OCIManifest != nil &&
		version.Spec.OCIArtifactConfigRef != nil
}

// resolveServableOCIVersions finds the OCIArtifact for the repository and returns its servable Versions. It writes the
// error response and returns false when the artifact is unknown, is not visible to the caller, or cannot be read.
func resolveServableOCIVersions(
	w http.ResponseWriter,
	r *http.Request,
	principal *ociPrincipal,
	namespace, repository string,
) ([]opendepotv1alpha1.Version, bool) {
	artifact, err := findOCIArtifact(r.Context(), principal.clientset, namespace, repository)
	if errors.Is(err, errOCIArtifactAmbiguous) {
		writeOCIError(w, http.StatusConflict, ociCodeUnknown, "repository name is claimed by more than one OCIArtifact")

		return nil, false
	}

	if err != nil {
		writeOCIKubeError(w, err)

		return nil, false
	}

	if artifact == nil || !ociArtifactVisible(principal, artifact.Name) {
		writeOCIError(w, http.StatusNotFound, ociCodeNameUnknown, "repository name not known to registry")

		return nil, false
	}

	versions, err := listOCIVersions(r.Context(), principal.clientset, namespace)
	if err != nil {
		writeOCIKubeError(w, err)

		return nil, false
	}

	servable := make([]opendepotv1alpha1.Version, 0, len(versions))
	for i := range versions {
		version := &versions[i]
		if version.Spec.Type == opendepotv1alpha1.OpenDepotOCIArtifact &&
			version.Labels[ociArtifactLabelKey] == artifact.Name &&
			ociVersionServable(version) {
			servable = append(servable, *version)
		}
	}

	return servable, true
}

// errOCIArtifactAmbiguous is returned when more than one OCIArtifact claims the same repository path.
var errOCIArtifactAmbiguous = errors.New("repository path is claimed by more than one OCIArtifact")

// findOCIArtifact returns the OCIArtifact whose repository path is repository, or nil when there is none. When more
// than one matches, it returns errOCIArtifactAmbiguous, since serving either one would be arbitrary.
func findOCIArtifact(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace, repository string,
) (*opendepotv1alpha1.OCIArtifact, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("ociartifacts").
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}

	var list opendepotv1alpha1.OCIArtifactList
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("unable to unmarshal ociartifacts list: %w", err)
	}

	var match *opendepotv1alpha1.OCIArtifact
	for i := range list.Items {
		if ociRepositoryName(&list.Items[i]) != repository {
			continue
		}

		if match != nil {
			return nil, fmt.Errorf("%w: %s", errOCIArtifactAmbiguous, repository)
		}

		match = &list.Items[i]
	}

	return match, nil
}

// listOCIVersions lists the Versions in the namespace in name order.
func listOCIVersions(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
) ([]opendepotv1alpha1.Version, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}

	var list opendepotv1alpha1.VersionList
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("unable to unmarshal versions list: %w", err)
	}

	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].Name < list.Items[j].Name
	})

	return list.Items, nil
}

// writeOCIKubeError maps a cluster read error to an OCI error response.
func writeOCIKubeError(w http.ResponseWriter, err error) {
	switch {
	case k8sApiErrors.IsNotFound(err):
		writeOCIError(w, http.StatusNotFound, ociCodeNameUnknown, "repository name not known to registry")
	case k8sApiErrors.IsForbidden(err):
		writeOCIError(w, http.StatusForbidden, ociCodeDenied, "access to the requested resource is not authorized")
	default:
		logger.Error("oci: unable to read cluster resources", "error", err)
		writeOCIError(w, http.StatusInternalServerError, ociCodeUnknown, "internal server error")
	}
}

// writeOCIContentError maps a storage or content error to an OCI error response.
func writeOCIContentError(w http.ResponseWriter, err error) {
	if errors.Is(err, errOCIStorageForbidden) {
		writeOCIError(w, http.StatusForbidden, ociCodeDenied, "storage location is not allowed")

		return
	}

	logger.Error("oci: unable to read stored content", "error", err)
	writeOCIError(w, http.StatusInternalServerError, ociCodeUnknown, "internal server error")
}

// writeOCIBytes writes a successful response body with its content type and length. HEAD writes the headers only.
func writeOCIBytes(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)

	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// ociManifest is one manifest or index that a Version serves.
type ociManifest struct {
	digest    digest.Digest
	body      []byte
	mediaType string
}

// ociVersionContent is the manifest graph of one OCIArtifact Version. It holds the root manifest or index, the
// platform manifests that were mirrored, and every config and layer blob that those manifests reference.
type ociVersionContent struct {
	manifests map[digest.Digest]ociManifest
	blobs     []ocispec.Descriptor
}

// blob returns the descriptor of the blob addressed by d, if the graph references it.
func (c *ociVersionContent) blob(d digest.Digest) (ocispec.Descriptor, bool) {
	for _, desc := range c.blobs {
		if desc.Digest == d {
			return desc, true
		}
	}

	return ocispec.Descriptor{}, false
}

// blobByMediaType returns the first blob of the graph with the media type, if there is one.
func (c *ociVersionContent) blobByMediaType(mediaType string) (ocispec.Descriptor, bool) {
	for _, desc := range c.blobs {
		if desc.MediaType == mediaType {
			return desc, true
		}
	}

	return ocispec.Descriptor{}, false
}

// addManifest records the config and layers of an image manifest.
func (c *ociVersionContent) addManifest(body []byte) error {
	var manifest ocispec.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("oci: decode image manifest: %w", err)
	}

	c.blobs = append(c.blobs, manifest.Config)
	c.blobs = append(c.blobs, manifest.Layers...)

	return nil
}

// ociVersionStore reads the content of one OCIArtifact Version from the store that the version controller wrote it to.
type ociVersionStore struct {
	store         *oci.Store
	backend       storage.Storage
	storageConfig *opendepotv1alpha1.StorageConfig
	version       *opendepotv1alpha1.Version
}

// loadContent reads the root manifest or index of the Version and, for an index, the platform manifests that it lists.
// Platform manifests that were not mirrored are skipped. A missing root returns an error that wraps
// storage.ErrNotFound.
func (vs *ociVersionStore) loadContent(ctx context.Context) (*ociVersionContent, error) {
	root := digest.Digest(vs.version.Status.OCIManifest.Digest)

	rootBody, err := vs.store.GetManifest(ctx, vs.version.Namespace, vs.version.Name, root)
	if err != nil {
		return nil, err
	}

	content := &ociVersionContent{manifests: map[digest.Digest]ociManifest{}}
	content.manifests[root] = ociManifest{
		digest:    root,
		body:      rootBody,
		mediaType: ociMediaType(vs.version.Status.OCIManifest.MediaType),
	}

	var index ocispec.Index
	if err := json.Unmarshal(rootBody, &index); err != nil {
		return nil, fmt.Errorf("oci: decode manifest %s: %w", root, err)
	}

	if len(index.Manifests) == 0 {
		if err := content.addManifest(rootBody); err != nil {
			return nil, err
		}

		return content, nil
	}

	if len(index.Manifests) > ociMaxIndexManifests {
		return nil, fmt.Errorf("oci: index %s lists more than %d manifests", root, ociMaxIndexManifests)
	}

	for _, child := range index.Manifests {
		body, err := vs.store.GetManifest(ctx, vs.version.Namespace, vs.version.Name, child.Digest)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, err
		}

		content.manifests[child.Digest] = ociManifest{
			digest:    child.Digest,
			body:      body,
			mediaType: ociMediaType(child.MediaType),
		}
		if err := content.addManifest(body); err != nil {
			return nil, err
		}
	}

	return content, nil
}

// ociMediaType returns mediaType, or the OCI image manifest media type when it is empty.
func ociMediaType(mediaType string) string {
	if mediaType == "" {
		return ocispec.MediaTypeImageManifest
	}

	return mediaType
}

// ociRequestStorage builds the store of each Version for one request. Versions that share a storage configuration
// share one storage backend.
type ociRequestStorage struct {
	backends []ociStorageBackend
}

type ociStorageBackend struct {
	config  *opendepotv1alpha1.StorageConfig
	backend storage.Storage
}

// forVersion returns the store of the Version, using the Version's storage configuration. It is the same location that
// the version controller writes to.
func (rs *ociRequestStorage) forVersion(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
) (*ociVersionStore, error) {
	if version.Spec.OCIArtifactConfigRef == nil || version.Spec.OCIArtifactConfigRef.StorageConfig == nil {
		return nil, fmt.Errorf("storage configuration not available for version %q", version.Name)
	}

	cfg := version.Spec.OCIArtifactConfigRef.StorageConfig
	if !ociStorageRootAllowed(cfg) {
		return nil, errOCIStorageForbidden
	}

	backend, err := rs.backendFor(ctx, cfg)
	if err != nil {
		return nil, err
	}

	name := version.Name
	wrapped := ociVersionBackend{Storage: backend, storageConfig: cfg, containerName: &name, version: version}

	store, err := oci.NewStore(wrapped, ociStorageRoot(cfg))
	if err != nil {
		return nil, fmt.Errorf("oci: open store for version %q: %w", version.Name, err)
	}

	return &ociVersionStore{store: store, backend: wrapped, storageConfig: cfg, version: version}, nil
}

// backendFor returns a storage backend for cfg, creating it on first use within the request.
func (rs *ociRequestStorage) backendFor(
	ctx context.Context,
	cfg *opendepotv1alpha1.StorageConfig,
) (storage.Storage, error) {
	for _, entry := range rs.backends {
		if equality.Semantic.DeepEqual(entry.config, cfg) {
			return entry.backend, nil
		}
	}

	backend, err := initStorageBackend(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("oci: init storage backend: %w", err)
	}

	rs.backends = append(rs.backends, ociStorageBackend{config: cfg, backend: backend})

	return backend, nil
}

// ociStorageRootAllowed reports whether a filesystem store root lies under the configured mount path. The filesystem
// download route applies the same rule. Other backends have no local root.
func ociStorageRootAllowed(cfg *opendepotv1alpha1.StorageConfig) bool {
	if cfg.FileSystem == nil {
		return true
	}

	if cfg.FileSystem.DirectoryPath == nil || opendepotFilesystemMountPath == nil {
		return false
	}

	allowedRoot := path.Clean(*opendepotFilesystemMountPath)

	return strings.HasPrefix(path.Clean(*cfg.FileSystem.DirectoryPath)+"/", allowedRoot+"/")
}

// ociStorageRoot returns the key prefix under which the OCI store writes. It matches the version controller's layout.
func ociStorageRoot(cfg *opendepotv1alpha1.StorageConfig) string {
	switch {
	case cfg.S3 != nil && cfg.S3.Key != nil:
		return strings.TrimRight(*cfg.S3.Key, "/")
	case cfg.FileSystem != nil && cfg.FileSystem.DirectoryPath != nil:
		return strings.TrimRight(*cfg.FileSystem.DirectoryPath, "/\\")
	default:
		return ""
	}
}

// ociVersionBackend adds the storage settings of a Version to each call that the OCI store makes to a backend. It
// mirrors the wrapper in services/version, so the server reads the objects that the controller wrote.
type ociVersionBackend struct {
	storage.Storage
	storageConfig *opendepotv1alpha1.StorageConfig
	containerName *string
	version       *opendepotv1alpha1.Version
}

func (b ociVersionBackend) configure(soi *storageTypes.StorageObjectInput) {
	soi.StorageConfig = b.storageConfig
	soi.ContainerName = b.containerName
	soi.Version = b.version
}

func (b ociVersionBackend) GetObject(ctx context.Context, soi *storageTypes.StorageObjectInput) (io.Reader, error) {
	b.configure(soi)

	return b.Storage.GetObject(ctx, soi)
}

func (b ociVersionBackend) GetObjectChecksum(ctx context.Context, soi *storageTypes.StorageObjectInput) error {
	b.configure(soi)

	return b.Storage.GetObjectChecksum(ctx, soi)
}

func (b ociVersionBackend) PresignObject(ctx context.Context, soi *storageTypes.StorageObjectInput) error {
	b.configure(soi)

	return b.Storage.PresignObject(ctx, soi)
}

// ociBlobObjectKey returns the object key of a blob. It matches the layout that pkg/oci writes:
// <root>/oci/<namespace>/<repo>/blobs/sha256/<hex>.
func ociBlobObjectKey(cfg *opendepotv1alpha1.StorageConfig, namespace, repo string, d digest.Digest) string {
	return path.Join(ociStorageRoot(cfg), "oci", namespace, repo, "blobs/sha256", d.Encoded())
}

// presignBlob returns a pre-signed URL for a blob. It checks the stored object's checksum first, as the agent archive
// presign does, so a redirect never points at an object whose content does not match its digest.
func (vs *ociVersionStore) presignBlob(
	ctx context.Context,
	d digest.Digest,
	presign *opendepotv1alpha1.PresignConfig,
) (string, error) {
	key := ociBlobObjectKey(vs.storageConfig, vs.version.Namespace, vs.version.Name, d)

	ttl := ociDefaultPresignTTL
	if presign.TTL != nil {
		ttl = presign.TTL.Duration
	}

	soi := &storageTypes.StorageObjectInput{FilePath: &key, PresignTTL: ttl}
	if err := vs.backend.GetObjectChecksum(ctx, soi); err != nil {
		return "", fmt.Errorf("checksum blob before presign: %w", err)
	}

	want, err := ociBlobChecksum(d)
	if err != nil {
		return "", err
	}

	if soi.ObjectChecksum == nil || *soi.ObjectChecksum != want {
		return "", errors.New("blob checksum mismatch; refusing to issue pre-signed URL")
	}

	if err := vs.backend.PresignObject(ctx, soi); err != nil {
		return "", fmt.Errorf("presign blob: %w", err)
	}

	if soi.PresignedURL == nil {
		return "", errors.New("storage backend returned no pre-signed URL for blob")
	}

	return *soi.PresignedURL, nil
}

// ociBlobChecksum returns the base64 SHA-256 of the raw digest bytes. Storage backends report object checksums in
// this form, as pkg/oci compares them.
func ociBlobChecksum(d digest.Digest) (string, error) {
	raw, err := hex.DecodeString(d.Encoded())
	if err != nil {
		return "", fmt.Errorf("decode blob digest: %w", err)
	}

	return base64.StdEncoding.EncodeToString(raw), nil
}

// findOCIManifest returns the manifest that the route addresses. A tag route looks at the Version with that tag, and a
// digest route looks for the digest among the root and platform manifests of every servable Version.
func findOCIManifest(
	ctx context.Context,
	versions []opendepotv1alpha1.Version,
	route ociRoute,
) (*ociManifest, bool, error) {
	rs := &ociRequestStorage{}
	for i := range versions {
		version := &versions[i]
		if route.tag != "" && version.Spec.Version != route.tag {
			continue
		}

		vs, err := rs.forVersion(ctx, version)
		if err != nil {
			return nil, false, err
		}

		content, err := vs.loadContent(ctx)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, false, err
		}

		d := route.contentDigest
		if route.tag != "" {
			d = digest.Digest(version.Status.OCIManifest.Digest)
		}

		if manifest, ok := content.manifests[d]; ok {
			return &manifest, true, nil
		}
	}

	return nil, false, nil
}

func serveOCIManifest(w http.ResponseWriter, r *http.Request, principal *ociPrincipal, route ociRoute) {
	versions, ok := resolveServableOCIVersions(w, r, principal, route.namespace, route.name)
	if !ok {
		return
	}

	manifest, found, err := findOCIManifest(r.Context(), versions, route)
	if err != nil {
		writeOCIContentError(w, err)

		return
	}

	if !found {
		writeOCIError(w, http.StatusNotFound, ociCodeManifestUnknown, "manifest unknown to registry")

		return
	}

	w.Header().Set("Docker-Content-Digest", manifest.digest.String())
	writeOCIBytes(w, r, manifest.mediaType, manifest.body)
}

func serveOCIBlob(w http.ResponseWriter, r *http.Request, principal *ociPrincipal, route ociRoute) {
	versions, ok := resolveServableOCIVersions(w, r, principal, route.namespace, route.name)
	if !ok {
		return
	}

	rs := &ociRequestStorage{}
	for i := range versions {
		vs, err := rs.forVersion(r.Context(), &versions[i])
		if err != nil {
			writeOCIContentError(w, err)

			return
		}

		content, err := vs.loadContent(r.Context())
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}

		if err != nil {
			writeOCIContentError(w, err)

			return
		}

		desc, found := content.blob(route.contentDigest)
		if !found {
			continue
		}

		serveOCIStoredBlob(w, r, vs, desc)

		return
	}

	writeOCIError(w, http.StatusNotFound, ociCodeBlobUnknown, "blob unknown to registry")
}

// serveOCIStoredBlob redirects to a pre-signed URL when presign is enabled. A presign failure falls back to streaming
// when FallbackToProxy is set, and otherwise returns an error. Without presign the blob streams through the server.
func serveOCIStoredBlob(w http.ResponseWriter, r *http.Request, vs *ociVersionStore, desc ocispec.Descriptor) {
	if presign := vs.storageConfig.Presign; presign != nil && presign.Enabled != nil && *presign.Enabled {
		presignedURL, err := vs.presignBlob(r.Context(), desc.Digest, presign)
		if err == nil {
			w.Header().Set("Docker-Content-Digest", desc.Digest.String())
			w.Header().Set("Cache-Control", "private, no-store")
			http.Redirect(w, r, presignedURL, http.StatusTemporaryRedirect)

			return
		}

		if presign.FallbackToProxy == nil || !*presign.FallbackToProxy {
			logger.Error("oci blob presign failed and fallback is disabled", "error", err, "version", vs.version.Name)
			writeOCIError(w, http.StatusInternalServerError, ociCodeUnknown, "internal server error")

			return
		}

		logger.Info("oci blob presign failed, falling back to proxy", "error", err, "version", vs.version.Name)
	}

	streamOCIBlob(w, r, vs, desc, ociBlobContentType)
}

// streamOCIBlob writes a stored blob with its digest, size, and content type. A Range request is served from a verified
// copy, so a range never comes from a corrupt blob. A full GET is also spooled and verified before any header is
// written, so a corrupt blob never completes with 200. HEAD writes the headers only.
func streamOCIBlob(
	w http.ResponseWriter,
	r *http.Request,
	vs *ociVersionStore,
	desc ocispec.Descriptor,
	contentType string,
) {
	if r.Header.Get("Range") != "" {
		serveRangedOCIBlob(w, r, vs, desc, contentType)

		return
	}

	var spool *os.File
	if r.Method != http.MethodHead {
		file, err := spoolOCIBlob(r.Context(), vs, desc)
		if err != nil {
			writeOCIContentError(w, err)

			return
		}

		defer removeOCITempFile(file)

		spool = file
	}

	setOCIBlobHeaders(w, desc, contentType)
	w.WriteHeader(http.StatusOK)

	if spool != nil {
		if _, err := io.Copy(w, spool); err != nil {
			logger.Error("oci blob stream failed", "error", err, "digest", desc.Digest.String())
		}
	}
}

// serveRangedOCIBlob answers a Range request with http.ServeContent. GET serves the range from a verified spool file.
// HEAD needs only the size, so it does not read the blob.
func serveRangedOCIBlob(
	w http.ResponseWriter,
	r *http.Request,
	vs *ociVersionStore,
	desc ocispec.Descriptor,
	contentType string,
) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Docker-Content-Digest", desc.Digest.String())

	if r.Method == http.MethodHead {
		http.ServeContent(w, r, "", time.Time{}, ociSizedBlob{size: desc.Size})

		return
	}

	spool, err := spoolOCIBlob(r.Context(), vs, desc)
	if err != nil {
		writeOCIContentError(w, err)

		return
	}

	defer removeOCITempFile(spool)

	http.ServeContent(w, r, "", time.Time{}, spool)
}

// setOCIBlobHeaders sets the headers of a blob response.
func setOCIBlobHeaders(w http.ResponseWriter, desc ocispec.Descriptor, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Docker-Content-Digest", desc.Digest.String())
	w.Header().Set("Content-Length", strconv.FormatInt(desc.Size, 10))
}

// spoolOCIBlob copies a stored blob to a temporary file, which verifies its digest, and returns the file rewound to
// its start. The caller removes the file.
func spoolOCIBlob(ctx context.Context, vs *ociVersionStore, desc ocispec.Descriptor) (*os.File, error) {
	rc, err := vs.store.GetBlob(ctx, vs.version.Namespace, vs.version.Name, desc.Digest)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rc.Close() }()

	spool, err := os.CreateTemp("", "opendepot-oci-blob-*")
	if err != nil {
		return nil, fmt.Errorf("oci: create spool file: %w", err)
	}

	if _, err := io.Copy(spool, rc); err != nil {
		removeOCITempFile(spool)

		return nil, fmt.Errorf("oci: spool blob %s: %w", desc.Digest, err)
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		removeOCITempFile(spool)

		return nil, fmt.Errorf("oci: rewind spool file: %w", err)
	}

	return spool, nil
}

// removeOCITempFile closes and deletes a spool file.
func removeOCITempFile(f *os.File) {
	_ = f.Close()
	_ = os.Remove(f.Name())
}

// ociSizedBlob reports the size of a blob without its content. It serves HEAD range requests, which need no bytes.
type ociSizedBlob struct {
	size int64
}

func (b ociSizedBlob) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (b ociSizedBlob) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekEnd {
		return b.size + offset, nil
	}

	return offset, nil
}

// ociTagList is the body of a tags/list response.
type ociTagList struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// serveOCITags lists the tags of the servable Versions, sorted and without duplicates.
func serveOCITags(w http.ResponseWriter, r *http.Request, principal *ociPrincipal, route ociRoute) {
	versions, ok := resolveServableOCIVersions(w, r, principal, route.namespace, route.name)
	if !ok {
		return
	}

	tags := make([]string, 0, len(versions))
	seen := make(map[string]bool, len(versions))
	for _, version := range versions {
		if version.Spec.Version == "" || seen[version.Spec.Version] {
			continue
		}

		seen[version.Spec.Version] = true
		tags = append(tags, version.Spec.Version)
	}

	sort.Strings(tags)

	payload, err := json.Marshal(ociTagList{Name: route.name, Tags: tags})
	if err != nil {
		writeOCIContentError(w, err)

		return
	}

	writeOCIBytes(w, r, ociContentTypeJSON, payload)
}

// ociReferrersIndex is the body of a referrers response.
type ociReferrersIndex struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Manifests     []ocispec.Descriptor `json:"manifests"`
}

// serveOCIReferrers answers the referrers of a manifest that a servable Version serves. Referrers are mirrored as
// manifests with no index, so the store cannot list them yet. The index is empty until the version controller records
// them. The empty index is still a valid OCI 1.1 referrers response.
func serveOCIReferrers(w http.ResponseWriter, r *http.Request, principal *ociPrincipal, route ociRoute) {
	versions, ok := resolveServableOCIVersions(w, r, principal, route.namespace, route.name)
	if !ok {
		return
	}

	_, found, err := findOCIManifest(r.Context(), versions, route)
	if err != nil {
		writeOCIContentError(w, err)

		return
	}

	if !found {
		writeOCIError(w, http.StatusNotFound, ociCodeManifestUnknown, "manifest unknown to registry")

		return
	}

	payload, err := json.Marshal(ociReferrersIndex{
		SchemaVersion: ociIndexSchemaVersion,
		MediaType:     ocispec.MediaTypeImageIndex,
		Manifests:     []ocispec.Descriptor{},
	})
	if err != nil {
		writeOCIContentError(w, err)

		return
	}

	writeOCIBytes(w, r, ocispec.MediaTypeImageIndex, payload)
}
