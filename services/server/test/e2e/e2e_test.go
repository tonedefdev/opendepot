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
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
		serverRepo, serverTag = splitImageRef(serverImage)
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
})
