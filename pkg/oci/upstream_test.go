package oci

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// testUpstream builds an Upstream that may reach the mock's loopback address over plain HTTP. Only in-package tests
// can request these relaxations.
func testUpstream(t *testing.T, allowed []string, dockerConfig []byte) *Upstream {
	t.Helper()

	up, err := newUpstream(allowed, dockerConfig, upstreamOptions{plainHTTP: true, allowLoopback: true})
	if err != nil {
		t.Fatalf("newUpstream: %v", err)
	}

	return up
}

func TestListTagsAndResolve(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	img := seedImage(reg)
	up := testUpstream(t, []string{reg.host()}, nil)
	repo := reg.host() + "/org/chart"

	tags, err := up.ListTags(ctx, repo)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}

	if !slices.Equal(tags, []string{"1.0.0", "latest"}) {
		t.Fatalf("ListTags = %v, want [1.0.0 latest]", tags)
	}

	desc, err := up.Resolve(ctx, repo, "1.0.0")
	if err != nil {
		t.Fatalf("Resolve tag: %v", err)
	}

	if desc.Digest != img.index.Digest {
		t.Fatalf("Resolve(1.0.0) digest = %s, want %s", desc.Digest, img.index.Digest)
	}

	if desc.MediaType != ocispec.MediaTypeImageIndex {
		t.Fatalf("Resolve media type = %s, want image index", desc.MediaType)
	}

	byDigest, err := up.Resolve(ctx, repo, img.amd64.Digest.String())
	if err != nil {
		t.Fatalf("Resolve digest: %v", err)
	}

	if byDigest.Digest != img.amd64.Digest {
		t.Fatalf("Resolve(digest) = %s, want %s", byDigest.Digest, img.amd64.Digest)
	}
}

func TestFetchManifestFiltersIndexByPlatform(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	img := seedImage(reg)
	up := testUpstream(t, []string{reg.host()}, nil)
	repo := reg.host() + "/org/chart"

	t.Run("single platform", func(t *testing.T) {
		got, err := up.FetchManifest(ctx, repo, "1.0.0", []string{"linux/arm64"})
		if err != nil {
			t.Fatalf("FetchManifest: %v", err)
		}

		if got.Descriptor.Digest != img.index.Digest || len(got.Content) == 0 {
			t.Fatalf("root descriptor/content not preserved: %+v", got.Descriptor)
		}

		if len(got.Manifests) != 1 || got.Manifests[0].Descriptor.Digest != img.arm64.Digest {
			t.Fatalf("expected only linux/arm64 manifest, got %+v", got.Manifests)
		}

		if len(got.Manifests[0].Manifest.Layers) != 1 {
			t.Fatalf("arm64 manifest layers = %d, want 1", len(got.Manifests[0].Manifest.Layers))
		}
	})

	t.Run("every platform, attestation excluded", func(t *testing.T) {
		got, err := up.FetchManifest(ctx, repo, "latest", nil)
		if err != nil {
			t.Fatalf("FetchManifest: %v", err)
		}

		digests := make([]digest.Digest, 0, len(got.Manifests))
		for _, pm := range got.Manifests {
			digests = append(digests, pm.Descriptor.Digest)
		}

		if !slices.Equal(digests, []digest.Digest{img.amd64.Digest, img.arm64.Digest}) {
			t.Fatalf("selected %v, want amd64 and arm64 only", digests)
		}
	})

	t.Run("no matching platform fails", func(t *testing.T) {
		if _, err := up.FetchManifest(ctx, repo, "latest", []string{"linux/s390x"}); err == nil {
			t.Fatal("expected error for a platform the index does not carry")
		}
	})

	t.Run("variant must match when given", func(t *testing.T) {
		if _, err := up.FetchManifest(ctx, repo, "latest", []string{"linux/arm64/v7"}); err == nil {
			t.Fatal("linux/arm64/v7 must not match a v8 entry")
		}

		got, err := up.FetchManifest(ctx, repo, "latest", []string{"linux/arm64/v8"})
		if err != nil {
			t.Fatalf("linux/arm64/v8: %v", err)
		}

		if len(got.Manifests) != 1 || got.Manifests[0].Descriptor.Digest != img.arm64.Digest {
			t.Fatalf("variant selection returned %+v", got.Manifests)
		}
	})

	t.Run("malformed platform fails", func(t *testing.T) {
		if _, err := up.FetchManifest(ctx, repo, "latest", []string{testOS}); err == nil {
			t.Fatal("platform without an arch must be rejected")
		}
	})

	t.Run("manifest by digest", func(t *testing.T) {
		got, err := up.FetchManifest(ctx, repo, img.amd64.Digest.String(), nil)
		if err != nil {
			t.Fatalf("FetchManifest by digest: %v", err)
		}

		if len(got.Manifests) != 1 || got.Manifests[0].Descriptor.Digest != img.amd64.Digest {
			t.Fatalf("expected the single amd64 manifest, got %+v", got.Manifests)
		}

		if got.Manifests[0].Manifest.Config.Digest == "" {
			t.Fatal("decoded manifest is missing its config descriptor")
		}
	})

	t.Run("unknown tag fails", func(t *testing.T) {
		if _, err := up.FetchManifest(ctx, repo, "9.9.9", nil); err == nil {
			t.Fatal("expected error for an unknown tag")
		}
	})
}

