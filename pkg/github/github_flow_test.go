package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v81/github"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newPKCS1PEM returns a PKCS1 RSA private key in PEM form for signing app JWTs in tests.
func newPKCS1PEM(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// stubDefaultTransport swaps http.DefaultTransport for the test duration.
func stubDefaultTransport(t *testing.T, fn func(*http.Request) (*http.Response, error)) {
	t.Helper()

	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = goModStubTransport(fn)
}

func stubResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func testVersion(ref string) *opendepotv1alpha1.Version {
	name := "module"

	return &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{
			Version: ref,
			ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{
				RepoOwner: "owner",
				Name:      &name,
			},
		},
	}
}

func TestGetModuleArchiveFromRef(t *testing.T) {
	archive := "archive-bytes"
	wantSum := sha256.Sum256([]byte(archive))
	wantChecksum := base64.StdEncoding.EncodeToString(wantSum[:])

	// The archive link redirects to /archive/{ref}. Only the bare ref is served, so the
	// 'v'-prefixed attempt receives a 404 and the function retries without the prefix.
	newServer := func(t *testing.T, archiveStatus map[string]int) *github.Client {
		t.Helper()

		mux := http.NewServeMux()
		mux.HandleFunc("/repos/owner/module/tarball/", func(w http.ResponseWriter, r *http.Request) {
			ref := strings.TrimPrefix(r.URL.Path, "/repos/owner/module/tarball/")
			w.Header().Set("Location", "http://"+r.Host+"/archive/"+ref)
			w.WriteHeader(http.StatusFound)
		})
		mux.HandleFunc("/archive/", func(w http.ResponseWriter, r *http.Request) {
			ref := strings.TrimPrefix(r.URL.Path, "/archive/")
			if status, ok := archiveStatus[ref]; ok && status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			_, _ = io.WriteString(w, archive)
		})

		return newTestGithubClient(t, mux)
	}

	t.Run("returns archive and checksum for the v-prefixed ref", func(t *testing.T) {
		client := newServer(t, map[string]int{})

		data, checksum, err := GetModuleArchiveFromRef(context.Background(), logr.Discard(), client,
			testVersion("v1.0.0"), github.Tarball)
		if err != nil {
			t.Fatalf("GetModuleArchiveFromRef returned error: %v", err)
		}

		if string(data) != archive {
			t.Fatalf("archive = %q, want %q", data, archive)
		}

		if checksum == nil || *checksum != wantChecksum {
			t.Fatalf("checksum = %v, want %s", checksum, wantChecksum)
		}
	})

	t.Run("adds v prefix and retries without it on 404", func(t *testing.T) {
		client := newServer(t, map[string]int{"v1.0.0": http.StatusNotFound})

		data, checksum, err := GetModuleArchiveFromRef(context.Background(), logr.Discard(), client,
			testVersion("1.0.0"), github.Tarball)
		if err != nil {
			t.Fatalf("GetModuleArchiveFromRef returned error: %v", err)
		}

		if string(data) != archive || checksum == nil || *checksum != wantChecksum {
			t.Fatalf("retry returned data %q checksum %v", data, checksum)
		}
	})

	t.Run("returns status error for non-404 failures", func(t *testing.T) {
		client := newServer(t, map[string]int{"v1.0.0": http.StatusInternalServerError})

		_, _, err := GetModuleArchiveFromRef(context.Background(), logr.Discard(), client,
			testVersion("1.0.0"), github.Tarball)
		if err == nil || !strings.Contains(err.Error(), "status code 500") {
			t.Fatalf("GetModuleArchiveFromRef error = %v, want status code 500", err)
		}
	})

	t.Run("rejects unsafe module repo names", func(t *testing.T) {
		client := newServer(t, map[string]int{})
		version := testVersion("1.0.0")
		bad := "../escape"
		version.Spec.ModuleConfigRef.Name = &bad

		_, _, err := GetModuleArchiveFromRef(context.Background(), logr.Discard(), client, version, github.Tarball)
		if err == nil {
			t.Fatal("GetModuleArchiveFromRef returned no error for unsafe repo name")
		}
	})
}

