package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	// labelPublic is the label key used to mark namespaces and resources as publicly browseable.
	labelPublic = "opendepot.defdev.io/public"
)

// k8sNamespace is a minimal struct for deserializing namespace objects.
type k8sNamespace struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
}

// k8sNamespaceList is a minimal struct for deserializing the namespace list API response.
type k8sNamespaceList struct {
	Items []k8sNamespace `json:"items"`
}

// isPublicNamespace reports whether the given label set marks a namespace as public.
func isPublicNamespace(labels map[string]string) bool {
	return labels[labelPublic] == "true"
}

// isPublicResource reports whether the given label set marks a resource as public.
func isPublicResource(labels map[string]string) bool {
	return labels[labelPublic] == "true"
}

// browseScanCounts tallies SecurityFindings by severity into a BrowseScanCounts.
func browseScanCounts(findings []opendepotv1alpha1.SecurityFinding) *BrowseScanCounts {
	if len(findings) == 0 {
		return nil
	}

	counts := &BrowseScanCounts{}
	for _, f := range findings {
		switch strings.ToUpper(f.Severity) {
		case "CRITICAL":
			counts.Critical++
		case "HIGH":
			counts.High++
		case "MEDIUM":
			counts.Medium++
		case "LOW":
			counts.Low++
		default:
			counts.Unknown++
		}
	}

	return counts
}

// latestOf returns the lexicographically latest non-empty string from the arguments.
func latestOf(times ...string) string {
	var latest string
	for _, t := range times {
		if t > latest {
			latest = t
		}
	}
	return latest
}

// severityRank returns a numeric rank for a severity string (lower = more severe).
func severityRank(s string) int {
	switch strings.ToUpper(s) {
	case "CRITICAL":
		return 0
	case "HIGH":
		return 1
	case "MEDIUM":
		return 2
	case "LOW":
		return 3
	default:
		return 4
	}
}

// highestSeverity returns the most severe level present in a BrowseScanCounts.
// Returns "" when counts is nil or all-zero.
func highestSeverity(c *BrowseScanCounts) string {
	if c == nil {
		return ""
	}

	switch {
	case c.Critical > 0:
		return "CRITICAL"
	case c.High > 0:
		return "HIGH"
	case c.Medium > 0:
		return "MEDIUM"
	case c.Low > 0:
		return "LOW"
	case c.Unknown > 0:
		return "UNKNOWN"
	default:
		return ""
	}
}

// browseSAClient creates a Kubernetes clientset using the server's own service account.
// All browse endpoint Kubernetes reads go through this client so that visibility
// filtering is applied in-process rather than relying on per-user Kubernetes RBAC.
func browseSAClient() (*kubernetes.Clientset, error) {
	return generateKubeClient(nil, nil, false)
}

// browseAuthClientCredentials attempts to authenticate via OAuth2 client credentials.
// Returns the GroupBinding and true on success, nil and false on any failure.
func browseAuthClientCredentials(ctx context.Context, rawToken string) (*opendepotv1alpha1.GroupBinding, bool) {
	if !*opendepotOIDCAllowClientCredentials || oidcCCVerifier == nil {
		return nil, false
	}

	iss, _ := parseUnsignedJWTIssuer(rawToken)
	if iss != *opendepotOIDCIssuerURL {
		return nil, false
	}

	ccToken, err := oidcCCVerifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, false
	}

	var ccClaims map[string]any
	if err := ccToken.Claims(&ccClaims); err != nil {
		return nil, false
	}

	sub, _ := ccClaims["sub"].(string)
	if sub == "" {
		return nil, false
	}

	cs, err := browseSAClient()
	if err != nil {
		return nil, false
	}

	b, err := findGroupBinding(ctx, cs, []string{"client:" + sub})
	if err != nil {
		return nil, false
	}

	return b, false
}

