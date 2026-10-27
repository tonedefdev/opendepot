/*
Copyright 2026.

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
	"context"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/testutils"
)

func TestConfigConflictMessage(t *testing.T) {
	name := "example"
	moduleRef := &opendepotv1alpha1.ModuleConfig{Name: &name}
	providerRef := &opendepotv1alpha1.ProviderConfig{Name: &name}
	agentRef := &opendepotv1alpha1.AgentSourceConfig{Name: &name}

	tests := []struct {
		name    string
		version opendepotv1alpha1.VersionSpec
		want    string
	}{
		{
			name:    "single module reference is valid",
			version: opendepotv1alpha1.VersionSpec{ModuleConfigRef: moduleRef},
			want:    "",
		},
		{
			name:    "single agent reference is valid",
			version: opendepotv1alpha1.VersionSpec{AgentSourceRef: agentRef},
			want:    "",
		},
		{
			name:    "module and provider conflict keeps the existing message",
			version: opendepotv1alpha1.VersionSpec{ModuleConfigRef: moduleRef, ProviderConfigRef: providerRef},
			want:    "Only one of 'ModuleConfigRef' or 'ProviderConfigRef' can be provided: both are defined",
		},
		{
			name:    "agent and module conflict",
			version: opendepotv1alpha1.VersionSpec{AgentSourceRef: agentRef, ModuleConfigRef: moduleRef},
			want:    "Only one of 'AgentSourceRef', 'ModuleConfigRef', or 'ProviderConfigRef' can be provided: multiple are defined",
		},
		{
			name:    "agent and provider conflict",
			version: opendepotv1alpha1.VersionSpec{AgentSourceRef: agentRef, ProviderConfigRef: providerRef},
			want:    "Only one of 'AgentSourceRef', 'ModuleConfigRef', or 'ProviderConfigRef' can be provided: multiple are defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := &opendepotv1alpha1.Version{Spec: tt.version}
			if got := configConflictMessage(version); got != tt.want {
				t.Errorf("configConflictMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionImmutable(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name    string
		version opendepotv1alpha1.VersionSpec
		want    bool
	}{
		{
			name:    "no source config is mutable",
			version: opendepotv1alpha1.VersionSpec{},
			want:    false,
		},
		{
			name: "module immutable",
			version: opendepotv1alpha1.VersionSpec{
				ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{Immutable: &trueVal},
			},
			want: true,
		},
		{
			name: "agent immutable",
			version: opendepotv1alpha1.VersionSpec{
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Immutable: &trueVal},
			},
			want: true,
		},
		{
			name: "agent explicitly mutable",
			version: opendepotv1alpha1.VersionSpec{
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Immutable: &falseVal},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := &opendepotv1alpha1.Version{Spec: tt.version}
			if got := versionImmutable(version); got != tt.want {
				t.Errorf("versionImmutable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentStorageNaming(t *testing.T) {
	name := "code-review"
	bucket := "bucket"
	storageConfig := &opendepotv1alpha1.StorageConfig{
		S3: &opendepotv1alpha1.AmazonS3Config{Bucket: bucket},
	}

	tests := []struct {
		name        string
		versionType string
		wantPrefix  string
	}{
		{name: "skill", versionType: opendepotv1alpha1.OpenDepotSkill, wantPrefix: "skill-code-review"},
		{name: "agent", versionType: opendepotv1alpha1.OpenDepotAgent, wantPrefix: "agent-code-review"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := &opendepotv1alpha1.Version{
				Spec: opendepotv1alpha1.VersionSpec{
					Type:           tt.versionType,
					AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name, StorageConfig: storageConfig},
				},
			}

			gotName, err := getVersionName(version)
			if err != nil {
				t.Fatalf("getVersionName() error = %v", err)
			}

			if *gotName != tt.wantPrefix {
				t.Errorf("getVersionName() = %q, want %q", *gotName, tt.wantPrefix)
			}

			gotConfig, err := getVersionStorageConfig(version)
			if err != nil {
				t.Fatalf("getVersionStorageConfig() error = %v", err)
			}

			if gotConfig != storageConfig {
				t.Errorf("getVersionStorageConfig() did not return the agentSourceRef storage config")
			}
		})
	}
}

func TestNeedsDefinitionBackfill(t *testing.T) {
	ref := &opendepotv1alpha1.DefinitionConfigMapRef{Name: "example-definition", Key: "definition", FileName: "SKILL.md"}

	tests := []struct {
		name    string
		version opendepotv1alpha1.Version
		want    bool
	}{
		{
			name:    "skill without a definition reference needs backfill",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotSkill}},
			want:    true,
		},
		{
			name:    "agent without a definition reference needs backfill",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotAgent}},
			want:    true,
		},
		{
			name: "agent with a definition reference does not need backfill",
			version: opendepotv1alpha1.Version{
				Spec:   opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotAgent},
				Status: opendepotv1alpha1.VersionStatus{DefinitionConfigMapRef: ref},
			},
			want: false,
		},
		{
			name:    "module without a definition reference does not need backfill",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotModule}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsDefinitionBackfill(&tt.version); got != tt.want {
				t.Errorf("needsDefinitionBackfill() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockedAndFailedWritesKeepDefinitionRef(t *testing.T) {
	ctx := context.Background()
	ref := &opendepotv1alpha1.DefinitionConfigMapRef{Name: "code-review-abc1234-definition", Key: "definition", FileName: "SKILL.md"}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "code-review-abc1234", Namespace: "default"}}

	tests := []struct {
		name  string
		write func(r *VersionReconciler) error
	}{
		{
			name: "held branch block keeps the definition reference",
			write: func(r *VersionReconciler) error {
				return r.persistAgentBlock(ctx, req, nil, nil, "Agent scan failed: boom")
			},
		},
		{
			name: "sync status write keeps the definition reference",
			write: func(r *VersionReconciler) error {
				return r.persistSyncStatus(ctx, req, false, "version is marked immutable")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := newDefinitionFixture(t, interceptor.Funcs{})

			current := &opendepotv1alpha1.Version{}
			if err := r.Get(ctx, req.NamespacedName, current); err != nil {
				t.Fatalf("Get: %v", err)
			}

			current.Status.DefinitionConfigMapRef = ref
			if err := r.Status().Update(ctx, current); err != nil {
				t.Fatalf("seed status: %v", err)
			}

			if err := tt.write(r); err != nil {
				t.Fatalf("write: %v", err)
			}

			got := &opendepotv1alpha1.Version{}
			if err := r.Get(ctx, req.NamespacedName, got); err != nil {
				t.Fatalf("Get after write: %v", err)
			}

			if got.Status.DefinitionConfigMapRef == nil || got.Status.DefinitionConfigMapRef.Name != ref.Name {
				t.Errorf("DefinitionConfigMapRef = %+v, want %+v", got.Status.DefinitionConfigMapRef, ref)
			}
		})
	}
}

func TestClearStaleReadmeRefOnlyChangesAgentVersions(t *testing.T) {
	reconciler, agent := newDefinitionFixture(t, interceptor.Funcs{})
	ctx := context.Background()

	module := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "example-module-1.0.0", Namespace: agent.Namespace},
		Spec:       opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotModule},
	}
	if err := reconciler.Create(ctx, module); err != nil {
		t.Fatalf("create module version: %v", err)
	}

	stale := &opendepotv1alpha1.ReadmeConfigMapRef{Name: "code-review-abc1234-readme", Key: "README.md"}
	for _, version := range []*opendepotv1alpha1.Version{agent, module} {
		current := &opendepotv1alpha1.Version{}
		if err := reconciler.Get(ctx, client.ObjectKeyFromObject(version), current); err != nil {
			t.Fatalf("get %s: %v", version.Name, err)
		}

		current.Status.ReadmeConfigMapRef = stale
		if err := reconciler.Status().Update(ctx, current); err != nil {
			t.Fatalf("set readme ref on %s: %v", version.Name, err)
		}
	}

	for _, version := range []*opendepotv1alpha1.Version{agent, module} {
		current := &opendepotv1alpha1.Version{}
		if err := reconciler.Get(ctx, client.ObjectKeyFromObject(version), current); err != nil {
			t.Fatalf("get %s: %v", version.Name, err)
		}

		req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(version)}
		if err := reconciler.clearStaleReadmeRef(ctx, req, current); err != nil {
			t.Fatalf("clearStaleReadmeRef(%s) error = %v", version.Name, err)
		}
	}

	stored := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(agent), stored); err != nil {
		t.Fatalf("get agent: %v", err)
	}

	if stored.Status.ReadmeConfigMapRef != nil {
		t.Fatalf("expected stale readme ref cleared on agent version, got %+v", stored.Status.ReadmeConfigMapRef)
	}

	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(module), stored); err != nil {
		t.Fatalf("get module: %v", err)
	}

	if stored.Status.ReadmeConfigMapRef == nil || *stored.Status.ReadmeConfigMapRef != *stale {
		t.Fatalf("expected module readme ref untouched, got %+v", stored.Status.ReadmeConfigMapRef)
	}
}

type jevRoundTripper func(*http.Request) (*http.Response, error)

func (f jevRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jevHTTPResponse(req *http.Request, status int, header http.Header, body []byte) *http.Response {
	if header == nil {
		header = http.Header{}
	}

	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)), Request: req}
}

// stubJevGitHub serves the Skill tarball through http.DefaultTransport, which the GitHub client uses. Other hosts,
// including the httptest Jev endpoint, pass through to the original transport.
func stubJevGitHub(t *testing.T, tarball []byte) {
	t.Helper()

	original := http.DefaultTransport
	http.DefaultTransport = jevRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "api.github.com" && strings.Contains(req.URL.Path, "/tarball/"):
			header := http.Header{"Location": []string{"https://codeload.test/archive.tar.gz"}}
			return jevHTTPResponse(req, http.StatusFound, header, nil), nil
		case req.URL.Host == "codeload.test":
			return jevHTTPResponse(req, http.StatusOK, nil, tarball), nil
		default:
			return original.RoundTrip(req)
		}
	})
	t.Cleanup(func() { http.DefaultTransport = original })
}

func stubJevTrivy(t *testing.T) {
	t.Helper()

	original := runTrivy
	runTrivy = func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`{"SchemaVersion":2,"Results":[]}`), nil
	}
	t.Cleanup(func() { runTrivy = original })
}

func setJevSigningKey(t *testing.T) {
	t.Helper()

	gpgHome, err := os.MkdirTemp("", "jev-gpg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(gpgHome) })

	_, _, privateKey, err := testutils.GenerateTestGPGKeyPair(gpgHome)
	if err != nil {
		t.Fatalf("GenerateTestGPGKeyPair: %v", err)
	}

	t.Setenv(agentSigningKeyEnv, privateKey)
}

func jevStatus(status int, body string) jevStub {
	return func(string) (int, string) {
		return status, body
	}
}

// jevReconcileTarball packs files under the top-level directory GitHub uses, beneath the Skill path skills/agent.
func jevReconcileTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()

	entries := make(map[string]string, len(files))
	for name, body := range files {
		entries["acme-agents-abc1234/skills/agent/"+name] = body
	}

	return jevTestArchive(t, entries)
}

type jevReconcileCase struct {
	stub      jevStub
	files     map[string]string
	policy    *opendepotv1alpha1.JevPolicy
	cached    *opendepotv1alpha1.JevAssessment
	forceSync bool
	noSecret  bool
	noRef     bool
	disabled  bool
}

type jevReconcileFixture struct {
	reconciler *VersionReconciler
	key        types.NamespacedName
	calls      *atomic.Int32
	recorder   *events.FakeRecorder
}

// newJevReconcileFixture runs the full Reconcile path for a Skill Version. GitHub, Trivy, and Jev are stubbed, and the
// finalizer is added before the test starts, so each later reconcile does the work.
func newJevReconcileFixture(t *testing.T, c jevReconcileCase) *jevReconcileFixture {
	t.Helper()

	if c.files == nil {
		c.files = map[string]string{"SKILL.md": jevEntry}
	}

	if c.stub == nil {
		c.stub = jevOK
	}

	endpoint, calls := newJevEndpoint(t, c.stub)
	stubJevGitHub(t, jevReconcileTarball(t, c.files))
	stubJevTrivy(t)
	setJevSigningKey(t)

	storageDir := t.TempDir()
	repoURL := "https://github.com/acme/agents"
	version := newJevTestVersion(c.policy, c.cached)
	version.Name = "agent-1-0-0"
	version.Spec.Version = "1.0.0"
	version.Spec.ForceSync = c.forceSync
	version.Spec.AgentSourceRef.RepoOwner = "acme"
	version.Spec.AgentSourceRef.RepoUrl = &repoURL
	version.Spec.AgentSourceRef.Path = "skills/agent"
	version.Spec.AgentSourceRef.StorageConfig = &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &storageDir},
	}
	if c.noRef {
		version.Spec.AgentSourceRef.JevSecretRef = nil
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "jev-token", Namespace: "default"},
		Data:       map[string][]byte{"jevToken": []byte(jevSentinelToken)},
	}

	objects := []client.Object{version}
	if !c.noSecret {
		objects = append(objects, secret)
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme core: %v", err)
	}

	if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	recorder := events.NewFakeRecorder(20)
	reconciler := &VersionReconciler{
		Client:        fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&opendepotv1alpha1.Version{}).Build(),
		Scheme:        scheme,
		Log:           logr.Discard(),
		ScanAgents:    true,
		scanSem:       make(chan struct{}, 1),
		JevEnabled:    !c.disabled,
		JevEndpoint:   endpoint,
		JevMaxRetries: 3,
		Recorder:      recorder,
	}

	key := types.NamespacedName{Name: version.Name, Namespace: version.Namespace}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("Reconcile adding finalizer: %v", err)
	}

	return &jevReconcileFixture{reconciler: reconciler, key: key, calls: calls, recorder: recorder}
}

func (f *jevReconcileFixture) reconcile(t *testing.T) ctrl.Result {
	t.Helper()

	result, err := f.reconcileResult(t)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	return result
}

func (f *jevReconcileFixture) reconcileResult(t *testing.T) (ctrl.Result, error) {
	t.Helper()

	return f.reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: f.key})
}

func (f *jevReconcileFixture) version(t *testing.T) *opendepotv1alpha1.Version {
	t.Helper()

	stored := &opendepotv1alpha1.Version{}
	if err := f.reconciler.Get(context.Background(), f.key, stored); err != nil {
		t.Fatalf("Get: %v", err)
	}

	return stored
}

func wantJevCalls(t *testing.T, f *jevReconcileFixture, want int32) {
	t.Helper()

	if got := f.calls.Load(); got != want {
		t.Fatalf("Jev HTTP calls = %d; want %d", got, want)
	}
}

func wantJevBlocked(t *testing.T, stored *opendepotv1alpha1.Version, wantRetry int32) {
	t.Helper()

	if stored.Status.Synced {
		t.Fatalf("Synced = true; want false")
	}

	if stored.Status.JevAssessment == nil || !stored.Status.JevAssessment.Blocked {
		t.Fatalf("JevAssessment = %+v; want blocked", stored.Status.JevAssessment)
	}

	if stored.Status.JevAssessment.RetryCount != wantRetry {
		t.Fatalf("RetryCount = %d; want %d", stored.Status.JevAssessment.RetryCount, wantRetry)
	}
}

// TestReconcileJevNoThresholdsFailsClosed covers a 503 with no thresholds: the Version stays unsynced and is re-run
// after 15 seconds as attempt 1, with 4 HTTP attempts for the one part.
func TestReconcileJevNoThresholdsFailsClosed(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevStatus(http.StatusServiceUnavailable, "unavailable")})

	result := f.reconcile(t)
	if result.RequeueAfter != jevRetryRequeue {
		t.Fatalf("RequeueAfter = %v; want %v", result.RequeueAfter, jevRetryRequeue)
	}

	stored := f.version(t)
	wantJevBlocked(t, stored, 1)
	if !strings.HasPrefix(stored.Status.SyncStatus, "JevUnavailable:") {
		t.Fatalf("SyncStatus = %q; want JevUnavailable prefix", stored.Status.SyncStatus)
	}

	if !strings.Contains(stored.Status.JevAssessment.Error, "part 1") {
		t.Fatalf("Error = %q; want it to name part 1", stored.Status.JevAssessment.Error)
	}

	wantJevCalls(t, f, 4)
}

// TestReconcileJevUnauthorizedRerunsWithoutCount covers 401: retryCount stays 0, the Version is re-run every 15
// seconds, and it publishes once Jev accepts the token.
func TestReconcileJevUnauthorizedRerunsWithoutCount(t *testing.T) {
	var reject atomic.Bool
	reject.Store(true)

	f := newJevReconcileFixture(t, jevReconcileCase{stub: func(state string) (int, string) {
		if reject.Load() {
			return http.StatusUnauthorized, "unauthorized"
		}

		return jevOK(state)
	}})

	for i := range 2 {
		result := f.reconcile(t)
		if result.RequeueAfter != jevRetryRequeue {
			t.Fatalf("attempt %d: RequeueAfter = %v; want %v", i, result.RequeueAfter, jevRetryRequeue)
		}

		wantJevBlocked(t, f.version(t), 0)
	}

	wantJevCalls(t, f, 2)

	reject.Store(false)
	f.reconcile(t)

	stored := f.version(t)
	if !stored.Status.Synced {
		t.Fatalf("Synced = false after Jev accepted the token; want true")
	}

	wantJevCalls(t, f, 3)
}

// TestReconcileJevSplitIOErrorRequeues covers a read error on the extracted tree. The Version blocks with a Service
// error, is re-run after 15 seconds, and makes no Jev call. Once the read succeeds, the retry publishes.
func TestReconcileJevSplitIOErrorRequeues(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{files: map[string]string{
		"SKILL.md":  jevEntry,
		"helper.md": "helper",
	}})

	jevChunkStat = func(name string) (os.FileInfo, error) {
		if filepath.Base(name) == "helper.md" {
			return nil, os.ErrPermission
		}

		return os.Stat(name)
	}
	defer func() { jevChunkStat = os.Stat }()

	result := f.reconcile(t)
	if result.RequeueAfter != jevRetryRequeue {
		t.Fatalf("RequeueAfter = %v; want %v", result.RequeueAfter, jevRetryRequeue)
	}

	stored := f.version(t)
	wantJevBlocked(t, stored, 1)
	if !strings.HasPrefix(stored.Status.SyncStatus, "JevUnavailable:") || stored.Status.JevAssessment.Error == "" {
		t.Fatalf("SyncStatus = %q, Error = %q; want a JevUnavailable Service failure", stored.Status.SyncStatus, stored.Status.JevAssessment.Error)
	}

	wantJevCalls(t, f, 0)

	jevChunkStat = os.Stat
	result = f.reconcile(t)
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter after the retry = %v; want 0", result.RequeueAfter)
	}

	if stored = f.version(t); !stored.Status.Synced {
		t.Fatalf("Synced = false after the retry; want true")
	}

	wantJevCalls(t, f, 1)
}

// TestReconcileJevMissingTokenSecretBlocks covers a missing token Secret without thresholds. The Version blocks, no
// HTTP request is sent, and the Service failure is re-run every 15 seconds.
func TestReconcileJevMissingTokenSecretBlocks(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{noSecret: true})

	result := f.reconcile(t)

	if result.RequeueAfter != jevRetryRequeue {
		t.Fatalf("RequeueAfter = %v; want %v", result.RequeueAfter, jevRetryRequeue)
	}

	wantJevBlocked(t, f.version(t), 1)
	wantJevCalls(t, f, 0)
}

// TestReconcileJevCeilingIsContentOutcome covers D2 and D11: content over the 16-part ceiling is a Content outcome.
// The Version blocks with no error and no requeue, and no Jev call is made.
func TestReconcileJevCeilingIsContentOutcome(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{files: map[string]string{
		"SKILL.md": jevEntry,
		"big.txt":  strings.Repeat("a", (jevMaxChunks+1)*jevChunkBodyBudget),
	}})

	result, err := f.reconcileResult(t)
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("result = %+v, err = %v; want no requeue and no error", result, err)
	}

	wantJevBlocked(t, f.version(t), 0)
	wantJevCalls(t, f, 0)
}

// TestReconcileJevMalformedResponseBlocks covers a 200 response that does not decode. The Version blocks, and a
// decode failure is not retried inside the attempt.
func TestReconcileJevMalformedResponseBlocks(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevStatus(http.StatusOK, "{not json")})

	f.reconcile(t)

	wantJevBlocked(t, f.version(t), 1)
	wantJevCalls(t, f, 1)
}

// TestReconcileJevDisabledBlocksWithoutCalls covers a source with a Jev secret on a controller with Jev disabled.
// The Version blocks with jevDisabledError and no HTTP request, with or without thresholds.
func TestReconcileJevDisabledBlocksWithoutCalls(t *testing.T) {
	tests := []struct {
		name   string
		policy *opendepotv1alpha1.JevPolicy
	}{
		{name: "no thresholds"},
		{name: "with thresholds", policy: &opendepotv1alpha1.JevPolicy{MinSafeProbability: f64(0.5)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newJevReconcileFixture(t, jevReconcileCase{policy: tt.policy, disabled: true})

			result, err := f.reconcileResult(t)
			if err == nil {
				t.Fatalf("Reconcile returned no error; want the jevDisabledError to hold the Version")
			}

			if result.RequeueAfter != 0 {
				t.Fatalf("RequeueAfter = %v; want 0, a disabled Jev is not retried", result.RequeueAfter)
			}

			stored := f.version(t)
			wantJevBlocked(t, stored, 0)
			if stored.Status.JevAssessment.Error != jevDisabledError {
				t.Fatalf("Error = %q; want jevDisabledError", stored.Status.JevAssessment.Error)
			}

			if !strings.HasPrefix(stored.Status.SyncStatus, "JevUnavailable:") {
				t.Fatalf("SyncStatus = %q; want JevUnavailable prefix", stored.Status.SyncStatus)
			}

			wantJevCalls(t, f, 0)
		})
	}
}

// TestReconcileJevDisabledThenEnabledStartsAtAttemptZero covers a disabled reconcile that is repeated without
// changing retryCount, then a failure once Jev is enabled. The failure sets retryCount to 1, not 2.
func TestReconcileJevDisabledThenEnabledStartsAtAttemptZero(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevStatus(http.StatusServiceUnavailable, "unavailable"), disabled: true})

	for range 2 {
		result, err := f.reconcileResult(t)
		if err == nil || result.RequeueAfter != 0 {
			t.Fatalf("disabled reconcile: result = %+v, err = %v; want the held error and no requeue", result, err)
		}
	}

	wantJevBlocked(t, f.version(t), 0)
	wantJevCalls(t, f, 0)

	f.reconciler.JevEnabled = true
	f.reconcile(t)

	wantJevBlocked(t, f.version(t), 1)
	wantJevCalls(t, f, 4)
}

// TestReconcileJevRetryLimitStopsAtFourAttempts covers attempts 0 to 3 failing with 503. After the fourth failure
// retryCount is 4, the Version stops requeueing, and a further reconcile makes no HTTP call.
func TestReconcileJevRetryLimitStopsAtFourAttempts(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevStatus(http.StatusServiceUnavailable, "unavailable")})

	for attempt := int32(1); attempt <= 3; attempt++ {
		result := f.reconcile(t)
		if result.RequeueAfter != jevRetryRequeue {
			t.Fatalf("attempt %d: RequeueAfter = %v; want %v", attempt, result.RequeueAfter, jevRetryRequeue)
		}

		wantJevBlocked(t, f.version(t), attempt)
	}

	result := f.reconcile(t)
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter after the fourth failure = %v; want 0", result.RequeueAfter)
	}

	wantJevBlocked(t, f.version(t), 4)
	wantJevCalls(t, f, 16)

	result = f.reconcile(t)
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter with retries exhausted = %v; want 0", result.RequeueAfter)
	}

	wantJevBlocked(t, f.version(t), 4)
	wantJevCalls(t, f, 16)
}

// TestReconcileJevForceSyncAfterExhaustionRunsOnce covers ForceSync at retryCount 4. The run is attempt 0, so a
// success publishes and retryCount is reset to 0.
func TestReconcileJevForceSyncAfterExhaustionRunsOnce(t *testing.T) {
	cached := &opendepotv1alpha1.JevAssessment{
		EvaluatedAt: "2026-01-01T00:00:00Z",
		Blocked:     true,
		Error:       "JevUnavailable: part 1: status 503",
		RetryCount:  4,
	}
	f := newJevReconcileFixture(t, jevReconcileCase{cached: cached, forceSync: true})

	f.reconcile(t)

	stored := f.version(t)
	if !stored.Status.Synced {
		t.Fatalf("Synced = false after ForceSync; want true")
	}

	if stored.Status.JevAssessment.RetryCount != 0 {
		t.Fatalf("RetryCount = %d; want 0", stored.Status.JevAssessment.RetryCount)
	}

	if stored.Spec.ForceSync {
		t.Fatalf("ForceSync = true after the run; want it cleared")
	}

	wantJevCalls(t, f, 1)
}

// TestReconcileJevForceSyncRerunsScoredAssessment covers ForceSync on a Scored assessment with a non-zero retryCount.
// The assessment is re-run once and retryCount is reset to 0.
func TestReconcileJevForceSyncRerunsScoredAssessment(t *testing.T) {
	cached := &opendepotv1alpha1.JevAssessment{
		EvaluatedAt: "2026-01-01T00:00:00Z",
		Model:       "jev-1.13.0",
		RiskScore:   f64(0.75),
		RetryCount:  2,
	}
	f := newJevReconcileFixture(t, jevReconcileCase{cached: cached, forceSync: true})

	f.reconcile(t)

	stored := f.version(t)
	if stored.Status.JevAssessment.RetryCount != 0 {
		t.Fatalf("RetryCount = %d; want 0", stored.Status.JevAssessment.RetryCount)
	}

	wantJevCalls(t, f, 1)
}

// TestReconcileJevRetrySucceedsPublishes covers a 503 that exhausts the part's HTTP attempts, followed by an automatic
// re-run that succeeds and publishes the Version.
func TestReconcileJevRetrySucceedsPublishes(t *testing.T) {
	var failures atomic.Int32
	f := newJevReconcileFixture(t, jevReconcileCase{stub: func(state string) (int, string) {
		if failures.Add(1) <= 4 {
			return http.StatusServiceUnavailable, "unavailable"
		}

		return jevOK(state)
	}})

	f.reconcile(t)
	wantJevBlocked(t, f.version(t), 1)

	f.reconcile(t)

	stored := f.version(t)
	if !stored.Status.Synced {
		t.Fatalf("Synced = false after the retry; want true")
	}

	wantJevCalls(t, f, 5)
}

// TestReconcileJevBinaryFileBlocksBeforeCall covers a bundled PNG with NUL bytes. The file is listed as Binary and no
// HTTP request is sent.
func TestReconcileJevBinaryFileBlocksBeforeCall(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{files: map[string]string{
		"SKILL.md": jevEntry,
		"logo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR",
	}})

	f.reconcile(t)

	stored := f.version(t)
	if stored.Status.JevAssessment == nil || !stored.Status.JevAssessment.Blocked {
		t.Fatalf("JevAssessment = %+v; want blocked", stored.Status.JevAssessment)
	}

	skipped := stored.Status.JevAssessment.SkippedFiles
	if len(skipped) != 1 || skipped[0].Path != "logo.png" || skipped[0].Reason != "Binary" {
		t.Fatalf("SkippedFiles = %+v; want logo.png with reason Binary", skipped)
	}

	wantJevCalls(t, f, 0)
}

// TestReconcileJevCeilingBlocksBeforeCall covers a Skill body of 17 parts. The Version blocks over the ceiling, and no
// HTTP request is sent.
func TestReconcileJevCeilingBlocksBeforeCall(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{files: map[string]string{
		"SKILL.md": jevEntry,
		"big.txt":  strings.Repeat("a", 17*jevChunkBodyBudget),
	}})

	f.reconcile(t)

	stored := f.version(t)
	if stored.Status.JevAssessment == nil || !stored.Status.JevAssessment.Blocked {
		t.Fatalf("JevAssessment = %+v; want blocked", stored.Status.JevAssessment)
	}

	if stored.Status.JevAssessment.ChunkCount < 17 {
		t.Fatalf("ChunkCount = %d; want at least 17", stored.Status.JevAssessment.ChunkCount)
	}

	wantJevCalls(t, f, 0)
}

// TestReconcileJevLegacyScoredAssessmentPublishesWithoutCall covers a legacy cached assessment with no thresholds on a
// reconcile that misses the fast path. The Version publishes with no HTTP request.
func TestReconcileJevLegacyScoredAssessmentPublishesWithoutCall(t *testing.T) {
	cached := &opendepotv1alpha1.JevAssessment{
		EvaluatedAt: "2026-01-01T00:00:00Z",
		Model:       "jev-1.13.0",
		RiskScore:   f64(0.75),
	}
	f := newJevReconcileFixture(t, jevReconcileCase{cached: cached})

	f.reconcile(t)

	if !f.version(t).Status.Synced {
		t.Fatalf("Synced = false; want true")
	}

	wantJevCalls(t, f, 0)
}

// TestReconcileJevLegacyBlockedAssessmentStaysBlocked covers a legacy blocked assessment. The Version stays blocked and
// no HTTP request is sent.
func TestReconcileJevLegacyBlockedAssessmentStaysBlocked(t *testing.T) {
	cached := &opendepotv1alpha1.JevAssessment{
		EvaluatedAt:  "2026-01-01T00:00:00Z",
		Model:        "jev-1.13.0",
		RiskScore:    f64(0.75),
		Blocked:      true,
		BlockReasons: []string{"risk level Critical"},
	}
	f := newJevReconcileFixture(t, jevReconcileCase{cached: cached})

	f.reconcile(t)

	stored := f.version(t)
	if stored.Status.Synced || stored.Status.JevAssessment == nil || !stored.Status.JevAssessment.Blocked {
		t.Fatalf("Synced = %v, JevAssessment = %+v; want blocked and unsynced", stored.Status.Synced, stored.Status.JevAssessment)
	}

	wantJevCalls(t, f, 0)
}

// TestReconcileJevSyncedVersionMakesNoCall covers a synced Version reconciled again, as after a restart. The second
// reconcile makes no HTTP request.
func TestReconcileJevSyncedVersionMakesNoCall(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{})

	f.reconcile(t)
	wantJevCalls(t, f, 1)

	f.reconcile(t)

	if !f.version(t).Status.Synced {
		t.Fatalf("Synced = false; want true")
	}

	wantJevCalls(t, f, 1)
}

// TestReconcileJevGateBlockIsNotRetried covers a policy block. The Version blocks with no automatic re-run, and the
// blocked assessment is not sent again.
func TestReconcileJevGateBlockIsNotRetried(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{policy: &opendepotv1alpha1.JevPolicy{MinSafeProbability: f64(0.999)}})

	result := f.reconcile(t)
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter = %v; want 0", result.RequeueAfter)
	}

	stored := f.version(t)
	if stored.Status.JevAssessment == nil || !stored.Status.JevAssessment.Blocked {
		t.Fatalf("JevAssessment = %+v; want blocked", stored.Status.JevAssessment)
	}

	if stored.Status.JevAssessment.RetryCount != 0 {
		t.Fatalf("RetryCount = %d; want 0", stored.Status.JevAssessment.RetryCount)
	}

	f.reconcile(t)
	wantJevCalls(t, f, 1)
}

// TestReconcileJevRefRequiredDoesNotRequeue covers a Jev-enabled Version that has a cached assessment but no
// jevSecretRef. The reconcile returns the ref-required error without a 15 second requeue, so it is not retried
// on a fixed timer.
func TestReconcileJevRefRequiredDoesNotRequeue(t *testing.T) {
	cached := &opendepotv1alpha1.JevAssessment{
		EvaluatedAt: "2026-01-01T00:00:00Z",
		ChunkCount:  1,
		Chunks:      []opendepotv1alpha1.JevChunkResult{{Index: 1, Label: "1", Status: "Scored"}},
	}
	f := newJevReconcileFixture(t, jevReconcileCase{cached: cached, noRef: true})

	result, err := f.reconcileResult(t)
	if err == nil || !strings.Contains(err.Error(), "jevSecretRef is required") {
		t.Fatalf("Reconcile error = %v; want the ref-required error", err)
	}

	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter = %v; want 0, a ref-required error is not retried on a timer", result.RequeueAfter)
	}

	wantJevCalls(t, f, 0)
}

// jevCriticalResponse rates the prompt-injection part Critical, so the Skill blocks with no policy. Destructive
// actions and the risk score are set so a policy can also breach its thresholds.
const jevCriticalResponse = `{
  "model": "jev-1.13.0",
  "answers": {
    "safe": {"type": "noul", "noul": 0.02},
    "prompt_injection": {"type": "noul", "noul": 0.9},
    "data_exfiltration": {"type": "noul", "noul": 0.01},
    "destructive_actions": {"type": "noul", "noul": 0.03},
    "hidden_instructions": {"type": "noul", "noul": 0.04},
    "scope_mismatch": {"type": "noul", "noul": 0.05},
    "remote_execution": {"type": "noul", "noul": 0.06},
    "risk": {
      "type": "score",
      "score": 4,
      "legend": {"0": "Minimal", "1": "Low", "2": "Moderate", "3": "High", "4": "Critical"},
      "probabilities": {"0": 0.01, "1": 0.01, "2": 0.02, "3": 0.06, "4": 0.9},
      "confidence": 0.95
    }
  },
  "usage": {"input_tokens": 1234, "output_tokens": 56}
}`

func jevCritical(string) (int, string) {
	return http.StatusOK, jevCriticalResponse
}

// jevExemptPolicy returns a ScanPolicy in the test namespace that exempts one digest. It matches every Version.
func jevExemptPolicy(name string, priority int, digest string, expires *metav1.Time) *opendepotv1alpha1.ScanPolicy {
	return &opendepotv1alpha1.ScanPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: opendepotv1alpha1.ScanPolicySpec{
			Priority: priority,
			JevExemptions: []opendepotv1alpha1.JevExemption{{
				ContentDigest: digest,
				Reason:        "reviewed by security",
				Expires:       expires,
			}},
		},
	}
}

func (f *jevReconcileFixture) addPolicy(t *testing.T, policy *opendepotv1alpha1.ScanPolicy) {
	t.Helper()

	if err := f.reconciler.Create(context.Background(), policy); err != nil {
		t.Fatalf("Create ScanPolicy %s: %v", policy.Name, err)
	}
}

// learnJevDigest runs the first reconcile, which blocks the Critical Skill, and returns the digest its assessment
// carries. The digest is the archive digest, so the test does not compute it.
func (f *jevReconcileFixture) learnJevDigest(t *testing.T) string {
	t.Helper()

	f.reconcile(t)

	digest := f.version(t).Status.JevAssessment.ContentDigest
	if digest == "" {
		t.Fatalf("ContentDigest is empty after the first reconcile")
	}

	return digest
}

// TestReconcileJevCriticalWithoutPolicyStaysBlocked covers D3: a Critical part with no policy stays blocked and sets
// needsReview.
func TestReconcileJevCriticalWithoutPolicyStaysBlocked(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevCritical})

	f.reconcile(t)

	stored := f.version(t)
	wantJevBlocked(t, stored, 0)
	if !stored.Status.JevAssessment.NeedsReview {
		t.Fatalf("NeedsReview = false; want true for a Critical block")
	}

	if stored.Status.JevAssessment.Exempted {
		t.Fatalf("Exempted = true with no policy; want false")
	}
}

// TestReconcileJevCriticalExemptionUnblocks covers a matching exemption on a Critical-only block: the Version is synced
// and the exemption fields name the policy.
func TestReconcileJevCriticalExemptionUnblocks(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevCritical})
	digest := f.learnJevDigest(t)

	f.addPolicy(t, jevExemptPolicy("release-exemptions", 0, digest, nil))
	f.reconcile(t)

	stored := f.version(t)
	if !stored.Status.Synced {
		t.Fatalf("Synced = false; want true after a matching exemption: %q", stored.Status.SyncStatus)
	}

	assessment := stored.Status.JevAssessment
	if assessment.Blocked || !assessment.Exempted {
		t.Fatalf("assessment = %+v; want exempted and unblocked", assessment)
	}

	if assessment.ExemptedBy != "release-exemptions" || assessment.ExemptionReason != "reviewed by security" || assessment.ExemptionExpires != nil {
		t.Fatalf("exemption fields = by %q reason %q expires %v; want the policy, its reason and no expiry", assessment.ExemptedBy, assessment.ExemptionReason, assessment.ExemptionExpires)
	}

	if !strings.Contains(stored.Status.SyncStatus, "release-exemptions") {
		t.Fatalf("SyncStatus = %q; want it to name the policy", stored.Status.SyncStatus)
	}
}

// TestReconcileJevCriticalThresholdsExemptionKeepsReasons covers D2: a matching exemption on Critical plus threshold
// reasons syncs the Version, keeps every block reason, and sets blocked to false.
func TestReconcileJevCriticalThresholdsExemptionKeepsReasons(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{
		stub:   jevCritical,
		policy: &opendepotv1alpha1.JevPolicy{MaxDestructiveProbability: f64(0.01), MaxRiskScore: f64(3)},
	})
	digest := f.learnJevDigest(t)

	before := f.version(t).Status.JevAssessment
	if len(before.BlockReasons) < 3 {
		t.Fatalf("BlockReasons = %v; want Critical plus two threshold reasons", before.BlockReasons)
	}

	f.addPolicy(t, jevExemptPolicy("release-exemptions", 0, digest, nil))
	f.reconcile(t)

	stored := f.version(t)
	if !stored.Status.Synced || stored.Status.JevAssessment.Blocked {
		t.Fatalf("Synced = %v, Blocked = %v; want synced and unblocked", stored.Status.Synced, stored.Status.JevAssessment.Blocked)
	}

	if len(stored.Status.JevAssessment.BlockReasons) != len(before.BlockReasons) {
		t.Fatalf("BlockReasons = %v; want all %v kept", stored.Status.JevAssessment.BlockReasons, before.BlockReasons)
	}
}

// TestReconcileJevTokenLimitExemptionUnblocks covers Q1: a part that hits the token limit is unblocked by a matching
// exemption on a Critical Skill.
func TestReconcileJevTokenLimitExemptionUnblocks(t *testing.T) {
	body := []byte(strings.Repeat("a", 3*jevChunkBodyBudget-4096))
	copy(body[3*jevChunkBodyBudget-8192:], "MARK-3")

	reject := jevRejectMark("MARK-3", 1<<30)
	f := newJevReconcileFixture(t, jevReconcileCase{
		files: map[string]string{"SKILL.md": jevEntry, "big.txt": string(body)},
		stub: func(state string) (int, string) {
			if strings.Contains(state, "MARK-3") {
				return reject(state)
			}

			return jevCritical(state)
		},
	})
	digest := f.learnJevDigest(t)

	if blocked := f.version(t).Status.JevAssessment; !strings.Contains(strings.Join(blocked.BlockReasons, " "), "cannot be split further") {
		t.Fatalf("BlockReasons = %v; want the token-limit reason", blocked.BlockReasons)
	}

	f.addPolicy(t, jevExemptPolicy("release-exemptions", 0, digest, nil))
	f.reconcile(t)

	stored := f.version(t)
	if !stored.Status.Synced || stored.Status.JevAssessment.Blocked {
		t.Fatalf("Synced = %v, Blocked = %v; want synced and unblocked", stored.Status.Synced, stored.Status.JevAssessment.Blocked)
	}
}

// TestReconcileJevExpiredExemptionStaysBlocked covers an expired entry: the Version stays blocked.
func TestReconcileJevExpiredExemptionStaysBlocked(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevCritical})
	digest := f.learnJevDigest(t)

	past := metav1.NewTime(time.Now().Add(-time.Hour))
	f.addPolicy(t, jevExemptPolicy("release-exemptions", 0, digest, &past))
	f.reconcile(t)

	wantJevBlocked(t, f.version(t), 0)
}

// TestReconcileJevPriorityShadowingStaysBlocked covers priority: the winning policy has no exemption for the digest,
// so a lower-priority policy that exempts it does not apply.
func TestReconcileJevPriorityShadowingStaysBlocked(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevCritical})
	digest := f.learnJevDigest(t)

	f.addPolicy(t, &opendepotv1alpha1.ScanPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "strict", Namespace: "default"},
		Spec:       opendepotv1alpha1.ScanPolicySpec{Priority: 10},
	})
	f.addPolicy(t, jevExemptPolicy("release-exemptions", 1, digest, nil))
	f.reconcile(t)

	wantJevBlocked(t, f.version(t), 0)
}

// TestReconcileJevDigestMismatchStaysBlocked covers a digest mismatch: an exemption for a different archive does not
// apply.
func TestReconcileJevDigestMismatchStaysBlocked(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevCritical})
	f.learnJevDigest(t)

	f.addPolicy(t, jevExemptPolicy("release-exemptions", 0, jevExemptTestDigest, nil))
	f.reconcile(t)

	wantJevBlocked(t, f.version(t), 0)
}

// jevPublishedVersion returns a published, exempted Skill Version. Its checksum encodes checksumDigest, the archive
// digest the stored path recomputes.
func jevPublishedVersion(t *testing.T, checksumDigest, exemptedBy string) *opendepotv1alpha1.Version {
	t.Helper()

	raw, err := hex.DecodeString(checksumDigest)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}

	checksum := base64.StdEncoding.EncodeToString(raw)
	version := newJevTestVersion(nil, &opendepotv1alpha1.JevAssessment{
		ContentDigest:   jevExemptTestDigest,
		Exempted:        true,
		ExemptedBy:      exemptedBy,
		ExemptionReason: "reviewed by security",
		BlockReasons:    []string{"Critical risk level"},
		NeedsReview:     true,
	})
	version.Name = "agent-1-0-0"
	version.Spec.Version = "1.0.0"
	version.Status.Synced = true
	version.Status.SyncStatus = "Synced"
	version.Status.Checksum = &checksum

	return version
}

// newJevStoredFixture returns a reconciler whose client holds a published Version and the given ScanPolicies.
func newJevStoredFixture(t *testing.T, version *opendepotv1alpha1.Version, policies ...client.Object) *jevReconcileFixture {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme core: %v", err)
	}

	if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	objects := append([]client.Object{version}, policies...)
	recorder := events.NewFakeRecorder(20)
	reconciler := &VersionReconciler{
		Client:     fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&opendepotv1alpha1.Version{}).Build(),
		Scheme:     scheme,
		Log:        logr.Discard(),
		ScanAgents: true,
		Recorder:   recorder,
	}

	return &jevReconcileFixture{reconciler: reconciler, key: types.NamespacedName{Name: version.Name, Namespace: version.Namespace}, recorder: recorder}
}

// reconcileStored runs the published fast path for the stored Version.
func (f *jevReconcileFixture) reconcileStored(t *testing.T) (ctrl.Result, jevExemptionChange) {
	t.Helper()

	result, change, err := f.reconciler.reconcileStoredScanPolicy(context.Background(), ctrl.Request{NamespacedName: f.key}, f.version(t))
	if err != nil {
		t.Fatalf("reconcileStoredScanPolicy: %v", err)
	}

	return result, change
}

// TestReconcileStoredExemptionRemovedReblocks covers D7: removing the entry re-blocks a published exempted Version.
func TestReconcileStoredExemptionRemovedReblocks(t *testing.T) {
	policy := jevExemptPolicy("release-exemptions", 0, jevExemptTestDigest, nil)
	f := newJevStoredFixture(t, jevPublishedVersion(t, jevExemptTestDigest, "release-exemptions"), policy)

	if _, change := f.reconcileStored(t); change.Applied || change.Revoked {
		t.Fatalf("change = %+v on an unchanged exemption; want none", change)
	}

	if err := f.reconciler.Delete(context.Background(), policy); err != nil {
		t.Fatalf("Delete ScanPolicy: %v", err)
	}

	_, change := f.reconcileStored(t)
	if !change.Revoked {
		t.Fatalf("change = %+v; want Revoked", change)
	}

	stored := f.version(t)
	if stored.Status.Synced || !strings.HasPrefix(stored.Status.SyncStatus, "JevBlocked:") {
		t.Fatalf("Synced = %v, SyncStatus = %q; want unsynced JevBlocked", stored.Status.Synced, stored.Status.SyncStatus)
	}

	if !stored.Status.JevAssessment.Blocked {
		t.Fatalf("JevAssessment = %+v; want blocked", stored.Status.JevAssessment)
	}
}

// TestReconcileStoredExemptionExpiresReblocks covers an expiring entry: the stored path requeues at the expiry, and the
// Version re-blocks on the next pass.
func TestReconcileStoredExemptionExpiresReblocks(t *testing.T) {
	expires := metav1.NewTime(time.Now().Add(time.Second))
	f := newJevStoredFixture(t,
		jevPublishedVersion(t, jevExemptTestDigest, "release-exemptions"),
		jevExemptPolicy("release-exemptions", 0, jevExemptTestDigest, &expires),
	)

	result, change := f.reconcileStored(t)
	if result.RequeueAfter <= 0 || change.Revoked {
		t.Fatalf("result = %+v, change = %+v; want a requeue before the expiry and no revoke", result, change)
	}

	if !f.version(t).Status.Synced {
		t.Fatalf("Synced = false before the expiry; want true")
	}

	time.Sleep(result.RequeueAfter + 100*time.Millisecond)

	_, change = f.reconcileStored(t)
	if !change.Revoked {
		t.Fatalf("change = %+v after expiry; want Revoked", change)
	}

	if f.version(t).Status.Synced {
		t.Fatalf("Synced = true after expiry; want false")
	}
}

// TestReconcileStoredExemptedBySwitchStaysSynced covers a policy switch: another policy with the same digest takes over,
// the Version stays synced, and exemptedBy is updated.
func TestReconcileStoredExemptedBySwitchStaysSynced(t *testing.T) {
	first := jevExemptPolicy("policy-a", 10, jevExemptTestDigest, nil)
	f := newJevStoredFixture(t,
		jevPublishedVersion(t, jevExemptTestDigest, "policy-a"),
		first,
		jevExemptPolicy("policy-b", 1, jevExemptTestDigest, nil),
	)

	if err := f.reconciler.Delete(context.Background(), first); err != nil {
		t.Fatalf("Delete ScanPolicy: %v", err)
	}

	_, change := f.reconcileStored(t)
	if change.Revoked || change.ExemptedBy != "policy-b" {
		t.Fatalf("change = %+v; want no revoke and ExemptedBy policy-b", change)
	}

	stored := f.version(t)
	if !stored.Status.Synced || stored.Status.JevAssessment.Blocked {
		t.Fatalf("Synced = %v, Blocked = %v; want synced and unblocked", stored.Status.Synced, stored.Status.JevAssessment.Blocked)
	}

	if stored.Status.JevAssessment.ExemptedBy != "policy-b" {
		t.Fatalf("ExemptedBy = %q; want policy-b", stored.Status.JevAssessment.ExemptedBy)
	}
}

// TestReconcileStoredDigestMismatchReblocks covers a digest mismatch: the archive on the stored checksum differs from
// the digest the exemption was granted for, so the Version re-blocks.
func TestReconcileStoredDigestMismatchReblocks(t *testing.T) {
	f := newJevStoredFixture(t,
		jevPublishedVersion(t, jevExemptTestOther, "release-exemptions"),
		jevExemptPolicy("release-exemptions", 0, jevExemptTestDigest, nil),
	)

	_, change := f.reconcileStored(t)
	if !change.Revoked {
		t.Fatalf("change = %+v; want Revoked on a digest mismatch", change)
	}

	if stored := f.version(t); stored.Status.Synced || !stored.Status.JevAssessment.Blocked {
		t.Fatalf("Synced = %v, assessment = %+v; want blocked", stored.Status.Synced, stored.Status.JevAssessment)
	}
}

// drainJevEvents returns the Events recorded since the last call, without blocking.
func (f *jevReconcileFixture) drainJevEvents() []string {
	var out []string
	for {
		select {
		case event := <-f.recorder.Events:
			out = append(out, event)
		default:
			return out
		}
	}
}

// TestReconcileJevExemptionEmitsEventsOnTransitions covers Step 6: one Applied Event when the exemption starts, none
// while it stays applied, and one Revoked Event when the entry is removed.
func TestReconcileJevExemptionEmitsEventsOnTransitions(t *testing.T) {
	f := newJevReconcileFixture(t, jevReconcileCase{stub: jevCritical})
	digest := f.learnJevDigest(t)
	f.drainJevEvents()

	policy := jevExemptPolicy("release-exemptions", 0, digest, nil)
	f.addPolicy(t, policy)
	f.reconcile(t)

	applied := f.drainJevEvents()
	if len(applied) != 1 || !strings.Contains(applied[0], "Normal JevExemptionApplied") || !strings.Contains(applied[0], "release-exemptions") {
		t.Fatalf("events = %q; want one Normal JevExemptionApplied naming the policy", applied)
	}

	if strings.Contains(applied[0], "reviewed by security") {
		t.Fatalf("event = %q; want no exemption reason text in the message", applied[0])
	}

	f.reconcile(t)
	if got := f.drainJevEvents(); len(got) != 0 {
		t.Fatalf("events = %q on an unchanged exemption; want none", got)
	}

	if err := f.reconciler.Delete(context.Background(), policy); err != nil {
		t.Fatalf("Delete ScanPolicy: %v", err)
	}

	f.reconcile(t)
	revoked := f.drainJevEvents()
	if len(revoked) != 1 || !strings.Contains(revoked[0], "Warning JevExemptionRevoked") || !strings.Contains(revoked[0], "release-exemptions") {
		t.Fatalf("events = %q; want one Warning JevExemptionRevoked naming the policy", revoked)
	}
}

// TestReconcileStoredExemptionEmitsRevokedEvent covers Step 6 on the published fast path.
func TestReconcileStoredExemptionEmitsRevokedEvent(t *testing.T) {
	policy := jevExemptPolicy("release-exemptions", 0, jevExemptTestDigest, nil)
	f := newJevStoredFixture(t, jevPublishedVersion(t, jevExemptTestDigest, "release-exemptions"), policy)

	f.reconcileStored(t)
	if got := f.drainJevEvents(); len(got) != 0 {
		t.Fatalf("events = %q on an unchanged exemption; want none", got)
	}

	if err := f.reconciler.Delete(context.Background(), policy); err != nil {
		t.Fatalf("Delete ScanPolicy: %v", err)
	}

	f.reconcileStored(t)
	revoked := f.drainJevEvents()
	if len(revoked) != 1 || !strings.Contains(revoked[0], "Warning JevExemptionRevoked") {
		t.Fatalf("events = %q; want one Warning JevExemptionRevoked", revoked)
	}
}

// TestRecordJevExemptionChangeNilRecorder covers Step 6: a nil Recorder disables Events without panicking.
func TestRecordJevExemptionChangeNilRecorder(t *testing.T) {
	reconciler := &VersionReconciler{}
	reconciler.recordJevExemptionChange(&opendepotv1alpha1.Version{}, jevExemptionChange{Applied: true}, &opendepotv1alpha1.JevAssessment{})
}
