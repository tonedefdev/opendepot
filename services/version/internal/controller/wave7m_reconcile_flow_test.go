package controller

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type wave7mArchiveCounts struct {
	tarball atomic.Int32
	zipball atomic.Int32
}

func wave7mArchiveTransport(t *testing.T, archive []byte, status int) *wave7mArchiveCounts {
	t.Helper()

	counts := &wave7mArchiveCounts{}
	original := http.DefaultTransport
	http.DefaultTransport = jevRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "api.github.com" && strings.Contains(req.URL.Path, "/tarball/"):
			counts.tarball.Add(1)
			header := http.Header{"Location": []string{"https://codeload.test/archive"}}
			return jevHTTPResponse(req, http.StatusFound, header, nil), nil
		case req.URL.Host == "api.github.com" && strings.Contains(req.URL.Path, "/zipball/"):
			counts.zipball.Add(1)
			header := http.Header{"Location": []string{"https://codeload.test/archive"}}
			return jevHTTPResponse(req, http.StatusFound, header, nil), nil
		case req.URL.Host == "codeload.test":
			return jevHTTPResponse(req, status, nil, archive), nil
		default:
			return jevHTTPResponse(req, http.StatusNotFound, nil, nil), nil
		}
	})
	t.Cleanup(func() { http.DefaultTransport = original })

	return counts
}

func wave7mInlineModule(name, dir string) *opendepotv1alpha1.Version {
	version := wave6KModuleVersion(name, wave6KFileSystemConfig(dir))
	version.Spec.ModuleConfigRef.RepoOwner = "acme"
	repoURL := "https://github.com/acme/vpc"
	version.Spec.ModuleConfigRef.RepoUrl = &repoURL
	controllerutil.AddFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer)

	return version
}

func wave7mReconciler(t *testing.T, objects ...client.Object) *VersionReconciler {
	t.Helper()

	reconciler := wave6KReconciler(t, objects...)
	reconciler.APIReader = reconciler.Client

	return reconciler
}

func wave7mProviderReconciler(t *testing.T, objects ...client.Object) *VersionReconciler {
	t.Helper()

	scheme := helpersCovScheme(t)

	return &VersionReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objects...).
			WithStatusSubresource(&opendepotv1alpha1.Version{}, &opendepotv1alpha1.Provider{}).
			Build(),
		Scheme: scheme,
		Log:    logr.Discard(),
	}
}

func wave7mGetVersion(t *testing.T, reconciler *VersionReconciler, name string) *opendepotv1alpha1.Version {
	t.Helper()

	version := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), k8stypes.NamespacedName{Name: name, Namespace: "default"}, version); err != nil {
		t.Fatalf("get version %s: %v", name, err)
	}

	return version
}

func TestWave7MReconcileModuleFirstSyncStoresArchive(t *testing.T) {
	dir := t.TempDir()
	counts := wave7mArchiveTransport(t, moduleTestArchive(t), http.StatusOK)
	version := wave7mInlineModule("vpc-sync-1-0-0", dir)
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	synced := wave7mGetVersion(t, reconciler, version.Name)
	if !synced.Status.Synced || synced.Status.Checksum == nil || synced.Spec.FileName == nil {
		t.Fatalf("status synced=%v checksum=%v fileName=%v, want a stored module", synced.Status.Synced, synced.Status.Checksum, synced.Spec.FileName)
	}

	if counts.zipball.Load() != 1 && counts.tarball.Load() != 1 {
		t.Fatalf("archive downloads: tarball=%d zipball=%d, want one", counts.tarball.Load(), counts.zipball.Load())
	}
}

func TestWave7MReconcileModuleFastPathSkipsDownload(t *testing.T) {
	dir := t.TempDir()
	counts := wave7mArchiveTransport(t, moduleTestArchive(t), http.StatusOK)
	version := wave7mInlineModule("vpc-fast-1-0-0", dir)
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	before := counts.tarball.Load() + counts.zipball.Load()
	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}

	if after := counts.tarball.Load() + counts.zipball.Load(); after != before {
		t.Fatalf("fast path downloaded again: downloads %d -> %d", before, after)
	}
}

