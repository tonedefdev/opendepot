// Package buildbroker serves the build broker of the version controller. Build Jobs fetch their context from it and upload
// their OCI layouts to it. Every request is authenticated with a one-time build token. The broker checks the token
// against the SHA-256 hashes in the Version's status.ociBuild, so the token itself is never stored, and it writes only
// through the OCI blob store of the Version that owns the build.
package buildbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

// RoutePrefix is the path prefix of every broker route. The routes are
// /internal/builds/v1/{namespace}/{version}/{buildID}/context, /blobs/{digest}, and /manifests/{platform}.
const RoutePrefix = "/internal/builds/v1/"

// The route kinds, which name the segment after the build ID.
const (
	routeContext   = "context"
	routeBlobs     = "blobs"
	routeManifests = "manifests"
)

const (
	// DefaultMaxBlobBytes is the largest blob that one upload may send. It matches the controller's per-blob image limit.
	DefaultMaxBlobBytes int64 = 4 << 30
	// DefaultMaxManifestBytes is the largest manifest that one upload may send.
	DefaultMaxManifestBytes int64 = 4 << 20
	// DefaultMaxConcurrentPerBuild is the number of requests that one build may have in flight.
	DefaultMaxConcurrentPerBuild = 2

	buildPhaseRunning = "Running"
	bearerPrefix      = "Bearer "
	sha256Prefix      = "sha256:"
	maxConfigBytes    = int64(1) << 20
	shutdownTimeout   = 10 * time.Second
	headerTimeout     = 10 * time.Second
)

// BlobSink stores the blobs and manifests of one Version. The version controller implements it with its OCI blob store.
type BlobSink interface {
	// GetBlob opens the blob addressed by d. It fails when the blob is missing.
	GetBlob(ctx context.Context, d digest.Digest) (io.ReadCloser, error)
	// PutBlob stores content as the blob addressed by d. The content must hash to d.
	PutBlob(ctx context.Context, d digest.Digest, content io.ReadSeeker) error
	// PutManifest stores a manifest and returns its digest.
	PutManifest(ctx context.Context, content []byte) (digest.Digest, error)
}

// SinkFunc returns the BlobSink of the Version namespace/name.
type SinkFunc func(ctx context.Context, namespace, name string) (BlobSink, error)

// Config holds the broker settings. Zero limits take their defaults.
type Config struct {
	// Addr is the listen address, such as :8090.
	Addr string
	// Reader reads Versions. It is the controller's cache, so the status is read without an API call per request.
	Reader client.Reader
	// Sinks returns the blob sink of a Version.
	Sinks SinkFunc
	// Log records broker errors. It never records tokens.
	Log logr.Logger
	// SpoolDir holds the blobs that are spooled before they are written. An empty value uses os.TempDir.
	SpoolDir string
	// MaxBlobBytes bounds one blob upload.
	MaxBlobBytes int64
	// MaxManifestBytes bounds one manifest upload.
	MaxManifestBytes int64
	// MaxConcurrentPerBuild bounds the requests that one build may have in flight.
	MaxConcurrentPerBuild int
}

// Server is the build broker. It runs on every version-controller replica, so it does not need leader election.
type Server struct {
	cfg      Config
	mu       sync.Mutex
	inflight map[string]int
}

// New returns a broker for cfg.
func New(cfg Config) (*Server, error) {
	if cfg.Reader == nil || cfg.Sinks == nil {
		return nil, errors.New("build broker requires a reader and a blob sink resolver")
	}

	if cfg.MaxBlobBytes <= 0 {
		cfg.MaxBlobBytes = DefaultMaxBlobBytes
	}

	if cfg.MaxManifestBytes <= 0 {
		cfg.MaxManifestBytes = DefaultMaxManifestBytes
	}

	if cfg.MaxConcurrentPerBuild <= 0 {
		cfg.MaxConcurrentPerBuild = DefaultMaxConcurrentPerBuild
	}

	return &Server{cfg: cfg, inflight: map[string]int{}}, nil
}

// NeedLeaderElection reports that the broker runs on every replica.
func (s *Server) NeedLeaderElection() bool {
	return false
}

// Start serves the broker until ctx is done.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s,
		ReadHeaderTimeout: headerTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	}
}

// route is one parsed broker request.
type route struct {
	namespace string
	name      string
	buildID   string
	// kind is "context", "blobs", or "manifests".
	kind string
	// arg is the digest for blobs and the platform for manifests.
	arg string
}

