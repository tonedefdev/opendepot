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
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/jev"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultJevSecretKey is the Secret key that holds the Jev token when jevSecretRef omits a key.
const defaultJevSecretKey = "jevToken"

// jevDisabledError is recorded on the assessment when a source sets jevSecretRef but Jev is disabled on the controller.
const jevDisabledError = "jev is disabled on this controller"

// jevRefRequiredError is returned when Jev is enabled on the controller but a branch source, or a Version already
// assessed by Jev, no longer sets jevSecretRef.
const jevRefRequiredError = "jevSecretRef is required for branch sources when Jev is enabled on the controller"

// jevMaxInFlight limits the Jev HTTP attempts in flight across the controller and the parts sent for one Version.
const jevMaxInFlight = 16

// jevRateLimitPerMinute and jevRateBurst limit the Jev HTTP attempts started by the controller.
const (
	jevRateLimitPerMinute = 1000
	jevRateBurst          = 16
)

// jevPartDeadline bounds the time spent sending the parts of one Version.
const jevPartDeadline = 5 * time.Minute

// jevRetryRequeue is how long a Version waits before its failed Jev assessment is re-run (D11).
const jevRetryRequeue = 15 * time.Second

// jevRetriesExhausted reports whether a failed Jev assessment has used its automatic retries (D11).
// A nil assessment or a 401/403 failure, which does not increment RetryCount, is never exhausted.
func jevRetriesExhausted(assessment *opendepotv1alpha1.JevAssessment, maxRetries int) bool {
	return assessment != nil && int(assessment.RetryCount) > maxRetries
}

// jevPartOutcome is the result of one part sent to Jev, or of one half of a part that was split after a token limit.
type jevPartOutcome struct {
	result  jevPartResult
	files   []string
	errText string
	auth    bool
}