func TestGetArchiveRequestErrors(t *testing.T) {
	t.Run("rejects non-redirect archive link responses", func(t *testing.T) {
		client := newTestGithubClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		_, err := GetArchiveRequest(context.Background(), client, "owner", "repo", github.Tarball, "v1.0.0")
		if err == nil || !strings.Contains(err.Error(), "status code 200") {
			t.Fatalf("GetArchiveRequest error = %v, want status code error", err)
		}
	})

	t.Run("reports failed archive download", func(t *testing.T) {
		client := newTestGithubClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "http://127.0.0.1:1/unreachable")
			w.WriteHeader(http.StatusFound)
		}))

		_, err := GetArchiveRequest(context.Background(), client, "owner", "repo", github.Tarball, "v1.0.0")
		if err == nil || !strings.Contains(err.Error(), "failed to execute HTTP request for archive link") {
			t.Fatalf("GetArchiveRequest error = %v, want execute error", err)
		}
	})

	t.Run("rejects invalid owner", func(t *testing.T) {
		_, err := GetArchiveRequest(context.Background(), github.NewClient(nil), "bad/owner", "repo", github.Tarball, "v1.0.0")
		if err == nil {
			t.Fatal("GetArchiveRequest returned no error for invalid owner")
		}
	})

	t.Run("rejects invalid ref", func(t *testing.T) {
		_, err := GetArchiveRequest(context.Background(), github.NewClient(nil), "owner", "repo", github.Tarball, "../x")
		if err == nil {
			t.Fatal("GetArchiveRequest returned no error for invalid ref")
		}
	})
}

func TestListMatchingTagsAPIError(t *testing.T) {
	client := newTestGithubClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	if _, err := ListMatchingTags(context.Background(), client, "owner", "repo", "", ""); err == nil {
		t.Fatal("ListMatchingTags returned no error for API failure")
	}

	if _, err := ListMatchingTags(context.Background(), client, "bad/owner", "repo", "", ""); err == nil {
		t.Fatal("ListMatchingTags returned no error for invalid owner")
	}
}

func TestGetModuleReadme(t *testing.T) {
	readme := base64.StdEncoding.EncodeToString([]byte("# Module"))

	newClient := func(t *testing.T, refs map[string]bool) *github.Client {
		t.Helper()

		return newTestGithubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ref := r.URL.Query().Get("ref")
			if !refs[ref] {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			fmt.Fprintf(w, `{"type":"file","encoding":"base64","content":%q}`, readme)
		}))
	}

	t.Run("returns README for v-prefixed ref", func(t *testing.T) {
		got, err := GetModuleReadme(context.Background(), newClient(t, map[string]bool{"v1.0.0": true}), "owner", "repo", "1.0.0")
		if err != nil {
			t.Fatalf("GetModuleReadme returned error: %v", err)
		}

		if string(got) != "# Module" {
			t.Fatalf("GetModuleReadme = %q", got)
		}
	})

	t.Run("falls back to bare ref", func(t *testing.T) {
		got, err := GetModuleReadme(context.Background(), newClient(t, map[string]bool{"1.0.0": true}), "owner", "repo", "1.0.0")
		if err != nil {
			t.Fatalf("GetModuleReadme returned error: %v", err)
		}

		if string(got) != "# Module" {
			t.Fatalf("GetModuleReadme = %q", got)
		}
	})

	t.Run("reports missing README with last error", func(t *testing.T) {
		_, err := GetModuleReadme(context.Background(), newClient(t, map[string]bool{}), "owner", "repo", "1.0.0")
		if err == nil || !strings.Contains(err.Error(), "README not found in owner/repo at version 1.0.0") {
			t.Fatalf("GetModuleReadme error = %v, want README not found", err)
		}
	})

	t.Run("rejects invalid repo name and ref", func(t *testing.T) {
		client := newClient(t, map[string]bool{})

		if _, err := GetModuleReadme(context.Background(), client, "bad/owner", "repo", "1.0.0"); err == nil {
			t.Fatal("GetModuleReadme returned no error for invalid owner")
		}

		if _, err := GetModuleReadme(context.Background(), client, "owner", "repo", "../1.0.0"); err == nil {
			t.Fatal("GetModuleReadme returned no error for invalid ref")
		}
	})
}