func TestWave7MReconcileForceSyncRedownloadsArchive(t *testing.T) {
	dir := t.TempDir()
	counts := wave7mArchiveTransport(t, moduleTestArchive(t), http.StatusOK)
	version := wave7mInlineModule("vpc-force-1-0-0", dir)
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	current := wave7mGetVersion(t, reconciler, version.Name)
	current.Spec.ForceSync = true
	if err := reconciler.Update(context.Background(), current); err != nil {
		t.Fatalf("set forceSync: %v", err)
	}

	before := counts.tarball.Load() + counts.zipball.Load()
	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err != nil {
		t.Fatalf("forced Reconcile: %v", err)
	}

	if after := counts.tarball.Load() + counts.zipball.Load(); after != before+1 {
		t.Fatalf("forced sync downloads %d -> %d, want one more download", before, after)
	}
}

func TestWave7MReconcileModuleZipFileNameUsesZipball(t *testing.T) {
	dir := t.TempDir()
	counts := wave7mArchiveTransport(t, moduleTestArchive(t), http.StatusOK)
	version := wave7mInlineModule("vpc-zip-1-0-0", dir)
	fileName := "vpc-zip-1-0-0.zip"
	version.Spec.FileName = &fileName
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if counts.zipball.Load() != 1 {
		t.Fatalf("zipball downloads = %d, want 1 for a .zip FileName", counts.zipball.Load())
	}
}

func TestWave7MReconcileModuleArchiveFailureRecordsStatus(t *testing.T) {
	dir := t.TempDir()
	wave7mArchiveTransport(t, nil, http.StatusInternalServerError)
	version := wave7mInlineModule("vpc-fail-1-0-0", dir)
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err == nil {
		t.Fatal("Reconcile succeeded although the archive download failed")
	}

	failed := wave7mGetVersion(t, reconciler, version.Name)
	if !strings.Contains(failed.Status.SyncStatus, "Failed to retrieve module archive") {
		t.Fatalf("SyncStatus = %q, want a retrieval failure", failed.Status.SyncStatus)
	}
}

func TestWave7MReconcileImmutableChecksumMismatchIsRejected(t *testing.T) {
	dir := t.TempDir()
	wave7mArchiveTransport(t, moduleTestArchive(t), http.StatusOK)
	version := wave7mInlineModule("vpc-immutable-1-0-0", dir)
	immutable := true
	version.Spec.ModuleConfigRef.Immutable = &immutable
	stale := "stale-checksum"
	version.Status.Checksum = &stale
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err == nil || !strings.Contains(err.Error(), "marked immutable") {
		t.Fatalf("err = %v, want an immutable checksum error", err)
	}
}

func TestWave7MReconcileAuthenticatedModuleWithoutSecretFails(t *testing.T) {
	dir := t.TempDir()
	wave7mArchiveTransport(t, moduleTestArchive(t), http.StatusOK)
	version := wave7mInlineModule("vpc-auth-1-0-0", dir)
	version.Spec.ModuleConfigRef.GithubClientConfig = &opendepotv1alpha1.GithubClientConfig{UseAuthenticatedClient: true}
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.Reconcile(context.Background(), wave6KRequest(version.Name)); err == nil {
		t.Fatal("Reconcile succeeded without the GitHub application secret")
	}
}

func TestWave7MPrepareModuleVersionInlineGeneratesFileName(t *testing.T) {
	version := wave6KModuleVersion("vpc-prepare-1-0-0", nil)
	version.Spec.ModuleConfigRef.Name = nil
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.prepareModuleVersion(context.Background(), wave6KRequest(version.Name), version); err != nil {
		t.Fatalf("prepareModuleVersion: %v", err)
	}

	if version.Spec.FileName == nil || !strings.HasSuffix(*version.Spec.FileName, ".zip") {
		t.Fatalf("FileName = %v, want a generated .zip name", version.Spec.FileName)
	}
}

