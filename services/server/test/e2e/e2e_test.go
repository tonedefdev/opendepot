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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

const (
	// serverLocalPort is the local port used for kubectl port-forward to the server Service.
	serverLocalPort = 19080
	// moduleNamespace is the namespace used when constructing module endpoint URLs.
	moduleNamespace = "opendepot-system"
	// moduleName is a non-existent module name used to probe the auth layer without
	// requiring real module data to be present.
	moduleName = "nonexistent-test-module"
	// moduleSystem is the provider/system segment of the module versions URL.
	moduleSystem = "aws"
	// tofuRegistryHost is the hostname used in tofu CLI integration tests.
	// *.localtest.me resolves to 127.0.0.1 via public DNS, so tofu init on the
	// test host reaches the kubectl port-forward tunnel at serverLocalPort without
	// any special DNS configuration inside the Kind cluster.
	tofuRegistryHost = "opendepot.localtest.me"
)

var _ = Describe("Server Authentication", Ordered, func() {
	var (
		chartPath  string
		serverRepo string
		serverTag  string
	)

	BeforeAll(func() {
		var err error
		chartPath, err = utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())
		serverRepo, serverTag = utils.SplitImageRef(serverImage)
	})

	// deployServer runs helm upgrade with a common base configuration plus any
	// extra --set flags supplied by the caller (e.g. auth-mode flags).
	deployServer := func(extraArgs ...string) {
		baseArgs := []string{
			"upgrade", helmReleaseName, chartPath,
			"--install",
			"--create-namespace",
			"--namespace", namespace,
			"--skip-crds",
			"--set", "global.image.tag=",
			"--set", "depot.enabled=false",
			"--set", "module.enabled=false",
			"--set", "provider.enabled=false",
			"--set", "version.enabled=false",
			"--set", "server.enabled=true",
			"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
			"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
			"--wait",
			"--timeout", "2m",
		}
		args := append(baseArgs, extraArgs...)
		cmd := exec.Command("helm", args...)
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to helm upgrade server")
	}

	// startPortForward starts a self-restarting kubectl port-forward to the server
	// Service and blocks until the port-forward tunnel is established. It returns a
	// cancel function that must be called (in AfterAll) to stop the port-forward.
	//
	// Auto-restart is needed because a Helm upgrade replaces the backing Pod during
	// the rollout. The port-forward process may connect to the terminating old Pod and
	// exit with "pod is not running"; we simply restart it until the new Pod is ready.
	startPortForward := func() context.CancelFunc {
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
					"svc/server",
					fmt.Sprintf("%d:80", serverLocalPort),
				)
				cmd.Stdout = GinkgoWriter
				cmd.Stderr = GinkgoWriter
				_ = cmd.Run()
				// Brief pause before restarting to avoid port-already-in-use races.
				time.Sleep(200 * time.Millisecond)
			}
		}()

		// Poll the service-discovery endpoint (no auth required) until the
		// port-forward tunnel is established.
		Eventually(func() error {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", serverLocalPort))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			return nil
		}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for server port-forward to be ready")

		return cancel
	}

	// stopPortForward cancels the context returned by startPortForward, which causes
	// the background goroutine to stop restarting kubectl port-forward.
	stopPortForward := func(cancel context.CancelFunc) {
		if cancel != nil {
			cancel()
		}
	}

	// moduleVersionsURL returns the module versions endpoint URL for the test module.
	moduleVersionsURL := func() string {
		return fmt.Sprintf("http://localhost:%d/opendepot/modules/v1/%s/%s/%s/versions",
			serverLocalPort, moduleNamespace, moduleName, moduleSystem)
	}

	Context("anonymous auth mode", Ordered, func() {
		var pfCancel context.CancelFunc

		BeforeAll(func() {
			By("deploying server with --anonymous-auth=true")
			deployServer(
				"--set", "server.anonymousAuth=true",
				"--set", "server.useBearerToken=false",
			)
			pfCancel = startPortForward()
		})

		AfterAll(func() {
			stopPortForward(pfCancel)
		})

		It("should return a non-401 response without an Authorization header", func() {
			resp, err := http.Get(moduleVersionsURL())
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"anonymous auth mode must not challenge clients with 401")
		})
	})

	Context("bearer token auth mode", Ordered, func() {
		var (
			pfCancel    context.CancelFunc
			bearerToken string
		)

		BeforeAll(func() {
			By("deploying server with --use-bearer-token=true")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=true",
			)
			pfCancel = startPortForward()

			By("creating a ServiceAccount with RBAC to list modules")
			saCmd := exec.Command("kubectl", "create", "serviceaccount", "test-authn-sa",
				"-n", namespace)
			_, _ = utils.Run(saCmd)

			roleCmd := exec.Command("kubectl", "create", "role", "test-authn-role",
				"--verb=get,list",
				"--resource=modules.opendepot.defdev.io",
				"-n", namespace)
			_, _ = utils.Run(roleCmd)

			rbCmd := exec.Command("kubectl", "create", "rolebinding", "test-authn-rb",
				"--role=test-authn-role",
				"--serviceaccount="+namespace+":test-authn-sa",
				"-n", namespace)
			_, _ = utils.Run(rbCmd)

			By("generating a bearer token from the test-authn-sa ServiceAccount")
			tokenCmd := exec.Command("kubectl", "create", "token", "test-authn-sa",
				"-n", namespace, "--duration=10m")
			token, err := utils.Run(tokenCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create bearer token")
			bearerToken = strings.TrimSpace(token)
		})

		AfterAll(func() {
			stopPortForward(pfCancel)

			for _, res := range [][]string{
				{"delete", "rolebinding", "test-authn-rb", "-n", namespace, "--ignore-not-found"},
				{"delete", "role", "test-authn-role", "-n", namespace, "--ignore-not-found"},
				{"delete", "serviceaccount", "test-authn-sa", "-n", namespace, "--ignore-not-found"},
				{"delete", "serviceaccount", "test-denied-sa", "-n", namespace, "--ignore-not-found"},
			} {
				cmd := exec.Command("kubectl", res...)
				_, _ = utils.Run(cmd)
			}
		})

		It("should return 401 when the Authorization header is missing", func() {
			resp, err := http.Get(moduleVersionsURL())
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"missing Authorization header must produce a 401")
		})

		It("should return a non-401 response with a valid bearer token", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+bearerToken)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"a valid bearer token must not produce a 401")
		})

		It("should return 403 for a token from an SA with no RBAC", func() {
			By("creating a ServiceAccount with no RBAC")
			saCmd := exec.Command("kubectl", "create", "serviceaccount", "test-denied-sa",
				"-n", namespace)
			_, _ = utils.Run(saCmd)

			By("generating a token for the SA with no RBAC")
			tokenCmd := exec.Command("kubectl", "create", "token", "test-denied-sa",
				"-n", namespace, "--duration=10m")
			deniedToken, err := utils.Run(tokenCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create token for denied SA")
			deniedToken = strings.TrimSpace(deniedToken)

			By("verifying the server propagates the Kubernetes 403 back to the client")
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+deniedToken)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a token for an SA with no RBAC must produce a 403")
		})
	})

	Context("kubeconfig auth mode", Ordered, func() {
		var (
			pfCancel      context.CancelFunc
			kubeconfigB64 string
		)

		BeforeAll(func() {
			By("deploying server with default kubeconfig auth mode (--anonymous-auth=false --use-bearer-token=false)")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
			)
			pfCancel = startPortForward()

			By("retrieving the cluster CA certificate")
			caCmd := exec.Command("kubectl", "get", "configmap", "kube-root-ca.crt",
				"-n", "kube-system",
				"-o", `jsonpath={.data.ca\.crt}`)
			caCert, err := utils.Run(caCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to retrieve cluster CA cert")
			caData := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(caCert)))

			By("creating a short-lived token for the server ServiceAccount")
			tokenCmd := exec.Command("kubectl", "create", "token", "server",
				"-n", namespace, "--duration=10m")
			saToken, err := utils.Run(tokenCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create SA token for kubeconfig")
			saToken = strings.TrimSpace(saToken)

			By("building an in-cluster kubeconfig")
			// The server pod resolves the API server at kubernetes.default.svc:443.
			// We embed the cluster CA and the SA token so the server can authenticate
			// using this kubeconfig from inside the cluster.
			kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: %s
    server: https://kubernetes.default.svc:443
  name: in-cluster
contexts:
- context:
    cluster: in-cluster
    namespace: %s
    user: server-sa
  name: in-cluster
current-context: in-cluster
users:
- name: server-sa
  user:
    token: %s`, caData, namespace, saToken)

			kubeconfigB64 = base64.StdEncoding.EncodeToString([]byte(kubeconfig))
		})

		AfterAll(func() {
			stopPortForward(pfCancel)
		})

		It("should return a non-401 response with a valid base64-encoded kubeconfig", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+kubeconfigB64)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"a valid base64-encoded kubeconfig must not produce a 401")
		})
	})

	// Context: tofu CLI integration
	//
	// These specs verify the behaviour of `tofu init` against the server over HTTP,
	// covering the three .tofurc configurations that users are likely to try:
	//   1. No credentials block at all          → 401 (no token sent)
	//   2. Token placed inside the host block   → 401 (silently ignored by OpenTofu)
	//   3. Token in the correct credentials block → authenticated (no 401)
	Context("tofu CLI integration", Ordered, func() {
		var tofuBin string

		BeforeAll(func() {
			var err error
			tofuBin, err = exec.LookPath("tofu")
			if err != nil {
				Skip("tofu CLI not found in PATH; skipping tofu integration tests")
			}
		})

		// runTofuInit writes a main.tf referencing a (non-existent) module from
		// the local port-forwarded server, writes rcContent as .tofurc, runs
		// `tofu init -no-color`, and returns the combined stdout+stderr output.
		// The module not existing is fine — auth failures surface before any
		// module download is attempted.
		//
		// The module source uses tofuRegistryHost (opendepot.localtest.me) as the
		// registry hostname. *.localtest.me resolves to 127.0.0.1 via public DNS,
		// so tofu reaches the port-forward tunnel on the test host without any
		// in-cluster DNS configuration. The port goes only in the service URL
		// inside the host block — not in the module source address.
		runTofuInit := func(rcContent string) string {
			workDir := GinkgoT().TempDir()
			mainTF := fmt.Sprintf(`module "test" {
  source = "%s/%s/%s/%s"
}
`, tofuRegistryHost, moduleNamespace, moduleName, moduleSystem)
			ExpectWithOffset(1, os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(mainTF), 0600)).To(Succeed())

			rcFile := filepath.Join(GinkgoT().TempDir(), ".tofurc")
			ExpectWithOffset(1, os.WriteFile(rcFile, []byte(rcContent), 0600)).To(Succeed())

			cmd := exec.Command(tofuBin, "init", "-no-color")
			cmd.Dir = workDir
			cmd.Env = append(os.Environ(), "TF_CLI_CONFIG_FILE="+rcFile)
			output, _ := cmd.CombinedOutput()
			_, _ = fmt.Fprintf(GinkgoWriter, "tofu init output:\n%s\n", output)
			return string(output)
		}

		Context("anonymous auth over HTTP", Ordered, func() {
			var pfCancel context.CancelFunc

			BeforeAll(func() {
				By("deploying server with --anonymous-auth=true")
				deployServer(
					"--set", "server.anonymousAuth=true",
					"--set", "server.useBearerToken=false",
				)
				pfCancel = startPortForward()
			})

			AfterAll(func() {
				stopPortForward(pfCancel)
			})

			It("tofu init without credentials must not fail with a 401", func() {
				// No credentials block — anonymous auth accepts unauthenticated requests.
				rc := fmt.Sprintf(`host "%s" {
  services = {
    "modules.v1" = "http://%s:%d/opendepot/modules/v1/"
  }
}
`, tofuRegistryHost, tofuRegistryHost, serverLocalPort)
				output := runTofuInit(rc)
				Expect(output).NotTo(ContainSubstring("401"),
					"anonymous auth must not produce a 401 during tofu init")
				Expect(output).NotTo(ContainSubstring("missing Authorization header"),
					"anonymous auth must not produce an auth error during tofu init")
			})
		})

		Context("bearer token auth over HTTP", Ordered, func() {
			var (
				pfCancel    context.CancelFunc
				bearerToken string
			)

			BeforeAll(func() {
				By("deploying server with --use-bearer-token=true")
				deployServer(
					"--set", "server.anonymousAuth=false",
					"--set", "server.useBearerToken=true",
				)
				pfCancel = startPortForward()

				By("generating a bearer token from the server ServiceAccount")
				tokenCmd := exec.Command("kubectl", "create", "token", "server",
					"-n", namespace, "--duration=10m")
				token, err := utils.Run(tokenCmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create bearer token")
				bearerToken = strings.TrimSpace(token)
			})

			AfterAll(func() {
				stopPortForward(pfCancel)
			})

			It("tofu init without a credentials block in .tofurc fails with 401", func() {
				// No credentials block — tofu sends no Authorization header.
				rc := fmt.Sprintf(`host "%s" {
  services = {
    "modules.v1" = "http://%s:%d/opendepot/modules/v1/"
  }
}
`, tofuRegistryHost, tofuRegistryHost, serverLocalPort)
				output := runTofuInit(rc)
				Expect(output).To(
					Or(ContainSubstring("401"), ContainSubstring("missing Authorization header")),
					"tofu init without credentials must produce a 401 in bearer token mode",
				)
			})

			It("tofu init with token placed in the host block fails with 401 (common misconfiguration)", func() {
				// Placing `token` directly inside the `host` block is valid HCL but
				// OpenTofu silently ignores it — credentials must live in a separate
				// `credentials` block.  This is the root cause of the quickstart
				// "missing Authorization header" issue.
				rc := fmt.Sprintf("host \"%s\" {\n  services = {\n    \"modules.v1\" = \"http://%s:%d/opendepot/modules/v1/\"\n  }\n  token = %q\n}\n",
					tofuRegistryHost, tofuRegistryHost, serverLocalPort, bearerToken)
				output := runTofuInit(rc)
				Expect(output).To(
					Or(ContainSubstring("401"), ContainSubstring("missing Authorization header")),
					"token inside host block must not be sent; server must return 401",
				)
			})

			It("tofu init with token in a credentials block must not produce a 401", func() {
				// Correct configuration: `credentials` block keyed to the registry
				// hostname, plus `host` block to configure the HTTP service URL.
				// OpenTofu reads the `credentials` block and includes the token in
				// the Authorization header for all requests to that hostname.
				rc := fmt.Sprintf("credentials \"%s\" {\n  token = %q\n}\nhost \"%s\" {\n  services = {\n    \"modules.v1\" = \"http://%s:%d/opendepot/modules/v1/\"\n  }\n}\n",
					tofuRegistryHost, bearerToken, tofuRegistryHost, tofuRegistryHost, serverLocalPort)
				output := runTofuInit(rc)
				Expect(output).NotTo(
					Or(ContainSubstring("401"), ContainSubstring("missing Authorization header")),
					"token in credentials block must be forwarded to the server; must not return 401",
				)
			})
		})
	})

	Context("OIDC auth mode", Ordered, func() {
		var (
			pfCancelServer context.CancelFunc
			pfCancelDex    context.CancelFunc
			oidcJWT        string
		)

		const (
			dexLocalPort     = 15556
			testClientSecret = "test-client-secret-for-e2e"
			testUserEmail    = "test@example.com"
			testUserPassword = "testpassword"
			testUserID       = "test-user-oidc-e2e"
		)

		// startDexPortForward starts a self-restarting kubectl port-forward to the Dex
		// service and blocks until the OIDC discovery endpoint is reachable.
		startDexPortForward := func(localPort int) context.CancelFunc {
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
						"svc/opendepot-dex",
						fmt.Sprintf("%d:5556", localPort),
					)
					cmd.Stdout = GinkgoWriter
					cmd.Stderr = GinkgoWriter
					_ = cmd.Run()
					time.Sleep(200 * time.Millisecond)
				}
			}()
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", localPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for Dex port-forward to be ready")
			return cancel
		}

		// acquireDexToken performs a Resource Owner Password Credentials grant against the
		// Dex token endpoint and returns the raw OIDC ID token string.
		acquireDexToken := func(localPort int, clientSecret, username, password string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", localPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {username},
				"password":      {password},
				"scope":         {"openid"},
				"client_id":     {"opendepot"},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("token request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
			}
			var tokenResp struct {
				IDToken string `json:"id_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode token response: %w", err)
			}
			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("token response contained empty id_token")
			}
			return tokenResp.IDToken, nil
		}

		BeforeAll(func() {
			By("generating bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(testUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			passwordHash := string(hashBytes)

			By("writing Dex e2e values file")
			dexValues := fmt.Sprintf(`
dex:
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
        - "urn:ietf:params:oauth:grant-type:device_code"
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: testuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
          - http://localhost:10010
    connectors: []
server:
  oidc:
    enabled: true
    clientSecret: %q
`, testUserEmail, passwordHash, testUserID, testClientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "dex-e2e-values.yaml")
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC auth mode and Dex enabled")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
				"--timeout", "10m",
			)

			pfCancelServer = startPortForward()
			pfCancelDex = startDexPortForward(dexLocalPort)

			By("acquiring a valid Dex JWT via ROPC")
			Eventually(func() error {
				token, err := acquireDexToken(dexLocalPort, testClientSecret, testUserEmail, testUserPassword)
				if err != nil {
					return err
				}
				oidcJWT = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT via ROPC")
		})

		AfterAll(func() {
			stopPortForward(pfCancelServer)
			stopPortForward(pfCancelDex)
		})

		It("should return 401 when the Authorization header is missing", func() {
			resp, err := http.Get(moduleVersionsURL())
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"OIDC mode must challenge clients with 401 when no Authorization header is present")
		})

		It("should return 401 with a garbage bearer token", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer this-is-not-a-valid-jwt")
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"a garbage bearer token must produce a 401")
		})

		It("should return 401 with an expired JWT", func() {
			// Construct a well-formed but expired JWT signed with a throwaway key.
			// go-oidc will reject it because the key ID is not in Dex's JWKS,
			// which is the same rejection path as a token whose signature cannot be
			// verified (e.g. after the signing key has rotated or the token has expired).
			hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"throwaway-key"}`))
			payloadBytes, err := json.Marshal(map[string]any{
				"iss": fmt.Sprintf("http://localhost:%d/dex", dexLocalPort),
				"aud": "opendepot",
				"sub": "expired-test-user",
				"exp": time.Now().Add(-1 * time.Hour).Unix(),
				"iat": time.Now().Add(-2 * time.Hour).Unix(),
			})

			Expect(err).NotTo(HaveOccurred())
			fakeSig := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("X", 256)))
			expiredJWT := hdr + "." + base64.RawURLEncoding.EncodeToString(payloadBytes) + "." + fakeSig

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+expiredJWT)
			resp, err := http.DefaultClient.Do(req)

			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized),
				"an expired JWT must produce a 401")
		})

		// Dex static passwords do not emit a "groups" claim. With the groups claim
		// required, a valid JWT that lacks it must be denied with 403 — not 401.
		// 401 = authentication failure (bad/missing token); 403 = authorization
		// failure (valid token, access not permitted).
		It("should return 403 with a valid Dex JWT that has no groups claim", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())

			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)

			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a valid JWT with no groups claim must be denied with 403")
		})

		// The groups claim is required when OIDC is enabled. A JWT that does not carry
		// the configured claim is denied with 403 — there is no bypass path.
		It("should return 403 when the configured groups claim is absent from the JWT", func() {
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"JWT missing the groups claim must be denied with 403 (groups claim is required)")
		})

		It("service discovery should include login.v1 when OIDC is enabled", func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", serverLocalPort))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var discovery map[string]interface{}
			Expect(json.NewDecoder(resp.Body).Decode(&discovery)).To(Succeed())

			Expect(discovery).To(HaveKey("login.v1"),
				"service discovery must advertise login.v1 when OIDC is enabled")
			loginV1, ok := discovery["login.v1"].(map[string]any)

			Expect(ok).To(BeTrue(), "login.v1 must be a JSON object")
			Expect(loginV1["authz"]).NotTo(BeEmpty(), "login.v1.authz must be a non-empty URL")
			Expect(loginV1["token"]).NotTo(BeEmpty(), "login.v1.token must be a non-empty URL")
		})
	})

	// Context: OIDC auth mode with GroupBinding
	//
	// These specs verify application-level access control via GroupBinding CRDs when
	// OIDC auth mode is active. The server is deployed with --oidc-groups-claim=email
	// so that the Dex-issued JWT's "email" claim is treated as the groups value —
	// this avoids the need for a groups-capable upstream connector while still
	// exercising the full GroupBinding evaluation path.
	Context("OIDC auth mode with GroupBinding", Ordered, func() {
		var (
			pfCancelServer context.CancelFunc
			pfCancelDex    context.CancelFunc
			oidcJWT        string
		)

		const (
			gbDexLocalPort     = 15557
			gbTestClientSecret = "test-client-secret-gb-e2e"
			gbTestUserEmail    = "gb-test@example.com"
			gbTestUserPassword = "gbtestpassword"
			gbTestUserID       = "gb-test-user-oidc-e2e"
			// groupBindingName is the name used for the test GroupBinding resource.
			groupBindingName = "e2e-test-groupbinding"
		)

		// gbStartDexPortForward starts a self-restarting port-forward to the Dex service
		// and waits until the OIDC discovery endpoint is reachable.
		gbStartDexPortForward := func(localPort int) context.CancelFunc {
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
						"svc/opendepot-dex",
						fmt.Sprintf("%d:5556", localPort),
					)
					cmd.Stdout = GinkgoWriter
					cmd.Stderr = GinkgoWriter
					_ = cmd.Run()
					time.Sleep(200 * time.Millisecond)
				}
			}()
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", localPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for Dex port-forward")
			return cancel
		}

		// gbAcquireDexToken performs an ROPC grant against Dex and returns the raw ID token.
		gbAcquireDexToken := func(localPort int, clientSecret, username, password string) (string, error) {
			tokenURL := fmt.Sprintf("http://localhost:%d/dex/token", localPort)
			form := url.Values{
				"grant_type":    {"password"},
				"username":      {username},
				"password":      {password},
				"scope":         {"openid email"},
				"client_id":     {"opendepot"},
				"client_secret": {clientSecret},
			}
			resp, err := http.PostForm(tokenURL, form)
			if err != nil {
				return "", fmt.Errorf("token request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
			}
			var tokenResp struct {
				IDToken string `json:"id_token"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				return "", fmt.Errorf("failed to decode token response: %w", err)
			}
			if tokenResp.IDToken == "" {
				return "", fmt.Errorf("token response contained empty id_token")
			}
			return tokenResp.IDToken, nil
		}

		// applyGroupBinding creates or replaces a GroupBinding with the given expression
		// and optional moduleResources / providerResources patterns.
		applyGroupBinding := func(name, expression string, moduleResources, providerResources []string) {
			mrYAML := ""
			for _, m := range moduleResources {
				mrYAML += fmt.Sprintf("    - %q\n", m)
			}
			prYAML := ""
			for _, p := range providerResources {
				prYAML += fmt.Sprintf("    - %q\n", p)
			}
			manifest := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: %s
  namespace: %s
spec:
  expression: %q
  moduleResources:
%s  providerResources:
%s`, name, namespace, expression, mrYAML, prYAML)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to apply GroupBinding %s", name)
		}

		// deleteGroupBinding deletes the named GroupBinding, ignoring not-found errors.
		deleteGroupBinding := func(name string) {
			cmd := exec.Command("kubectl", "delete", "groupbinding", name, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		}

		BeforeAll(func() {
			By("uninstalling previous Helm release to avoid Dex server-side apply conflicts")
			uninstallCmd := exec.Command("helm", "uninstall", helmReleaseName,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(uninstallCmd)

			By("generating bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(gbTestUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			passwordHash := string(hashBytes)

			By("writing Dex + GroupBinding e2e values file")
			dexValues := fmt.Sprintf(`
dex:
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
        - "urn:ietf:params:oauth:grant-type:device_code"
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: gbtestuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
          - http://localhost:10010
    connectors: []
server:
  oidc:
    enabled: true
    clientSecret: %q
    groupsClaim: email
`, gbTestUserEmail, passwordHash, gbTestUserID, gbTestClientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "gb-e2e-values.yaml")
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC auth mode, Dex enabled, and --oidc-groups-claim=email")
			deployServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
				"--timeout", "10m",
			)

			pfCancelServer = startPortForward()
			pfCancelDex = gbStartDexPortForward(gbDexLocalPort)

			By("acquiring a valid Dex JWT via ROPC")
			Eventually(func() error {
				token, err := gbAcquireDexToken(gbDexLocalPort, gbTestClientSecret, gbTestUserEmail, gbTestUserPassword)
				if err != nil {
					return err
				}
				oidcJWT = token
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT via ROPC")
		})

		AfterAll(func() {
			stopPortForward(pfCancelServer)
			stopPortForward(pfCancelDex)
			deleteGroupBinding(groupBindingName)
		})

		It("should return 403 with a valid JWT when no GroupBinding matches", func() {
			// No GroupBinding exists yet — findGroupBinding should return no match → 403.
			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"a valid JWT with no matching GroupBinding must produce a 403")
		})

		It("should return non-403 when a matching GroupBinding allows all modules", func() {
			// The email claim is used as the groups value; the expression checks for the user's email.
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"*"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"a valid JWT with a matching GroupBinding and wildcard moduleResources must not produce a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"a valid JWT with a matching GroupBinding must not produce a 401")
		})

		It("should return 403 when the module name does not match any moduleResources pattern", func() {
			// Replace the GroupBinding with one that has a narrower module pattern
			// that does NOT match "nonexistent-test-module".
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"only-this-exact-module"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"module name that does not match moduleResources must produce a 403")
		})

		It("should allow access when module name matches a glob pattern in moduleResources", func() {
			// "nonexistent-*" should match "nonexistent-test-module".
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"nonexistent-*"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"a glob moduleResources pattern matching the module must not produce a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"a glob moduleResources pattern matching the module must not produce a 401")
		})

		It("should return 403 when a GroupBinding has an invalid expression", func() {
			// An invalid expression should be skipped (Warn logged) and no binding matches → 403.
			applyGroupBinding(groupBindingName,
				"this is [[ not valid expr",
				[]string{"*"},
				[]string{},
			)

			req, err := http.NewRequest(http.MethodGet, moduleVersionsURL(), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"an invalid GroupBinding expression must be skipped and produce a 403 (not a 500)")
		})

		It("should return 403 for a provider endpoint when the provider is not in providerResources", func() {
			// Allow the expression to match but restrict providerResources to something
			// that does not match the provider type "aws".
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{},
				[]string{"only-this-provider"},
			)

			providerURL := fmt.Sprintf("http://localhost:%d/opendepot/providers/v1/%s/aws/versions",
				serverLocalPort, namespace)
			req, err := http.NewRequest(http.MethodGet, providerURL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"provider type not in providerResources must produce a 403")
		})

		It("should allow provider access when provider type matches providerResources", func() {
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{},
				[]string{"aws"},
			)

			providerURL := fmt.Sprintf("http://localhost:%d/opendepot/providers/v1/%s/aws/versions",
				serverLocalPort, namespace)
			req, err := http.NewRequest(http.MethodGet, providerURL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"provider type matching providerResources must not produce a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"provider type matching providerResources must not produce a 401")
		})

		It("should allow all provider access when providerResources contains \"*\"", func() {
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{},
				[]string{"*"},
			)

			providerURL := fmt.Sprintf("http://localhost:%d/opendepot/providers/v1/%s/aws/versions",
				serverLocalPort, namespace)
			req, err := http.NewRequest(http.MethodGet, providerURL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).NotTo(Equal(http.StatusForbidden),
				"providerResources [\"*\"] must allow all provider types without producing a 403")
			Expect(resp.StatusCode).NotTo(Equal(http.StatusUnauthorized),
				"providerResources [\"*\"] must not produce a 401")
		})

		It("should return 403 when providerResources contains a partial pattern that does not exactly match", func() {
			// "aws*" is NOT a glob in providerResources — it is treated as a literal string.
			// A request for provider type "aws" must not be allowed.
			applyGroupBinding(groupBindingName,
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{},
				[]string{"aws*"},
			)

			providerURL := fmt.Sprintf("http://localhost:%d/opendepot/providers/v1/%s/aws/versions",
				serverLocalPort, namespace)
			req, err := http.NewRequest(http.MethodGet, providerURL, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"partial pattern \"aws*\" must not be expanded as a glob; only the literal \"*\" allows all providers")
		})

		// First-match semantics: when multiple GroupBindings match the JWT groups,
		// the first one in alphabetical-name order wins. If that binding denies the
		// resource, the request is 403 even if a later binding would allow it.
		It("should use first-match semantics when multiple GroupBindings match", func() {
			// aaa-e2e-groupbinding lists first (alphabetical); it allows only "only-this-exact-module".
			applyGroupBinding("aaa-e2e-groupbinding",
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"only-this-exact-module"},
				[]string{},
			)
			// zzz-e2e-groupbinding lists second; it allows everything.
			applyGroupBinding("zzz-e2e-groupbinding",
				fmt.Sprintf(`"%s" in groups`, gbTestUserEmail),
				[]string{"*"},
				[]string{},
			)

			// Request for a module NOT in aaa's moduleResources — aaa matches first
			// and denies it, so the result must be 403 regardless of zzz.
			req, err := http.NewRequest(http.MethodGet,
				fmt.Sprintf("http://localhost:%d/opendepot/modules/v1/%s/other-module/aws/versions",
					serverLocalPort, namespace),
				nil,
			)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+oidcJWT)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden),
				"first-match binding (aaa) denies the resource; zzz binding must not override it")

			// Clean up both bindings.
			deleteGroupBinding("aaa-e2e-groupbinding")
			deleteGroupBinding("zzz-e2e-groupbinding")
		})
	})
})