// jevRun holds the state shared by the goroutines that send the parts of one Version.
type jevRun struct {
	client *jev.Client
	token  string
	kind   string
	in     *jevChunkInput
	total  int
	calls  atomic.Int64
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

// jevPublishedStatus returns the SyncStatus of a Skill or Agent Version that is published. When Jev recorded an
// error that no threshold gates, the error stays visible in SyncStatus and the message says why the Version was
// published.
func jevPublishedStatus(jevAssessment *opendepotv1alpha1.JevAssessment) string {
	if jevAssessment == nil || jevAssessment.Error == "" {
		return "Successfully synced version"
	}

	return fmt.Sprintf("Successfully synced version; Jev assessment did not complete: %s. Version was published because no Jev thresholds are set", jevAssessment.Error)
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

// jevServiceError marks a Service outcome: a failed part, an unreadable token Secret, or a read error on the extracted
// content. Only these are retried at jevRetryRequeue. A Content outcome returns no error, so it is never retried.
type jevServiceError struct {
	msg string
}

func (e *jevServiceError) Error() string {
	return e.msg
}

// jevIncomplete records a Service outcome that could not finish and blocks the Version. The returned error keeps the
// Version unsynced, so an incomplete assessment never publishes.
func jevIncomplete(retries int32, chunks int, msg string) (*opendepotv1alpha1.JevAssessment, error) {
	return &opendepotv1alpha1.JevAssessment{
		EvaluatedAt:  time.Now().UTC().Format(time.RFC3339),
		ChunkCount:   int32(chunks),
		RetryCount:   retries,
		Error:        msg,
		Blocked:      true,
		BlockReasons: []string{msg},
	}, &jevServiceError{msg: msg}
}

// jevRefRequired reports whether a Version without jevSecretRef must fail its Jev check. It applies while Jev is enabled
// on the controller and the Version is a branch source or was already assessed, so removing the ref cannot skip the check.
func (r *VersionReconciler) jevRefRequired(version *opendepotv1alpha1.Version) bool {
	return r.JevEnabled && (version.Spec.SourceCommit != "" || version.Status.JevAssessment != nil)
}

// acquireJevSlot takes one slot from the semaphore. A nil semaphore means no cap, which unit tests use.
func acquireJevSlot(ctx context.Context, sem chan struct{}) error {
	if sem == nil {
		return ctx.Err()
	}

	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseJevSlot returns a slot taken by acquireJevSlot.
func releaseJevSlot(sem chan struct{}) {
	if sem != nil {
		<-sem
	}
}

// waitJevRate takes one token from the limiter. A nil limiter means no cap, which unit tests use.
func waitJevRate(ctx context.Context, limiter *rate.Limiter) error {
	if limiter == nil {
		return nil
	}

	return limiter.Wait(ctx)
}

// jevAttemptGate returns the gate every HTTP attempt to Jev passes. It takes a semaphore slot and a limiter token,
// and the caller releases the slot before any backoff sleep.
func jevAttemptGate(sem chan struct{}, limiter *rate.Limiter) jev.AttemptGate {
	return func(ctx context.Context) (func(), error) {
		if err := acquireJevSlot(ctx, sem); err != nil {
			return nil, err
		}

		if err := waitJevRate(ctx, limiter); err != nil {
			releaseJevSlot(sem)
			return nil, err
		}

		return func() { releaseJevSlot(sem) }, nil
	}
}

// reserveJevHalving takes the two calls a halving needs from the budget. It reports false when fewer than two remain.
func (run *jevRun) reserveJevHalving() bool {
	for {
		left := run.calls.Load()
		if left < 2 {
			return false
		}

		if run.calls.CompareAndSwap(left, left-2) {
			return true
		}
	}
}

// assess sends one part to Jev. A 400 max_tokens_exceeded halves the part and sends both halves in turn. It always
// returns outcomes and never an error, so a failing part cannot cancel the others.
func (run *jevRun) assess(ctx context.Context, part jevChunkPart) []jevPartOutcome {
	failed := func(status, errText string, auth bool) []jevPartOutcome {
		return []jevPartOutcome{{
			result:  jevPartResult{label: part.label, status: status},
			files:   part.files,
			errText: errText,
			auth:    auth,
		}}
	}

	if err := ctx.Err(); err != nil {
		return failed(jevPartNotSent, "deadline exceeded before the part was sent", false)
	}

	state, err := run.in.build(part, run.total)
	if err != nil {
		return failed(jevPartFailed, err.Error(), false)
	}

	result, err := run.client.Assess(ctx, run.token, run.kind, state)
	if err == nil {
		return []jevPartOutcome{{
			result: jevPartResult{label: part.label, status: jevPartScored, result: *result},
			files:  part.files,
		}}
	}

	if !jev.IsTokenLimitExceeded(err) {
		var apiErr *jev.APIError
		auth := errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden)

		return failed(jevPartFailed, err.Error(), auth)
	}

	first, second, ok, err := run.in.halve(part, run.total)
	if err != nil {
		return failed(jevPartFailed, err.Error(), false)
	}

	if !ok {
		return failed(jevPartTokenLimitExceeded, "part exceeds the Jev token limit and cannot be split further", false)
	}

	if !run.reserveJevHalving() {
		return failed(jevPartTokenLimitExceeded, "no call budget left under the 16-call limit", false)
	}

	return append(run.assess(ctx, first), run.assess(ctx, second)...)
}

// jevNewAssessment returns an assessment stamped with the evaluation time and the retry and chunk counts.
func jevNewAssessment(retries int32, chunks int) *opendepotv1alpha1.JevAssessment {
	return &opendepotv1alpha1.JevAssessment{
		EvaluatedAt: time.Now().UTC().Format(time.RFC3339),
		ChunkCount:  int32(chunks),
		RetryCount:  retries,
	}
}

// jevSkippedFiles converts the skipped files of a layout to the API type.
func jevSkippedFiles(layout jevChunkLayout) []opendepotv1alpha1.JevSkippedFile {
	skipped := make([]opendepotv1alpha1.JevSkippedFile, 0, len(layout.skipped))
	for _, s := range layout.skipped {
		skipped = append(skipped, opendepotv1alpha1.JevSkippedFile{Path: jevChunkTruncate(s.path, jevChunkPathLimit), Reason: s.reason})
	}

	return skipped
}

// jevChunks converts sorted part outcomes to the API type. Each entry lists at most the list limit of files.
func jevChunks(outcomes []jevPartOutcome) []opendepotv1alpha1.JevChunkResult {
	chunks := make([]opendepotv1alpha1.JevChunkResult, 0, len(outcomes))
	for _, o := range outcomes {
		files := o.files
		if len(files) > jevChunkListLimit {
			files = files[:jevChunkListLimit]
		}

		listed := make([]string, 0, len(files))
		for _, f := range files {
			listed = append(listed, jevChunkTruncate(f, jevChunkPathLimit))
		}

		index, _ := jevSplitLabel(o.result.label)
		chunk := opendepotv1alpha1.JevChunkResult{
			Index:     int32(index),
			Label:     o.result.label,
			Files:     listed,
			FileCount: int32(len(o.files)),
			Status:    o.result.status,
			Error:     jevChunkTruncate(o.errText, jevChunkErrorLimit),
		}

		if o.result.status == jevPartScored {
			chunk.RiskLevel = o.result.result.RiskLevel
			chunk.RiskScore = ptr.To(o.result.result.RiskScore)
			chunk.RiskConfidence = ptr.To(o.result.result.RiskConfidence)
			chunk.SafeProbability = ptr.To(o.result.result.SafeProbability)
		}

		chunks = append(chunks, chunk)
	}

	return chunks
}

// jevNotSentOutcomes returns one NotSent outcome for each part, with the given reason.
func jevNotSentOutcomes(parts []jevChunkPart, errText string) []jevPartOutcome {
	outcomes := make([]jevPartOutcome, 0, len(parts))
	for _, part := range parts {
		outcomes = append(outcomes, jevPartOutcome{
			result:  jevPartResult{label: part.label, status: jevPartNotSent},
			files:   part.files,
			errText: errText,
		})
	}

	return outcomes
}

// runAgentJev runs the TypeSafe Jev assessment for a Skill or Agent Version. It runs when the source sets a
// JevSecretRef, and only after the deterministic gates have passed. The content is split into parts of at most
// the Jev state limit, each part is sent concurrently within the controller's Jev limits, and the outcomes are
// aggregated and gated. A cached assessment is reused unless ForceSync is set, or it failed and its retry budget
// is not spent, so a failing Jev is retried a bounded number of times.
//
// It returns nil when Jev does not apply. An error is returned when the assessment is incomplete and the Version
// must stay unsynced. A policy block is recorded on the assessment and returns no error.
func (r *VersionReconciler) runAgentJev(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	content *agentContent,
) (*opendepotv1alpha1.JevAssessment, error) {
	sourceRef := version.Spec.AgentSourceRef
	if sourceRef == nil || sourceRef.JevSecretRef == nil {
		if r.jevRefRequired(version) {
			return nil, errors.New(jevRefRequiredError)
		}

		return nil, nil
	}

	gated := hasJevThresholds(sourceRef.JevPolicy)
	cached := version.Status.JevAssessment
	reuse := cached != nil && !version.Spec.ForceSync && cached.Error != jevDisabledError

	var retries int32
	if reuse {
		retries = cached.RetryCount
	}

	if !r.JevEnabled {
		var carried int32
		if cached != nil {
			carried = cached.RetryCount
		}

		assessment, err := jevFailure(errors.New(jevDisabledError), true)
		assessment.Blocked = true
		assessment.BlockReasons = []string{jevDisabledError}
		assessment.RetryCount = carried

		return assessment, err
	}

	if reuse && cached.ChunkCount == 0 && len(cached.Chunks) == 0 && !cached.Blocked {
		if cached.Error != "" && gated {
			return cached, errors.New(cached.Error)
		}

		return cached, nil
	}

	if reuse && (cached.Error == "" || int(retries) > r.JevMaxRetries) {
		if cached.Error != "" {
			return cached, errors.New(cached.Error)
		}

		return cached, nil
	}

	kind := strings.ToLower(version.Spec.Type)
	in := &jevChunkInput{
		dir:          content.dir,
		entry:        content.entryRel,
		entryFlagged: content.entryFlagged,
		flagged:      content.flaggedFiles,
		files:        content.files,
		kind:         kind,
		name:         content.definition.Name,
		description:  content.definition.Description,
		tools:        content.definition.Tools,
	}

	layout, err := jevChunkPlan(in)
	if err != nil {
		if errors.Is(err, errJevChunkIO) {
			r.Log.Error(err, "Jev split could not read the extracted content", "version", version.Name)

			return jevIncomplete(retries+1, 0, "jev assessment incomplete: the extracted content could not be read")
		}

		assessment := jevNewAssessment(retries, 0)
		assessment.Blocked = true
		assessment.BlockReasons = []string{fmt.Sprintf("jev assessment incomplete: split the content: %s", err)}

		return assessment, nil
	}

	if layout.over {
		reasons := []string{fmt.Sprintf("jev assessment incomplete: the content needs at least %d parts of up to %d bytes, above the limit of %d per Version; no Jev calls were made", layout.count, jev.MaxStateBytes, jevMaxChunks)}
		if layout.skippedCount > 0 {
			reasons = append(reasons, fmt.Sprintf("jev assessment incomplete: %d bundled file(s) were withheld or skipped, so Jev cannot see the full content", layout.skippedCount))
		}

		assessment := jevNewAssessment(retries, layout.count)
		assessment.Blocked = true
		assessment.BlockReasons = reasons

		return assessment, nil
	}

	if layout.skippedCount > 0 {
		assessment := jevNewAssessment(retries, len(layout.parts))
		assessment.Chunks = jevChunks(jevNotSentOutcomes(layout.parts, "not sent: the content omitted bundled files"))
		assessment.SkippedFiles = jevSkippedFiles(layout)
		assessment.SkippedFileCount = int32(layout.skippedCount)
		assessment.Blocked = true
		assessment.BlockReasons = []string{fmt.Sprintf("jev assessment incomplete: %d bundled file(s) were withheld or skipped, so Jev cannot see the full content; no Jev calls were made", layout.skippedCount)}

		return assessment, nil
	}

	token, err := r.readJevToken(ctx, version.Namespace, sourceRef.JevSecretRef)
	if err != nil {
		return jevIncomplete(retries+1, len(layout.parts), err.Error())
	}

	opts := []jev.Option{jev.WithAttemptGate(jevAttemptGate(r.jevSem, r.jevLimiter))}
	if r.JevEndpoint != "" {
		opts = append(opts, jev.WithEndpoint(r.JevEndpoint))
	}

	deadline := jevPartDeadline
	if r.jevDeadline > 0 {
		deadline = r.jevDeadline
	}

	sendCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	run := &jevRun{
		client: jev.NewClient(opts...),
		token:  token,
		kind:   kind,
		in:     in,
		total:  len(layout.parts),
	}
	run.calls.Store(int64(jevMaxChunks - len(layout.parts)))

	var g errgroup.Group
	g.SetLimit(jevMaxInFlight)
	slots := make([][]jevPartOutcome, len(layout.parts))
	for i := range layout.parts {
		g.Go(func() error {
			slots[i] = run.assess(sendCtx, layout.parts[i])

			return nil
		})
	}

	g.Wait()

	var outcomes []jevPartOutcome
	for _, slot := range slots {
		outcomes = append(outcomes, slot...)
	}

	sort.SliceStable(outcomes, func(i, j int) bool {
		return jevPartLabelLess(outcomes[i].result.label, outcomes[j].result.label)
	})

	parts := make([]jevPartResult, len(outcomes))
	for i, o := range outcomes {
		parts[i] = o.result
	}

	normalized := jevNormalizeParts(parts)
	for i := range outcomes {
		if outcomes[i].result.status == jevPartScored && normalized[i].status == jevPartFailed {
			outcomes[i].errText = "unrecognized risk level"
		}

		outcomes[i].result = normalized[i]
	}

	result, assessed := aggregateJevParts(normalized)
	gateReasons, needsReview := evaluateJevPartsGate(sourceRef.JevPolicy, normalized)

	assessment := jevNewAssessment(retries, len(layout.parts))
	assessment.Chunks = jevChunks(outcomes)
	assessment.AssessedChunks = int32(assessed)
	if assessed > 0 {
		assessment.Model = result.Model
		assessment.SafeProbability = ptr.To(result.SafeProbability)
		assessment.InjectionProbability = ptr.To(result.PromptInjectionProbability)
		assessment.ExfiltrationProbability = ptr.To(result.DataExfiltrationProbability)
		assessment.DestructiveProbability = ptr.To(result.DestructiveActionsProbability)
		assessment.HiddenInstructionsProbability = ptr.To(result.HiddenInstructionsProbability)
		assessment.ScopeMismatchProbability = ptr.To(result.ScopeMismatchProbability)
		assessment.RemoteExecutionProbability = ptr.To(result.RemoteExecutionProbability)
		assessment.RiskScore = ptr.To(result.RiskScore)
		assessment.RiskLevel = result.RiskLevel
		assessment.RiskConfidence = ptr.To(result.RiskConfidence)
	}

	nonAuthFailure := false
	firstFailure := -1
	tokenLimit := false
	for i, o := range outcomes {
		switch o.result.status {
		case jevPartFailed, jevPartNotSent:
			if firstFailure < 0 {
				firstFailure = i
			}

			if !o.auth {
				nonAuthFailure = true
			}
		case jevPartTokenLimitExceeded:
			tokenLimit = true
		}
	}

	if firstFailure >= 0 {
		if nonAuthFailure {
			assessment.RetryCount = retries + 1
		}

		msg := fmt.Sprintf("jev assessment incomplete: part %s failed: %s", outcomes[firstFailure].result.label, outcomes[firstFailure].errText)
		assessment.Error = msg
		assessment.Blocked = true
		assessment.BlockReasons = append([]string{msg}, gateReasons...)

		return assessment, &jevServiceError{msg: msg}
	}

	if tokenLimit {
		var reasons []string
		for _, o := range outcomes {
			if o.result.status == jevPartTokenLimitExceeded {
				reasons = append(reasons, fmt.Sprintf("part %s: %s", o.result.label, o.errText))
			}
		}

		assessment.Blocked = true
		assessment.BlockReasons = append(reasons, gateReasons...)

		return assessment, nil
	}

	assessment.NeedsReview = needsReview
	if len(gateReasons) > 0 {
		assessment.Blocked = true
		assessment.BlockReasons = gateReasons
	}

	return assessment, nil
}