func TestGetProviderGoModFromAPIAndRaw(t *testing.T) {
	gomodContent := base64.StdEncoding.EncodeToString([]byte("module example.com/provider"))

	t.Run("returns raw go.mod when the CDN responds", func(t *testing.T) {
		previous := http.DefaultClient.Transport
		t.Cleanup(func() { http.DefaultClient.Transport = previous })
		http.DefaultClient.Transport = goModStubTransport(func(req *http.Request) (*http.Response, error) {
			return stubResponse(req, http.StatusOK, "module raw"), nil
		})

		got, err := GetProviderGoMod(context.Background(), github.NewClient(nil), "owner", "repo", "1.0.0")
		if err != nil {
			t.Fatalf("GetProviderGoMod returned error: %v", err)
		}

		if string(got) != "module raw" {
			t.Fatalf("GetProviderGoMod = %q", got)
		}
	})

	t.Run("falls back to the GitHub API when the CDN misses", func(t *testing.T) {
		previous := http.DefaultClient.Transport
		t.Cleanup(func() { http.DefaultClient.Transport = previous })
		http.DefaultClient.Transport = goModStubTransport(func(req *http.Request) (*http.Response, error) {
			return stubResponse(req, http.StatusNotFound, ""), nil
		})

		client := newTestGithubClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("ref") != "1.0.0" {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			fmt.Fprintf(w, `{"type":"file","encoding":"base64","content":%q}`, gomodContent)
		}))

		got, err := GetProviderGoMod(context.Background(), client, "owner", "repo", "1.0.0")
		if err != nil {
			t.Fatalf("GetProviderGoMod returned error: %v", err)
		}

		if string(got) != "module example.com/provider" {
			t.Fatalf("GetProviderGoMod = %q", got)
		}
	})

	t.Run("reports go.mod not found", func(t *testing.T) {
		previous := http.DefaultClient.Transport
		t.Cleanup(func() { http.DefaultClient.Transport = previous })
		http.DefaultClient.Transport = goModStubTransport(func(req *http.Request) (*http.Response, error) {
			return stubResponse(req, http.StatusNotFound, ""), nil
		})

		client := newTestGithubClient(t, http.NotFoundHandler())
		_, err := GetProviderGoMod(context.Background(), client, "owner", "repo", "1.0.0")
		if err == nil || !strings.Contains(err.Error(), "go.mod not found") {
			t.Fatalf("GetProviderGoMod error = %v, want not found", err)
		}
	})

	t.Run("rejects invalid repo name", func(t *testing.T) {
		if _, err := GetProviderGoMod(context.Background(), github.NewClient(nil), "bad/owner", "repo", "1.0.0"); err == nil {
			t.Fatal("GetProviderGoMod returned no error for invalid owner")
		}
	})
}

func TestJWTTransportSetsAuthorizationHeader(t *testing.T) {
	transport := &jwtTransport{
		Transport: goModStubTransport(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Authorization"); !strings.Contains(got, "jwt-token") {
				return nil, fmt.Errorf("authorization header = %q", got)
			}

			return stubResponse(req, http.StatusOK, ""), nil
		}),
		JWT: "jwt-token",
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.github.com/app", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	resp.Body.Close()
}

func TestGenerateAuthenticatedGithubClient(t *testing.T) {
	validKey := newPKCS1PEM(t)

	t.Run("rejects PEM data without an RSA block", func(t *testing.T) {
		_, err := GenerateAuthenticatedGithubClient(context.Background(), &GithubClientConfig{
			PrivateKeyData: []byte("not pem"),
		})
		if err == nil || !strings.Contains(err.Error(), "failed to decode PEM block") {
			t.Fatalf("error = %v, want PEM decode error", err)
		}
	})

	t.Run("rejects malformed RSA key bytes", func(t *testing.T) {
		malformed := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("junk")})

		_, err := GenerateAuthenticatedGithubClient(context.Background(), &GithubClientConfig{
			PrivateKeyData: malformed,
		})
		if err == nil || !strings.Contains(err.Error(), "failed to parse private key") {
			t.Fatalf("error = %v, want parse error", err)
		}
	})

	t.Run("exchanges the app JWT for an installation token", func(t *testing.T) {
		stubDefaultTransport(t, func(req *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(req.URL.Path, "/app/installations/42/access_tokens") {
				return stubResponse(req, http.StatusNotFound, ""), nil
			}

			return stubResponse(req, http.StatusCreated, `{"token":"ghs_test"}`), nil
		})

		client, err := GenerateAuthenticatedGithubClient(context.Background(), &GithubClientConfig{
			AppID:          7,
			InstallationID: 42,
			PrivateKeyData: validKey,
		})
		if err != nil {
			t.Fatalf("GenerateAuthenticatedGithubClient returned error: %v", err)
		}

		if client == nil {
			t.Fatal("GenerateAuthenticatedGithubClient returned nil client")
		}
	})

	t.Run("reports installation token failures", func(t *testing.T) {
		stubDefaultTransport(t, func(req *http.Request) (*http.Response, error) {
			return stubResponse(req, http.StatusUnauthorized, `{"message":"Bad credentials"}`), nil
		})

		_, err := GenerateAuthenticatedGithubClient(context.Background(), &GithubClientConfig{
			AppID:          7,
			InstallationID: 42,
			PrivateKeyData: validKey,
		})
		if err == nil || !strings.Contains(err.Error(), "failed to create installation token") {
			t.Fatalf("error = %v, want installation token error", err)
		}
	})
}

