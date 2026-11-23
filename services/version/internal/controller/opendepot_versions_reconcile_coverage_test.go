package controller

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func wave6KReconciler(t *testing.T, objects ...client.Object) *VersionReconciler {
	t.Helper()

	scheme := helpersCovScheme(t)

	return &VersionReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objects...).
			WithStatusSubresource(&opendepotv1alpha1.Version{}).
			Build(),
		Scheme: scheme,
		Log:    logr.Discard(),
	}
}

func wave6KFileSystemConfig(dir string) *opendepotv1alpha1.StorageConfig {
	return &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dir},
	}
}

func wave6KModuleVersion(name string, storage *opendepotv1alpha1.StorageConfig) *opendepotv1alpha1.Version {
	moduleName := "vpc"
	fileFormat := "zip"

	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotModule,
			Version: "1.0.0",
			ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{
				Name:          &moduleName,
				FileFormat:    &fileFormat,
				StorageConfig: storage,
			},
		},
	}
}

func wave6KRequest(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: client.ObjectKey{Name: name, Namespace: "default"}}
}

func TestWave6KReconcileNotFoundIsIgnored(t *testing.T) {
	ctx := context.Background()
	r := wave6KReconciler(t)

	result, err := r.Reconcile(ctx, wave6KRequest("missing"))
	if err != nil {
		t.Fatalf("expected no error for a missing Version, got %v", err)
	}

	if result != (ctrl.Result{}) {
		t.Fatalf("expected empty result, got %+v", result)
	}
}

func TestWave6KReconcileAddsFinalizerAndRequeues(t *testing.T) {
	ctx := context.Background()
	version := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	r := wave6KReconciler(t, version)

	result, err := r.Reconcile(ctx, wave6KRequest(version.Name))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if result.RequeueAfter != time.Second {
		t.Fatalf("expected 1s requeue, got %v", result.RequeueAfter)
	}

	stored := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, wave6KRequest(version.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if !slices.Contains(stored.Finalizers, opendepotv1alpha1.OpenDepotFinalizer) {
		t.Fatalf("expected finalizer %q, got %v", opendepotv1alpha1.OpenDepotFinalizer, stored.Finalizers)
	}
}

func TestWave6KReconcileDotInNameWritesTerminalStatusOnce(t *testing.T) {
	ctx := context.Background()
	version := wave6KModuleVersion("vpc.1.0.0", wave6KFileSystemConfig(t.TempDir()))
	version.Finalizers = []string{opendepotv1alpha1.OpenDepotFinalizer}
	r := wave6KReconciler(t, version)

	result, err := r.Reconcile(ctx, wave6KRequest(version.Name))
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	if result != (ctrl.Result{}) {
		t.Fatalf("expected empty result, got %+v", result)
	}

	stored := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, wave6KRequest(version.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if stored.Status.Synced {
		t.Fatalf("expected Synced to be false")
	}

	if !strings.Contains(stored.Status.SyncStatus, "must not contain '.' characters") {
		t.Fatalf("unexpected sync status: %q", stored.Status.SyncStatus)
	}

	// The terminal status is already written, so a second pass is a no-op.
	if _, err := r.Reconcile(ctx, wave6KRequest(version.Name)); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
}

func TestWave6KReconcileUnknownTypeReturnsError(t *testing.T) {
	ctx := context.Background()
	version := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	version.Spec.Type = "Unknown"
	version.Finalizers = []string{opendepotv1alpha1.OpenDepotFinalizer}
	r := wave6KReconciler(t, version)

	_, err := r.Reconcile(ctx, wave6KRequest(version.Name))
	if err == nil || !strings.Contains(err.Error(), "no usable type provided") {
		t.Fatalf("expected unusable type error, got %v", err)
	}
}

func TestWave6KReconcileMissingConfigRefsReturnErrors(t *testing.T) {
	ctx := context.Background()

	module := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	module.Finalizers = []string{opendepotv1alpha1.OpenDepotFinalizer}
	module.Spec.ModuleConfigRef = nil
	r := wave6KReconciler(t, module)

	_, err := r.Reconcile(ctx, wave6KRequest(module.Name))
	if err == nil || !strings.Contains(err.Error(), "moduleConfigRef is required") {
		t.Fatalf("expected moduleConfigRef error, got %v", err)
	}

	provider := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "null-3-2-4",
			Namespace:  "default",
			Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotProvider,
			Version: "3.2.4",
		},
	}
	r = wave6KReconciler(t, provider)

	_, err = r.Reconcile(ctx, wave6KRequest(provider.Name))
	if err == nil || !strings.Contains(err.Error(), "providerConfigRef is required") {
		t.Fatalf("expected providerConfigRef error, got %v", err)
	}
}

