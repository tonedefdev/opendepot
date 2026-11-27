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
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	// cosignSignatureAnnotation is the layer annotation that holds the base64 signature over a simple-signing payload.
	cosignSignatureAnnotation = "dev.cosignproject.cosign/signature"
	// cosignSignatureType is the critical.type that a cosign container image signature payload declares.
	cosignSignatureType = "cosign container image signature"
	// maxCosignPayloadBytes bounds a simple-signing payload read from the registry.
	maxCosignPayloadBytes int64 = 64 << 10
)

// errNoCosignSignature is returned when the registry holds no cosign signature for an artifact.
var errNoCosignSignature = errors.New("no cosign signature found")

// cosignSimpleSigning is the part of a cosign simple-signing payload that verification reads.
type cosignSimpleSigning struct {
	Critical struct {
		Type  string `json:"type"`
		Image struct {
			DockerManifestDigest string `json:"docker-manifest-digest"`
		} `json:"image"`
	} `json:"critical"`
}

// cosignSignatureTag returns the tag that cosign stores the signatures of subject under: sha256-<hex>.sig.
func cosignSignatureTag(subject digest.Digest) string {
	return strings.ReplaceAll(subject.String(), ":", "-") + ".sig"
}

// parseCosignPublicKey parses a PEM-encoded PKIX public key, the format that `cosign generate-key-pair` writes.
func parseCosignPublicKey(pemData []byte) (crypto.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("cosign public key is not PEM encoded")
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse cosign public key: %w", err)
	}

	switch key.(type) {
	case *ecdsa.PublicKey, ed25519.PublicKey, *rsa.PublicKey:
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported cosign public key type %T", key)
	}
}

// verifyCosignPayloadSignature checks sig over payload with key. ECDSA signatures are ASN.1 encoded over the SHA-256
// digest of the payload, which is how cosign signs. Ed25519 signs the payload directly and RSA uses PKCS#1 v1.5.
func verifyCosignPayloadSignature(key crypto.PublicKey, payload, sig []byte) error {
	sum := sha256.Sum256(payload)
	switch k := key.(type) {
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(k, sum[:], sig) {
			return errors.New("signature does not match the cosign public key")
		}
	case ed25519.PublicKey:
		if !ed25519.Verify(k, payload, sig) {
			return errors.New("signature does not match the cosign public key")
		}
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(k, crypto.SHA256, sum[:], sig); err != nil {
			return errors.New("signature does not match the cosign public key")
		}
	default:
		return fmt.Errorf("unsupported cosign public key type %T", key)
	}

	return nil
}

// verifyCosignSignature checks that subject carries a cosign signature made by the public key. The signatures are read
// from the cosign signature tag of subject in repository. Each one is verified over its simple-signing payload, and the
// payload must declare subject as its manifest digest. Verification is offline: no transparency log is contacted.
func verifyCosignSignature(
	ctx context.Context,
	registry ociRegistry,
	repository string,
	subject digest.Digest,
	publicKeyPEM []byte,
) error {
	key, err := parseCosignPublicKey(publicKeyPEM)
	if err != nil {
		return err
	}

	sigs, err := registry.FetchManifest(ctx, repository, cosignSignatureTag(subject), nil)
	if err != nil {
		return fmt.Errorf("%w for %s: %w", errNoCosignSignature, subject, err)
	}

	failure := errNoCosignSignature
	for _, pm := range sigs.Manifests {
		for _, layer := range pm.Manifest.Layers {
			encoded, ok := layer.Annotations[cosignSignatureAnnotation]
			if !ok {
				continue
			}

			failure = verifyCosignLayer(ctx, registry, repository, key, subject, layer, encoded)
			if failure == nil {
				return nil
			}
		}
	}

	return failure
}

// verifyCosignLayer verifies one cosign signature layer against subject.
func verifyCosignLayer(
	ctx context.Context,
	registry ociRegistry,
	repository string,
	key crypto.PublicKey,
	subject digest.Digest,
	layer ocispec.Descriptor,
	encoded string,
) error {
	sig, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode cosign signature: %w", err)
	}

	payload, err := readOCIBlob(ctx, registry, repository, layer, maxCosignPayloadBytes)
	if err != nil {
		return err
	}

	if err := verifyCosignPayloadSignature(key, payload, sig); err != nil {
		return err
	}

	var claim cosignSimpleSigning
	if err := json.Unmarshal(payload, &claim); err != nil {
		return fmt.Errorf("decode cosign simple-signing payload: %w", err)
	}

	if claim.Critical.Type != cosignSignatureType {
		return fmt.Errorf("signature payload type %q is not a cosign image signature", claim.Critical.Type)
	}

	if claim.Critical.Image.DockerManifestDigest != subject.String() {
		return fmt.Errorf("signature is for %s, not %s", claim.Critical.Image.DockerManifestDigest, subject)
	}

	return nil
}

// readOCIBlob reads a small blob from the registry. The blob is verified against desc by the registry client, and it
// is refused when it is larger than limit.
func readOCIBlob(
	ctx context.Context,
	registry ociRegistry,
	repository string,
	desc ocispec.Descriptor,
	limit int64,
) ([]byte, error) {
	if desc.Size > limit {
		return nil, fmt.Errorf("blob %s exceeds %d bytes", desc.Digest, limit)
	}

	rc, err := registry.FetchBlob(ctx, repository, desc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", desc.Digest, err)
	}

	if int64(len(data)) > limit {
		return nil, fmt.Errorf("blob %s exceeds %d bytes", desc.Digest, limit)
	}

	return data, nil
}

// cosignPublicKey reads the PEM public key that an OCIArtifact uses to verify its upstream signatures.
func (r *VersionReconciler) cosignPublicKey(ctx context.Context, namespace string, ref *corev1.SecretKeySelector) ([]byte, error) {
	if ref == nil {
		return nil, errors.New("cosignPublicKeySecretRef is not set")
	}

	return r.readOCISecretValue(ctx, namespace, ref.Name, ref.Key, "")
}
