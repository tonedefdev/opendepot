package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"k8s.io/client-go/kubernetes"
)

const (
	agentCatalogDefaultPageSize = 20
	agentCatalogMaxPageSize     = 100
	agentCatalogMaxNameLength   = 63
)

var agentCatalogNamespacePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// agentCatalogKinds are the browse kinds listed by the catalog.
var agentCatalogKinds = []string{"skill", "agent"}

// handleAgentCatalog returns a paginated catalog of the Skills and Agents the caller can see.
// Authentication and visibility follow the UI browse endpoint. The catalog fails with 500 when any
// required list cannot be read, so it never returns a partial catalog as complete.
// GET /opendepot/agents/v1/catalog
//
// Query parameters:
//
//	namespace - optional namespace filter
//	page - 1-based page number (default: 1)
//	page_size - items per page (default: 20, max: 100)
func handleAgentCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	namespace, page, pageSize, err := parseAgentCatalogQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("catalog: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	items, err := buildAgentCatalogItems(cs, r, namespace, binding, allAccess)
	if err != nil {
		logger.Error("catalog: failed to build catalog", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	offset := len(items)

	if page-1 <= len(items)/pageSize {
		offset = (page - 1) * pageSize
	}

	end := min(offset+pageSize, len(items))
	response := AgentCatalogResponse{
		Items:      items[offset:end],
		TotalCount: len(items),
		Page:       page,
		PageSize:   pageSize,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("catalog: failed to encode response", "error", err)
	}
}

// parseAgentCatalogQuery validates the catalog query parameters and applies their defaults.
func parseAgentCatalogQuery(q url.Values) (string, int, int, error) {
	namespace := ""

	if values, ok := q["namespace"]; ok {
		if len(values) != 1 || len(values[0]) > agentCatalogMaxNameLength || !agentCatalogNamespacePattern.MatchString(values[0]) {
			return "", 0, 0, errors.New("invalid namespace")
		}

		namespace = values[0]
	}

	page := 1

	if raw := q.Get("page"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p < 1 {
			return "", 0, 0, errors.New("page must be a positive integer")
		}

		page = p
	}

	pageSize := agentCatalogDefaultPageSize

	if raw := q.Get("page_size"); raw != "" {
		ps, err := strconv.Atoi(raw)
		if err != nil || ps < 1 || ps > agentCatalogMaxPageSize {
			return "", 0, 0, fmt.Errorf("page_size must be between 1 and %d", agentCatalogMaxPageSize)
		}

		pageSize = ps
	}

	return namespace, page, pageSize, nil
}

// buildAgentCatalogItems returns one item per visible Skill or Agent, sorted by namespace, kind, and name.
// Any failed list returns an error, so the caller never serves a partial catalog.
func buildAgentCatalogItems(cs *kubernetes.Clientset, r *http.Request, namespace string, binding *opendepotv1alpha1.GroupBinding, allAccess bool) ([]AgentCatalogItem, error) {
	namespaces, err := browseListNamespaces(cs, r)
	if err != nil {
		return nil, err
	}

	nsPublic := make(map[string]bool, len(namespaces))
	for _, ns := range namespaces {
		nsPublic[ns.Metadata.Name] = isPublicNamespace(ns.Metadata.Labels)
	}

	versions, err := agentCatalogVersions(cs, r)
	if err != nil {
		return nil, err
	}

	versionsByParent := make(map[string][]*opendepotv1alpha1.Version)

	for i := range versions {
		v := &versions[i]
		if v.Spec.AgentSourceRef == nil || v.Spec.AgentSourceRef.Name == nil {
			continue
		}

		key := agentCatalogParentKey(v.Namespace, v.Spec.Type, *v.Spec.AgentSourceRef.Name)
		versionsByParent[key] = append(versionsByParent[key], v)
	}

	items := make([]AgentCatalogItem, 0)

	for _, kindName := range agentCatalogKinds {
		kind := browseAgentKind(kindName)
		parents, listErr := agentCatalogParents(cs, r, kindName)
		if listErr != nil {
			return nil, listErr
		}

		for _, parent := range parents {
			ns := parent.Metadata.Namespace

			if namespace != "" && ns != namespace {
				continue
			}

			name := parent.Metadata.Name
			pub := nsPublic[ns] && isPublicResource(parent.Metadata.Labels)

			if !isBrowseVisible(pub, false, allAccess, binding, kindName, name) {
				continue
			}

			key := agentCatalogParentKey(ns, kind.versionType, name)
			items = append(items, agentCatalogItemFor(kind, kindName, ns, name, versionsByParent[key]))
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}

		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}

		return items[i].Name < items[j].Name
	})

	return items, nil
}

