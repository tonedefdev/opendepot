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

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

const (
	// agentPresignEmulatorPort is the local port the presigned URL host (localhost:9297) must use because SigV4 signs the host.
	agentPresignEmulatorPort = 9297
	// agentPresignDexPort is the local port used for the Dex port-forward in the OIDC scenarios.
	agentPresignDexPort = 15571
	// agentPresignRegion is the region of the emulator bucket.
	agentPresignRegion = "us-east-1"
	// agentPresignTTL is the presigned URL lifetime. It is short so the expiry scenario only needs a brief sleep.
	agentPresignTTL = "3s"

	agentPresignPrivateNS   = "agent-presign-private-e2e"
	agentPresignPublicNS    = "agent-presign-public-e2e"
	agentPresignAgent       = "presign-reviewer-e2e"
	agentPresignPropNS      = "agent-presign-propagation-e2e"
	agentPresignPropAgent   = "presign-propagation-e2e"
	agentPresignPropVersion = "agent-presign-propagation-e2e-1-2-0"
	agentPresignAgentImage  = "agent-controller:presign-e2e"
	agentPresignVersion     = "1.2.0"
	agentPresignVersionName = "agent-presign-reviewer-e2e-1-2-0"
	agentPresignFileName    = "presign-reviewer-e2e-1.2.0.tar.gz"
	agentPresignBindingName = "agent-presign-groupbinding"
	agentPresignUserID      = "agent-presign-user-e2e"
	agentPresignUserEmail   = "agent-presign-e2e@example.com"
)

// agentPresignArchive is the archive stored in the emulator bucket for the presign scenarios.
var agentPresignArchive = []byte("presign reviewer archive for e2e checksum verification")

// agentPresignPortForward starts a self-restarting port-forward to target and returns its cancel function.
func agentPresignPortForward(target string, localPort, remotePort int) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
				"-n", namespace,
				target,
				fmt.Sprintf("%d:%d", localPort, remotePort),
			)
			cmd.Stdout = GinkgoWriter
			cmd.Stderr = GinkgoWriter
			_ = cmd.Run()
			time.Sleep(200 * time.Millisecond)
		}
	}()

	return cancel
}

// agentPresignWaitForEndpoint blocks until the endpoint answers over the client.
func agentPresignWaitForEndpoint(client *http.Client, endpoint, description string) {
	Eventually(func() error {
		resp, err := client.Get(endpoint)
		if err != nil {
			return err
		}
		resp.Body.Close()

		return nil
	}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for the %s port-forward", description)
}

// agentPresignInstall deploys the server with the AWS emulator sidecar. The emulator image tag is supplied at install
// time because it changes on every run.
func agentPresignInstall(repoRoot, emulatorImage string, extraArgs ...string) {
	repo, tag := utils.SplitImageRef(serverImage)
	args := []string{
		"upgrade", helmReleaseName, filepath.Join(repoRoot, "chart", "opendepot"),
		"--install",
		"--create-namespace",
		"--namespace", namespace,
		"--skip-crds",
		"--set", "monitoring.enabled=false",
		"--set", "monitoring.bundled.enabled=false",
		"--set", "global.image.tag=",
		"--set", "depot.enabled=false",
		"--set", "module.enabled=false",
		"--set", "provider.enabled=false",
		"--set", "version.enabled=false",
		"--set", "scanning.enabled=false",
		"--set", "scanning.providerScanning=false",
		"--set", "server.enabled=true",
		"--set", "server.image.repository=" + repo,
		"--set", "server.image.tag=" + tag,
		"--set", "valkey.dataStorage.enabled=false",
		"-f", filepath.Join(repoRoot, "services", "server", "test", "e2e", "values-agent-presign-e2e.yaml"),
		"--set-string", "server.extraInitContainers[0].image=" + emulatorImage,
		"--wait",
		"--timeout", "10m",
	}
	args = append(args, extraArgs...)
	args = append(args, utils.CoverageHelmArgs()...)

	cmd := exec.Command("helm", args...)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to helm upgrade server with the AWS emulator sidecar")
}

// agentPresignUninstall removes the previous release so the next install starts from a clean state.
func agentPresignUninstall() {
	cmd := exec.Command("helm", "uninstall", helmReleaseName, "--namespace", namespace, "--ignore-not-found")
	_, _ = utils.Run(cmd)
}

// agentPresignEnsureNamespace creates the namespace and labels it public when requested.
func agentPresignEnsureNamespace(name string, public bool) {
	cmd := exec.Command("kubectl", "create", "namespace", name)
	_, _ = utils.Run(cmd)

	if public {
		cmd = exec.Command("kubectl", "label", "namespace", name, "opendepot.defdev.io/public=true", "--overwrite")
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to label the public namespace")
	}
}

// agentPresignApply applies a manifest with kubectl.
func agentPresignApply(manifest string) {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply manifest")
}

// agentPresignPatchStatus merge-patches the status subresource of a Version.
func agentPresignPatchStatus(ns, patch string) {
	cmd := exec.Command("kubectl", "patch", "versions", agentPresignVersionName,
		"-n", ns, "--subresource=status", "--type=merge", "-p", patch)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to patch Version status")
}

// agentPresignVersionManifest returns the Agent Version fixture. The presign block is included only when presign is true.
func agentPresignVersionManifest(ns, bucket string, presign bool) string {
	presignBlock := ""
	if presign {
		presignBlock = fmt.Sprintf(`
      presign:
        enabled: true
        ttl: %s`, agentPresignTTL)
	}

	return fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  type: Agent
  version: %[3]q
  fileName: %[4]s
  agentSourceRef:
    name: %[5]s
    storageConfig:
      s3:
        bucket: %[6]s
        region: %[7]s%[8]s
`, agentPresignVersionName, ns, agentPresignVersion, agentPresignFileName, agentPresignAgent, bucket, agentPresignRegion, presignBlock)
}

// agentPresignBindingManifest returns a GroupBinding that grants the test user access to the presign Agent.
func agentPresignBindingManifest() string {
	return fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: %s
  namespace: %s
spec:
  expression: %q
  agentResources:
    - %s
`, agentPresignBindingName, namespace, fmt.Sprintf("%q in groups", agentPresignUserEmail), agentPresignAgent)
}

