/*
Copyright 2026 Tony Owens.

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
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/jev"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultJevSecretKey is the Secret key that holds the Jev token when jevSecretRef omits a key.
const defaultJevSecretKey = "jevToken"

// jevDisabledError is recorded on the assessment when a source sets jevSecretRef but Jev is disabled on the controller.
const jevDisabledError = "jev is disabled on this controller"

// jevFileListHeader separates the entry file from the bundled file list in the Jev state.
const jevFileListHeader = "\n\nBundled files:\n"

// evaluateJevGate applies the configured JevPolicy thresholds to a Jev result. It is the only place
// where a Jev answer can block a Version. Each threshold is optional: an unset threshold does not
// block. MinConfidence never blocks and only sets needsReview.
func evaluateJevGate(policy *opendepotv1alpha1.JevPolicy, result jev.Result) (reasons []string, needsReview bool) {
	if policy == nil {
		return nil, false
	}

	if policy.MinSafeProbability != nil && result.SafeProbability < *policy.MinSafeProbability {
		reasons = append(reasons, fmt.Sprintf("safe probability %.3f is below minSafeProbability %.3f", result.SafeProbability, *policy.MinSafeProbability))
	}

	if policy.MaxInjectionProbability != nil && result.PromptInjectionProbability > *policy.MaxInjectionProbability {
		reasons = append(reasons, fmt.Sprintf("prompt injection probability %.3f exceeds maxInjectionProbability %.3f", result.PromptInjectionProbability, *policy.MaxInjectionProbability))
	}

	if policy.MaxExfiltrationProbability != nil && result.DataExfiltrationProbability > *policy.MaxExfiltrationProbability {
		reasons = append(reasons, fmt.Sprintf("data exfiltration probability %.3f exceeds maxExfiltrationProbability %.3f", result.DataExfiltrationProbability, *policy.MaxExfiltrationProbability))
	}

	if policy.MaxDestructiveProbability != nil && result.DestructiveActionsProbability > *policy.MaxDestructiveProbability {
		reasons = append(reasons, fmt.Sprintf("destructive actions probability %.3f exceeds maxDestructiveProbability %.3f", result.DestructiveActionsProbability, *policy.MaxDestructiveProbability))
	}

	if policy.MaxHiddenInstructionsProbability != nil && result.HiddenInstructionsProbability > *policy.MaxHiddenInstructionsProbability {
		reasons = append(reasons, fmt.Sprintf("hidden instructions probability %.3f exceeds maxHiddenInstructionsProbability %.3f", result.HiddenInstructionsProbability, *policy.MaxHiddenInstructionsProbability))
	}

	if policy.MaxScopeMismatchProbability != nil && result.ScopeMismatchProbability > *policy.MaxScopeMismatchProbability {
		reasons = append(reasons, fmt.Sprintf("scope mismatch probability %.3f exceeds maxScopeMismatchProbability %.3f", result.ScopeMismatchProbability, *policy.MaxScopeMismatchProbability))
	}

	if policy.MaxRemoteExecutionProbability != nil && result.RemoteExecutionProbability > *policy.MaxRemoteExecutionProbability {
		reasons = append(reasons, fmt.Sprintf("remote execution probability %.3f exceeds maxRemoteExecutionProbability %.3f", result.RemoteExecutionProbability, *policy.MaxRemoteExecutionProbability))
	}

	if policy.MaxRiskScore != nil && result.RiskScore > *policy.MaxRiskScore {
		reasons = append(reasons, fmt.Sprintf("risk score %.3f exceeds maxRiskScore %.3f", result.RiskScore, *policy.MaxRiskScore))
	}

	needsReview = policy.MinConfidence != nil && result.RiskConfidence < *policy.MinConfidence

	return reasons, needsReview
}

// hasJevThresholds reports whether any blocking JevPolicy threshold is configured. MinConfidence never blocks.
func hasJevThresholds(policy *opendepotv1alpha1.JevPolicy) bool {
	return policy != nil && (policy.MinSafeProbability != nil ||
		policy.MaxInjectionProbability != nil ||
		policy.MaxExfiltrationProbability != nil ||
		policy.MaxDestructiveProbability != nil ||
		policy.MaxHiddenInstructionsProbability != nil ||
		policy.MaxScopeMismatchProbability != nil ||
		policy.MaxRemoteExecutionProbability != nil ||
		policy.MaxRiskScore != nil)
}

// buildJevState returns the content sent to Jev: the entry file and the bundled file list, truncated to
// the Jev limit at a UTF-8 boundary. A flagged entry file is omitted so that a detected secret is never sent.
func buildJevState(content *agentContent) string {
	var b strings.Builder
	if !content.entryFlagged {
		b.Write(content.entryBytes)
	}

	b.WriteString(jevFileListHeader)
	for _, file := range content.files {
		b.WriteString("- " + file + "\n")
	}

	state := b.String()
	if len(state) <= jev.MaxStateBytes {
		return state
	}

	cut := jev.MaxStateBytes
	for cut > 0 && !utf8.RuneStart(state[cut]) {
		cut--
	}

	return state[:cut]
}

// readJevToken reads the Jev token from the Secret referenced by the source. The token is never logged
// and never included in an error.
func (r *VersionReconciler) readJevToken(ctx context.Context, namespace string, ref *corev1.SecretKeySelector) (string, error) {
	key := ref.Key
	if key == "" {
		key = defaultJevSecretKey
	}

	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, &secret); err != nil {
		return "", fmt.Errorf("read jev secret '%s': %w", ref.Name, err)
	}

	token := strings.TrimSpace(string(secret.Data[key]))
	if token == "" {
		return "", fmt.Errorf("jev secret '%s' has no value for key '%s'", ref.Name, key)
	}

	return token, nil
}

// jevFailure records a failed Jev assessment. The error is returned only when a threshold is configured,
// because a Version with thresholds must stay unsynced until Jev can evaluate them.
func jevFailure(err error, gated bool) (*opendepotv1alpha1.JevAssessment, error) {
	assessment := &opendepotv1alpha1.JevAssessment{
		EvaluatedAt: time.Now().UTC().Format(time.RFC3339),
		Error:       err.Error(),
	}

	if gated {
		return assessment, err
	}

	return assessment, nil
}

// runAgentJev runs the TypeSafe Jev assessment for a Skill or Agent Version. It runs when the source sets a
// JevSecretRef, and only after the deterministic gates have passed. When Jev is disabled on the controller,
// the assessment records that and no call is made. A cached assessment is reused unless ForceSync is set,
// including an errored one, so a failing Jev is not called again on every reconcile.
//
// It returns nil when Jev does not apply. An error is returned when the assessment failed and the policy has
// thresholds that cannot be evaluated; the Version must then stay unsynced.
func (r *VersionReconciler) runAgentJev(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	content *agentContent,
) (*opendepotv1alpha1.JevAssessment, error) {
	sourceRef := version.Spec.AgentSourceRef
	if sourceRef == nil || sourceRef.JevSecretRef == nil {
		return nil, nil
	}

	gated := hasJevThresholds(sourceRef.JevPolicy)
	if !r.JevEnabled {
		return jevFailure(errors.New(jevDisabledError), gated)
	}

	if cached := version.Status.JevAssessment; cached != nil && !version.Spec.ForceSync && cached.Error != jevDisabledError {
		if cached.Error != "" && gated {
			return cached, errors.New(cached.Error)
		}

		return cached, nil
	}

	token, err := r.readJevToken(ctx, version.Namespace, sourceRef.JevSecretRef)
	if err != nil {
		return jevFailure(err, gated)
	}

	kind := strings.ToLower(version.Spec.Type)
	var opts []jev.Option
	if r.JevEndpoint != "" {
		opts = append(opts, jev.WithEndpoint(r.JevEndpoint))
	}

	result, err := jev.NewClient(opts...).Assess(ctx, token, kind, buildJevState(content))
	if err != nil {
		return jevFailure(err, gated)
	}

	reasons, needsReview := evaluateJevGate(sourceRef.JevPolicy, *result)

	return &opendepotv1alpha1.JevAssessment{
		EvaluatedAt:                   time.Now().UTC().Format(time.RFC3339),
		Model:                         result.Model,
		SafeProbability:               ptr.To(result.SafeProbability),
		InjectionProbability:          ptr.To(result.PromptInjectionProbability),
		ExfiltrationProbability:       ptr.To(result.DataExfiltrationProbability),
		DestructiveProbability:        ptr.To(result.DestructiveActionsProbability),
		HiddenInstructionsProbability: ptr.To(result.HiddenInstructionsProbability),
		ScopeMismatchProbability:      ptr.To(result.ScopeMismatchProbability),
		RemoteExecutionProbability:    ptr.To(result.RemoteExecutionProbability),
		RiskScore:                     ptr.To(result.RiskScore),
		RiskLevel:                     result.RiskLevel,
		RiskConfidence:                ptr.To(result.RiskConfidence),
		NeedsReview:                   needsReview,
		Blocked:                       len(reasons) > 0,
		BlockReasons:                  reasons,
	}, nil
}