func TestCreateGithubClient(t *testing.T) {
	t.Run("requires config when authenticated", func(t *testing.T) {
		if _, err := CreateGithubClient(context.Background(), true, nil); err == nil {
			t.Fatal("CreateGithubClient returned no error for nil config")
		}
	})

	t.Run("returns unauthenticated client", func(t *testing.T) {
		client, err := CreateGithubClient(context.Background(), false, nil)
		if err != nil {
			t.Fatalf("CreateGithubClient returned error: %v", err)
		}

		if client == nil {
			t.Fatal("CreateGithubClient returned nil client")
		}
	})

	t.Run("wraps authenticated client failures", func(t *testing.T) {
		_, err := CreateGithubClient(context.Background(), true, &GithubClientConfig{PrivateKeyData: []byte("bad")})
		if err == nil || !strings.Contains(err.Error(), "unable to generate authenticated github client") {
			t.Fatalf("error = %v, want wrapped authentication error", err)
		}
	})
}

func TestGetGithubApplicationSecret(t *testing.T) {
	const namespace = "opendepot"
	keyB64 := base64.StdEncoding.EncodeToString([]byte("key-bytes"))

	newSecret := func(data map[string][]byte) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: opendepotv1alpha1.OpenDepotGithubSecretName, Namespace: namespace},
			Data:       data,
		}
	}

	validData := map[string][]byte{
		opendepotv1alpha1.OpenDepotGithubSecretDataFieldAppID:      []byte("123"),
		opendepotv1alpha1.OpenDepotGithubSecretDataFieldInstallID:  []byte("456"),
		opendepotv1alpha1.OpenDepotGithubSecretDataFieldPrivateKey: []byte(keyB64),
	}

	t.Run("reads app credentials from the secret", func(t *testing.T) {
		reader := fake.NewClientBuilder().WithObjects(newSecret(validData)).Build()

		cfg, err := GetGithubApplicationSecret(context.Background(), reader, namespace)
		if err != nil {
			t.Fatalf("GetGithubApplicationSecret returned error: %v", err)
		}

		if cfg.AppID != 123 || cfg.InstallationID != 456 || string(cfg.PrivateKeyData) != "key-bytes" {
			t.Fatalf("config = %+v, unexpected values", cfg)
		}
	})

	t.Run("returns error when the secret is missing", func(t *testing.T) {
		reader := fake.NewClientBuilder().Build()

		if _, err := GetGithubApplicationSecret(context.Background(), reader, namespace); err == nil {
			t.Fatal("GetGithubApplicationSecret returned no error for missing secret")
		}
	})

	t.Run("rejects non-numeric app and installation IDs", func(t *testing.T) {
		badApp := map[string][]byte{}
		for k, v := range validData {
			badApp[k] = v
		}
		badApp[opendepotv1alpha1.OpenDepotGithubSecretDataFieldAppID] = []byte("not-a-number")

		reader := fake.NewClientBuilder().WithObjects(newSecret(badApp)).Build()
		if _, err := GetGithubApplicationSecret(context.Background(), reader, namespace); err == nil {
			t.Fatal("GetGithubApplicationSecret returned no error for bad app ID")
		}

		badInstall := map[string][]byte{}
		for k, v := range validData {
			badInstall[k] = v
		}
		badInstall[opendepotv1alpha1.OpenDepotGithubSecretDataFieldInstallID] = []byte("x")

		reader = fake.NewClientBuilder().WithObjects(newSecret(badInstall)).Build()
		if _, err := GetGithubApplicationSecret(context.Background(), reader, namespace); err == nil {
			t.Fatal("GetGithubApplicationSecret returned no error for bad installation ID")
		}
	})

	t.Run("rejects undecodable private key", func(t *testing.T) {
		badKey := map[string][]byte{}
		for k, v := range validData {
			badKey[k] = v
		}
		badKey[opendepotv1alpha1.OpenDepotGithubSecretDataFieldPrivateKey] = []byte("!!!not-base64")

		reader := fake.NewClientBuilder().WithObjects(newSecret(badKey)).Build()
		if _, err := GetGithubApplicationSecret(context.Background(), reader, namespace); err == nil {
			t.Fatal("GetGithubApplicationSecret returned no error for undecodable key")
		}
	})
}