func TestFetchBlobVerifiesDigest(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	img := seedImage(reg)
	up := testUpstream(t, []string{reg.host()}, nil)
	repo := reg.host() + "/org/chart"

	rc, err := up.FetchBlob(ctx, repo, img.amd64Layer)
	if err != nil {
		t.Fatalf("FetchBlob: %v", err)
	}

	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}

	if string(got) != "layer for amd64" {
		t.Fatalf("blob content = %q", got)
	}

	reg.mu.Lock()
	reg.tampered[img.amd64Layer.Digest] = []byte("swapped by the registry")
	reg.mu.Unlock()

	rc, err = up.FetchBlob(ctx, repo, img.amd64Layer)
	if err != nil {
		// oras rejects the tampered response at fetch time (Content-Length versus the descriptor size). Either
		// rejection point is fail-closed.
		return
	}
	defer func() { _ = rc.Close() }()

	if _, err := io.ReadAll(rc); err == nil {
		t.Fatal("tampered blob must fail verification")
	}
}

func TestReferrers(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	img := seedImage(reg)
	up := testUpstream(t, []string{reg.host()}, nil)
	repo := reg.host() + "/org/chart"

	all, err := up.Referrers(ctx, repo, img.index, "")
	if err != nil {
		t.Fatalf("Referrers: %v", err)
	}

	if len(all) != 1 || all[0].Digest != img.signature.Digest {
		t.Fatalf("referrers = %+v, want the signature", all)
	}

	if all[0].ArtifactType != "application/vnd.example.signature.v1" {
		t.Fatalf("referrer artifactType = %q", all[0].ArtifactType)
	}

	filtered, err := up.Referrers(ctx, repo, img.index, "application/vnd.example.other")
	if err != nil {
		t.Fatalf("Referrers filtered: %v", err)
	}

	if len(filtered) != 0 {
		t.Fatalf("filtered referrers = %+v, want none", filtered)
	}
}

func TestUpstreamAuthenticatesWithDockerConfig(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	seedImage(reg)
	reg.basicUser, reg.basicPass = testUser, testPassword
	repo := reg.host() + "/org/chart"

	if _, err := testUpstream(t, []string{reg.host()}, nil).ListTags(ctx, repo); err == nil {
		t.Fatal("anonymous access to a protected registry must fail")
	}

	token := base64.StdEncoding.EncodeToString([]byte("robot:s3cret"))
	cfg := []byte(fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, reg.host(), token))
	tags, err := testUpstream(t, []string{reg.host()}, cfg).ListTags(ctx, repo)
	if err != nil {
		t.Fatalf("ListTags with dockerconfigjson credentials: %v", err)
	}

	if len(tags) != 2 {
		t.Fatalf("tags = %v", tags)
	}
}

func TestRedirectsAreAllowListedAndStripCredentials(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	img := seedImage(reg)
	reg.basicUser, reg.basicPass = testUser, testPassword

	var (
		authMu  sync.Mutex
		gotAuth []string
	)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authMu.Lock()
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		authMu.Unlock()
		_, _ = w.Write([]byte("layer for amd64"))
	}))
	t.Cleanup(cdn.Close)

	token := base64.StdEncoding.EncodeToString([]byte("robot:s3cret"))
	cfg := []byte(fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, reg.host(), token))
	repo := reg.host() + "/org/chart"

	reg.mu.Lock()
	reg.redirects[img.amd64Layer.Digest] = cdn.URL + "/blob"
	reg.mu.Unlock()

	up := testUpstream(t, []string{reg.host(), strings.TrimPrefix(cdn.URL, "http://")}, cfg)
	rc, err := up.FetchBlob(ctx, repo, img.amd64Layer)
	if err != nil {
		t.Fatalf("FetchBlob through allow-listed redirect: %v", err)
	}

	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read redirected blob: %v", err)
	}

	if string(body) != "layer for amd64" {
		t.Fatalf("redirected body = %q", body)
	}

	authMu.Lock()
	defer authMu.Unlock()
	if len(gotAuth) == 0 || gotAuth[0] != "" {
		t.Fatalf("Authorization leaked to cross-origin redirect target: %q", gotAuth)
	}
}

