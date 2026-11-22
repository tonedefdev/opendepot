package controller

import (
	"context"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema"
	"github.com/tonedefdev/opendepot/pkg/storage"
	"github.com/tonedefdev/opendepot/pkg/storage/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func wave7mProviderVersion(name string, withSchema bool) *opendepotv1alpha1.Version {
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotProvider,
			Version: "1.0.0",
		},
	}

	if withSchema {
		version.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: "schema", Digest: "abc", ExtractedAt: "now"}
	}

	return version
}

func wave7mModuleWithGrade(name string, grade string) *opendepotv1alpha1.Version {
	version := wave6KModuleVersion(name, nil)
	version.Status.ContractConfigMapRef = &opendepotv1alpha1.ContractConfigMapRef{Name: name, Key: "contract", Grade: grade}

	return version
}

func TestWave7MModuleVersionsForProviderSchemaFansOutToPendingModules(t *testing.T) {
	provider := wave7mProviderVersion("aws-1-0-0", true)
	partial := wave7mModuleWithGrade("vpc-partial", hclschema.GradePartial)
	unscored := wave6KModuleVersion("vpc-unscored", nil)
	full := wave7mModuleWithGrade("vpc-full", hclschema.GradeFull)
	otherProvider := wave7mProviderVersion("gcp-1-0-0", true)
	reconciler := wave6KReconciler(t, provider, partial, unscored, full, otherProvider)

	requests := reconciler.moduleVersionsForProviderSchema(context.Background(), provider)

	got := map[string]bool{}
	for _, request := range requests {
		got[request.Name] = true
	}

	if !got["vpc-partial"] || !got["vpc-unscored"] {
		t.Fatalf("requests = %v, want partial and unscored modules", requests)
	}

	if got["vpc-full"] || got["aws-1-0-0"] || got["gcp-1-0-0"] {
		t.Fatalf("requests = %v, must skip full-grade modules and non-module versions", requests)
	}
}

func TestWave7MModuleVersionsForProviderSchemaIgnoresUnsuitableObjects(t *testing.T) {
	reconciler := wave6KReconciler(t)

	if got := reconciler.moduleVersionsForProviderSchema(context.Background(), &metav1.PartialObjectMetadata{}); got != nil {
		t.Fatalf("non-Version object requests = %v, want nil", got)
	}

	if got := reconciler.moduleVersionsForProviderSchema(context.Background(), wave7mProviderVersion("aws-2-0-0", false)); got != nil {
		t.Fatalf("provider without schema requests = %v, want nil", got)
	}

	if got := reconciler.moduleVersionsForProviderSchema(context.Background(), wave6KModuleVersion("vpc-2-0-0", nil)); got != nil {
		t.Fatalf("module version requests = %v, want nil", got)
	}
}

func TestWave7MVersionsInNamespaceMapsEveryVersionInNamespace(t *testing.T) {
	first := wave6KModuleVersion("vpc-1-0-0", nil)
	second := wave7mProviderVersion("aws-1-0-0", false)
	elsewhere := wave6KModuleVersion("vpc-1-0-0-other", nil)
	elsewhere.Namespace = "other"
	reconciler := wave6KReconciler(t, first, second, elsewhere)

	scanPolicy := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "default"}}
	requests := reconciler.versionsInNamespace(context.Background(), scanPolicy)

	want := map[reconcile.Request]bool{
		{NamespacedName: client.ObjectKeyFromObject(first)}:  true,
		{NamespacedName: client.ObjectKeyFromObject(second)}: true,
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %v, want %d entries", requests, len(want))
	}

	for _, request := range requests {
		if !want[request] {
			t.Fatalf("unexpected request %v in %v", request, requests)
		}
	}
}

func TestWave7MVersionsInNamespaceReturnsNilOnListFailure(t *testing.T) {
	reconciler := wave6KReconciler(t)
	reconciler.Client = fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

	scanPolicy := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "default"}}
	if got := reconciler.versionsInNamespace(context.Background(), scanPolicy); got != nil {
		t.Fatalf("requests = %v, want nil when the list fails", got)
	}
}

func TestWave7MResolveStorageInterfaceSelectsFileSystem(t *testing.T) {
	version := wave6KModuleVersion("vpc-storage-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	reconciler := wave6KReconciler(t, version)

	storageInterface, err := reconciler.resolveStorageInterface(context.Background(), &types.StorageObjectInput{Version: version})
	if err != nil {
		t.Fatalf("resolveStorageInterface: %v", err)
	}

	if _, ok := storageInterface.(*storage.FileSystem); !ok {
		t.Fatalf("storage = %T, want *storage.FileSystem", storageInterface)
	}
}

func TestWave7MResolveStorageInterfaceRejectsMissingStorageConfig(t *testing.T) {
	version := wave6KModuleVersion("vpc-storage-2-0-0", nil)
	reconciler := wave6KReconciler(t, version)

	if _, err := reconciler.resolveStorageInterface(context.Background(), &types.StorageObjectInput{Version: version}); err == nil {
		t.Fatal("resolveStorageInterface accepted a version without storage config")
	}
}

func TestWave7MGetVersionFilePathBuildsFileSystemKey(t *testing.T) {
	version := wave6KModuleVersion("vpc-path-1-0-0", wave6KFileSystemConfig(t.TempDir()))
	fileName := "vpc-1.0.0.zip"
	version.Spec.FileName = &fileName

	path, err := getVersionFilePath(version)
	if err != nil {
		t.Fatalf("getVersionFilePath: %v", err)
	}

	if path == nil || *path == "" {
		t.Fatalf("getVersionFilePath returned an empty path: %v", path)
	}
}

func TestWave7MGetVersionFilePathRejectsBadFileNames(t *testing.T) {
	version := wave6KModuleVersion("vpc-path-2-0-0", wave6KFileSystemConfig(t.TempDir()))

	if _, err := getVersionFilePath(version); err == nil || !strings.Contains(err.Error(), "fileName is nil") {
		t.Fatalf("err = %v, want a nil fileName error", err)
	}

	traversal := "../escape.zip"
	version.Spec.FileName = &traversal
	if _, err := getVersionFilePath(version); err == nil {
		t.Fatal("getVersionFilePath accepted a path-traversal fileName")
	}
}