// agentCatalogItemFor summarizes one Skill or Agent from its Versions. LatestVersion is the newest
// installable Version, and its risk describes that Version only. UnavailableCandidate is the newest
// unavailable candidate by creation time. Status follows the AgentCatalogItem rule.
func agentCatalogItemFor(kind agentKind, kindName, namespace, name string, versions []*opendepotv1alpha1.Version) AgentCatalogItem {
	var latest *opendepotv1alpha1.Version
	var newest *opendepotv1alpha1.Version
	var newestCandidate AgentUnavailableCandidate

	for _, v := range versions {
		if agentVersionServable(v, kind, name) && !v.Spec.Yanked && v.Spec.Version != "" &&
			(latest == nil || compareVersionDesc(v.Spec.Version, latest.Spec.Version)) {
			latest = v
		}

		candidate, ok := agentUnavailableCandidateFor(v, kind, name)
		if ok && (newest == nil || agentCatalogNewer(v, newest)) {
			newest = v
			newestCandidate = candidate
		}
	}

	item := AgentCatalogItem{
		Kind:      kindName,
		Namespace: namespace,
		Name:      name,
		Status:    AgentAvailabilityPending,
	}

	if newest != nil {
		item.UnavailableCandidate = &newestCandidate
	}

	if latest != nil {
		item.LatestVersion = trimVersionPrefix(latest.Spec.Version)
		if assessment := latest.Status.JevAssessment; assessment != nil {
			item.RiskLevel = assessment.RiskLevel
			item.RiskScore = assessment.RiskScore
		}

		item.Description = agentDescriptionFor(versions, latest.Spec.Version)
	}

	switch {
	case newest != nil && (newestCandidate.Status == AgentAvailabilityBlocked || newestCandidate.Status == AgentAvailabilityRejected):
		item.Status = newestCandidate.Status
	case latest != nil:
		item.Status = AgentAvailabilityAvailable
	case newest != nil:
		item.Status = newestCandidate.Status
	}

	return item
}

// agentCatalogNewer reports whether Version a was created after Version b. Ties break on name.
func agentCatalogNewer(a, b *opendepotv1alpha1.Version) bool {
	if a.CreationTimestamp.Time.Equal(b.CreationTimestamp.Time) {
		return a.Name < b.Name
	}

	return a.CreationTimestamp.Time.After(b.CreationTimestamp.Time)
}

// agentCatalogParentKey identifies the Skill or Agent that a Version belongs to.
func agentCatalogParentKey(namespace, versionType, name string) string {
	return namespace + "/" + versionType + "/" + name
}

// agentCatalogParents lists the Skills or Agents of one browse kind.
func agentCatalogParents(cs *kubernetes.Clientset, r *http.Request, kindName string) ([]agentCardObject, error) {
	resource := browseAgentResource(kindName)
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Resource(resource).
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", resource, err)
	}

	var list struct {
		Items []agentCardObject `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s list: %w", resource, err)
	}

	return list.Items, nil
}

// agentCatalogVersions lists every Version. Only Skill and Agent Versions are matched to a parent.
func agentCatalogVersions(cs *kubernetes.Clientset, r *http.Request) ([]opendepotv1alpha1.Version, error) {
	raw, err := cs.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list versions: %w", err)
	}

	var list opendepotv1alpha1.VersionList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal versions list: %w", err)
	}

	return list.Items, nil
}
