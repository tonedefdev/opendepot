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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// cosignSimplesigningMediaType is the media type of the simple-signing payload layer that cosign writes.
const cosignSimplesigningMediaType = "application/vnd.dev.cosign.simplesigning.v1+json"

// The vector below was produced by the upstream cosign CLI: `cosign generate-key-pair`, then `cosign sign` of a scratch
// image pushed to a registry:2 instance. The payload is the simple-signing blob that cosign stored, and the signature
// is the value of its dev.cosignproject.cosign/signature annotation.
const (
	cosignVectorPublicKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEGO/LPclEFJk5ELSjJcL09aCkB7LZ
RVuS6sveYYfa/tXcwQPRu9Z7zYSxQKmslT2tQ8rxr0VdvE+scft6dywBnQ==
-----END PUBLIC KEY-----
`
	cosignVectorSubject       = "sha256:3f9093ed83ce8bc8a64adc50350f484cb4952ee860b7b144275d206c96847db4"
	cosignVectorPayload       = `{"critical":{"identity":{"docker-reference":"127.0.0.1:5055/vector/app"},"image":{"docker-manifest-digest":"sha256:3f9093ed83ce8bc8a64adc50350f484cb4952ee860b7b144275d206c96847db4"},"type":"cosign container image signature"},"optional":null}`
	cosignVectorPayloadDigest = "sha256:993bbe898459c4d23128ba500486652f3013365e705bf699c8dc888ec1d96c74"
	cosignVectorSignature     = "MEUCIEnl8ZOQCD5ymWcfRHikY299ZfCIhwyBZj1YTbXHMa1uAiEAkkm9il8vJ9N9FPZvN1rfiMqxyV+epsSa/WStXlYeMHA="
)

// pemPublicKey encodes an ECDSA public key as the PEM PKIX document that cosign writes.
func pemPublicKey(key *ecdsa.PrivateKey) []byte {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	Expect(err).NotTo(HaveOccurred())

	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// cosignPayload returns a cosign simple-signing payload that declares subject as the manifest digest.
func cosignPayload(subject digest.Digest) []byte {
	return []byte(fmt.Sprintf(
		`{"critical":{"identity":{"docker-reference":"registry.example.com/team/app"},"image":{"docker-manifest-digest":%q},"type":%q},"optional":null}`,
		subject.String(),
		cosignSignatureType,
	))
}

// signCosignPayload signs the SHA-256 digest of payload with key, as cosign does for ECDSA keys.
func signCosignPayload(key *ecdsa.PrivateKey, payload []byte) []byte {
	sum := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, key, sum[:])
	Expect(err).NotTo(HaveOccurred())

	return sig
}

// addCosignSignature stores a cosign signature for subject under its .sig tag. The signature is carried as a layer
// annotation, and the payload is the layer's blob.
func (f *fakeOCIRegistry) addCosignSignature(subject digest.Digest, payload, sig []byte) {
	layer := f.addBlob(cosignSimplesigningMediaType, payload)
	layer.Annotations = map[string]string{cosignSignatureAnnotation: base64.StdEncoding.EncodeToString(sig)}

	signature := f.addImage(f.addBlob(ocispec.MediaTypeEmptyJSON, []byte("{}")), "", layer)
	f.tag(cosignSignatureTag(subject), signature)
}

var _ = Describe("OCIArtifact cosign signature verification", func() {
	var (
		signingKey *ecdsa.PrivateKey
		otherKey   *ecdsa.PrivateKey
		registry   *fakeOCIRegistry
		index      ocispec.Descriptor
		repository = "registry.example.com/team/app"
		ctx        = context.Background()
	)

	BeforeEach(func() {
		var err error
		signingKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		Expect(err).NotTo(HaveOccurred())

		otherKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		Expect(err).NotTo(HaveOccurred())

		registry = newFakeOCIRegistry()
		index = seedImageIndex(registry, "signed")
		registry.tag("1.0.0", index)
		ensureOCITestNamespace()
	})

	Describe("verifyCosignSignature", func() {
		It("accepts a signature made by the key over a payload that binds the digest", func() {
			payload := cosignPayload(index.Digest)
			registry.addCosignSignature(index.Digest, payload, signCosignPayload(signingKey, payload))

			Expect(verifyCosignSignature(ctx, registry, repository, index.Digest, pemPublicKey(signingKey))).To(Succeed())
		})

		It("rejects a payload that was changed after it was signed", func() {
			original := cosignPayload(index.Digest)
			sig := signCosignPayload(signingKey, original)
			altered := []byte(fmt.Sprintf(`{"critical":{"image":{"docker-manifest-digest":%q},"type":%q}}`, index.Digest.String(), cosignSignatureType+" "))
			registry.addCosignSignature(index.Digest, altered, sig)

			err := verifyCosignSignature(ctx, registry, repository, index.Digest, pemPublicKey(signingKey))
			Expect(err).To(MatchError(ContainSubstring("does not match the cosign public key")))
		})

		It("rejects a signature that binds a different digest", func() {
			other := seedImageIndex(registry, "other")
			payload := cosignPayload(other.Digest)
			registry.addCosignSignature(index.Digest, payload, signCosignPayload(signingKey, payload))

			err := verifyCosignSignature(ctx, registry, repository, index.Digest, pemPublicKey(signingKey))
			Expect(err).To(MatchError(ContainSubstring("signature is for")))
		})

		It("rejects a signature that was made by a different key", func() {
			payload := cosignPayload(index.Digest)
			registry.addCosignSignature(index.Digest, payload, signCosignPayload(otherKey, payload))

			err := verifyCosignSignature(ctx, registry, repository, index.Digest, pemPublicKey(signingKey))
			Expect(err).To(MatchError(ContainSubstring("does not match the cosign public key")))
		})

		It("reports a missing signature", func() {
			err := verifyCosignSignature(ctx, registry, repository, index.Digest, pemPublicKey(signingKey))
			Expect(errors.Is(err, errNoCosignSignature)).To(BeTrue())
		})

		It("rejects a public key that is not PEM encoded", func() {
			err := verifyCosignSignature(ctx, registry, repository, index.Digest, []byte("not a key"))
			Expect(err).To(MatchError(ContainSubstring("PEM decoding failed")))
		})

		It("verifies a P-384 signature over a SHA-384 digest of the payload", func() {
			p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			Expect(err).NotTo(HaveOccurred())

			payload := cosignPayload(index.Digest)
			sum := sha512.Sum384(payload)
			sig, err := ecdsa.SignASN1(rand.Reader, p384Key, sum[:])
			Expect(err).NotTo(HaveOccurred())
			registry.addCosignSignature(index.Digest, payload, sig)

			Expect(verifyCosignSignature(ctx, registry, repository, index.Digest, pemPublicKey(p384Key))).To(Succeed())
		})

		It("verifies the signature that the upstream cosign CLI wrote for an image", func() {
			Expect(digest.FromString(cosignVectorPayload)).To(Equal(digest.Digest(cosignVectorPayloadDigest)))

			sig, err := base64.StdEncoding.DecodeString(cosignVectorSignature)
			Expect(err).NotTo(HaveOccurred())

			subject := digest.Digest(cosignVectorSubject)
			registry.addCosignSignature(subject, []byte(cosignVectorPayload), sig)

			Expect(verifyCosignSignature(ctx, registry, repository, subject, []byte(cosignVectorPublicKey))).To(Succeed())
		})

		It("rejects the upstream cosign signature when it is presented for a different digest", func() {
			sig, err := base64.StdEncoding.DecodeString(cosignVectorSignature)
			Expect(err).NotTo(HaveOccurred())

			// The payload binds the vector digest, so the same bytes must not verify for the index digest.
			registry.addCosignSignature(index.Digest, []byte(cosignVectorPayload), sig)

			err = verifyCosignSignature(ctx, registry, repository, index.Digest, []byte(cosignVectorPublicKey))
			Expect(err).To(MatchError(ContainSubstring("signature is for")))
		})

		It("verifies a signature that the local cosign CLI generates", func() {
			cosignBin, err := exec.LookPath("cosign")
			if err != nil {
				Skip("the cosign CLI is not installed; the upstream signature vector covers the same verifier")
			}

			runCosign := func(args ...string) {
				cmd := exec.CommandContext(ctx, cosignBin, args...)
				cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
				out, err := cmd.CombinedOutput()
				Expect(err).NotTo(HaveOccurred(), string(out))
			}

			dir := GinkgoT().TempDir()
			keyPrefix := filepath.Join(dir, "cosign")
			runCosign("generate-key-pair", "--output-key-prefix", keyPrefix)

			payload := cosignPayload(index.Digest)
			payloadPath := filepath.Join(dir, "payload.json")
			Expect(os.WriteFile(payloadPath, payload, 0o600)).To(Succeed())

			sigPath := filepath.Join(dir, "payload.sig")
			runCosign("sign-blob", "--key", keyPrefix+".key", "--tlog-upload=false", "--yes",
				"--output-signature", sigPath, payloadPath)

			encodedSig, err := os.ReadFile(sigPath)
			Expect(err).NotTo(HaveOccurred())
			sig, err := base64.StdEncoding.DecodeString(string(encodedSig))
			Expect(err).NotTo(HaveOccurred())

			publicKey, err := os.ReadFile(keyPrefix + ".pub")
			Expect(err).NotTo(HaveOccurred())

			registry.addCosignSignature(index.Digest, payload, sig)
			Expect(verifyCosignSignature(ctx, registry, repository, index.Digest, publicKey)).To(Succeed())
		})
	})

	Describe("reconciling a mirror with verify configured", func() {
		var storageDir string

		BeforeEach(func() {
			storageDir = GinkgoT().TempDir()
		})

		verifiedConfig := func(secretName string, required bool) *opendepotv1alpha1.OCIArtifactConfig {
			config := mirrorConfig("Image", storageDir)
			config.Mirror.Verify = &opendepotv1alpha1.OCIVerifyConfig{
				CosignPublicKeySecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "cosign.pub",
				},
				Required: required,
			}

			return config
		}

		createKeySecret := func(name string, publicKey []byte) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ociTestNamespace},
				Data:       map[string][]byte{"cosign.pub": publicKey},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, secret) })
		}

		DescribeTable("records the outcome and blocks only when required",
			func(name, signature, keyChoice string, required, wantSynced bool) {
				payload := cosignPayload(index.Digest)
				switch signature {
				case "valid":
					registry.addCosignSignature(index.Digest, payload, signCosignPayload(signingKey, payload))
				case "tampered":
					sig := signCosignPayload(signingKey, payload)
					altered := []byte(fmt.Sprintf(`{"critical":{"image":{"docker-manifest-digest":%q},"type":%q},"optional":{"tampered":true}}`, index.Digest.String(), cosignSignatureType))
					registry.addCosignSignature(index.Digest, altered, sig)
				}

				publicKey := pemPublicKey(signingKey)
				if keyChoice == "wrong" {
					publicKey = pemPublicKey(otherKey)
				}

				secretName := "cosign-key-" + name
				createKeySecret(secretName, publicKey)
				createOCIVersion(name, verifiedConfig(secretName, required))

				result, err := reconcileOCI(newOCIReconciler(registry), name)
				if wantSynced {
					Expect(err).NotTo(HaveOccurred())
					Expect(result.RequeueAfter).To(Equal(ociDriftRequeueInterval))
				} else {
					Expect(err).To(HaveOccurred())
				}

				current := getOCIVersion(name)
				Expect(current.Status.Synced).To(Equal(wantSynced))
				if wantSynced {
					Expect(current.Status.OCIManifest).NotTo(BeNil())
					Expect(current.Status.OCIManifest.SignatureVerified).NotTo(BeNil())
					Expect(*current.Status.OCIManifest.SignatureVerified).To(Equal(signature == "valid" && keyChoice == "right"))
					return
				}

				Expect(current.Status.SyncStatus).To(ContainSubstring("Signature verification failed"))
				Expect(current.Status.OCIManifest).To(BeNil())
			},
			Entry("a valid signature is verified", "valid-verified", "valid", "right", false, true),
			Entry("a valid signature is verified when required", "valid-required", "valid", "right", true, true),
			Entry("a tampered payload is unverified and allowed when optional", "tampered-optional", "tampered", "right", false, true),
			Entry("a tampered payload blocks when required", "tampered-required", "tampered", "right", true, false),
			Entry("a signature from the wrong key blocks when required", "wrong-key-required", "valid", "wrong", true, false),
			Entry("a missing signature is unverified and allowed when optional", "missing-optional", "missing", "right", false, true),
			Entry("a missing signature blocks when required", "missing-required", "missing", "right", true, false),
		)
	})
})