// browseAuthState determines the authentication level for a browse request without
// requiring auth (browse is public-accessible). Returns:
//   - binding != nil: OIDC-authenticated caller; visibility = public ∪ GroupBinding-allowed
//   - binding == nil, allAccess == true: anonymous-auth mode; all resources visible
//   - binding == nil, allAccess == false: unauthenticated or authenticated without GroupBinding;
//     visibility = public only
func browseAuthState(r *http.Request) (binding *opendepotv1alpha1.GroupBinding, allAccess bool) {
	if *opendepotAnonymousAuth {
		// Anonymous auth mode: everyone sees all resources.
		return nil, true
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, false
	}

	if oidcVerifier == nil {
		// Non-OIDC mode: no GroupBinding concept applies; public-only visibility.
		return nil, false
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, false
	}

	rawToken := strings.TrimPrefix(authHeader, "Bearer ")
	idToken, err := oidcVerifier.Verify(r.Context(), rawToken)
	if err != nil {
		return browseAuthClientCredentials(r.Context(), rawToken)
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, false
	}

	groups, _ := extractGroupsClaim(claims, *opendepotOIDCGroupsClaim)
	if len(groups) == 0 {
		// JWT present and valid but missing groups — authenticated without GroupBinding.
		// Public-only visibility.
		return nil, false
	}

	cs, err := browseSAClient()
	if err != nil {
		return nil, false
	}

	b, err := findGroupBinding(r.Context(), cs, groups)
	if err != nil {
		// Authenticated but no matching GroupBinding → public-only visibility.
		return nil, false
	}

	return b, false
}

// browseListNamespaces returns all Kubernetes namespace objects via the server SA.
func browseListNamespaces(cs *kubernetes.Clientset, r *http.Request) ([]k8sNamespace, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/api/v1/namespaces").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var list k8sNamespaceList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal namespace list: %w", err)
	}

	return list.Items, nil
}

// browseGetNamespaceLabels returns labels for a single namespace using the raw REST client.
func browseGetNamespaceLabels(cs *kubernetes.Clientset, r *http.Request, namespace string) (map[string]string, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/api/v1/namespaces/" + namespace).
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace %s: %w", namespace, err)
	}

	var ns k8sNamespace
	if err := json.Unmarshal(raw, &ns); err != nil {
		return nil, fmt.Errorf("failed to unmarshal namespace %s: %w", namespace, err)
	}

	return ns.Metadata.Labels, nil
}

// isBrowseVisible reports whether a resource should be included in browse results.
//
// Visibility rules:
//   - allAccess (anonymous-auth mode): all resources visible; publicOnly still applies.
//   - pub true: public resource, always visible unless publicOnly with !pub (noop).
//   - pub false, binding nil: hidden (no GroupBinding → public-only).
//   - pub false, binding not nil: visible only if isResourceAllowed grants access.
func isBrowseVisible(pub, publicOnly, allAccess bool, binding *opendepotv1alpha1.GroupBinding, kind, name string) bool {
	if allAccess {
		if publicOnly && !pub {
			return false
		}
		return true
	}

	if !pub {
		// Non-public resource: requires a matching GroupBinding.
		if binding == nil {
			return false
		}
		if !isResourceAllowed(binding, kind, name) {
			return false
		}
	}

	if publicOnly && !pub {
		return false
	}

	return true
}

// browseCollectModules fetches and filters Module resources for the browse endpoint.
func browseCollectModules(cs *kubernetes.Clientset, r *http.Request, nsFilter, nsPublic map[string]bool, binding *opendepotv1alpha1.GroupBinding, allAccess, publicOnly bool) ([]BrowseResource, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Resource("modules").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list modules: %w", err)
	}

	var list opendepotv1alpha1.ModuleList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal module list: %w", err)
	}

	var items []BrowseResource
	for _, m := range list.Items {
		ns := m.Namespace
		if len(nsFilter) > 0 && !nsFilter[ns] {
			continue
		}
		pub := nsPublic[ns] && isPublicResource(m.Labels)
		if !isBrowseVisible(pub, publicOnly, allAccess, binding, "module", m.Name) {
			continue
		}
		resource := moduleToCard(m, pub)
		versionData, _ := browseListModuleVersions(cs, r, ns, m.Name)
		enrichModuleCard(&resource, versionData)
		items = append(items, resource)
	}
	return items, nil
}

// browseCollectProviders fetches and filters Provider resources for the browse endpoint.
func browseCollectProviders(cs *kubernetes.Clientset, r *http.Request, nsFilter, nsPublic map[string]bool, binding *opendepotv1alpha1.GroupBinding, allAccess, publicOnly bool, filterOS, filterArch string) ([]BrowseResource, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Resource("providers").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}

	var list opendepotv1alpha1.ProviderList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider list: %w", err)
	}

	var items []BrowseResource
	for _, p := range list.Items {
		ns := p.Namespace
		if len(nsFilter) > 0 && !nsFilter[ns] {
			continue
		}
		pub := nsPublic[ns] && isPublicResource(p.Labels)
		if !isBrowseVisible(pub, publicOnly, allAccess, binding, "provider", p.Name) {
			continue
		}
		resource := providerToCard(p, pub)
		versionData, _ := browseListProviderVersions(cs, r, ns)
		enrichProviderCard(&resource, p, versionData, filterOS, filterArch)
		items = append(items, resource)
	}
	return items, nil
}