func TestWave7MPrepareModuleVersionMissingConfigRefErrors(t *testing.T) {
	version := wave6KModuleVersion("vpc-prepare-2-0-0", nil)
	version.Spec.ModuleConfigRef = nil
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.prepareModuleVersion(context.Background(), wave6KRequest(version.Name), version); err == nil {
		t.Fatal("prepareModuleVersion accepted a version without moduleConfigRef")
	}
}

func TestWave7MPrepareModuleVersionUsesModuleCRVersionRef(t *testing.T) {
	module := &opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "vpc", Namespace: "default"},
		Spec: opendepotv1alpha1.ModuleSpec{
			ModuleConfig: opendepotv1alpha1.ModuleConfig{Provider: "aws", RepoOwner: "acme"},
		},
	}
	fileName := "vpc-1.0.0.zip"
	module.Status.ModuleVersionRefs = map[string]*opendepotv1alpha1.ModuleVersion{
		"1.0.0": {FileName: &fileName},
	}

	moduleName := "vpc"
	version := wave6KModuleVersion("vpc-prepare-3-0-0", nil)
	version.Spec.ModuleConfigRef.Name = &moduleName
	reconciler := wave7mReconciler(t, module, version)

	if _, err := reconciler.prepareModuleVersion(context.Background(), wave6KRequest(version.Name), version); err != nil {
		t.Fatalf("prepareModuleVersion: %v", err)
	}

	if version.Spec.FileName == nil || *version.Spec.FileName != fileName {
		t.Fatalf("FileName = %v, want %s from the Module CR", version.Spec.FileName, fileName)
	}

	if version.Spec.ModuleConfigRef.Provider != "aws" {
		t.Fatalf("Provider = %q, want the Module CR provider", version.Spec.ModuleConfigRef.Provider)
	}
}

func TestWave7MPrepareModuleVersionMissingModuleCRFallsBackToInline(t *testing.T) {
	moduleName := "absent-module"
	version := wave6KModuleVersion("vpc-prepare-4-0-0", nil)
	version.Spec.ModuleConfigRef.Name = &moduleName
	reconciler := wave7mReconciler(t, version)

	if _, err := reconciler.prepareModuleVersion(context.Background(), wave6KRequest(version.Name), version); err != nil {
		t.Fatalf("prepareModuleVersion: %v", err)
	}

	if version.Spec.FileName == nil {
		t.Fatal("inline fallback did not generate a FileName")
	}
}

func TestWave7MPrepareProviderVersionDefaultsNameFromLabel(t *testing.T) {
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aws-1-0-0",
			Namespace: "default",
			Labels:    map[string]string{"opendepot.defdev.io/provider": "aws"},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:              opendepotv1alpha1.OpenDepotProvider,
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{},
		},
	}

	reconciler := wave7mReconciler(t, version)
	if _, err := reconciler.prepareProviderVersion(version); err != nil {
		t.Fatalf("prepareProviderVersion: %v", err)
	}

	if version.Spec.ProviderConfigRef.Name == nil || *version.Spec.ProviderConfigRef.Name != "aws" {
		t.Fatalf("ProviderConfigRef.Name = %v, want the label value", version.Spec.ProviderConfigRef.Name)
	}
}

func TestWave7MPrepareProviderVersionRequiresNameOrLabel(t *testing.T) {
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "aws-2-0-0", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:              opendepotv1alpha1.OpenDepotProvider,
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{},
		},
	}

	reconciler := wave7mReconciler(t, version)
	if _, err := reconciler.prepareProviderVersion(version); err == nil {
		t.Fatal("prepareProviderVersion accepted a provider version with no name or label")
	}
}

