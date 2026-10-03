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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema"
)

// contractConfigMapKey is the ConfigMap data key holding a module Version's gzipped,
// base64 encoded Assembly Line contract JSON.
const contractConfigMapKey = "contract.json"

// contractNeedsDerivation reports whether a module Version's contract must be built.
// A contract is derived on first sync, and re-derived when it is degraded and a provider
// schema in the namespace has been extracted since the contract was last built.
func (r *VersionReconciler) contractNeedsDerivation(ctx context.Context, version *opendepotv1alpha1.Version) bool {
	ref := version.Status.ContractConfigMapRef
	if ref == nil {
		return true
	}

	if ref.Grade == hclschema.GradeFull {
		return false
	}

	derivedAt, err := time.Parse(time.RFC3339, ref.DerivedAt)
	if err != nil {
		return true
	}

	versions := &opendepotv1alpha1.VersionList{}
	if err := r.List(ctx, versions, client.InNamespace(version.Namespace)); err != nil {
		return false
	}

	for i := range versions.Items {
		schemaRef := versions.Items[i].Status.ProviderSchemaRef
		if schemaRef == nil {
			continue
		}

		extractedAt, err := time.Parse(time.RFC3339, schemaRef.ExtractedAt)
		if err != nil {
			continue
		}

		if extractedAt.After(derivedAt) {
			return true
		}
	}

	return false
}

// providerSchemaNeedsExtraction reports whether a provider Version still needs its reduced
// schema extracted. Versions whose platform does not match the controller's own are never
// extracted, so they must not be treated as pending.
func (r *VersionReconciler) providerSchemaNeedsExtraction(version *opendepotv1alpha1.Version) bool {
	if version.Spec.OperatingSystem != runtime.GOOS || version.Spec.Architecture != runtime.GOARCH {
		return false
	}

	return version.Status.ProviderSchemaRef == nil
}

// deriveModuleContract extracts a module archive, derives its Assembly Line contract and
// stores it in a ConfigMap owned by the Version. Every failure mode is non-fatal: a module
// that cannot be parsed is recorded with an "unsupported" grade rather than blocking the
// Version's reconciliation.
func (r *VersionReconciler) deriveModuleContract(ctx context.Context, version *opendepotv1alpha1.Version, archiveBytes []byte) (*opendepotv1alpha1.ContractConfigMapRef, error) {
	meta := contractMetaFor(version)

	contract, err := r.buildContractForArchive(ctx, version, archiveBytes, meta)
	if err != nil {
		r.Log.V(5).Info("module contract derivation failed; recording an unsupported contract",
			"version", version.Name, "error", err.Error())
		contract = hclschema.UnsupportedContract(meta, err.Error())
	}

	return r.upsertContractConfigMap(ctx, version, contract)
}

// buildContractForArchive extracts archiveBytes to a temp directory, parses the module and
// assembles its contract against whatever provider schemas are currently onboarded.
func (r *VersionReconciler) buildContractForArchive(ctx context.Context, version *opendepotv1alpha1.Version, archiveBytes []byte, meta hclschema.ContractMeta) (*hclschema.Contract, error) {
	if len(archiveBytes) == 0 {
		return nil, fmt.Errorf("no module archive bytes are available to derive a contract from")
	}

	tmpDir, cleanup, err := extractArchiveToTempDir(archiveBytes, "opendepot-contract-*")
	if err != nil {
		return nil, err
	}
	defer cleanup()

	mod, err := hclschema.ParseModule(tmpDir)
	if err != nil {
		return nil, err
	}

	resolver, resolverWarnings := r.buildSchemaResolver(ctx, version, mod.RequiredProviders)
	mod.Diagnostics = append(mod.Diagnostics, resolverWarnings...)

	// A nil *schemaResolver stored in the interface would be non-nil at the interface
	// level, so pass an untyped nil when no provider schema could be loaded.
	if resolver == nil {
		return hclschema.BuildContract(mod, meta, nil)
	}

	return hclschema.BuildContract(mod, meta, resolver)
}

// contractMetaFor derives the module identity a contract is built for from the Version CR.
func contractMetaFor(version *opendepotv1alpha1.Version) hclschema.ContractMeta {
	meta := hclschema.ContractMeta{
		Namespace: version.Namespace,
		Version:   version.Spec.Version,
	}

	if version.Spec.ModuleConfigRef == nil {
		return meta
	}

	if version.Spec.ModuleConfigRef.Name != nil {
		meta.Name = *version.Spec.ModuleConfigRef.Name
	}

	meta.Provider = version.Spec.ModuleConfigRef.Provider

	if meta.Name != "" {
		meta.Source = fmt.Sprintf("%s/%s/%s", version.Namespace, meta.Name, meta.Provider)
	}

	return meta
}

// upsertContractConfigMap creates or updates the ConfigMap holding a module Version's
// gzipped, base64 encoded contract JSON and returns a reference to it. The ConfigMap is
// owned by the Version so it is garbage collected when the Version is deleted.
func (r *VersionReconciler) upsertContractConfigMap(ctx context.Context, version *opendepotv1alpha1.Version, contract *hclschema.Contract) (*opendepotv1alpha1.ContractConfigMapRef, error) {
	encoded, err := encodeContract(contract)
	if err != nil {
		return nil, err
	}

	cmName := version.Name + "-contract"

	var moduleName string
	if version.Spec.ModuleConfigRef != nil && version.Spec.ModuleConfigRef.Name != nil {
		moduleName = *version.Spec.ModuleConfigRef.Name
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: version.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		labels := map[string]string{
			"opendepot.defdev.io/version":   version.Name,
			"opendepot.defdev.io/namespace": version.Namespace,
		}

		if moduleName != "" {
			labels["opendepot.defdev.io/module"] = moduleName
		}

		cm.Labels = labels

		if cm.Data == nil {
			cm.Data = map[string]string{}
		}

		cm.Data[contractConfigMapKey] = encoded

		return controllerutil.SetControllerReference(version, cm, r.Scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upsert contract configmap %s: %w", cmName, err)
	}

	return &opendepotv1alpha1.ContractConfigMapRef{
		Name:      cmName,
		Key:       contractConfigMapKey,
		Grade:     contract.Compatibility.Grade,
		DerivedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// encodeContract marshals, gzips and base64 encodes a contract for ConfigMap storage.
// Contracts for large modules routinely exceed 100 KiB of raw JSON; gzipping keeps them
// well inside the etcd object size limit.
func encodeContract(contract *hclschema.Contract) (string, error) {
	raw, err := json.Marshal(contract)
	if err != nil {
		return "", fmt.Errorf("failed to marshal module contract: %w", err)
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(raw); err != nil {
		return "", fmt.Errorf("failed to gzip module contract: %w", err)
	}

	if err := gw.Close(); err != nil {
		return "", fmt.Errorf("failed to finalise gzipped module contract: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
