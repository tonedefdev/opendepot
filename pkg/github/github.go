package github

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v81/github"
	"github.com/hashicorp/go-version"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var (
	repoSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	refSegmentPattern  = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,200}$`)
)

// jwtTransport is a custom HTTP transport that adds the JWT to the Authorization header.
type jwtTransport struct {
	Transport http.RoundTripper
	JWT       string
}

// GithubClientConfig defines the configuration for an authenticated Github client.
type GithubClientConfig struct {
	// The Github application's ID.
	AppID int64
	// The Github application's install ID.
	InstallationID int64
	// The Github application's private key as a byte slice.
	PrivateKeyData []byte
}

// CreateGithubClient creates an authenticated client with the provided GithubClientConfig.
// If the client config is nil a github.Client is returned with a default http.Client type.
func CreateGithubClient(ctx context.Context, useAuthenticatedClient bool, githubConfig *GithubClientConfig) (*github.Client, error) {
	if useAuthenticatedClient && githubConfig == nil {
		return nil, fmt.Errorf("resource is marked to UseAuthenticatedClient but GithubClientConfig is nil")
	}

	if useAuthenticatedClient && githubConfig != nil {
		authClient, err := GenerateAuthenticatedGithubClient(ctx, githubConfig)
		if err != nil {
			return nil, fmt.Errorf("unable to generate authenticated github client: %v", err)
		}
		return authClient, nil
	}

	return github.NewClient(nil), nil
}

// GetModuleArchiveFromRef gets a module from Github based on its ref and returns a byte slice and the file's base64 encoded SHA256 checksum.
func GetModuleArchiveFromRef(ctx context.Context, log logr.Logger, githubClient *github.Client, version *opendepotv1alpha1.Version, format github.ArchiveFormat) (moduleBytes []byte, checksum *string, err error) {
	ref := version.Spec.Version
	if !strings.HasPrefix(ref, "v") {
		ref = "v" + ref
	}

	var moduleReq *http.Response
	moduleReq, err = GetArchiveRequest(ctx, githubClient, version.Spec.ModuleConfigRef.RepoOwner, *version.Spec.ModuleConfigRef.Name, format, ref)
	if err != nil {
		return nil, nil, err
	}
	defer moduleReq.Body.Close()

	log.V(5).Info("module req status code with 'v' prefix", "statusCode", moduleReq.StatusCode)

	if moduleReq.StatusCode != 200 {
		// If we get a 404 not found error it may be because the tag is prefixed without a 'v'
		// so we try again without the 'v' prefix before returning an error.
		if moduleReq.StatusCode == 404 {
			var moduleReq *http.Response

			refNoV := strings.TrimPrefix(ref, "v")
			moduleReq, err = GetArchiveRequest(ctx, githubClient, version.Spec.ModuleConfigRef.RepoOwner, *version.Spec.ModuleConfigRef.Name, format, refNoV)
			if err != nil {
				return nil, nil, err
			}
			defer moduleReq.Body.Close()

			log.V(5).Info("module req status code without 'v' prefix", "statusCode", moduleReq.StatusCode)

			moduleBytes, err = io.ReadAll(moduleReq.Body)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read module archive data: %w", err)
			}

			log.V(5).Info("module req bytes length", "length", len(moduleBytes))

			sha256Sum := sha256.Sum256(moduleBytes)
			checksumSha256 := base64.StdEncoding.EncodeToString(sha256Sum[:])
			checksum = &checksumSha256
			return
		}

		return nil, nil, fmt.Errorf("failed to get module archive from Github: status code %d", moduleReq.StatusCode)
	}

	moduleBytes, err = io.ReadAll(moduleReq.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read module archive data: %w", err)
	}

	log.V(5).Info("module req bytes length", "length", len(moduleBytes))

	sha256Sum := sha256.Sum256(moduleBytes)
	checksumSha256 := base64.StdEncoding.EncodeToString(sha256Sum[:])
	checksum = &checksumSha256

	return
}

// validRepoSegment reports whether s is a safe GitHub owner or repository name. Names may contain only
// letters, digits, hyphens, underscores, and dots, and must not be the dot segments "." or "..".
func validRepoSegment(s string) bool {
	if s == "." || s == ".." {
		return false
	}

	return repoSegmentPattern.MatchString(s)
}

// ValidateRepoName returns an error when owner or repo cannot be safely placed into a GitHub API path.
func ValidateRepoName(owner, repo string) error {
	if !validRepoSegment(owner) || !validRepoSegment(repo) {
		return fmt.Errorf("invalid GitHub owner or repository name %q/%q", owner, repo)
	}

	return nil
}

// validRefName reports whether ref is a safe git ref for a GitHub API path. Each slash-separated segment must
// match refSegmentPattern and must not be a dot segment, so the ref cannot climb to another repository's path.
func validRefName(ref string) bool {
	for segment := range strings.SplitSeq(ref, "/") {
		if segment == "." || segment == ".." || !refSegmentPattern.MatchString(segment) {
			return false
		}
	}

	return true
}

// validateRefs returns an error when any of the refs is not a safe git ref.
func validateRefs(refs []string) error {
	for _, ref := range refs {
		if !validRefName(ref) {
			return fmt.Errorf("invalid git ref %q", ref)
		}
	}

	return nil
}

// GetArchiveRequest retrieves the archive link for a given repository and reference (branch, tag, or commit SHA).
func GetArchiveRequest(ctx context.Context, githubClient *github.Client, owner, repo string, format github.ArchiveFormat, ref string) (*http.Response, error) {
	if err := ValidateRepoName(owner, repo); err != nil {
		return nil, err
	}

	if err := validateRefs([]string{ref}); err != nil {
		return nil, err
	}

	al, alResp, err := githubClient.Repositories.GetArchiveLink(ctx, owner, repo, format, &github.RepositoryContentGetOptions{
		Ref: ref,
	}, 10)

	if err != nil {
		if alResp != nil {
			alResp.Body.Close()
		}
		return nil, err
	}

	defer alResp.Body.Close()

	if al == nil || alResp == nil {
		return nil, fmt.Errorf("the response from the Github API was nil")
	}

	if alResp.StatusCode != 302 {
		return nil, fmt.Errorf("failed to get Github archive link: status code %d", alResp.StatusCode)
	}

	archiveReq, err := http.NewRequestWithContext(ctx, http.MethodGet, al.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request for archive link: %w", err)
	}

	archiveResp, err := http.DefaultClient.Do(archiveReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request for archive link: %w", err)
	}

	return archiveResp, nil
}

// TagCandidates returns the tag names to try for a version in preference order: the 'v' prefixed tag first,
// then the bare version, mirroring the fallback used by GetModuleArchiveFromRef.
func TagCandidates(tagPrefix, versionString string) []string {
	bare := strings.TrimPrefix(versionString, "v")
	return []string{tagPrefix + "v" + bare, tagPrefix + bare}
}

// AgentSourceRepoName returns the repository name that holds an agent source. The name is the last path
// segment of RepoUrl, falling back to the source Name when RepoUrl is not set.
func AgentSourceRepoName(sourceConfig *opendepotv1alpha1.AgentSourceConfig) string {
	if sourceConfig.RepoUrl != nil && strings.TrimSpace(*sourceConfig.RepoUrl) != "" {
		segments := strings.Split(strings.TrimSuffix(strings.TrimSpace(*sourceConfig.RepoUrl), "/"), "/")
		return segments[len(segments)-1]
	}

	if sourceConfig.Name != nil {
		return *sourceConfig.Name
	}

	return ""
}

// ListMatchingTags lists every tag in the repository that begins with tagPrefix, strips the prefix, and returns
// the remaining versions that parse as semver and satisfy versionConstraints. An empty versionConstraints matches all semver tags.
func ListMatchingTags(ctx context.Context, githubClient *github.Client, owner, repo, tagPrefix, versionConstraints string) ([]string, error) {
	if err := ValidateRepoName(owner, repo); err != nil {
		return nil, err
	}

	var constraints version.Constraints
	if strings.TrimSpace(versionConstraints) != "" {
		var err error
		constraints, err = version.NewConstraint(versionConstraints)
		if err != nil {
			return nil, err
		}
	}

	opt := &github.ListOptions{
		Page:    1,
		PerPage: 100,
	}

	var matched []string
	for {
		tags, resp, err := githubClient.Repositories.ListTags(ctx, owner, repo, opt)
		if err != nil {
			return nil, err
		}

		if resp == nil {
			return nil, fmt.Errorf("tags response was nil")
		}

		for _, tag := range tags {
			if !strings.HasPrefix(tag.GetName(), tagPrefix) {
				continue
			}

			candidate := strings.TrimPrefix(tag.GetName(), tagPrefix)
			tagVersion, err := version.NewVersion(candidate)
			if err != nil {
				continue
			}

			// Constraints returned from version.NewConstraint use AND semantics,
			// so a tag must satisfy the full expression (e.g. >=1.0.0, <2.0.0).
			if constraints != nil && !constraints.Check(tagVersion) {
				continue
			}

			matched = append(matched, candidate)
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}

	return matched, nil
}

// GenerateGithubClient creates a GitHub client using a GitHub Application for authentication.
func GenerateAuthenticatedGithubClient(ctx context.Context, githubClientConfig *GithubClientConfig) (*github.Client, error) {
	// Parse the private key
	block, _ := pem.Decode(githubClientConfig.PrivateKeyData)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, errors.New("failed to decode PEM block containing private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create a JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Minute * 10).Unix(),
		"iss": githubClientConfig.AppID,
	})

	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign JWT: %w", err)
	}

	// Create a custom HTTP client with the JWT in the Authorization header
	jwtHTTPClient := &http.Client{
		Transport: &jwtTransport{
			Transport: http.DefaultTransport,
			JWT:       signedToken,
		},
	}
	jwtClient := github.NewClient(jwtHTTPClient)

	// Use the JWT-authenticated client to fetch the installation token
	instToken, _, err := jwtClient.Apps.CreateInstallationToken(ctx, githubClientConfig.InstallationID, &github.InstallationTokenOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create installation token: %w", err)
	}

	// Create an authenticated GitHub client with the installation token
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: instToken.GetToken()})
	oauthClient := oauth2.NewClient(ctx, ts)
	return github.NewClient(oauthClient), nil
}

// GetGithubApplicationSecret retrieves the opendepot-github-application-secret kubernetes secret from the cluster
// using the client received by k8sClient. It returns a GithubClientConfig for making authenticated requests to the Github API.
// The k8sClient parameter should be received by the controller's client.
func GetGithubApplicationSecret(ctx context.Context, k8sClient client.Client, secretNamespace string) (*GithubClientConfig, error) {
	object := client.ObjectKey{
		Name:      opendepotv1alpha1.OpenDepotGithubSecretName,
		Namespace: secretNamespace,
	}

	secret := corev1.Secret{}
	if err := k8sClient.Get(ctx, object, &secret); err != nil {
		return nil, err
	}

	appID, err := strconv.ParseInt(string(secret.Data[opendepotv1alpha1.OpenDepotGithubSecretDataFieldAppID]), 0, 64)
	if err != nil {
		return nil, fmt.Errorf("unable to parse '%s' as int64: %w", opendepotv1alpha1.OpenDepotGithubSecretDataFieldAppID, err)
	}

	installID, err := strconv.ParseInt(string(secret.Data[opendepotv1alpha1.OpenDepotGithubSecretDataFieldInstallID]), 0, 64)
	if err != nil {
		return nil, fmt.Errorf("unable to parse '%s' as int64: %w", opendepotv1alpha1.OpenDepotGithubSecretDataFieldInstallID, err)
	}

	keyData, err := base64.StdEncoding.DecodeString(string(secret.Data[opendepotv1alpha1.OpenDepotGithubSecretDataFieldPrivateKey]))
	if err != nil {
		return nil, fmt.Errorf("unable to decode '%s': %w", opendepotv1alpha1.OpenDepotGithubSecretDataFieldPrivateKey, err)
	}

	githubClientConfig := &GithubClientConfig{
		AppID:          appID,
		InstallationID: installID,
		PrivateKeyData: keyData,
	}

	return githubClientConfig, nil
}

// GetProviderGoMod fetches the go.mod file for a provider version from its GitHub source repository.
// Both 'v{version}' and bare '{version}' ref formats are tried. The raw.githubusercontent.com CDN
// is tried first (no API rate limit); the authenticated GitHub client is used as a fallback (required
// for private repositories or when the CDN is unavailable).
func GetProviderGoMod(ctx context.Context, githubClient *github.Client, owner, repo, version string) ([]byte, error) {
	bare := strings.TrimPrefix(version, "v")
	refs := []string{"v" + bare, bare}
	if err := ValidateRepoName(owner, repo); err != nil {
		return nil, err
	}

	if err := validateRefs(refs); err != nil {
		return nil, err
	}

	// Try raw.githubusercontent.com first — not subject to GitHub API rate limits.
	for _, ref := range refs {
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/go.mod", owner, repo, ref)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			continue
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read go.mod from %s: %w", rawURL, err)
			}
			return data, nil
		}
	}

	// Fall back to GitHub API (required for private repos; subject to 60 req/hr unauthenticated limit).
	for _, ref := range refs {
		fileContent, _, _, err := githubClient.Repositories.GetContents(ctx, owner, repo, "go.mod",
			&github.RepositoryContentGetOptions{Ref: ref})
		if err != nil {
			continue
		}
		if fileContent == nil {
			continue
		}
		data, err := fileContent.GetContent()
		if err != nil {
			return nil, fmt.Errorf("failed to decode go.mod content for %s/%s@%s: %w", owner, repo, ref, err)
		}
		return []byte(data), nil
	}
	return nil, fmt.Errorf("go.mod not found in %s/%s at version %s", owner, repo, version)
}

// GetModuleReadme fetches a module version's README content from its GitHub source repository.
// Both 'v{version}' and bare '{version}' ref formats are tried using go-github's dedicated
// README resolver, which handles filename casing and extension variants (README.md, readme.rst,
// etc.) automatically. Returns a nil error with nil content only when no README could be resolved
// at any tried ref; callers should treat that as a non-fatal "not found" case.
func GetModuleReadme(ctx context.Context, githubClient *github.Client, owner, repo, version string) ([]byte, error) {
	bare := strings.TrimPrefix(version, "v")
	refs := []string{"v" + bare, bare}
	if err := ValidateRepoName(owner, repo); err != nil {
		return nil, err
	}

	if err := validateRefs(refs); err != nil {
		return nil, err
	}

	var lastErr error
	for _, ref := range refs {
		readme, _, err := githubClient.Repositories.GetReadme(ctx, owner, repo, &github.RepositoryContentGetOptions{Ref: ref})
		if err != nil {
			lastErr = err
			continue
		}

		if readme == nil {
			continue
		}

		data, err := readme.GetContent()
		if err != nil {
			return nil, fmt.Errorf("failed to decode README content for %s/%s@%s: %w", owner, repo, ref, err)
		}

		return []byte(data), nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("README not found in %s/%s at version %s: %w", owner, repo, version, lastErr)
	}

	return nil, fmt.Errorf("README not found in %s/%s at version %s", owner, repo, version)
}

// RoundTrip sets the authorization header and executes a single HTTP transaction, returning a Response for the provided Request.
func (t *jwtTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.JWT))
	return t.Transport.RoundTrip(req)
}