func TestWave6KReconcileDualConfigConflictWritesTerminalStatus(t *testing.T) {
	ctx := context.Background()
	version := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	version.Finalizers = []string{opendepotv1alpha1.OpenDepotFinalizer}
	providerName := "null"
	version.Spec.ProviderConfigRef = &opendepotv1alpha1.ProviderConfig{Name: &providerName}
	r := wave6KReconciler(t, version)

	result, err := r.Reconcile(ctx, wave6KRequest(version.Name))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if result != (ctrl.Result{}) {
		t.Fatalf("expected empty result for a permanent config error, got %+v", result)
	}

	stored := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, wave6KRequest(version.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	want := "Only one of 'ModuleConfigRef' or 'ProviderConfigRef' can be provided: both are defined"
	if stored.Status.Synced || stored.Status.SyncStatus != want {
		t.Fatalf("unexpected status: synced=%v syncStatus=%q", stored.Status.Synced, stored.Status.SyncStatus)
	}
}

func TestWave6KReconcileDeletionWithoutFinalizerIsNoop(t *testing.T) {
	ctx := context.Background()
	now := metav1.NewTime(time.Now())
	version := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	version.DeletionTimestamp = &now
	version.Finalizers = []string{"example.com/other"}
	r := wave6KReconciler(t, version)

	result, err := r.Reconcile(ctx, wave6KRequest(version.Name))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if result != (ctrl.Result{}) {
		t.Fatalf("expected empty result, got %+v", result)
	}
}

func TestWave6KReconcileDeletionRemovesArtifactAndFinalizer(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := metav1.NewTime(time.Now())
	fileName := "archive.zip"
	version := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(dir))
	version.Spec.FileName = &fileName
	version.DeletionTimestamp = &now
	version.Finalizers = []string{opendepotv1alpha1.OpenDepotFinalizer}

	filePath, err := getVersionFilePath(version)
	if err != nil {
		t.Fatalf("resolve file path: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(*filePath, []byte("archive"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	r := wave6KReconciler(t, version)

	if _, err := r.Reconcile(ctx, wave6KRequest(version.Name)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if _, err := os.Stat(*filePath); !os.IsNotExist(err) {
		t.Fatalf("expected artifact to be removed, stat err=%v", err)
	}

	err = r.Get(ctx, wave6KRequest(version.Name).NamespacedName, &opendepotv1alpha1.Version{})
	if !k8serr.IsNotFound(err) {
		t.Fatalf("expected version to be garbage collected after finalizer removal, got %v", err)
	}
}

func TestWave6KReconcileDeletionWithoutStoragePathRemovesFinalizer(t *testing.T) {
	ctx := context.Background()
	now := metav1.NewTime(time.Now())
	version := wave6KModuleVersion("vpc-1-0-0", nil)
	version.DeletionTimestamp = &now
	version.Finalizers = []string{opendepotv1alpha1.OpenDepotFinalizer}
	r := wave6KReconciler(t, version)

	if _, err := r.Reconcile(ctx, wave6KRequest(version.Name)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	err := r.Get(ctx, wave6KRequest(version.Name).NamespacedName, &opendepotv1alpha1.Version{})
	if !k8serr.IsNotFound(err) {
		t.Fatalf("expected version to be removed after finalizer removal, got %v", err)
	}
}

func TestWave6KReconcileSyncedAgentSkipsWhenScanAgentsDisabled(t *testing.T) {
	ctx := context.Background()
	agentName := "reviewer"
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "agent-1-0-0",
			Namespace:  "default",
			Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:           opendepotv1alpha1.OpenDepotAgent,
			Version:        "1.0.0",
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &agentName},
		},
		Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"},
	}
	r := wave6KReconciler(t, version)

	result, err := r.Reconcile(ctx, wave6KRequest(version.Name))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if result != (ctrl.Result{}) {
		t.Fatalf("expected empty result, got %+v", result)
	}
}

func TestWave6KReconcileAgentWithoutSourceRefReturnsError(t *testing.T) {
	ctx := context.Background()
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "skill-1-0-0",
			Namespace:  "default",
			Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotSkill,
			Version: "1.0.0",
		},
	}
	r := wave6KReconciler(t, version)

	_, err := r.Reconcile(ctx, wave6KRequest(version.Name))
	if err == nil || !strings.Contains(err.Error(), "agentSourceRef is required for skill version") {
		t.Fatalf("expected agentSourceRef error, got %v", err)
	}
}

