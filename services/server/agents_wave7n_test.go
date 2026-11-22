package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func wave7nAgentParent(t *testing.T, refs map[string]map[string]any) string {
	t.Helper()

	return string(agentTestJSON(t, map[string]any{
		"metadata": map[string]any{"name": "reviewer", "namespace": "team"},
		"status":   map[string]any{"versionRefs": refs},
	}))
}

func wave7nAgentVersion(name string, mutate func(*opendepotv1alpha1.Version)) opendepotv1alpha1.Version {
	version := agentTestVersion(name)
	version.Status.Synced = false
	version.Status.Checksum = nil
	version.Status.ShaSums = ""
	version.Status.ShaSumsSignature = ""
	mutate(version)

	return *version
}

func wave7nAgentCandidates(t *testing.T, response *httptest.ResponseRecorder) map[string]AgentUnavailableCandidate {
	t.Helper()

	var body AgentVersionsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v: %s", err, response.Body.String())
	}

	candidates := make(map[string]AgentUnavailableCandidate, len(body.UnavailableCandidates))
	for _, candidate := range body.UnavailableCandidates {
		key := candidate.Version
		if key == "" {
			key = candidate.Commit
		}
		candidates[key] = candidate
	}

	return candidates
}

func TestWave7NGetAgentVersionsUnavailableCandidates(t *testing.T) {
	blockedBranch := wave7nAgentVersion("agent-reviewer-branch-blocked", func(v *opendepotv1alpha1.Version) {
		v.Spec.Version = ""
		v.Spec.SourceCommit = "c-blocked"
		v.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true, BlockReasons: []string{"blocked by policy"}}
	})

	branch := func(commit, phase, syncStatus string) opendepotv1alpha1.Version {
		return wave7nAgentVersion("agent-reviewer-branch-"+commit, func(v *opendepotv1alpha1.Version) {
			v.Spec.Version = ""
			v.Spec.SourceCommit = commit
			v.Status.BranchPhase = phase
			v.Status.SyncStatus = syncStatus
		})
	}

	items := []opendepotv1alpha1.Version{
		*agentTestVersion("agent-reviewer-1-2-0"),
		wave7nAgentVersion("wave7n-alias-agent", func(v *opendepotv1alpha1.Version) {
			v.Spec.Version = "1.4.0"
			v.Status.Synced = true
		}),
		wave7nAgentVersion("agent-reviewer-1-7-0", func(v *opendepotv1alpha1.Version) {
			v.Spec.Version = "1.7.0"
			v.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true, BlockReasons: []string{"too risky"}}
		}),
		wave7nAgentVersion("agent-reviewer-1-8-0", func(v *opendepotv1alpha1.Version) {
			v.Spec.Version = "1.8.0"
			v.Status.SyncStatus = "Scan policy violation: critical"
		}),
		wave7nAgentVersion("agent-reviewer-1-9-0", func(v *opendepotv1alpha1.Version) {
			v.Spec.Version = "1.9.0"
			v.Status.SyncStatus = "Failed to fetch archive"
		}),
		wave7nAgentVersion("agent-reviewer-1-10-0", func(v *opendepotv1alpha1.Version) {
			v.Spec.Version = "1.10.0"
			v.Status.SyncStatus = "Waiting for sync"
		}),
		blockedBranch,
		branch("c-superseded", branchPhaseRejected, "Superseded: newer commit"),
		branch("c-invalid", branchPhaseRejected, "Invalid sourceCommit: not a sha"),
		branch("c-disabled", branchPhaseRejected, "branch sourcing is disabled"),
		branch("c-other", branchPhaseRejected, "something else"),
		branch("c-failed", "", "Failed to clone repository"),
		branch("c-pending", branchPhasePending, ""),
		branch("c-held", branchPhaseHeld, ""),
		branch("c-new", "", ""),
		branch("c-synced", "", ""),
	}
	items[len(items)-1].Status.Synced = true
	foreign := wave7nAgentVersion("agent-other-1-0-0", func(v *opendepotv1alpha1.Version) {
		v.Spec.Version = "1.0.0"
		v.Spec.Type = "Agent"
		v.Spec.AgentSourceRef.Name = covGStringPtr("other")
	})
	items = append(items, foreign)

	versionList := opendepotv1alpha1.VersionList{Items: items}
	parent := wave7nAgentParent(t, map[string]map[string]any{
		"1.2.0": {"name": "agent-reviewer-1-2-0", "version": "1.2.0", "synced": true},
		"1.4.0": {"name": "wave7n-alias-agent", "version": "1.4.0", "synced": true},
		"1.5.0": {"name": "", "version": "1.5.0"},
		"1.6.0": {"name": "missing", "version": "1.6.0"},
	})

	wave7nModuleCluster(t, false, map[string]wave7nReply{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/reviewer": {body: parent},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions":        {body: string(agentTestJSON(t, versionList))},
	}, nil)

	response := httptest.NewRecorder()
	getAgentVersions(response, covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "kind": "agents", "name": "reviewer"}))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	candidates := wave7nAgentCandidates(t, response)
	expected := map[string]struct {
		status  AgentAvailabilityStatus
		reasons []string
	}{
		"1.7.0":        {AgentAvailabilityBlocked, []string{"too risky"}},
		"1.8.0":        {AgentAvailabilityRejected, []string{agentReasonScanPolicy}},
		"1.9.0":        {AgentAvailabilityAssessmentFailed, []string{agentReasonAssessmentFailed}},
		"c-blocked":    {AgentAvailabilityRejected, []string{"blocked by policy"}},
		"c-superseded": {AgentAvailabilityRejected, []string{agentReasonSuperseded}},
		"c-invalid":    {AgentAvailabilityRejected, []string{agentReasonInvalidCommit}},
		"c-disabled":   {AgentAvailabilityRejected, []string{agentReasonSourcingDisabled}},
		"c-other":      {AgentAvailabilityRejected, []string{agentReasonBranchRejected}},
		"c-failed":     {AgentAvailabilityAssessmentFailed, []string{agentReasonAssessmentFailed}},
		"c-pending":    {AgentAvailabilityPending, []string{agentReasonAwaitingSibling}},
		"c-held":       {AgentAvailabilityPending, []string{agentReasonAwaitingAssignment}},
		"c-new":        {AgentAvailabilityPending, []string{agentReasonNotYetAssessed}},
	}

	for key, want := range expected {
		candidate, ok := candidates[key]
		if !ok {
			t.Fatalf("candidate %q missing from %v", key, candidates)
		}

		if candidate.Status != want.status {
			t.Fatalf("candidate %q status = %q, want %q", key, candidate.Status, want.status)
		}

		if len(candidate.Reasons) != len(want.reasons) || candidate.Reasons[0] != want.reasons[0] {
			t.Fatalf("candidate %q reasons = %v, want %v", key, candidate.Reasons, want.reasons)
		}
	}

	for _, omitted := range []string{"1.10.0", "c-synced", "1.0.0"} {
		if _, ok := candidates[omitted]; ok {
			t.Fatalf("candidate %q should be omitted", omitted)
		}
	}
}

