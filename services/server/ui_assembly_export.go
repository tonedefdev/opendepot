package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"

	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema"
	"github.com/tonedefdev/opendepot/pkg/hclschema/assemblyexport"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
	opendepotUtils "github.com/tonedefdev/opendepot/pkg/utils"
)

type assemblyResolutionFailure struct {
	status     int
	diagnostic assemblyexport.Diagnostic
}

func handleAssemblyExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	r.Body = http.MaxBytesReader(w, r.Body, *opendepotAssemblyMaxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request assemblyexport.Request
	if err := decoder.Decode(&request); err != nil {
		status := http.StatusBadRequest
		code := "invalid_request"
		if strings.Contains(err.Error(), "request body too large") {
			status = http.StatusRequestEntityTooLarge
			code = "request_too_large"
		}
		writeAssemblyError(w, status, assemblyexport.ErrorResponse{Error: code, Message: "Assembly export request is invalid", Diagnostics: []assemblyexport.Diagnostic{{Code: code, Message: err.Error()}}})

		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeAssemblyError(w, http.StatusBadRequest, assemblyexport.ErrorResponse{Error: "invalid_request", Message: "Assembly export request contains trailing data"})

		return
	}

	nodeCount := len(request.Variables) + len(request.Modules) + len(request.Providers)
	if nodeCount > *opendepotAssemblyMaxNodes {
		writeAssemblyError(w, http.StatusRequestEntityTooLarge, assemblyexport.ErrorResponse{
			Error:   "too_many_nodes",
			Message: fmt.Sprintf("Assembly export exceeds the %d node limit", *opendepotAssemblyMaxNodes),
		})

		return
	}

	binding, allAccess, ok := browseAuthCheck(w, r)
	if !ok {
		return
	}

	clientset, err := browseSAClient()
	if err != nil {
		logger.Error("assembly: failed to create service account client", "error", err)
		writeAssemblyError(w, http.StatusInternalServerError, assemblyexport.ErrorResponse{Error: "internal_error", Message: "internal server error"})

		return
	}

	documents := assemblyexport.AuthoritativeDocuments{
		Modules:   make(map[string]hclschema.Contract, len(request.Modules)),
		Providers: make(map[string]assemblyexport.AuthoritativeProvider, len(request.Providers)),
	}
	var resolutionDiagnostics []assemblyexport.Diagnostic

	for index, module := range request.Modules {
		contract, failure := resolveAssemblyModule(clientset, r, binding, allAccess, module, fmt.Sprintf("modules[%d]", index))
		if failure != nil {
			if failure.status == http.StatusUnauthorized || failure.status == http.StatusForbidden || failure.status == http.StatusNotFound {
				writeAssemblyError(w, failure.status, assemblyexport.ErrorResponse{Error: failure.diagnostic.Code, Message: failure.diagnostic.Message, Diagnostics: []assemblyexport.Diagnostic{failure.diagnostic}})

				return
			}
			resolutionDiagnostics = append(resolutionDiagnostics, failure.diagnostic)

			continue
		}
		documents.Modules[module.NodeID] = *contract
	}

	for index, provider := range request.Providers {
		document, failure := resolveAssemblyProvider(clientset, r, binding, allAccess, provider, fmt.Sprintf("providers[%d]", index))
		if failure != nil {
			if failure.status == http.StatusUnauthorized || failure.status == http.StatusForbidden || failure.status == http.StatusNotFound {
				writeAssemblyError(w, failure.status, assemblyexport.ErrorResponse{Error: failure.diagnostic.Code, Message: failure.diagnostic.Message, Diagnostics: []assemblyexport.Diagnostic{failure.diagnostic}})

				return
			}
			resolutionDiagnostics = append(resolutionDiagnostics, failure.diagnostic)

			continue
		}
		documents.Providers[provider.NodeID] = *document
	}

	model, diagnostics := assemblyexport.BuildModel(request, *opendepotRegistryHost, documents)
	diagnostics = append(resolutionDiagnostics, diagnostics...)
	if len(diagnostics) > 0 {
		writeAssemblyError(w, http.StatusUnprocessableEntity, assemblyexport.ErrorResponse{Error: "invalid_canvas", Message: "Assembly canvas is invalid", Diagnostics: diagnostics})

		return
	}

	files, err := assemblyexport.Render(model)
	if err != nil {
		writeAssemblyError(w, http.StatusUnprocessableEntity, assemblyexport.ErrorResponse{
			Error:       "render_failed",
			Message:     "Assembly canvas could not be rendered",
			Diagnostics: []assemblyexport.Diagnostic{{Code: "render_failed", Message: err.Error()}},
		})

		return
	}

	token := requestBearerToken(r)
	if !*opendepotAnonymousAuth && token == "" {
		writeAssemblyError(w, http.StatusUnprocessableEntity, assemblyexport.ErrorResponse{
			Error:   "registry_auth_unavailable",
			Message: "Assembly export requires Bearer authentication so OpenTofu can download authorized registry artifacts",
		})

		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/x-ndjson") {
		streamAssemblyExport(w, r, files, model.Providers, token)

		return
	}

	archive, err := validateAndPackageAssembly(r.Context(), files, model.Providers, token, nil)
	if err != nil {
		var commandError *assemblyCommandError
		if errors.As(err, &commandError) {
			status := http.StatusUnprocessableEntity
			code := "tofu_" + commandError.Phase + "_failed"
			var exitError *exec.ExitError
			if commandError.Timeout {
				status = http.StatusGatewayTimeout
				code = "tofu_" + commandError.Phase + "_timeout"
			} else if !errors.As(commandError.Err, &exitError) {
				status = http.StatusInternalServerError
				code = "tofu_execution_failed"
			}
			writeAssemblyError(w, status, assemblyexport.ErrorResponse{Error: code, Message: "OpenTofu validation failed", Output: sanitizeAssemblyOutput(commandError.Output, token)})

			return
		}

		logger.Error("assembly: failed to package validated export", "error", err)
		writeAssemblyError(w, http.StatusInternalServerError, assemblyexport.ErrorResponse{Error: "internal_error", Message: "Assembly export failed"})

		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="assembly-line.zip"`)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(archive)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

type assemblyStreamEvent struct {
	Type    string                        `json:"type"`
	Phase   string                        `json:"phase,omitempty"`
	Output  string                        `json:"output,omitempty"`
	Archive string                        `json:"archive,omitempty"`
	Error   *assemblyexport.ErrorResponse `json:"error,omitempty"`
}

func streamAssemblyExport(w http.ResponseWriter, r *http.Request, files assemblyexport.Files, providers []assemblyexport.RenderProvider, token string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAssemblyError(w, http.StatusInternalServerError, assemblyexport.ErrorResponse{Error: "streaming_unavailable", Message: "Assembly progress streaming is unavailable"})

		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	writeEvent := func(event assemblyStreamEvent) {
		if err := json.NewEncoder(w).Encode(event); err == nil {
			flusher.Flush()
		}
	}
	writeEvent(assemblyStreamEvent{Type: "started", Output: "Preparing generated configuration"})

	progress := func(phase, output string) {
		writeEvent(assemblyStreamEvent{Type: "output", Phase: phase, Output: sanitizeAssemblyOutput(output, token)})
	}
	archive, err := validateAndPackageAssembly(r.Context(), files, providers, token, progress)
	if err != nil {
		response := assemblyexport.ErrorResponse{Error: "internal_error", Message: "Assembly export failed"}
		var commandError *assemblyCommandError
		if errors.As(err, &commandError) {
			response = assemblyexport.ErrorResponse{
				Error:   "tofu_" + commandError.Phase + "_failed",
				Message: "OpenTofu validation failed",
				Output:  sanitizeAssemblyOutput(commandError.Output, token),
			}
			if commandError.Timeout {
				response.Error = "tofu_" + commandError.Phase + "_timeout"
			}
		}
		writeEvent(assemblyStreamEvent{Type: "error", Error: &response})

		return
	}

	writeEvent(assemblyStreamEvent{Type: "complete", Archive: base64.StdEncoding.EncodeToString(archive)})
}

func resolveAssemblyModule(clientset *kubernetes.Clientset, r *http.Request, binding *opendepotv1alpha1.GroupBinding, allAccess bool, node assemblyexport.ModuleNode, path string) (*hclschema.Contract, *assemblyResolutionFailure) {
	rawModule, err := clientset.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(node.Namespace).Resource("modules").Name(node.Name).DoRaw(r.Context())
	if err != nil {
		if k8sApiErrors.IsNotFound(err) {
			return nil, assemblyFailure(http.StatusNotFound, "module_not_found", "module was not found", path, node.NodeID)
		}

		return nil, assemblyFailure(http.StatusInternalServerError, "module_unavailable", "module could not be resolved", path, node.NodeID)
	}

	var module opendepotv1alpha1.Module
	if err := json.Unmarshal(rawModule, &module); err != nil {
		return nil, assemblyFailure(http.StatusInternalServerError, "module_unavailable", "module could not be resolved", path, node.NodeID)
	}

	labels, err := browseGetNamespaceLabels(clientset, r, node.Namespace)
	if err != nil {
		return nil, assemblyFailure(http.StatusInternalServerError, "module_unavailable", "module namespace could not be resolved", path, node.NodeID)
	}
	public := isPublicNamespace(labels) && isPublicResource(module.Labels)
	if !isBrowseVisible(public, false, allAccess, binding, "module", module.Name) {
		return nil, assemblyFailure(http.StatusNotFound, "module_not_found", "module was not found", path, node.NodeID)
	}

	versions, err := browseListModuleVersions(clientset, r, node.Namespace, node.Name)
	if err != nil {
		return nil, assemblyFailure(http.StatusInternalServerError, "module_unavailable", "module versions could not be resolved", path, node.NodeID)
	}
	target := opendepotUtils.SanitizeVersion(node.Version)
	var reference *opendepotv1alpha1.ContractConfigMapRef
	for index := range versions {
		version := &versions[index]
		if opendepotUtils.SanitizeVersion(version.Spec.Version) == target && version.Status.Synced && version.Status.ContractConfigMapRef != nil {
			reference = version.Status.ContractConfigMapRef

			break
		}
	}
	if reference == nil {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "module_version_unavailable", "exact module version has no available Assembly contract", path+".version", node.NodeID)
	}

	rawConfigMap, err := clientset.RESTClient().Get().AbsPath("/api/v1").Namespace(node.Namespace).Resource("configmaps").Name(reference.Name).DoRaw(r.Context())
	if err != nil {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "module_contract_unavailable", "module contract could not be read", path, node.NodeID)
	}
	var configMap k8sConfigMap
	if err := json.Unmarshal(rawConfigMap, &configMap); err != nil {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "module_contract_unavailable", "module contract could not be decoded", path, node.NodeID)
	}
	encoded, exists := configMap.Data[reference.Key]
	if !exists {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "module_contract_unavailable", "module contract could not be read", path, node.NodeID)
	}
	contractJSON, err := decodeContract(encoded)
	if err != nil {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "module_contract_unavailable", "module contract could not be decoded", path, node.NodeID)
	}

	var contract hclschema.Contract
	if err := json.Unmarshal(contractJSON, &contract); err != nil {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "module_contract_unavailable", "module contract could not be decoded", path, node.NodeID)
	}

	return &contract, nil
}

func resolveAssemblyProvider(clientset *kubernetes.Clientset, r *http.Request, binding *opendepotv1alpha1.GroupBinding, allAccess bool, node assemblyexport.ProviderNode, path string) (*assemblyexport.AuthoritativeProvider, *assemblyResolutionFailure) {
	rawProvider, err := clientset.RESTClient().Get().AbsPath("/apis/opendepot.defdev.io/v1alpha1").Namespace(node.Namespace).Resource("providers").Name(node.Name).DoRaw(r.Context())
	if err != nil {
		if k8sApiErrors.IsNotFound(err) {
			return nil, assemblyFailure(http.StatusNotFound, "provider_not_found", "provider was not found", path, node.NodeID)
		}

		return nil, assemblyFailure(http.StatusInternalServerError, "provider_unavailable", "provider could not be resolved", path, node.NodeID)
	}

	var provider opendepotv1alpha1.Provider
	if err := json.Unmarshal(rawProvider, &provider); err != nil {
		return nil, assemblyFailure(http.StatusInternalServerError, "provider_unavailable", "provider could not be resolved", path, node.NodeID)
	}
	labels, err := browseGetNamespaceLabels(clientset, r, node.Namespace)
	if err != nil {
		return nil, assemblyFailure(http.StatusInternalServerError, "provider_unavailable", "provider namespace could not be resolved", path, node.NodeID)
	}
	public := isPublicNamespace(labels) && isPublicResource(provider.Labels)
	if !isBrowseVisible(public, false, allAccess, binding, "provider", provider.Name) {
		return nil, assemblyFailure(http.StatusNotFound, "provider_not_found", "provider was not found", path, node.NodeID)
	}

	if !assemblyProviderSupported(&provider.Spec.ProviderConfig) {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "provider_registry_unsupported", "Assembly Line supports only registry.opentofu.org providers", path, node.NodeID)
	}

	providerName := node.Name
	if provider.Spec.ProviderConfig.Name != nil && strings.TrimSpace(*provider.Spec.ProviderConfig.Name) != "" {
		providerName = strings.TrimSpace(*provider.Spec.ProviderConfig.Name)
	}
	providerNamespace := "hashicorp"
	if provider.Spec.ProviderConfig.Namespace != nil && strings.TrimSpace(*provider.Spec.ProviderConfig.Namespace) != "" {
		providerNamespace = strings.TrimSpace(*provider.Spec.ProviderConfig.Namespace)
	}

	versions, err := browseListProviderVersions(clientset, r, node.Namespace)
	if err != nil {
		return nil, assemblyFailure(http.StatusInternalServerError, "provider_unavailable", "provider versions could not be resolved", path, node.NodeID)
	}
	versions = providerVersionsFor(versions, providerName)
	target := opendepotUtils.SanitizeVersion(node.Version)
	var selected *opendepotv1alpha1.Version
	for index := range versions {
		version := &versions[index]
		if opendepotUtils.SanitizeVersion(version.Spec.Version) == target && version.Status.Synced && version.Status.ProviderSchemaRef != nil {
			selected = version

			break
		}
	}
	if selected == nil || selected.Spec.ProviderConfigRef == nil || selected.Spec.ProviderConfigRef.StorageConfig == nil || selected.Spec.ProviderConfigRef.Name == nil {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "provider_schema_unavailable", "exact provider version has no available configuration schema", path+".version", node.NodeID)
	}

	backend, err := initStorageBackend(r.Context(), selected.Spec.ProviderConfigRef.StorageConfig)
	if err != nil {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "provider_schema_unavailable", "provider schema storage is unavailable", path, node.NodeID)
	}
	key := selected.Status.ProviderSchemaRef.Key
	storageInput := &storageTypes.StorageObjectInput{
		Method:        storageTypes.Get,
		FilePath:      &key,
		StorageConfig: selected.Spec.ProviderConfigRef.StorageConfig,
		ContainerName: selected.Spec.ProviderConfigRef.Name,
		Version:       selected,
	}
	reader, err := backend.GetObject(r.Context(), storageInput)
	if err != nil || reader == nil {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "provider_schema_unavailable", "provider configuration schema could not be read", path, node.NodeID)
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}
	schema, err := decodeProviderSchema(reader, selected.Status.ProviderSchemaRef.Digest)
	if err != nil || schema.SchemaVersion != "assembly.provider.v1" {
		return nil, assemblyFailure(http.StatusUnprocessableEntity, "provider_schema_unavailable", "provider configuration schema is invalid", path, node.NodeID)
	}

	return &assemblyexport.AuthoritativeProvider{
		Namespace:         node.Namespace,
		Name:              node.Name,
		ProviderNamespace: providerNamespace,
		ProviderName:      providerName,
		Version:           target,
		Configuration:     schema.ProviderBlock,
	}, nil
}

func assemblyProviderSupported(config *opendepotv1alpha1.ProviderConfig) bool {
	return opendepotv1alpha1.ProviderUpstreamRegistry(config) == opendepotv1alpha1.OpenTofuRegistryHost
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}

		return err
	}

	return nil
}

func requestBearerToken(r *http.Request) string {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func sanitizeAssemblyOutput(output, token string) string {
	if token == "" {
		return output
	}

	return strings.ReplaceAll(output, token, "[redacted]")
}

func assemblyFailure(status int, code, message, path, nodeID string) *assemblyResolutionFailure {
	return &assemblyResolutionFailure{status: status, diagnostic: assemblyexport.Diagnostic{Code: code, Message: message, Path: path, NodeID: nodeID}}
}

func writeAssemblyError(w http.ResponseWriter, status int, response assemblyexport.ErrorResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