func TestWave7MPatchProviderResolvedRepoSetsEmptyStatus(t *testing.T) {
	providerName := "aws"
	provider := &opendepotv1alpha1.Provider{ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "default"}}
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "aws-3-0-0", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{Name: &providerName},
		},
	}
	reconciler := wave7mProviderReconciler(t, provider, version)

	if err := reconciler.patchProviderResolvedRepo(context.Background(), version, "https://github.com/acme/terraform-provider-aws"); err != nil {
		t.Fatalf("patchProviderResolvedRepo: %v", err)
	}

	patched := &opendepotv1alpha1.Provider{}
	if err := reconciler.Get(context.Background(), k8stypes.NamespacedName{Name: "aws", Namespace: "default"}, patched); err != nil {
		t.Fatalf("get provider: %v", err)
	}

	if patched.Status.ResolvedSourceRepository != "https://github.com/acme/terraform-provider-aws" {
		t.Fatalf("ResolvedSourceRepository = %q, want the repo URL", patched.Status.ResolvedSourceRepository)
	}
}

func TestWave7MPatchProviderResolvedRepoKeepsExistingStatus(t *testing.T) {
	providerName := "aws"
	provider := &opendepotv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "default"},
		Status:     opendepotv1alpha1.ProviderStatus{ResolvedSourceRepository: "https://github.com/original/repo"},
	}
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "aws-4-0-0", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{Name: &providerName},
		},
	}
	reconciler := wave7mProviderReconciler(t, provider, version)

	if err := reconciler.patchProviderResolvedRepo(context.Background(), version, "https://github.com/acme/other"); err != nil {
		t.Fatalf("patchProviderResolvedRepo: %v", err)
	}

	kept := &opendepotv1alpha1.Provider{}
	if err := reconciler.Get(context.Background(), k8stypes.NamespacedName{Name: "aws", Namespace: "default"}, kept); err != nil {
		t.Fatalf("get provider: %v", err)
	}

	if kept.Status.ResolvedSourceRepository != "https://github.com/original/repo" {
		t.Fatalf("ResolvedSourceRepository = %q, want the original value kept", kept.Status.ResolvedSourceRepository)
	}
}

func TestWave7MPatchProviderResolvedRepoSkipsWithoutRepoOrProvider(t *testing.T) {
	providerName := "missing"
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "aws-5-0-0", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{Name: &providerName},
		},
	}
	reconciler := wave7mProviderReconciler(t, version)

	if err := reconciler.patchProviderResolvedRepo(context.Background(), version, ""); err != nil {
		t.Fatalf("empty repo URL: %v", err)
	}

	if err := reconciler.patchProviderResolvedRepo(context.Background(), version, "https://github.com/acme/x"); err != nil {
		t.Fatalf("missing provider: %v", err)
	}
}

func TestWave7MFetchModuleArchiveAuthenticatedWithoutSecretErrors(t *testing.T) {
	version := wave6KModuleVersion("vpc-fetch-1-0-0", nil)
	version.Spec.ModuleConfigRef.GithubClientConfig = &opendepotv1alpha1.GithubClientConfig{UseAuthenticatedClient: true}
	reconciler := wave7mReconciler(t, version)

	if _, _, err := reconciler.fetchModuleArchive(context.Background(), version); err == nil {
		t.Fatal("fetchModuleArchive succeeded without the application secret")
	}
}

func TestWave7MFetchModuleArchiveDownloadsTarball(t *testing.T) {
	wave7mArchiveTransport(t, moduleTestArchive(t), http.StatusOK)
	version := wave7mInlineModule("vpc-fetch-2-0-0", t.TempDir())
	reconciler := wave7mReconciler(t, version)

	archive, checksum, err := reconciler.fetchModuleArchive(context.Background(), version)
	if err != nil {
		t.Fatalf("fetchModuleArchive: %v", err)
	}

	if len(archive) == 0 || checksum == nil {
		t.Fatalf("fetchModuleArchive returned %d bytes, checksum=%v", len(archive), checksum)
	}
}