func TestWave7NGetAgentVersionsFailures(t *testing.T) {
	list := string(agentTestJSON(t, opendepotv1alpha1.VersionList{}))
	parent := wave7nAgentParent(t, map[string]map[string]any{})

	wave7nModuleCluster(t, false, map[string]wave7nReply{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/reviewer":  {body: parent},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/malformed": {body: "not-json"},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/forbidden": {status: http.StatusForbidden, body: wave7nModuleStatus("Forbidden", http.StatusForbidden)},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/broken":    {status: http.StatusInternalServerError, body: wave7nModuleStatus("InternalError", http.StatusInternalServerError)},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions":         {body: list},
	}, nil)

	versionsFor := func(name string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		getAgentVersions(response, covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "kind": "agents", "name": name}))

		return response
	}

	tests := []struct {
		name string
		want int
	}{
		{name: "absent", want: http.StatusNotFound},
		{name: "forbidden", want: http.StatusForbidden},
		{name: "broken", want: http.StatusInternalServerError},
		{name: "malformed", want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := versionsFor(tt.name)
			if response.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.want, response.Body.String())
			}
		})
	}

	t.Run("malformed version list is internal error", func(t *testing.T) {
		wave7nModuleCluster(t, false, map[string]wave7nReply{
			"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/reviewer": {body: parent},
			"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions":        {body: "not-json"},
		}, nil)

		response := versionsFor("reviewer")
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("failed version list is internal error", func(t *testing.T) {
		wave7nModuleCluster(t, false, map[string]wave7nReply{
			"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/reviewer": {body: parent},
			"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions":        {status: http.StatusInternalServerError, body: wave7nModuleStatus("InternalError", http.StatusInternalServerError)},
		}, nil)

		response := versionsFor("reviewer")
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NGetAgentVersionsGroupBindingDenied(t *testing.T) {
	parent := wave7nAgentParent(t, map[string]map[string]any{})
	wave7nModuleCluster(t, true, map[string]wave7nReply{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/reviewer": {body: parent},
	}, []opendepotv1alpha1.GroupBinding{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "platform"},
			Spec: opendepotv1alpha1.GroupBindingSpec{
				Expression:     `"platform" in groups`,
				AgentResources: []string{"other-agent"},
			},
		},
	})

	req := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "kind": "agents", "name": "reviewer"})
	req.Header.Set("Authorization", "Bearer "+agentTestBearerToken(t, []string{"platform"}))
	response := httptest.NewRecorder()
	getAgentVersions(response, req)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestWave7NGetAgentDownloadMetadataFailures(t *testing.T) {
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", "")

	unavailable := agentTestVersion("agent-reviewer-1-2-0")
	unavailable.Spec.FileName = nil

	badChecksum := agentTestVersion("agent-reviewer-1-3-0")
	badChecksum.Name = "agent-reviewer-1-3-0"
	notBase64 := "!!!not-base64!!!"
	badChecksum.Status.Checksum = &notBase64

	noSigningKey := agentTestVersion("agent-reviewer-1-4-0")

	wave7nModuleCluster(t, false, map[string]wave7nReply{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/agent-reviewer-1-2-0": {body: string(agentTestJSON(t, unavailable))},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/agent-reviewer-1-3-0": {body: string(agentTestJSON(t, badChecksum))},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/agent-reviewer-1-4-0": {body: string(agentTestJSON(t, noSigningKey))},
	}, nil)

	download := func(version string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		getAgentDownloadMetadata(response, covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "kind": "agents", "name": "reviewer", "version": version}))

		return response
	}

	t.Run("artifact not yet available", func(t *testing.T) {
		response := download("1.2.0")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
	})

	t.Run("undecodable checksum is internal error", func(t *testing.T) {
		response := download("1.3.0")
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusInternalServerError, response.Body.String())
		}
	})

	t.Run("missing signing key is reported", func(t *testing.T) {
		response := download("1.4.0")
		if response.Code == http.StatusOK {
			t.Fatalf("status = %d, want a signing key error: %s", response.Code, response.Body.String())
		}
	})

	t.Run("unknown version is not found", func(t *testing.T) {
		response := download("9.9.9")
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
		}
	})
}