// agentPresignArchivePath returns the archive endpoint path for the presign Agent Version.
func agentPresignArchivePath(ns string) string {
	return fmt.Sprintf("/opendepot/agents/v1/%s/agents/%s/%s/archive", ns, agentPresignAgent, agentPresignVersion)
}

// agentPresignPropArchiveURL returns the archive URL of the propagation Version on the port-forwarded server.
func agentPresignPropArchiveURL() string {
	return fmt.Sprintf("http://localhost:%d/opendepot/agents/v1/%s/agents/%s/%s/archive",
		serverLocalPort, agentPresignPropNS, agentPresignPropAgent, agentPresignVersion)
}

// agentPresignObjectKey returns the bucket key the version controller writes the archive to.
func agentPresignObjectKey() string {
	return fmt.Sprintf("agent-%s/%s", agentPresignAgent, agentPresignFileName)
}

// agentPresignPropagationManifest returns an Agent whose spec.versions entry matches the presign Version fixture name.
// Presign is disabled on the source so the Version starts without a presign block.
func agentPresignPropagationManifest(ns, bucket string) string {
	return fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Agent
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  agentSourceConfig:
    name: %[1]s
    repoOwner: tonedefdev
    repoUrl: https://github.com/tonedefdev/opendepot
    path: agents/presign-propagation
    storageConfig:
      presign:
        enabled: false
      s3:
        bucket: %[3]s
        region: %[4]s
  versions:
    - version: %[5]q
`, agentPresignPropAgent, ns, bucket, agentPresignRegion, agentPresignVersion)
}

// agentPresignSetSourcePresign merge-patches the presign block on the Agent source storageConfig.
func agentPresignSetSourcePresign(ns, enabled string) {
	patch := fmt.Sprintf(`{"spec":{"agentSourceConfig":{"storageConfig":{"presign":{"enabled":%s,"ttl":"15m"}}}}}`, enabled)
	cmd := exec.Command("kubectl", "patch", "agent", agentPresignPropAgent, "-n", ns, "--type=merge", "-p", patch)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to patch the Agent presign setting")
}

// agentPresignVersionField returns a field of the propagation Version with a kubectl JSONPath expression. It returns the
// error instead of asserting so callers can poll it with Eventually while the Version is still being created.
func agentPresignVersionField(ns, name, jsonPath string) (string, error) {
	cmd := exec.Command("kubectl", "get", "version", name, "-n", ns, "-o", "jsonpath="+jsonPath)
	out, err := utils.Run(cmd)
	if err != nil {
		return "", fmt.Errorf("read the Version field %s: %w", jsonPath, err)
	}

	return strings.TrimSpace(out), nil
}

// agentPresignEmulatorFiles copies the emulator CA bundle and shared credentials file into dir. The files are read
// from the emulator sidecar at runtime, so no key or certificate is committed.
func agentPresignEmulatorFiles(dir string) (caPath, credsPath string) {
	caPath = filepath.Join(dir, "ca.pem")
	credsPath = filepath.Join(dir, "emulator_credentials")

	for _, file := range []struct{ source, dest string }{
		{"/home/emulator/.aws/emulator/9297/ca.pem", caPath},
		{"/home/emulator/.aws/emulator/9297/emulator_credentials", credsPath},
	} {
		cmd := exec.Command("kubectl", "exec",
			"-n", namespace,
			"deploy/server",
			"-c", "aws-emulator",
			"--", "cat", file.source,
		)
		content, err := cmd.Output()
		ExpectWithOffset(2, err).NotTo(HaveOccurred(), "Failed to read %s from the emulator", file.source)
		ExpectWithOffset(2, os.WriteFile(file.dest, content, 0o600)).To(Succeed())
	}

	return caPath, credsPath
}

// agentPresignS3Client returns a path-style S3 client that talks to the emulator on localhost:9297 with the
// emulator CA and credentials.
func agentPresignS3Client(caPath, credsPath string) *s3.Client {
	caFile, err := os.Open(caPath)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	defer caFile.Close()

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(agentPresignRegion),
		config.WithSharedCredentialsFiles([]string{credsPath}),
		config.WithCustomCABundle(caFile),
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
	)
	ExpectWithOffset(2, err).NotTo(HaveOccurred(), "Failed to load the emulator S3 config")

	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(fmt.Sprintf("https://localhost:%d", agentPresignEmulatorPort))
		options.UsePathStyle = true
	})
}

// agentPresignStageArchive creates the bucket and uploads the archive with its SHA256 checksum. It returns the
// base64 encoded checksum the Version status must carry.
func agentPresignStageArchive(client *s3.Client, bucket string) string {
	return agentPresignStageArchiveAt(client, bucket, agentPresignObjectKey())
}

// agentPresignStageArchiveAt creates the bucket and uploads the archive under key. It returns the base64 encoded
// checksum the Version status must carry.
func agentPresignStageArchiveAt(client *s3.Client, bucket, key string) string {
	ctx := context.Background()

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	ExpectWithOffset(2, err).NotTo(HaveOccurred(), "Failed to create the emulator bucket")

	sum := sha256.Sum256(agentPresignArchive)
	checksum := base64.StdEncoding.EncodeToString(sum[:])

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(key),
		Body:              bytes.NewReader(agentPresignArchive),
		ChecksumAlgorithm: s3types.ChecksumAlgorithmSha256,
		ChecksumSHA256:    aws.String(checksum),
	})
	ExpectWithOffset(2, err).NotTo(HaveOccurred(), "Failed to upload the archive to the emulator")

	return checksum
}

// agentPresignClient returns an HTTP client that trusts the emulator CA and does not follow redirects, so the 307
// and its Location header can be asserted directly.
func agentPresignClient(caPath string) *http.Client {
	caPEM, err := os.ReadFile(caPath)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	pool := x509.NewCertPool()
	ExpectWithOffset(2, pool.AppendCertsFromPEM(caPEM)).To(BeTrue(), "Failed to parse the emulator CA bundle")

	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// agentPresignGet performs a GET with an optional bearer token and returns the status, headers, and body.
func agentPresignGet(client *http.Client, target, token string) (int, http.Header, []byte) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	return resp.StatusCode, resp.Header, body
}

// agentPresignChecksumOf returns the base64 encoded SHA256 of body.
func agentPresignChecksumOf(body []byte) string {
	sum := sha256.Sum256(body)

	return base64.StdEncoding.EncodeToString(sum[:])
}

var _ = Describe("Agent presign", Ordered, func() {
	var (
		repoRoot         string
		emulatorRegistry string
		emulatorTag      string
		emulatorImage    string
	)

	BeforeAll(func() {
		if os.Getenv("E2E_AWS_EMULATOR") != "true" {
			Skip("E2E_AWS_EMULATOR is not true; skipping the agent presign scenarios")
		}

		kindCluster := os.Getenv("KIND_CLUSTER")
		Expect(kindCluster).NotTo(BeEmpty(), "KIND_CLUSTER must name the isolated Kind cluster")
		Expect(kindCluster).NotTo(Equal("opendepot"), "agent presign e2e must not run against the shared opendepot cluster")

		var err error
		repoRoot, err = utils.GetRepoRoot()
		Expect(err).NotTo(HaveOccurred(), "Failed to determine repo root")

		emulatorRegistry = agentE2EEnvOr("REGISTRY", "ghcr.io/tonedefdev/opendepot")
		emulatorTag = fmt.Sprintf("presign-%d", time.Now().Unix())
		emulatorImage = emulatorRegistry + "/aws-emulator:" + emulatorTag

		By("building and loading the AWS emulator image on the isolated Kind cluster")
		cmd := exec.Command("make", "build-aws-emulator", "load-aws-emulator",
			"REGISTRY="+emulatorRegistry,
			"TAG="+emulatorTag,
			"KIND_CLUSTER="+kindCluster,
		)
		_, err = utils.RunAt(cmd, repoRoot)
		Expect(err).NotTo(HaveOccurred(), "Failed to build and load the AWS emulator image")
	})

	Context("anonymous auth mode", Ordered, func() {
		var (
			pfServerCancel   context.CancelFunc
			pfEmulatorCancel context.CancelFunc
			client           *http.Client
			caPath           string
			checksum         string
		)

		BeforeAll(func() {
			By("uninstalling the previous release")
			agentPresignUninstall()

			By("deploying the server with anonymous auth and the AWS emulator sidecar")
			agentPresignInstall(repoRoot, emulatorImage, "--set", "server.anonymousAuth=true")

			pfServerCancel = agentPresignPortForward("svc/server", serverLocalPort, 80)
			pfEmulatorCancel = agentPresignPortForward("deploy/server", agentPresignEmulatorPort, agentPresignEmulatorPort)

			var credsPath string
			caPath, credsPath = agentPresignEmulatorFiles(GinkgoT().TempDir())
			client = agentPresignClient(caPath)
			agentPresignWaitForEndpoint(client, fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", serverLocalPort), "server")
			agentPresignWaitForEndpoint(client, fmt.Sprintf("https://localhost:%d/", agentPresignEmulatorPort), "emulator")

			By("staging the public Agent archive in the emulator bucket")
			agentPresignEnsureNamespace(agentPresignPublicNS, true)
			bucket := fmt.Sprintf("presign-public-%d", time.Now().UnixNano())
			checksum = agentPresignStageArchive(agentPresignS3Client(caPath, credsPath), bucket)

			agentPresignApply(agentPresignVersionManifest(agentPresignPublicNS, bucket, false))
			agentPresignPatchStatus(agentPresignPublicNS, fmt.Sprintf(
				`{"status":{"synced":true,"syncStatus":"Synced","fileName":%q,"checksum":%q}}`,
				agentPresignFileName, checksum))
		})

		AfterAll(func() {
			if pfServerCancel != nil {
				pfServerCancel()
			}
			if pfEmulatorCancel != nil {
				pfEmulatorCancel()
			}
		})

		It("serves a public Agent anonymously with the checksum verified", func() {
			status, headers, body := agentPresignGet(client,
				fmt.Sprintf("http://localhost:%d%s", serverLocalPort, agentPresignArchivePath(agentPresignPublicNS)), "")
			Expect(status).To(Equal(http.StatusOK))
			Expect(headers.Get("Location")).To(BeEmpty())
			Expect(agentPresignChecksumOf(body)).To(Equal(checksum))
		})
	})

	Context("source presign propagation", Ordered, func() {
		var (
			pfServerCancel   context.CancelFunc
			pfEmulatorCancel context.CancelFunc
			client           *http.Client
			checksum         string
		)

		BeforeAll(func() {
			By("uninstalling the previous release")
			agentPresignUninstall()

			By("building the agent controller image from this worktree and loading it on the isolated Kind cluster")
			agentRepo, agentTag := utils.SplitImageRef(agentPresignAgentImage)
			buildCmd := exec.Command("docker", "build", "-t", agentPresignAgentImage,
				"-f", "services/agent/Dockerfile", ".")
			_, err := utils.RunAt(buildCmd, repoRoot)
			Expect(err).NotTo(HaveOccurred(), "Failed to build the agent controller image")
			Expect(utils.LoadImageToKindClusterWithName(agentPresignAgentImage)).To(Succeed(),
				"Failed to load the agent controller image into Kind")

			By("deploying the server with anonymous auth, the agent controller, and the AWS emulator sidecar")
			agentPresignInstall(repoRoot, emulatorImage, "--set", "server.anonymousAuth=true",
				"--set", "agent.enabled=true",
				"--set", "agent.image.repository="+agentRepo,
				"--set", "agent.image.tag="+agentTag)

			pfServerCancel = agentPresignPortForward("svc/server", serverLocalPort, 80)
			pfEmulatorCancel = agentPresignPortForward("deploy/server", agentPresignEmulatorPort, agentPresignEmulatorPort)

			caPath, credsPath := agentPresignEmulatorFiles(GinkgoT().TempDir())
			client = agentPresignClient(caPath)
			agentPresignWaitForEndpoint(client, fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", serverLocalPort), "server")
			agentPresignWaitForEndpoint(client, fmt.Sprintf("https://localhost:%d/", agentPresignEmulatorPort), "emulator")

			By("creating a public Agent whose versions entry matches the presign Version name")
			agentPresignEnsureNamespace(agentPresignPropNS, true)
			bucket := fmt.Sprintf("presign-propagation-%d", time.Now().UnixNano())
			agentPresignApply(agentPresignPropagationManifest(agentPresignPropNS, bucket))

			By("waiting for the controller to create the Version and staging the archive under its fileName")
			Eventually(func() (string, error) {
				return agentPresignVersionField(agentPresignPropNS, agentPresignPropVersion, "{.spec.fileName}")
			}, 2*time.Minute, 2*time.Second).ShouldNot(BeEmpty())
			fileName, err := agentPresignVersionField(agentPresignPropNS, agentPresignPropVersion, "{.spec.fileName}")
			Expect(err).NotTo(HaveOccurred(), "Failed to read the Version fileName")

			checksum = agentPresignStageArchiveAt(agentPresignS3Client(caPath, credsPath), bucket,
				fmt.Sprintf("agent-%s/%s", agentPresignPropAgent, fileName))

			cmd := exec.Command("kubectl", "patch", "versions", agentPresignPropVersion,
				"-n", agentPresignPropNS, "--subresource=status", "--type=merge", "-p",
				fmt.Sprintf(`{"status":{"synced":true,"syncStatus":"Synced","fileName":%q,"checksum":%q}}`,
					fileName, checksum))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to patch Version status")
		})

		AfterAll(func() {
			if pfServerCancel != nil {
				pfServerCancel()
			}
			if pfEmulatorCancel != nil {
				pfEmulatorCancel()
			}
		})

		It("serves the archive proxied while presign is disabled on the source", func() {
			field, err := agentPresignVersionField(agentPresignPropNS, agentPresignPropVersion,
				"{.spec.agentSourceRef.storageConfig.presign.enabled}")
			Expect(err).NotTo(HaveOccurred())
			Expect(field).To(Equal("false"))

			status, headers, body := agentPresignGet(client, agentPresignPropArchiveURL(), "")
			Expect(status).To(Equal(http.StatusOK))
			Expect(headers.Get("Location")).To(BeEmpty())
			Expect(agentPresignChecksumOf(body)).To(Equal(checksum))
		})

		It("redirects to a presigned URL after presign is enabled on the source", func() {
			agentPresignSetSourcePresign(agentPresignPropNS, "true")

			Eventually(func() (string, error) {
				return agentPresignVersionField(agentPresignPropNS, agentPresignPropVersion,
					"{.spec.agentSourceRef.storageConfig.presign.enabled}")
			}, 60*time.Second, 1*time.Second).Should(Equal("true"))

			status, headers, _ := agentPresignGet(client, agentPresignPropArchiveURL(), "")
			Expect(status).To(Equal(http.StatusTemporaryRedirect))
			Expect(headers.Get("Location")).NotTo(BeEmpty())
		})

		It("proxies the archive again after presign is disabled on the source", func() {
			agentPresignSetSourcePresign(agentPresignPropNS, "false")

			Eventually(func() (string, error) {
				return agentPresignVersionField(agentPresignPropNS, agentPresignPropVersion,
					"{.spec.agentSourceRef.storageConfig.presign.enabled}")
			}, 60*time.Second, 1*time.Second).Should(Equal("false"))

			status, headers, body := agentPresignGet(client, agentPresignPropArchiveURL(), "")
			Expect(status).To(Equal(http.StatusOK))
			Expect(headers.Get("Location")).To(BeEmpty())
			Expect(agentPresignChecksumOf(body)).To(Equal(checksum))
		})
	})

	Context("OIDC auth mode with presign", Ordered, func() {
		var (
			pfServerCancel   context.CancelFunc
			pfDexCancel      context.CancelFunc
			pfEmulatorCancel context.CancelFunc
			client           *http.Client
			caPath           string
			checksum         string
			oidcJWT          string
			presignedURL     string
		)

		BeforeAll(func() {
			clientSecret := agentE2ESecretOr("OPENDEPOT_AGENT_PRESIGN_E2E_CLIENT_SECRET")
			userPassword := agentE2ESecretOr("OPENDEPOT_AGENT_PRESIGN_E2E_USER_PASSWORD")

			By("uninstalling the previous release to avoid Dex server-side apply conflicts")
			agentPresignUninstall()

			By("generating a bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(userPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			ensureOIDCClientSecret(clientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "agent-presign-e2e-values.yaml")
			dexValues := fmt.Sprintf(`dex:
  enabled: true
  config:
    issuer: http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex
    storage:
      type: memory
    enablePasswordDB: true
    oauth2:
      responseTypes:
        - code
      grantTypes:
        - authorization_code
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: agentpresignuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
    connectors: []
server:
  oidc:
    enabled: true
    clientSecretName: opendepot-dex-client-secret
    dexProxy:
      enabled: false
    groupsClaim: email
`, agentPresignUserEmail, string(hashBytes), agentPresignUserID)
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0o600)).To(Succeed())

			By("deploying the server with OIDC auth, Dex, and the AWS emulator sidecar")
			agentPresignInstall(repoRoot, emulatorImage,
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
			)

			pfServerCancel = agentPresignPortForward("svc/server", serverLocalPort, 80)
			pfDexCancel = agentPresignPortForward("svc/opendepot-dex", agentPresignDexPort, 5556)
			pfEmulatorCancel = agentPresignPortForward("deploy/server", agentPresignEmulatorPort, agentPresignEmulatorPort)

			var credsPath string
			caPath, credsPath = agentPresignEmulatorFiles(GinkgoT().TempDir())
			client = agentPresignClient(caPath)
			agentPresignWaitForEndpoint(client, fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", serverLocalPort), "server")
			agentPresignWaitForEndpoint(client, fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", agentPresignDexPort), "Dex")
			agentPresignWaitForEndpoint(client, fmt.Sprintf("https://localhost:%d/", agentPresignEmulatorPort), "emulator")

			By("staging the private Agent archive in the emulator bucket")
			agentPresignEnsureNamespace(agentPresignPrivateNS, false)
			bucket := fmt.Sprintf("presign-private-%d", time.Now().UnixNano())
			checksum = agentPresignStageArchive(agentPresignS3Client(caPath, credsPath), bucket)

			agentPresignApply(agentPresignVersionManifest(agentPresignPrivateNS, bucket, true))
			agentPresignPatchStatus(agentPresignPrivateNS, fmt.Sprintf(
				`{"status":{"synced":true,"syncStatus":"Synced","fileName":%q,"checksum":%q}}`,
				agentPresignFileName, checksum))

			By("acquiring a Dex JWT via the password grant")
			Eventually(func() error {
				token, err := agentAcquireDexToken(agentPresignDexPort, clientSecret, agentPresignUserEmail, userPassword)
				if err != nil {
					return err
				}
				oidcJWT = token

				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT")
		})

		AfterAll(func() {
			if pfServerCancel != nil {
				pfServerCancel()
			}
			if pfDexCancel != nil {
				pfDexCancel()
			}
			if pfEmulatorCancel != nil {
				pfEmulatorCancel()
			}

			cmd := exec.Command("kubectl", "delete", "groupbinding", agentPresignBindingName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("denies a private Agent without a token", func() {
			status, headers, _ := agentPresignGet(client,
				fmt.Sprintf("http://localhost:%d%s", serverLocalPort, agentPresignArchivePath(agentPresignPrivateNS)), "")
			Expect(status).To(Equal(http.StatusUnauthorized))
			Expect(headers.Get("Location")).To(BeEmpty())
		})

		It("redirects a bound request to a presigned URL with private no-store caching", func() {
			agentPresignApply(agentPresignBindingManifest())

			status, headers, _ := agentPresignGet(client,
				fmt.Sprintf("http://localhost:%d%s", serverLocalPort, agentPresignArchivePath(agentPresignPrivateNS)), oidcJWT)
			Expect(status).To(Equal(http.StatusTemporaryRedirect))
			Expect(headers.Get("Cache-Control")).To(Equal("private, no-store"))

			presignedURL = headers.Get("Location")
			Expect(presignedURL).To(HavePrefix(fmt.Sprintf("https://localhost:%d/", agentPresignEmulatorPort)))
		})

		It("serves the presigned archive with the checksum verified", func() {
			Expect(presignedURL).NotTo(BeEmpty())

			status, _, body := agentPresignGet(client, presignedURL, "")
			Expect(status).To(Equal(http.StatusOK))
			Expect(agentPresignChecksumOf(body)).To(Equal(checksum))
		})

		It("rejects a presigned URL with a tampered signature", func() {
			Expect(presignedURL).NotTo(BeEmpty())

			signatureStart := strings.Index(presignedURL, "X-Amz-Signature=")
			Expect(signatureStart).To(BeNumerically(">", 0))

			signature := presignedURL[signatureStart+len("X-Amz-Signature="):]
			if end := strings.IndexAny(signature, "&"); end >= 0 {
				signature = signature[:end]
			}

			replacement := "0"
			if signature[len(signature)-1] == '0' {
				replacement = "1"
			}
			tampered := strings.Replace(presignedURL, signature, signature[:len(signature)-1]+replacement, 1)

			status, _, _ := agentPresignGet(client, tampered, "")
			Expect(status).To(Equal(http.StatusForbidden))
		})

		It("rejects a presigned URL after its TTL has elapsed", func() {
			status, headers, _ := agentPresignGet(client,
				fmt.Sprintf("http://localhost:%d%s", serverLocalPort, agentPresignArchivePath(agentPresignPrivateNS)), oidcJWT)
			Expect(status).To(Equal(http.StatusTemporaryRedirect))

			time.Sleep(4 * time.Second)

			status, _, _ = agentPresignGet(client, headers.Get("Location"), "")
			Expect(status).To(Equal(http.StatusForbidden))
		})

		It("denies a revoked GroupBinding without a redirect", func() {
			cmd := exec.Command("kubectl", "delete", "groupbinding", agentPresignBindingName, "-n", namespace, "--ignore-not-found")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			status, headers, _ := agentPresignGet(client,
				fmt.Sprintf("http://localhost:%d%s", serverLocalPort, agentPresignArchivePath(agentPresignPrivateNS)), oidcJWT)
			Expect(status).To(Equal(http.StatusForbidden))
			Expect(headers.Get("Location")).To(BeEmpty())
		})

		It("fails closed on a checksum mismatch without a redirect", func() {
			agentPresignApply(agentPresignBindingManifest())

			mismatch := base64.StdEncoding.EncodeToString(make([]byte, sha256.Size))
			agentPresignPatchStatus(agentPresignPrivateNS, fmt.Sprintf(`{"status":{"checksum":%q}}`, mismatch))

			status, headers, _ := agentPresignGet(client,
				fmt.Sprintf("http://localhost:%d%s", serverLocalPort, agentPresignArchivePath(agentPresignPrivateNS)), oidcJWT)
			Expect(status).To(Equal(http.StatusInternalServerError))
			Expect(headers.Get("Location")).To(BeEmpty())
		})
	})
})