func TestRedirectToUnlistedHostIsRefused(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	img := seedImage(reg)

	reg.mu.Lock()
	reg.redirects[img.amd64Layer.Digest] = "http://169.254.169.254/latest/meta-data/"
	reg.mu.Unlock()

	up := testUpstream(t, []string{reg.host()}, nil)
	rc, err := up.FetchBlob(ctx, reg.host()+"/org/chart", img.amd64Layer)
	if err == nil {
		_ = rc.Close()
	}

	if !errors.Is(err, ErrUpstreamDenied) {
		t.Fatalf("expected ErrUpstreamDenied, got %v", err)
	}
}

func TestEmptyAllowListDeniesUpstreamCalls(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	seedImage(reg)

	up := testUpstream(t, nil, nil)
	if _, err := up.ListTags(ctx, reg.host()+"/org/chart"); !errors.Is(err, ErrUpstreamDenied) {
		t.Fatalf("empty allow-list must deny, got %v", err)
	}
}

func TestProductionUpstreamRefusesLoopbackRegistry(t *testing.T) {
	ctx := context.Background()
	reg := newMockRegistry(t)
	seedImage(reg)

	up, err := NewUpstream([]string{reg.host()}, nil)
	if err != nil {
		t.Fatalf("NewUpstream: %v", err)
	}

	if _, err := up.ListTags(ctx, reg.host()+"/org/chart"); !errors.Is(err, ErrUpstreamDenied) {
		t.Fatalf("production upstream must refuse a loopback registry, got %v", err)
	}
}

func TestSelectPlatformManifests(t *testing.T) {
	amd := ocispec.Descriptor{
		Digest:   digest.FromString("amd"),
		Platform: &ocispec.Platform{OS: testOS, Architecture: "amd64"},
	}
	armV7 := ocispec.Descriptor{
		Digest:   digest.FromString("v7"),
		Platform: &ocispec.Platform{OS: testOS, Architecture: "arm", Variant: "v7"},
	}
	armV8 := ocispec.Descriptor{
		Digest:   digest.FromString("v8"),
		Platform: &ocispec.Platform{OS: testOS, Architecture: "arm64", Variant: "v8"},
	}
	win := ocispec.Descriptor{
		Digest:   digest.FromString("win"),
		Platform: &ocispec.Platform{OS: "windows", Architecture: "amd64"},
	}
	att := ocispec.Descriptor{
		Digest:   digest.FromString("att"),
		Platform: &ocispec.Platform{OS: testUnknownOS, Architecture: testUnknownOS},
	}
	idx := ocispec.Index{Manifests: []ocispec.Descriptor{amd, armV7, armV8, win, att}}

	all := []digest.Digest{amd.Digest, armV7.Digest, armV8.Digest, win.Digest}
	cases := []struct {
		name      string
		platforms []string
		want      []digest.Digest
		wantErr   bool
	}{
		{"empty selects all but attestation", nil, all, false},
		{"single arch any variant", []string{"linux/arm"}, []digest.Digest{armV7.Digest}, false},
		{"exact variant", []string{"linux/arm/v7"}, []digest.Digest{armV7.Digest}, false},
		{"two platforms keep index order", []string{"windows/amd64", testAMD64Plat}, []digest.Digest{amd.Digest, win.Digest},
			false},
		{"duplicate requests are deduplicated", []string{testAMD64Plat, testAMD64Plat}, []digest.Digest{amd.Digest}, false},
		{"attestation never matches", []string{"unknown/unknown"}, nil, true},
		{"no match is an error", []string{"darwin/arm64"}, nil, true},
		{"malformed platform is an error", []string{"linux/"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectPlatformManifests(idx, tc.platforms)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}

			var digests []digest.Digest
			for _, d := range got {
				digests = append(digests, d.Digest)
			}

			if !slices.Equal(digests, tc.want) {
				t.Fatalf("selected %v, want %v", digests, tc.want)
			}
		})
	}

	if _, err := selectPlatformManifests(ocispec.Index{}, nil); err == nil {
		t.Fatal("an empty index must be rejected")
	}
}
