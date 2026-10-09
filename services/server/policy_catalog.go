package main

import (
	"encoding/json"
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

type scanPolicyCatalog struct {
	WritesEnabled bool                         `json:"writesEnabled"`
	Items         []scanPolicyCatalogNamespace `json:"items"`
}

type scanPolicyCatalogNamespace struct {
	Namespace                      string                      `json:"namespace"`
	CanRead                        bool                        `json:"canRead"`
	CanWrite                       bool                        `json:"canWrite"`
	CanManageNamespaceWidePolicies bool                        `json:"canManageNamespaceWidePolicies"`
	Modules                        []scanPolicyCatalogResource `json:"modules"`
	Providers                      []scanPolicyCatalogResource `json:"providers"`
	Skills                         []scanPolicyCatalogResource `json:"skills"`
	Agents                         []scanPolicyCatalogResource `json:"agents"`
}

type scanPolicyCatalogResource struct {
	Name string `json:"name"`
}

func handleScanPolicyCatalog(w http.ResponseWriter, r *http.Request) {
	clientset, _, binding, _, err := policyRequestAuth(w, r)
	if err != nil {
		return
	}

	if oidcVerifier != nil && binding == nil {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	namespaces, err := catalogNamespaces(r, clientset.RESTClient(), binding)
	if err != nil {
		writeKubernetesError(w, err)

		return
	}

	items := make([]scanPolicyCatalogNamespace, 0, len(namespaces))
	for _, namespace := range namespaces {
		item, err := catalogNamespace(r, clientset.RESTClient(), binding, namespace)
		if err != nil {
			writeKubernetesError(w, err)

			return
		}

		if len(item.Modules) == 0 && len(item.Providers) == 0 && len(item.Skills) == 0 && len(item.Agents) == 0 && !item.CanManageNamespaceWidePolicies {
			continue
		}

		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Namespace < items[j].Namespace })
	writeJSONValue(w, http.StatusOK, scanPolicyCatalog{
		WritesEnabled: *opendepotPolicyManagementEnabled && !*opendepotAnonymousAuth,
		Items:         items,
	})
}

func catalogNamespaces(r *http.Request, request rest.Interface, binding *opendepotv1alpha1.SecurityGroupBinding) ([]string, error) {
	if binding == nil {
		return nil, nil
	}

	for _, namespace := range binding.Spec.Namespaces {
		if namespace == "*" {
			raw, err := request.Get().AbsPath("/api/v1").Resource("namespaces").DoRaw(r.Context())
			if err != nil {
				return nil, err
			}

			var list corev1.NamespaceList
			if err := json.Unmarshal(raw, &list); err != nil {
				return nil, err
			}

			namespaces := make([]string, 0, len(list.Items))
			for _, item := range list.Items {
				namespaces = append(namespaces, item.Name)
			}
			sort.Strings(namespaces)

			return namespaces, nil
		}
	}

	namespaces := append([]string(nil), binding.Spec.Namespaces...)
	sort.Strings(namespaces)
	return namespaces, nil
}

func catalogNamespace(r *http.Request, request rest.Interface, binding *opendepotv1alpha1.SecurityGroupBinding, namespace string) (scanPolicyCatalogNamespace, error) {
	item := scanPolicyCatalogNamespace{
		Namespace:                      namespace,
		CanRead:                        true,
		CanWrite:                       !*opendepotAnonymousAuth && *opendepotPolicyManagementEnabled,
		CanManageNamespaceWidePolicies: binding == nil || binding.Spec.NamespaceWidePolicyManagement,
		Modules:                        []scanPolicyCatalogResource{},
		Providers:                      []scanPolicyCatalogResource{},
		Skills:                         []scanPolicyCatalogResource{},
		Agents:                         []scanPolicyCatalogResource{},
	}

	moduleRaw, err := request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("modules").DoRaw(r.Context())
	if err != nil {
		return item, err
	}
	var modules opendepotv1alpha1.ModuleList
	if err := json.Unmarshal(moduleRaw, &modules); err != nil {
		return item, err
	}
	for _, module := range modules.Items {
		if isSecurityResourceAllowed(binding, "module", module.Name) {
			item.Modules = append(item.Modules, scanPolicyCatalogResource{Name: module.Name})
		}
	}

	providerRaw, err := request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("providers").DoRaw(r.Context())
	if err != nil {
		return item, err
	}
	var providers opendepotv1alpha1.ProviderList
	if err := json.Unmarshal(providerRaw, &providers); err != nil {
		return item, err
	}
	for _, provider := range providers.Items {
		if isSecurityResourceAllowed(binding, "provider", provider.Name) {
			item.Providers = append(item.Providers, scanPolicyCatalogResource{Name: provider.Name})
		}
	}

	skillRaw, err := request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("skills").DoRaw(r.Context())
	if err != nil {
		return item, err
	}
	var skills opendepotv1alpha1.SkillList
	if err := json.Unmarshal(skillRaw, &skills); err != nil {
		return item, err
	}
	for _, skill := range skills.Items {
		if isSecurityResourceAllowed(binding, "skill", skill.Name) {
			item.Skills = append(item.Skills, scanPolicyCatalogResource{Name: skill.Name})
		}
	}

	agentRaw, err := request.Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(namespace).Resource("agents").DoRaw(r.Context())
	if err != nil {
		return item, err
	}
	var agents opendepotv1alpha1.AgentList
	if err := json.Unmarshal(agentRaw, &agents); err != nil {
		return item, err
	}
	for _, agent := range agents.Items {
		if isSecurityResourceAllowed(binding, "agent", agent.Name) {
			item.Agents = append(item.Agents, scanPolicyCatalogResource{Name: agent.Name})
		}
	}

	sort.Slice(item.Modules, func(i, j int) bool { return item.Modules[i].Name < item.Modules[j].Name })
	sort.Slice(item.Providers, func(i, j int) bool { return item.Providers[i].Name < item.Providers[j].Name })
	sort.Slice(item.Skills, func(i, j int) bool { return item.Skills[i].Name < item.Skills[j].Name })
	sort.Slice(item.Agents, func(i, j int) bool { return item.Agents[i].Name < item.Agents[j].Name })
	return item, nil
}