func TestWave6KPersistSyncStatusWritesOnlySyncFields(t *testing.T) {
	ctx := context.Background()
	version := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	version.Status.Synced = true
	version.Status.SyncStatus = "Synced"
	r := wave6KReconciler(t, version)

	if err := r.persistSyncStatus(ctx, wave6KRequest(version.Name), false, "Blocked for review"); err != nil {
		t.Fatalf("persistSyncStatus: %v", err)
	}

	stored := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, wave6KRequest(version.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if stored.Status.Synced || stored.Status.SyncStatus != "Blocked for review" {
		t.Fatalf("unexpected status: synced=%v syncStatus=%q", stored.Status.Synced, stored.Status.SyncStatus)
	}
}

func TestWave6KPersistSyncStatusMissingVersionReturnsNotFound(t *testing.T) {
	r := wave6KReconciler(t)

	err := r.persistSyncStatus(context.Background(), wave6KRequest("missing"), true, "Synced")
	if !k8serr.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestWave6KPersistScanViolationRecordsFindingsAndBranchPhase(t *testing.T) {
	ctx := context.Background()
	version := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	version.Spec.SourceCommit = "abc123"
	version.Status.Synced = true
	r := wave6KReconciler(t, version)

	sourceScan := &opendepotv1alpha1.SourceScan{}
	scanErr := context.DeadlineExceeded
	if err := r.persistScanViolation(ctx, wave6KRequest(version.Name), nil, sourceScan, scanErr); err != nil {
		t.Fatalf("persistScanViolation: %v", err)
	}

	stored := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, wave6KRequest(version.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if stored.Status.Synced {
		t.Fatalf("expected Synced to be false")
	}

	if stored.Status.SyncStatus != "Scan policy violation: "+scanErr.Error() {
		t.Fatalf("unexpected sync status: %q", stored.Status.SyncStatus)
	}

	if stored.Status.SourceScan == nil {
		t.Fatalf("expected source scan to be recorded")
	}

	if stored.Status.BranchPhase != branchPhaseRejected {
		t.Fatalf("expected branch phase %q, got %q", branchPhaseRejected, stored.Status.BranchPhase)
	}
}

func TestWave6KPersistAgentBlockSetsBranchPhase(t *testing.T) {
	ctx := context.Background()
	version := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	version.Spec.SourceCommit = "abc123"
	version.Status.Synced = true
	r := wave6KReconciler(t, version)

	if err := r.persistAgentBlock(ctx, wave6KRequest(version.Name), nil, nil, nil, nil, "JevUnavailable: timeout"); err != nil {
		t.Fatalf("persistAgentBlock: %v", err)
	}

	stored := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, wave6KRequest(version.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if stored.Status.Synced || stored.Status.SyncStatus != "JevUnavailable: timeout" {
		t.Fatalf("unexpected status: synced=%v syncStatus=%q", stored.Status.Synced, stored.Status.SyncStatus)
	}

	if stored.Status.BranchPhase != branchPhaseHeld {
		t.Fatalf("expected branch phase %q, got %q", branchPhaseHeld, stored.Status.BranchPhase)
	}

	if err := r.persistAgentBlock(ctx, wave6KRequest(version.Name), nil, nil, nil, nil, "JevBlocked: policy"); err != nil {
		t.Fatalf("persistAgentBlock blocked: %v", err)
	}

	if err := r.Get(ctx, wave6KRequest(version.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if stored.Status.BranchPhase != branchPhaseRejected {
		t.Fatalf("expected branch phase %q, got %q", branchPhaseRejected, stored.Status.BranchPhase)
	}
}

func TestWave6KPersistReadmeRefAndClearStaleRef(t *testing.T) {
	ctx := context.Background()
	ref := &opendepotv1alpha1.ReadmeConfigMapRef{Name: "vpc-readme"}

	skill := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "skill-1-0-0", Namespace: "default"},
		Spec:       opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotSkill, Version: "1.0.0"},
		Status:     opendepotv1alpha1.VersionStatus{ReadmeConfigMapRef: ref},
	}
	r := wave6KReconciler(t, skill)

	if err := r.clearStaleReadmeRef(ctx, wave6KRequest(skill.Name), skill); err != nil {
		t.Fatalf("clearStaleReadmeRef: %v", err)
	}

	if skill.Status.ReadmeConfigMapRef != nil {
		t.Fatalf("expected in-memory readme ref to be cleared")
	}

	stored := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, wave6KRequest(skill.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if stored.Status.ReadmeConfigMapRef != nil {
		t.Fatalf("expected stored readme ref to be cleared")
	}

	module := wave6KModuleVersion("vpc-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	module.Status.ReadmeConfigMapRef = ref
	r = wave6KReconciler(t, module)

	if err := r.clearStaleReadmeRef(ctx, wave6KRequest(module.Name), module); err != nil {
		t.Fatalf("clearStaleReadmeRef module: %v", err)
	}

	if err := r.Get(ctx, wave6KRequest(module.Name).NamespacedName, stored); err != nil {
		t.Fatalf("get module version: %v", err)
	}

	if stored.Status.ReadmeConfigMapRef == nil || stored.Status.ReadmeConfigMapRef.Name != "vpc-readme" {
		t.Fatalf("module readme ref must not be cleared")
	}

	if err := r.persistReadmeRef(ctx, wave6KRequest(module.Name), ref); err != nil {
		t.Fatalf("persistReadmeRef unchanged: %v", err)
	}
}