// parseRoute parses the escaped request path. Each segment is unescaped after the split, so a platform such as
// linux/amd64 is sent as linux%2Famd64 and stays one segment.
func parseRoute(escapedPath string) (route, bool) {
	rest, ok := strings.CutPrefix(escapedPath, RoutePrefix)
	if !ok {
		return route{}, false
	}

	raw := strings.Split(rest, "/")
	segments := make([]string, 0, len(raw))
	for i, seg := range raw {
		v, err := url.PathUnescape(seg)
		if err != nil || v == "" || (i < 3 && strings.Contains(v, "/")) {
			return route{}, false
		}

		segments = append(segments, v)
	}

	switch {
	case len(segments) == 4 && segments[3] == routeContext:
		return route{namespace: segments[0], name: segments[1], buildID: segments[2], kind: routeContext}, true
	case len(segments) == 5 && segments[3] == routeBlobs:
		return route{namespace: segments[0], name: segments[1], buildID: segments[2], kind: routeBlobs, arg: segments[4]}, true
	case len(segments) == 5 && segments[3] == routeManifests:
		return route{namespace: segments[0], name: segments[1], buildID: segments[2], kind: routeManifests, arg: segments[4]}, true
	default:
		return route{}, false
	}
}

// ServeHTTP authenticates the request and dispatches it to its route.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt, ok := parseRoute(r.URL.EscapedPath())
	if !ok {
		http.NotFound(w, r)
		return
	}

	token, ok := bearerToken(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	version := &opendepotv1alpha1.Version{}
	if err := s.cfg.Reader.Get(r.Context(), client.ObjectKey{Namespace: rt.namespace, Name: rt.name}, version); err != nil {
		if k8serr.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}

		s.cfg.Log.Error(err, "build broker: read version", "namespace", rt.namespace, "version", rt.name)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	build := version.Status.OCIBuild
	if build == nil || build.BuildID != rt.buildID || build.Phase != buildPhaseRunning {
		http.NotFound(w, r)
		return
	}

	// Each route accepts one token only. A valid token for the other route is refused, and it never reaches the sink.
	wantHash, otherHash := build.UploadTokenHash, build.FetchTokenHash
	if rt.kind == routeContext {
		wantHash, otherHash = build.FetchTokenHash, build.UploadTokenHash
	}

	if !tokenMatches(token, wantHash) {
		if tokenMatches(token, otherHash) {
			http.Error(w, "token is not valid for this route", http.StatusForbidden)
			return
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	release, ok := s.acquire(rt.buildID)
	if !ok {
		http.Error(w, "too many requests for this build", http.StatusTooManyRequests)
		return
	}
	defer release()

	sink, err := s.cfg.Sinks(r.Context(), rt.namespace, rt.name)
	if err != nil {
		s.cfg.Log.Error(err, "build broker: open blob store", "namespace", rt.namespace, "version", rt.name)
		http.Error(w, "storage is unavailable", http.StatusServiceUnavailable)
		return
	}

	switch rt.kind {
	case routeContext:
		s.serveContext(w, r, sink, build)
	case routeBlobs:
		s.putBlob(w, r, sink, rt.arg)
	case routeManifests:
		s.putManifest(w, r, sink, rt.arg)
	}
}

// serveContext streams the staged context blob. The sink verifies its digest while it is read.
func (s *Server) serveContext(w http.ResponseWriter, r *http.Request, sink BlobSink, build *opendepotv1alpha1.OCIBuildStatus) {
	if build.ContextDigest == "" {
		http.NotFound(w, r)
		return
	}

	rc, err := sink.GetBlob(r.Context(), digest.Digest(build.ContextDigest))
	if err != nil {
		s.cfg.Log.Error(err, "build broker: open context blob")
		http.Error(w, "context is unavailable", http.StatusNotFound)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "application/gzip")
	_, _ = io.Copy(w, rc)
}

// putBlob spools one blob to a temporary file while it hashes it, then stores it through the sink. A blob whose digest
// does not match the route is refused before anything is stored.
func (s *Server) putBlob(w http.ResponseWriter, r *http.Request, sink BlobSink, arg string) {
	want, err := digest.Parse(arg)
	if err != nil || want.Algorithm() != digest.SHA256 {
		http.Error(w, "blob digest must be sha256", http.StatusBadRequest)
		return
	}

	if r.ContentLength > s.cfg.MaxBlobBytes {
		http.Error(w, "blob exceeds the size limit", http.StatusRequestEntityTooLarge)
		return
	}

	spool, err := os.CreateTemp(s.cfg.SpoolDir, "odb-blob-*")
	if err != nil {
		s.cfg.Log.Error(err, "build broker: create spool file")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
	}()

	verifier := want.Verifier()
	body := http.MaxBytesReader(w, r.Body, s.cfg.MaxBlobBytes)
	if _, err := io.Copy(io.MultiWriter(spool, verifier), body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "blob exceeds the size limit", http.StatusRequestEntityTooLarge)
			return
		}

		http.Error(w, "failed to read the blob", http.StatusBadRequest)
		return
	}

	if !verifier.Verified() {
		http.Error(w, "blob digest does not match the route", http.StatusBadRequest)
		return
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		s.cfg.Log.Error(err, "build broker: rewind spool file")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := sink.PutBlob(r.Context(), want, spool); err != nil {
		s.cfg.Log.Error(err, "build broker: store blob", "digest", want.String())
		http.Error(w, "failed to store the blob", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// putManifest accepts one image manifest for a platform. Every blob that it references must already be stored, and
// its config must name the platform in the route.
func (s *Server) putManifest(w http.ResponseWriter, r *http.Request, sink BlobSink, arg string) {
	platform, err := ocibuild.ParsePlatform(arg)
	if err != nil {
		http.Error(w, "unsupported platform", http.StatusBadRequest)
		return
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != ocispec.MediaTypeImageManifest {
		http.Error(w, "manifest media type must be "+ocispec.MediaTypeImageManifest, http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxManifestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "manifest exceeds the size limit", http.StatusRequestEntityTooLarge)
			return
		}

		http.Error(w, "failed to read the manifest", http.StatusBadRequest)
		return
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		http.Error(w, "manifest is not valid JSON", http.StatusUnprocessableEntity)
		return
	}

	if err := s.checkManifest(r, sink, platform, manifest); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	stored, err := sink.PutManifest(r.Context(), body)
	if err != nil {
		s.cfg.Log.Error(err, "build broker: store manifest", "platform", platform.String())
		http.Error(w, "failed to store the manifest", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"manifestDigest": stored.String()})
}

// checkManifest refuses a manifest that is not a plain platform image, that references a blob which is not stored, or
// whose config names a different platform.
func (s *Server) checkManifest(r *http.Request, sink BlobSink, platform ocibuild.Platform, m ocispec.Manifest) error {
	if m.Subject != nil || m.Annotations[attestationKey] == attestationValue {
		return errors.New("attestation and referrer manifests are not allowed")
	}

	if m.Config.MediaType != ocispec.MediaTypeImageConfig {
		return errors.New("config media type must be " + ocispec.MediaTypeImageConfig)
	}

	if len(m.Layers) > ocibuild.DefaultMaxLayers {
		return fmt.Errorf("manifest has more than %d layers", ocibuild.DefaultMaxLayers)
	}

	for _, layer := range m.Layers {
		if strings.HasPrefix(layer.MediaType, inTotoPrefix) {
			return errors.New("attestation layers are not allowed")
		}
	}

	if err := s.checkConfigPlatform(r, sink, platform, m.Config); err != nil {
		return err
	}

	for _, desc := range append([]ocispec.Descriptor{m.Config}, m.Layers...) {
		rc, err := sink.GetBlob(r.Context(), desc.Digest)
		if err != nil {
			return fmt.Errorf("manifest references blob %s, which is not stored", desc.Digest)
		}

		_ = rc.Close()
	}

	return nil
}

// checkConfigPlatform reads the config blob and checks that it names the platform of the route.
func (s *Server) checkConfigPlatform(r *http.Request, sink BlobSink, platform ocibuild.Platform, desc ocispec.Descriptor) error {
	rc, err := sink.GetBlob(r.Context(), desc.Digest)
	if err != nil {
		return fmt.Errorf("manifest references blob %s, which is not stored", desc.Digest)
	}
	defer func() { _ = rc.Close() }()

	var config ocispec.Image
	if err := json.NewDecoder(io.LimitReader(rc, maxConfigBytes)).Decode(&config); err != nil {
		return errors.New("config blob is not valid JSON")
	}

	if config.OS != platform.OS || config.Architecture != platform.Architecture || canonicalVariant(config.Architecture, config.Variant) != canonicalVariant(platform.Architecture, platform.Variant) {
		return fmt.Errorf("config platform %s/%s does not match the route platform %s", config.OS, config.Architecture, platform)
	}

	return nil
}

const (
	attestationKey   = "vnd.docker.reference.type"
	attestationValue = "attestation-manifest"
	inTotoPrefix     = "application/vnd.in-toto"
)

// canonicalVariant treats an arm64 image with no variant as v8, as the OCI layout reader does.
func canonicalVariant(arch, variant string) string {
	if arch == "arm64" && variant == "" {
		return "v8"
	}

	return variant
}

// acquire reserves one in-flight slot for buildID. It returns false when the build is at its limit.
func (s *Server) acquire(buildID string) (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.inflight[buildID] >= s.cfg.MaxConcurrentPerBuild {
		return nil, false
	}

	s.inflight[buildID]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.inflight[buildID]--
		if s.inflight[buildID] == 0 {
			delete(s.inflight, buildID)
		}
	}, true
}

// bearerToken returns the token of an Authorization: Bearer header.
func bearerToken(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
	if !ok || token == "" {
		return "", false
	}

	return token, true
}

// tokenMatches compares the SHA-256 of token with a stored hash in constant time. The stored hash carries the
// sha256: prefix, which is removed before the comparison. An empty stored hash never matches.
func tokenMatches(token, storedHash string) bool {
	if storedHash == "" {
		return false
	}

	return ocibuild.TokenMatches(token, strings.TrimPrefix(storedHash, sha256Prefix))
}