// handleBrowseNamespaces returns the list of namespaces visible to the caller.
// Unauthenticated callers see only public namespaces; anonymous-auth mode and
// authenticated callers (with or without GroupBinding) see all namespaces.
// GET /opendepot/ui/v1/namespaces
func handleBrowseNamespaces(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	binding, allAccess := browseAuthState(r)
	// Show all namespaces when in anonymous-auth mode or when the caller has a
	// GroupBinding (they may have access to resources in non-public namespaces).
	showAll := allAccess || binding != nil

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	namespaces, err := browseListNamespaces(cs, r)
	if err != nil {
		logger.Error("browse: failed to list namespaces", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	result := BrowseNamespaceList{Items: []BrowseNamespace{}}
	for _, ns := range namespaces {
		public := isPublicNamespace(ns.Metadata.Labels)
		if !public && !showAll {
			continue
		}
		result.Items = append(result.Items, BrowseNamespace{
			Name:   ns.Metadata.Name,
			Public: public,
		})
	}

	json.NewEncoder(w).Encode(result)
}

// handleBrowseResources returns the paginated, filtered, sorted list of visible resources.
// GET /opendepot/ui/v1/resources
//
// Query parameters:
//
//	namespace   - repeat for multi-namespace filter (default: all visible namespaces)
//	kind        - "module" or "provider" (default: both)
//	q           - search text matched against name, namespace, provider, repoUrl
//	synced      - "true"/"false" filter by sync status
//	os          - filter providers by supported OS
//	arch        - filter providers by supported architecture
//	severity    - minimum severity: CRITICAL/HIGH/MEDIUM/LOW
//	public_only - "true" show only public resources
//	sort_by     - name|namespace|latest_version|synced|severity|last_scanned (default: name)
//	sort_dir    - asc|desc (default: asc)
//	page        - 1-based page number (default: 1)
//	page_size   - items per page (default: 20, max: 100)
func handleBrowseResources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	binding, allAccess := browseAuthState(r)

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	filterNamespaces := q["namespace"]
	filterKind := strings.ToLower(q.Get("kind"))
	searchText := strings.ToLower(q.Get("q"))
	filterSynced := q.Get("synced")
	filterOS := q.Get("os")
	filterArch := q.Get("arch")
	filterSeverity := strings.ToUpper(q.Get("severity"))
	publicOnly := q.Get("public_only") == "true"
	sortBy := q.Get("sort_by")

	if sortBy == "" {
		sortBy = "name"
	}

	sortDir := strings.ToLower(q.Get("sort_dir"))
	if sortDir == "" {
		sortDir = "asc"
	}

	page := 1
	if p, err2 := strconv.Atoi(q.Get("page")); err2 == nil && p > 0 {
		page = p
	}

	pageSize := 20
	if ps, err2 := strconv.Atoi(q.Get("page_size")); err2 == nil && ps > 0 {
		if ps > 100 {
			ps = 100
		}
		pageSize = ps
	}

	allNamespaces, err := browseListNamespaces(cs, r)
	if err != nil {
		logger.Error("browse: failed to list namespaces", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsFilter := make(map[string]bool, len(filterNamespaces))
	for _, ns := range filterNamespaces {
		nsFilter[ns] = true
	}

	nsPublic := make(map[string]bool, len(allNamespaces))
	for _, ns := range allNamespaces {
		nsPublic[ns.Metadata.Name] = isPublicNamespace(ns.Metadata.Labels)
	}

	var items []BrowseResource

	if filterKind == "" || filterKind == "module" {
		modules, err := browseCollectModules(cs, r, nsFilter, nsPublic, binding, allAccess, publicOnly)
		if err != nil {
			logger.Error("browse: failed to collect modules", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		items = append(items, modules...)
	}

	if filterKind == "" || filterKind == "provider" {
		providers, err := browseCollectProviders(cs, r, nsFilter, nsPublic, binding, allAccess, publicOnly, filterOS, filterArch)
		if err != nil {
			logger.Error("browse: failed to collect providers", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		items = append(items, providers...)
	}

	items = applyBrowseFilters(items, searchText, filterSynced, filterSeverity)
	sortBrowseItems(items, sortBy, sortDir)

	totalCount := len(items)
	start := min((page-1)*pageSize, totalCount)
	end := min(start+pageSize, totalCount)

	resp := BrowseResourceList{
		Items:      items[start:end],
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
	}

	if resp.Items == nil {
		resp.Items = []BrowseResource{}
	}

	json.NewEncoder(w).Encode(resp)
}

// handleBrowseResourceDetail returns the full drill-down payload for a single resource.
// GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}
func handleBrowseResourceDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	namespace := chi.URLParam(r, "namespace")
	kind := strings.ToLower(chi.URLParam(r, "kind"))
	name := chi.URLParam(r, "name")

	binding, allAccess := browseAuthState(r)

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("browse: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	nsLabels, _ := browseGetNamespaceLabels(cs, r, namespace)
	nsPublic := isPublicNamespace(nsLabels)

	switch kind {
	case "module":
		rawModule, err := cs.RESTClient().
			Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Namespace(namespace).
			Resource("modules").
			Name(name).
			DoRaw(r.Context())
		if err != nil {
			if k8sApiErrors.IsNotFound(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			logger.Error("browse: failed to get module", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var m opendepotv1alpha1.Module
		if err := json.Unmarshal(rawModule, &m); err != nil {
			logger.Error("browse: failed to unmarshal module", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		pub := nsPublic && isPublicResource(m.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "module", m.Name) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		card := moduleToCard(m, pub)
		versions, _ := browseListModuleVersions(cs, r, namespace, name)
		enrichModuleCard(&card, versions)

		detail := BrowseResourceDetail{BrowseResource: card}
		detail.Versions = moduleVersionSummaries(versions)
		detail.SourceScanFindings = collectModuleSourceFindings(versions)
		json.NewEncoder(w).Encode(detail)

	case "provider":
		rawProvider, err := cs.RESTClient().
			Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Namespace(namespace).
			Resource("providers").
			Name(name).
			DoRaw(r.Context())
		if err != nil {
			if k8sApiErrors.IsNotFound(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			logger.Error("browse: failed to get provider", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		var p opendepotv1alpha1.Provider
		if err := json.Unmarshal(rawProvider, &p); err != nil {
			logger.Error("browse: failed to unmarshal provider", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		pub := nsPublic && isPublicResource(p.Labels)
		if !isBrowseVisible(pub, false, allAccess, binding, "provider", p.Name) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		card := providerToCard(p, pub)
		versions, _ := browseListProviderVersions(cs, r, namespace)
		enrichProviderCard(&card, p, versions, "", "")

		detail := BrowseResourceDetail{BrowseResource: card}
		detail.Versions = providerVersionSummaries(p, versions)
		if p.Status.SourceScan != nil {
			detail.SourceScanFindings = p.Status.SourceScan.Findings
		}

		detail.BinaryScanFindings = collectBinaryFindings(versions)
		json.NewEncoder(w).Encode(detail)

	default:
		http.Error(w, "kind must be 'module' or 'provider'", http.StatusBadRequest)
	}
}

// moduleToCard converts a Module resource to a BrowseResource card.
func moduleToCard(m opendepotv1alpha1.Module, public bool) BrowseResource {
	return BrowseResource{
		Kind:          "module",
		Namespace:     m.Namespace,
		Name:          m.Name,
		Synced:        m.Status.Synced,
		SyncStatus:    m.Status.SyncStatus,
		Public:        public,
		Provider:      m.Spec.ModuleConfig.Provider,
		RepoURL:       derefString(m.Spec.ModuleConfig.RepoUrl),
		LatestVersion: derefString(m.Status.LatestVersion),
	}
}

// providerToCard converts a Provider resource to a BrowseResource card.
func providerToCard(p opendepotv1alpha1.Provider, public bool) BrowseResource {
	r := BrowseResource{
		Kind:              "provider",
		Namespace:         p.Namespace,
		Name:              p.Name,
		Synced:            p.Status.Synced,
		SyncStatus:        p.Status.SyncStatus,
		Public:            public,
		LatestVersion:     derefString(p.Status.LatestVersion),
		ProviderNamespace: derefString(p.Spec.ProviderConfig.Namespace),
	}

	if p.Status.SourceScan != nil {
		r.ScanCounts = browseScanCounts(p.Status.SourceScan.Findings)
		r.LastScanned = p.Status.SourceScan.ScannedAt
	}

	return r
}

// enrichModuleCard sets ScanCounts and LastScanned on a module card from its versions.
func enrichModuleCard(card *BrowseResource, versions []opendepotv1alpha1.Version) {
	var allFindings []opendepotv1alpha1.SecurityFinding
	var times []string
	for _, v := range versions {
		if v.Status.SourceScan != nil {
			allFindings = append(allFindings, v.Status.SourceScan.Findings...)
			times = append(times, v.Status.SourceScan.ScannedAt)
		}
	}

	if len(allFindings) > 0 {
		card.ScanCounts = browseScanCounts(allFindings)
	}

	card.LastScanned = latestOf(times...)
}

// enrichProviderCard sets Platforms, ScanCounts, and LastScanned on a provider card.
func enrichProviderCard(card *BrowseResource, p opendepotv1alpha1.Provider, versions []opendepotv1alpha1.Version, filterOS, filterArch string) {
	platformSet := make(map[string]ProviderPlatform)
	var allFindings []opendepotv1alpha1.SecurityFinding
	var times []string

	for _, v := range versions {
		if v.Spec.ProviderConfigRef == nil || v.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *v.Spec.ProviderConfigRef.Name != p.Name {
			continue
		}

		osName := v.Spec.OperatingSystem
		arch := v.Spec.Architecture

		if filterOS != "" && osName != filterOS {
			continue
		}

		if filterArch != "" && arch != filterArch {
			continue
		}

		key := osName + "/" + arch
		platformSet[key] = ProviderPlatform{OS: osName, Arch: arch}

		if v.Status.BinaryScan != nil {
			allFindings = append(allFindings, v.Status.BinaryScan.Findings...)
			times = append(times, v.Status.BinaryScan.ScannedAt)
		}
	}

	if p.Status.SourceScan != nil {
		allFindings = append(allFindings, p.Status.SourceScan.Findings...)
		times = append(times, p.Status.SourceScan.ScannedAt)
	}

	platforms := make([]ProviderPlatform, 0, len(platformSet))
	for _, pl := range platformSet {
		platforms = append(platforms, pl)
	}

	sort.Slice(platforms, func(i, j int) bool {
		if platforms[i].OS != platforms[j].OS {
			return platforms[i].OS < platforms[j].OS
		}
		return platforms[i].Arch < platforms[j].Arch
	})

	card.Platforms = platforms
	if len(allFindings) > 0 {
		card.ScanCounts = browseScanCounts(allFindings)
	}

	card.LastScanned = latestOf(times...)
}

// browseListModuleVersions returns Version resources in namespace that belong to the named module.
func browseListModuleVersions(cs *kubernetes.Clientset, r *http.Request, namespace, moduleName string) ([]opendepotv1alpha1.Version, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list versions in %s: %w", namespace, err)
	}

	var list opendepotv1alpha1.VersionList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version list: %w", err)
	}

	var result []opendepotv1alpha1.Version
	for _, v := range list.Items {
		if v.Spec.ModuleConfigRef == nil || v.Spec.ModuleConfigRef.Name == nil {
			continue
		}

		if *v.Spec.ModuleConfigRef.Name != moduleName {
			continue
		}

		result = append(result, v)
	}

	return result, nil
}

// browseListProviderVersions returns all provider Version resources in the given namespace.
func browseListProviderVersions(cs *kubernetes.Clientset, r *http.Request, namespace string) ([]opendepotv1alpha1.Version, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list versions in %s: %w", namespace, err)
	}

	var list opendepotv1alpha1.VersionList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version list: %w", err)
	}

	var result []opendepotv1alpha1.Version
	for _, v := range list.Items {
		if v.Spec.ProviderConfigRef == nil {
			continue
		}

		result = append(result, v)
	}

	return result, nil
}

// moduleVersionSummaries converts a slice of Version resources to BrowseVersionSummary.
func moduleVersionSummaries(versions []opendepotv1alpha1.Version) []BrowseVersionSummary {
	summaries := make([]BrowseVersionSummary, 0, len(versions))
	for _, v := range versions {
		s := BrowseVersionSummary{
			Version:    v.Spec.Version,
			Synced:     v.Status.Synced,
			SyncStatus: v.Status.SyncStatus,
		}

		if v.Status.SourceScan != nil {
			s.ScanCounts = browseScanCounts(v.Status.SourceScan.Findings)
			s.LastScanned = v.Status.SourceScan.ScannedAt
		}

		summaries = append(summaries, s)
	}

	return summaries
}

// providerVersionSummaries converts provider Version resources to BrowseVersionSummary.
func providerVersionSummaries(p opendepotv1alpha1.Provider, versions []opendepotv1alpha1.Version) []BrowseVersionSummary {
	summaries := make([]BrowseVersionSummary, 0)
	for _, v := range versions {
		if v.Spec.ProviderConfigRef == nil || v.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *v.Spec.ProviderConfigRef.Name != p.Name {
			continue
		}

		s := BrowseVersionSummary{
			Version:    normalizeVersion(v.Spec.Version),
			Synced:     v.Status.Synced,
			SyncStatus: v.Status.SyncStatus,
			OS:         v.Spec.OperatingSystem,
			Arch:       v.Spec.Architecture,
		}

		if v.Status.BinaryScan != nil {
			s.ScanCounts = browseScanCounts(v.Status.BinaryScan.Findings)
			s.LastScanned = v.Status.BinaryScan.ScannedAt
		}

		summaries = append(summaries, s)
	}

	return summaries
}

// collectModuleSourceFindings gathers deduplicated IaC scan findings across module versions.
func collectModuleSourceFindings(versions []opendepotv1alpha1.Version) []opendepotv1alpha1.SecurityFinding {
	seen := make(map[string]struct{})
	var findings []opendepotv1alpha1.SecurityFinding
	for _, v := range versions {
		if v.Status.SourceScan == nil {
			continue
		}

		for _, f := range v.Status.SourceScan.Findings {
			key := f.VulnerabilityID + "/" + f.PkgName
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			findings = append(findings, f)
		}
	}

	return findings
}

// collectBinaryFindings builds a map of "os/arch" → []SecurityFinding from provider Version resources.
func collectBinaryFindings(versions []opendepotv1alpha1.Version) map[string][]opendepotv1alpha1.SecurityFinding {
	result := make(map[string][]opendepotv1alpha1.SecurityFinding)
	for _, v := range versions {
		if v.Status.BinaryScan == nil || len(v.Status.BinaryScan.Findings) == 0 {
			continue
		}

		key := v.Spec.OperatingSystem + "/" + v.Spec.Architecture
		result[key] = append(result[key], v.Status.BinaryScan.Findings...)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// applyBrowseFilters applies search text, synced, and severity filters.
func applyBrowseFilters(items []BrowseResource, searchText, filterSynced, filterSeverity string) []BrowseResource {
	if searchText == "" && filterSynced == "" && filterSeverity == "" {
		return items
	}

	minSeverityRank := -1
	if filterSeverity != "" {
		minSeverityRank = severityRank(filterSeverity)
	}

	filtered := make([]BrowseResource, 0, len(items))
	for _, item := range items {
		if searchText != "" {
			haystack := strings.Join([]string{
				strings.ToLower(item.Name),
				strings.ToLower(item.Namespace),
				strings.ToLower(item.Provider),
				strings.ToLower(item.ProviderNamespace),
				strings.ToLower(item.RepoURL),
				strings.ToLower(item.LatestVersion),
			}, " ")

			if !strings.Contains(haystack, searchText) {
				continue
			}
		}

		if filterSynced == "true" && !item.Synced {
			continue
		}

		if filterSynced == "false" && item.Synced {
			continue
		}

		if minSeverityRank >= 0 {
			hs := highestSeverity(item.ScanCounts)
			if hs == "" || severityRank(hs) > minSeverityRank {
				continue
			}
		}

		filtered = append(filtered, item)
	}

	return filtered
}

// sortBrowseItems sorts items in place by the given field and direction.
func sortBrowseItems(items []BrowseResource, by, dir string) {
	less := func(i, j int) bool {
		a, b := items[i], items[j]
		switch by {
		case "namespace":
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
			return a.Name < b.Name
		case "latest_version":
			return a.LatestVersion < b.LatestVersion
		case "synced":
			if a.Synced != b.Synced {
				return a.Synced
			}

			return a.Name < b.Name
		case "severity":
			ra := severityRank(highestSeverity(a.ScanCounts))
			rb := severityRank(highestSeverity(b.ScanCounts))
			if ra != rb {
				return ra < rb
			}

			return a.Name < b.Name
		case "last_scanned":
			return a.LastScanned < b.LastScanned
		default: // "name"
			if a.Name != b.Name {
				return a.Name < b.Name
			}

			return a.Namespace < b.Namespace
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if dir == "desc" {
			return less(j, i)
		}

		return less(i, j)
	})
}

// derefString safely dereferences a *string, returning "" when nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
