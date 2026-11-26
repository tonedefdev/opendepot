package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPServerSpec defines the desired state of MCPServer.
type MCPServerSpec struct {
	// A flag to force an MCPServer to synchronize.
	ForceSync bool `json:"forceSync,omitempty"`
	// The configuration of the MCPServer, which defines where its content comes from and how it is versioned.
	MCPServerSourceConfig MCPServerSourceConfig `json:"mcpServerSourceConfig"`
	// The versions of the MCPServer that the controller should keep in sync.
	Versions []MCPServerVersion `json:"versions"`
}

// MCPServerStatus defines the observed state of MCPServer.
type MCPServerStatus struct {
	// The latest available version of the MCPServer.
	LatestVersion *string `json:"latestVersion,omitempty"`
	// A flag to determine if the MCPServer has successfully synced to its desired state.
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled.
	SyncStatus string `json:"syncStatus"`
	// A map of the MCPServerVersion references that have been successfully created by the controller.
	VersionRefs map[string]*MCPServerVersion `json:"versionRefs,omitempty"`
}

// MCPServerVersion holds details about the Version resource under management.
type MCPServerVersion struct {
	// The name of the MCPServer.
	Name string `json:"name"`
	// The version of the MCPServer.
	Version string `json:"version"`
	// The file name of the MCP server package.
	FileName *string `json:"fileName,omitempty"`
	// Whether the Version for the MCPServer has synced or not.
	Synced bool `json:"synced"`
}

// MCPServerSourceConfig is the configuration settings for the MCPServer and for each Version created by the agent controller.
type MCPServerSourceConfig struct {
	// The name of the MCP server. If omitted, the name of the MCPServer resource is used in its place.
	Name *string `json:"name,omitempty"`
	// The source that the MCP server package is built from.
	// +kubebuilder:validation:Required
	Source MCPServerSource `json:"source"`
	// The transport that the MCP server speaks.
	// +kubebuilder:validation:Enum=stdio;streamable-http
	// +kubebuilder:default=stdio
	Transport string `json:"transport,omitempty"`
	// The entrypoint that overrides the default binary or console script of the package.
	// +optional
	Entrypoint *MCPEntrypoint `json:"entrypoint,omitempty"`
	// A comma separated list of version constraints such as '1.2.1' or '>= 1.0.0, < 2.0.0' or '~> 1.0.0, != 1.0.2'.
	VersionConstraints string `json:"versionConstraints,omitempty"`
	// The number of versions to keep stored in the registry at any given time.
	// +kubebuilder:validation:Minimum=0
	VersionHistoryLimit *int `json:"versionHistoryLimit,omitempty"`
	// When true, enforces that the ChecksumSHA256 of the Version always matches the value stored in this field
	// and in any destination storage config.
	Immutable *bool `json:"immutable,omitempty"`
	// The Github client configuration settings.
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	// The external storage configuration settings.
	StorageConfig *StorageConfig `json:"storageConfig,omitempty"`
	// A reference to the key in a Secret in the same namespace that holds the TypeSafe Jev token.
	// The key defaults to 'jevToken' when omitted.
	JevSecretRef *corev1.SecretKeySelector `json:"jevSecretRef,omitempty"`
	// The thresholds that gate a Version on its Jev assessment.
	JevPolicy *JevPolicy `json:"jevPolicy,omitempty"`
}

// MCPServerSource selects the package source of an MCPServer. Exactly the block that matches type must be set.
// +kubebuilder:validation:XValidation:rule="(self.type == 'Image') == has(self.image) && (self.type == 'Npm') == has(self.npm) && (self.type == 'PyPI') == has(self.pypi) && (self.type == 'Source') == has(self.source)",message="set exactly the block that matches type"
type MCPServerSource struct {
	// The type of the package source. One of 'Image', 'Npm', 'PyPI', or 'Source'.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Image;Npm;PyPI;Source
	Type string `json:"type"`
	// The OCIArtifact that holds the container image. Only valid when type is 'Image'.
	Image *MCPImageSource `json:"image,omitempty"`
	// The npm package. Only valid when type is 'Npm'.
	Npm *MCPNpmSource `json:"npm,omitempty"`
	// The PyPI package. Only valid when type is 'PyPI'.
	PyPI *MCPPyPISource `json:"pypi,omitempty"`
	// The GitHub repository. Only valid when type is 'Source'.
	Source *MCPRepoSource `json:"source,omitempty"`
}

// MCPImageSource references an OCIArtifact of artifactType Image that holds the MCP server container image.
type MCPImageSource struct {
	// A reference to the OCIArtifact resource in the same namespace. The OCIArtifact must have artifactType Image.
	// +kubebuilder:validation:Required
	OCIArtifactRef corev1.LocalObjectReference `json:"ociArtifactRef"`
}

// MCPNpmSource is an npm package that holds the MCP server.
type MCPNpmSource struct {
	// The name of the npm package.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=214
	// +kubebuilder:validation:Pattern=`^(@[a-z0-9-~][a-z0-9-._~]*/)?[a-z0-9-~][a-z0-9-._~]*$`
	Package string `json:"package"`
	// The npm registry that hosts the package. It must be on the allow-list.
	Registry *string `json:"registry,omitempty"`
}

// MCPPyPISource is a PyPI package that holds the MCP server.
type MCPPyPISource struct {
	// The name of the PyPI package.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=255
	Package string `json:"package"`
	// The simple index that hosts the package. It must be on the allow-list.
	Index *string `json:"index,omitempty"`
	// The wheel platform tags to vendor, such as manylinux_2_28_x86_64.
	// +kubebuilder:validation:MaxItems=8
	Platforms []string `json:"platforms,omitempty"`
	// The Python version to vendor wheels for, such as 3.12.
	// +kubebuilder:validation:Pattern=`^3\.[0-9]+$`
	PythonVersion *string `json:"pythonVersion,omitempty"`
}

// MCPRepoSource is a GitHub repository that holds the MCP server source.
type MCPRepoSource struct {
	// Owner of the Github repository.
	// +kubebuilder:validation:Required
	RepoOwner string `json:"repoOwner"`
	// The full URL of the Github repository.
	RepoUrl *string `json:"repoUrl,omitempty"`
	// The path within the repository to the directory that contains the MCP server.
	// +kubebuilder:validation:Required
	Path string `json:"path"`
	// The prefix of the Git tags that version this source, such as 'my-server/'. The prefix is
	// stripped from each tag to produce the version. If omitted, tags are used as-is.
	TagPrefix *string `json:"tagPrefix,omitempty"`
}

// MCPEntrypoint overrides the default binary or console script that an MCP server package starts.
type MCPEntrypoint struct {
	// The command to run.
	// +kubebuilder:validation:MaxLength=255
	Command string `json:"command,omitempty"`
	// The arguments to pass to the command.
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MaxLength=1024
	Args []string `json:"args,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.mcpServerSourceConfig.source.type",description="The type of the MCP server package source. One of 'Image', 'Npm', 'PyPI', or 'Source'"
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest available version of the MCPServer"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the MCPServer has synced successfully"

// MCPServer is the Schema for the mcpservers API.
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPServerSpec   `json:"spec,omitempty"`
	Status MCPServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPServerList contains a list of MCPServer.
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MCPServer{}, &MCPServerList{})
}
