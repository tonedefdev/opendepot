package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const maxScanPolicyBody = 1 << 20

var dnsNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

var policyRequestAuth = getPolicyKubeClientFromRequest

// handleScanPolicies exposes the first server-side write surface for ScanPolicy CRs.
// Kubernetes remains the source of truth for validation, authorization, resource versions,
// and audit metadata; this endpoint only adds payload validation and a stable UI contract.
func handleScanPolicies(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if namespace == "" {
		http.Error(w, "namespace is required", http.StatusBadRequest)

		return
	}

	clientset, _, securityBinding, subject, err := policyRequestAuth(w, r)
	if err != nil {
		return
	}

	if oidcVerifier != nil && !policyNamespaceAuthorized(securityBinding, namespace) {
		http.Error(w, "not found", http.StatusNotFound)

		return
	}

	if r.Method != http.MethodGet && *opendepotAnonymousAuth {
		http.Error(w, "ScanPolicy mutations require authenticated access", http.StatusForbidden)

		return
	}

	if oidcVerifier != nil && r.Method != http.MethodGet && !policyWriteAuthorized(securityBinding, namespace) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	if r.Method != http.MethodGet && !*opendepotPolicyManagementEnabled {
		http.Error(w, "ScanPolicy writes are disabled", http.StatusNotFound)

		return
	}

	request := clientset.RESTClient()

	switch r.Method {
	case http.MethodGet:
		handleScanPolicyGet(w, r, request, namespace, name, securityBinding)
	case http.MethodPost:
		handleScanPolicyCreate(w, r, request, namespace, subject, securityBinding)
	case http.MethodPut:
		handleScanPolicyUpdate(w, r, request, namespace, name, subject, securityBinding)
	case http.MethodDelete:
		handleScanPolicyDelete(w, r, request, namespace, name, subject, securityBinding)
	default:
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func policyNamespaceAuthorized(binding *opendepotv1alpha1.SecurityGroupBinding, namespace string) bool {
	if oidcVerifier == nil {
		return true
	}

	if binding == nil {
		return false
	}

	for _, allowedNamespace := range binding.Spec.Namespaces {
		if allowedNamespace == "*" || allowedNamespace == namespace {
			return true
		}
	}

	return false
}

func policyWriteAuthorized(binding *opendepotv1alpha1.SecurityGroupBinding, namespace string) bool {
	if oidcVerifier == nil {
		return true
	}

	return policyNamespaceAuthorized(binding, namespace)
}

func policyScopeAuthorized(binding *opendepotv1alpha1.SecurityGroupBinding, namespace string, policy *opendepotv1alpha1.ScanPolicy) bool {
	if oidcVerifier == nil {
		return true
	}

	if !policyNamespaceAuthorized(binding, namespace) {
		return false
	}

	if policy.Spec.Selector != nil || len(policy.Spec.TargetRefs) == 0 {
		return binding.Spec.NamespaceWidePolicyManagement
	}

	for _, target := range policy.Spec.TargetRefs {
		resourceType := strings.ToLower(target.Kind)
		if !isSecurityResourceAllowed(binding, resourceType, target.Name) {
			return false
		}
	}

	return true
}

func policyReadAuthorized(binding *opendepotv1alpha1.SecurityGroupBinding, namespace string, policy *opendepotv1alpha1.ScanPolicy) bool {
	return policyScopeAuthorized(binding, namespace, policy)
}

func handleScanPolicyCapabilitiesRequest(w http.ResponseWriter, r *http.Request) {
	_, _, securityBinding, _, err := policyRequestAuth(w, r)
	if err != nil {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	if oidcVerifier != nil && !policyNamespaceAuthorized(securityBinding, namespace) {
		http.Error(w, "not found", http.StatusNotFound)

		return
	}

	handleScanPolicyCapabilities(w, r, securityBinding, namespace)
}

func handleScanPolicyPreviewRequest(w http.ResponseWriter, r *http.Request) {
	clientset, _, securityBinding, _, err := policyRequestAuth(w, r)
	if err != nil {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	if oidcVerifier != nil && !policyNamespaceAuthorized(securityBinding, namespace) {
		http.Error(w, "not found", http.StatusNotFound)

		return
	}

	handleScanPolicyPreview(w, r, clientset.RESTClient(), securityBinding, namespace)
}

func handleScanPolicyGet(w http.ResponseWriter, r *http.Request, request rest.Interface, namespace, name string, binding *opendepotv1alpha1.SecurityGroupBinding) {
	var raw []byte
	var err error
	if name == "" {
		raw, err = request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("scanpolicies").DoRaw(r.Context())
	} else {
		raw, err = request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("scanpolicies").Name(name).DoRaw(r.Context())
	}

	if err != nil {
		writeKubernetesError(w, err)

		return
	}

	if name == "" {
		var list opendepotv1alpha1.ScanPolicyList
		if err := json.Unmarshal(raw, &list); err != nil {
			http.Error(w, "failed to decode ScanPolicy list", http.StatusInternalServerError)

			return
		}

		filtered := list.Items[:0]
		for i := range list.Items {
			if policyReadAuthorized(binding, namespace, &list.Items[i]) {
				filtered = append(filtered, list.Items[i])
			}
		}
		list.Items = filtered
		raw, err = json.Marshal(list)
		if err != nil {
			http.Error(w, "failed to encode ScanPolicy list", http.StatusInternalServerError)

			return
		}
	} else {
		var policy opendepotv1alpha1.ScanPolicy
		if err := json.Unmarshal(raw, &policy); err != nil {
			http.Error(w, "failed to decode ScanPolicy", http.StatusInternalServerError)

			return
		}

		if !policyReadAuthorized(binding, namespace, &policy) {
			http.Error(w, "not found", http.StatusNotFound)

			return
		}
	}

	writeJSON(w, http.StatusOK, raw)
}

func handleScanPolicyCapabilities(w http.ResponseWriter, r *http.Request, binding *opendepotv1alpha1.SecurityGroupBinding, namespace string) {
	canWrite := !*opendepotAnonymousAuth && *opendepotPolicyManagementEnabled && policyWriteAuthorized(binding, namespace)
	writeJSONValue(w, http.StatusOK, map[string]any{
		"writesEnabled":                  *opendepotPolicyManagementEnabled && !*opendepotAnonymousAuth,
		"canRead":                        *opendepotAnonymousAuth || oidcVerifier == nil || binding != nil,
		"canWrite":                       canWrite,
		"canManageNamespaceWidePolicies": oidcVerifier == nil || binding != nil && binding.Spec.NamespaceWidePolicyManagement,
		"methods":                        []string{"GET", "POST", "PUT", "DELETE"},
		"supportsPreview":                true,
	})
}

func handleScanPolicyPreview(w http.ResponseWriter, r *http.Request, request rest.Interface, binding *opendepotv1alpha1.SecurityGroupBinding, namespace string) {
	if oidcVerifier != nil && binding == nil {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	body, err := readScanPolicyBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	var policy opendepotv1alpha1.ScanPolicy
	if err := decodeAndValidateScanPolicy(body, &policy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if policy.Namespace == "" {
		policy.Namespace = namespace
	}

	if err := validateScanPolicyTargets(r.Context(), request, policy.Namespace, &policy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if oidcVerifier != nil && !policyScopeAuthorized(binding, policy.Namespace, &policy) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	writeJSONValue(w, http.StatusOK, map[string]any{
		"valid":  true,
		"policy": policy,
	})
}

func handleScanPolicyCreate(w http.ResponseWriter, r *http.Request, request rest.Interface, namespace, subject string, binding *opendepotv1alpha1.SecurityGroupBinding) {
	body, err := readScanPolicyBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	var policy opendepotv1alpha1.ScanPolicy
	if err := decodeAndValidateScanPolicy(body, &policy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if policy.Namespace == "" {
		policy.Namespace = namespace
	}

	if policy.Namespace != namespace {
		http.Error(w, "metadata.namespace must match the URL namespace", http.StatusBadRequest)

		return
	}

	if err := validateScanPolicyTargets(r.Context(), request, namespace, &policy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if oidcVerifier != nil && !policyScopeAuthorized(binding, namespace, &policy) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	body, err = json.Marshal(policy)
	if err != nil {
		http.Error(w, "failed to encode ScanPolicy", http.StatusInternalServerError)

		return
	}

	raw, err := request.Post().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("scanpolicies").Body(body).DoRaw(r.Context())
	if err != nil {
		writeKubernetesError(w, err)

		return
	}

	logger.Info("ScanPolicy created", "namespace", namespace, "name", policy.Name, "subject", subject)
	writeJSON(w, http.StatusCreated, raw)
}

func handleScanPolicyUpdate(w http.ResponseWriter, r *http.Request, request rest.Interface, namespace, name, subject string, binding *opendepotv1alpha1.SecurityGroupBinding) {
	resourceVersion, err := requireIfMatch(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusPreconditionRequired)

		return
	}

	body, err := readScanPolicyBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	var policy opendepotv1alpha1.ScanPolicy
	if err := decodeAndValidateScanPolicy(body, &policy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if policy.Name != "" && policy.Name != name {
		http.Error(w, "metadata.name must match the URL name", http.StatusBadRequest)

		return
	}

	if policy.ResourceVersion == "" || policy.ResourceVersion != resourceVersion {
		http.Error(w, "If-Match must match metadata.resourceVersion", http.StatusConflict)

		return
	}

	policy.Name = name
	policy.Namespace = namespace

	if err := validateScanPolicyTargets(r.Context(), request, namespace, &policy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	existingRaw, err := request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("scanpolicies").Name(name).DoRaw(r.Context())
	if err != nil {
		writeKubernetesError(w, err)

		return
	}

	var existing opendepotv1alpha1.ScanPolicy
	if err := json.Unmarshal(existingRaw, &existing); err != nil {
		http.Error(w, "failed to decode persisted ScanPolicy", http.StatusInternalServerError)

		return
	}

	if oidcVerifier != nil && (!policyScopeAuthorized(binding, namespace, &existing) || !policyScopeAuthorized(binding, namespace, &policy)) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	body, err = json.Marshal(policy)
	if err != nil {
		http.Error(w, "failed to encode ScanPolicy", http.StatusInternalServerError)

		return
	}

	raw, err := request.Put().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("scanpolicies").Name(name).Body(body).DoRaw(r.Context())
	if err != nil {
		writeKubernetesError(w, err)

		return
	}

	logger.Info("ScanPolicy updated", "namespace", namespace, "name", name, "subject", subject)
	writeJSON(w, http.StatusOK, raw)
}

func handleScanPolicyDelete(w http.ResponseWriter, r *http.Request, request rest.Interface, namespace, name, subject string, binding *opendepotv1alpha1.SecurityGroupBinding) {
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)

		return
	}

	resourceVersion, err := requireIfMatch(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusPreconditionRequired)

		return
	}

	existingRaw, err := request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("scanpolicies").Name(name).DoRaw(r.Context())
	if err != nil {
		writeKubernetesError(w, err)

		return
	}

	var existing opendepotv1alpha1.ScanPolicy
	if err := json.Unmarshal(existingRaw, &existing); err != nil {
		http.Error(w, "failed to decode persisted ScanPolicy", http.StatusInternalServerError)

		return
	}

	if oidcVerifier != nil && !policyScopeAuthorized(binding, namespace, &existing) {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	deleteBody, err := json.Marshal(metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{ResourceVersion: &resourceVersion},
	})
	if err != nil {
		http.Error(w, "failed to encode delete precondition", http.StatusInternalServerError)

		return
	}

	_, err = request.Delete().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("scanpolicies").Name(name).Body(deleteBody).DoRaw(r.Context())
	if err != nil {
		writeKubernetesError(w, err)

		return
	}

	logger.Info("ScanPolicy deleted", "namespace", namespace, "name", name, "subject", subject)
	w.WriteHeader(http.StatusNoContent)
}

func requireIfMatch(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	value = strings.Trim(value, `"`)
	if value == "" || value == "*" {
		return "", fmt.Errorf("If-Match header is required")
	}

	return value, nil
}

func readScanPolicyBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxScanPolicyBody+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read request body")
	}

	if len(body) > maxScanPolicyBody {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxScanPolicyBody)
	}

	return body, nil
}

func decodeAndValidateScanPolicy(body []byte, policy *opendepotv1alpha1.ScanPolicy) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(policy); err != nil {
		return fmt.Errorf("invalid ScanPolicy JSON: %w", err)
	}

	if policy.Name != "" && !dnsNamePattern.MatchString(policy.Name) {
		return fmt.Errorf("metadata.name must be a DNS-compatible name")
	}

	switch strings.ToUpper(policy.Spec.SeverityThreshold) {
	case "", "CRITICAL", "HIGH", "MEDIUM", "LOW", "NONE":
	default:
		return fmt.Errorf("spec.severityThreshold must be one of CRITICAL, HIGH, MEDIUM, LOW, or NONE")
	}

	for i, target := range policy.Spec.TargetRefs {
		if target.Kind != "Module" && target.Kind != "Provider" && target.Kind != "Skill" && target.Kind != "Agent" {
			return fmt.Errorf("spec.targetRefs[%d].kind must be Module, Provider, Skill, or Agent", i)
		}
		if target.Name == "" {
			return fmt.Errorf("spec.targetRefs[%d].name is required", i)
		}
	}

	for i, exemption := range policy.Spec.Exemptions {
		if strings.TrimSpace(exemption.Reason) == "" {
			return fmt.Errorf("spec.exemptions[%d].reason is required", i)
		}
	}

	return nil
}

func validateScanPolicyTargets(ctx context.Context, request rest.Interface, namespace string, policy *opendepotv1alpha1.ScanPolicy) error {
	resources := map[string]map[string]struct{}{}

	for _, target := range policy.Spec.TargetRefs {
		if target.Name == "*" {
			return fmt.Errorf("spec.targetRefs cannot target wildcard resource %q", target.Name)
		}

		resourceType := strings.ToLower(target.Kind)
		names, ok := resources[resourceType]
		if !ok {
			raw, err := request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource(resourceType + "s").DoRaw(ctx)
			if err != nil {
				return fmt.Errorf("failed to list %s resources: %w", target.Kind, err)
			}

			names = map[string]struct{}{}
			switch resourceType {
			case "module":
				var list opendepotv1alpha1.ModuleList
				if err := json.Unmarshal(raw, &list); err != nil {
					return fmt.Errorf("failed to decode Module list: %w", err)
				}
				for _, item := range list.Items {
					names[item.Name] = struct{}{}
				}
			case "skill":
				var list opendepotv1alpha1.SkillList
				if err := json.Unmarshal(raw, &list); err != nil {
					return fmt.Errorf("failed to decode Skill list: %w", err)
				}
				for _, item := range list.Items {
					names[item.Name] = struct{}{}
				}
			case "agent":
				var list opendepotv1alpha1.AgentList
				if err := json.Unmarshal(raw, &list); err != nil {
					return fmt.Errorf("failed to decode Agent list: %w", err)
				}
				for _, item := range list.Items {
					names[item.Name] = struct{}{}
				}
			default:
				var list opendepotv1alpha1.ProviderList
				if err := json.Unmarshal(raw, &list); err != nil {
					return fmt.Errorf("failed to decode Provider list: %w", err)
				}
				for _, item := range list.Items {
					names[item.Name] = struct{}{}
				}
			}
			resources[resourceType] = names
		}

		if _, ok := names[target.Name]; !ok {
			return fmt.Errorf("spec.targetRefs target %s/%s is not an onboarded resource in namespace %q", target.Kind, target.Name, namespace)
		}
	}

	return nil
}

func writeKubernetesError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if apiStatus, ok := err.(k8serrors.APIStatus); ok {
		status = int(apiStatus.Status().Code)
	} else if strings.Contains(err.Error(), "the server rejected our request") {
		status = http.StatusConflict
	}

	http.Error(w, http.StatusText(status), status)
}

func writeJSON(w http.ResponseWriter, status int, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func writeJSONValue(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)

		return
	}

	writeJSON(w, status, raw)
}
