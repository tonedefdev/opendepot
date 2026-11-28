package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// agentMCPServerPrefix is the agents.v1 URL segment and GroupBinding prefix for MCPServer resources.
const agentMCPServerPrefix = "mcpserver"

// agentKindMCPServer is the agents.v1 kind for MCPServer resources, addressed as "mcpserver" in the URL.
var agentKindMCPServer = agentKind{
	resource:    "mcpservers",
	prefix:      agentMCPServerPrefix,
	versionType: opendepotv1alpha1.OpenDepotMCPServer,
}

// mcpImageSourceType is the MCPServer source type that packages a container image instead of an archive.
const mcpImageSourceType = "Image"

// agentVersionSourceName returns the name of the Skill, Agent, or MCPServer that a Version is sourced from.
func agentVersionSourceName(version *opendepotv1alpha1.Version) *string {
	if version.Spec.Type == opendepotv1alpha1.OpenDepotMCPServer {
		if version.Spec.MCPServerSourceRef == nil {
			return nil
		}

		return version.Spec.MCPServerSourceRef.Name
	}

	if version.Spec.AgentSourceRef == nil {
		return nil
	}

	return version.Spec.AgentSourceRef.Name
}

// agentArchiveVersion returns the Version the archive helpers read. An MCPServer Version keeps its type, so its
// archive resolves to the mcpserver storage container. Its storage is read from spec.mcpServerSourceRef, which is
// copied into the source ref the archive helpers expect. The returned copy is never written back.
func agentArchiveVersion(version *opendepotv1alpha1.Version) *opendepotv1alpha1.Version {
	if version.Spec.Type != opendepotv1alpha1.OpenDepotMCPServer || version.Spec.MCPServerSourceRef == nil {
		return version
	}

	source := version.Spec.MCPServerSourceRef
	archive := *version
	archive.Spec.AgentSourceRef = &opendepotv1alpha1.AgentSourceConfig{
		Name:          source.Name,
		StorageConfig: source.StorageConfig,
	}

	return &archive
}

// isMCPImageVersion reports whether an MCPServer Version packages a container image rather than an archive.
func isMCPImageVersion(version *opendepotv1alpha1.Version) bool {
	return version.Spec.Type == opendepotv1alpha1.OpenDepotMCPServer &&
		version.Spec.MCPServerSourceRef != nil &&
		version.Spec.MCPServerSourceRef.Source.Type == mcpImageSourceType &&
		version.Spec.MCPServerSourceRef.Source.Image != nil
}

// agentCatalogKind returns the agent kind for a catalog kind name.
func agentCatalogKind(kindName string) agentKind {
	if kindName == agentKindMCPServer.prefix {
		return agentKindMCPServer
	}

	return browseAgentKind(kindName)
}

// applyAgentCatalogMCPFields sets the source type, transport, and runnable fields of an MCPServer catalog item. The
// fields come from the installable Version, or from the newest Version when none is installable. Runnable is set
// only from the synced metadata.
func applyAgentCatalogMCPFields(item *AgentCatalogItem, latest, newest *opendepotv1alpha1.Version) {
	version := latest
	if version == nil {
		version = newest
	}

	if version == nil || version.Spec.MCPServerSourceRef == nil {
		return
	}

	source := version.Spec.MCPServerSourceRef
	item.SourceType = source.Source.Type
	item.Transport = source.Transport

	if metadata := version.Status.MCPMetadata; metadata != nil {
		if metadata.Transport != "" {
			item.Transport = metadata.Transport
		}

		runnable := metadata.Runnable
		item.Runnable = &runnable
	}
}

// getOCIArtifactByName gets the OCIArtifact with the given name in the namespace.
func getOCIArtifactByName(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace, name string,
) (*opendepotv1alpha1.OCIArtifact, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("ociartifacts").
		Name(name).
		DoRaw(ctx)
	if err != nil {
		return nil, err
	}

	var artifact opendepotv1alpha1.OCIArtifact
	if err = json.Unmarshal(result, &artifact); err != nil {
		return nil, fmt.Errorf("unable to unmarshal ociartifact %s: %w", name, err)
	}

	return &artifact, nil
}

// buildMCPImageDownload returns the download response for an Image MCPServer Version. It carries the OCI reference
// and the package digest instead of archive URLs. The reference names the OCIArtifact repository in the namespace,
// on the configured registry host or the request host. It writes the error response and returns false on failure.
func buildMCPImageDownload(
	w http.ResponseWriter,
	r *http.Request,
	namespace, name string,
	version *opendepotv1alpha1.Version,
) (AgentImageDownloadResponse, bool) {
	metadata := version.Status.MCPMetadata
	if metadata == nil || metadata.PackageDigest == "" {
		http.Error(w, "version artifact not yet available", http.StatusServiceUnavailable)
		return AgentImageDownloadResponse{}, false
	}

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("mcpserver image: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return AgentImageDownloadResponse{}, false
	}

	artifactName := version.Spec.MCPServerSourceRef.Source.Image.OCIArtifactRef.Name

	artifact, err := getOCIArtifactByName(r.Context(), cs, namespace, artifactName)
	if k8sApiErrors.IsNotFound(err) {
		http.Error(w, "version artifact not yet available", http.StatusServiceUnavailable)
		return AgentImageDownloadResponse{}, false
	}

	if err != nil {
		logger.Error("mcpserver image: unable to get ociartifact",
			"error", err, "namespace", namespace, "ociartifact", artifactName)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return AgentImageDownloadResponse{}, false
	}

	host := r.Host
	if *opendepotRegistryHost != "" {
		host = *opendepotRegistryHost
	}

	return AgentImageDownloadResponse{
		Protocols:    []string{"1.0"},
		Kind:         agentKindMCPServer.prefix,
		Name:         name,
		Version:      version.Spec.Version,
		Yanked:       version.Spec.Yanked,
		OCIReference: fmt.Sprintf("%s/%s/%s@%s", host, namespace, ociRepositoryName(artifact), metadata.PackageDigest),
		OCIDigest:    metadata.PackageDigest,
		Assessment:   browseJevAssessment(version.Status.JevAssessment),
	}, true
}
