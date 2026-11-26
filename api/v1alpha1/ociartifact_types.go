package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OCIArtifactSpec defines the desired state of OCIArtifact.
type OCIArtifactSpec struct {
	// A flag to force an OCIArtifact to synchronize.
	ForceSync bool `json:"forceSync,omitempty"`
	// The configuration of the OCIArtifact, which defines where its content comes from and how it is versioned.
	OCIArtifactConfig OCIArtifactConfig `json:"ociArtifactConfig"`
	// The versions of the OCIArtifact that the controller should keep in sync.
	Versions []OCIArtifactVersion `json:"versions"`
}

// OCIArtifactStatus defines the observed state of OCIArtifact.
type OCIArtifactStatus struct {
	// The latest available version of the OCIArtifact.
	LatestVersion *string `json:"latestVersion,omitempty"`
	// A flag to determine if the OCIArtifact has successfully synced to its desired state.
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled.
	SyncStatus string `json:"syncStatus"`
	// A map of the OCIArtifactVersion references that have been successfully created by the controller.
	VersionRefs map[string]*OCIArtifactVersion `json:"versionRefs,omitempty"`
}

// OCIArtifactVersion holds details about the Version resource under management.
type OCIArtifactVersion struct {
	// The name of the OCIArtifact.
	Name string `json:"name"`
	// The tag of the OCIArtifact version.
	Version string `json:"version"`
	// The sha256 digest of the manifest or index that the Version serves.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	// +optional
	Digest string `json:"digest,omitempty"`
	// Whether the Version for the OCIArtifact has synced or not.
	Synced bool `json:"synced"`
}

// OCIArtifactConfig is the configuration settings for the OCIArtifact and for each Version created by the OCI controller.
// +kubebuilder:validation:XValidation:rule="has(self.mirror) != has(self.github)",message="exactly one of mirror or github is required"
// +kubebuilder:validation:XValidation:rule="!has(self.github) || self.artifactType != 'Image'",message="images can only be mirrored"
// +kubebuilder:validation:XValidation:rule="!has(self.platforms) || self.artifactType == 'Image'",message="platforms is only valid for Image"
// +kubebuilder:validation:XValidation:rule="!has(self.tagPattern) || !has(self.versionConstraints) || size(self.versionConstraints) == 0",message="tagPattern and versionConstraints are mutually exclusive"
type OCIArtifactConfig struct {
	// The repository path of the OCIArtifact under the namespace. If omitted, the name of the OCIArtifact
	// resource is used in its place.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`
	// +kubebuilder:validation:MaxLength=255
	Name *string `json:"name,omitempty"`
	// The type of the artifact. One of 'Helm', 'Image', or 'Generic'.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Helm;Image;Generic
	ArtifactType string `json:"artifactType"`
	// The upstream OCI registry that the artifact is mirrored from.
	Mirror *OCIMirrorSource `json:"mirror,omitempty"`
	// The GitHub release assets that the artifact is built from. Only valid for Helm and Generic artifacts.
	Github *OCIGithubSource `json:"github,omitempty"`
	// The platforms, such as linux/amd64, that are mirrored for an Image. Only valid for Image.
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]+/[a-z0-9]+(/[a-z0-9.]+)?$`
	// +optional
	Platforms []string `json:"platforms,omitempty"`
	// An RE2 regular expression that selects the tags to version when they are not semantic versions.
	// +kubebuilder:validation:MaxLength=256
	// +optional
	TagPattern *string `json:"tagPattern,omitempty"`
	// The version constraints that select the tags to version. Only the depot controller reads this field.
	VersionConstraints string `json:"versionConstraints,omitempty"`
	// The number of versions to keep stored in the registry at any given time.
	// +kubebuilder:validation:Minimum=0
	VersionHistoryLimit *int `json:"versionHistoryLimit,omitempty"`
	// When true, enforces that the digest of a synced tag never changes. Tag drift is reported and never re-synced.
	// Defaults to true when omitted.
	Immutable *bool `json:"immutable,omitempty"`
	// The Github client configuration settings.
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	// The external storage configuration settings.
	StorageConfig *StorageConfig `json:"storageConfig,omitempty"`
}

// OCIMirrorSource is an upstream OCI registry repository that an OCIArtifact is mirrored from.
type OCIMirrorSource struct {
	// The host of the upstream registry, optionally with a port. It must be on the chart allow-list.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9.-]+(:[0-9]{1,5})?$`
	Registry string `json:"registry"`
	// The repository path within the upstream registry.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=255
	Repository string `json:"repository"`
	// A reference to a kubernetes.io/dockerconfigjson Secret in the same namespace that holds the upstream credentials.
	CredentialsSecretRef *corev1.LocalObjectReference `json:"credentialsSecretRef,omitempty"`
	// The signature verification settings for the upstream artifact.
	Verify *OCIVerifyConfig `json:"verify,omitempty"`
}

// OCIVerifyConfig holds the cosign settings used to verify the upstream signature of an artifact.
type OCIVerifyConfig struct {
	// A reference to the key in a Secret in the same namespace that holds the cosign public key.
	CosignPublicKeySecretRef *corev1.SecretKeySelector `json:"cosignPublicKeySecretRef,omitempty"`
	// When true, a missing or invalid upstream signature blocks the Version.
	Required bool `json:"required,omitempty"`
}

// OCIGithubSource is a GitHub repository whose release assets are packaged into an OCIArtifact.
type OCIGithubSource struct {
	// Owner of the Github repository.
	// +kubebuilder:validation:Required
	RepoOwner string `json:"repoOwner"`
	// The full URL of the Github repository.
	RepoUrl *string `json:"repoUrl,omitempty"`
	// The prefix of the Git tags that version this source, such as 'my-chart/'. The prefix is
	// stripped from each tag to produce the version. If omitted, tags are used as-is.
	TagPrefix *string `json:"tagPrefix,omitempty"`
	// A glob that selects the release asset to package, such as '*.tgz'.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=256
	AssetPattern string `json:"assetPattern"`
	// The media type of the artifact layer. Only used for Generic artifacts.
	// +kubebuilder:validation:MaxLength=255
	MediaType *string `json:"mediaType,omitempty"`
	// The artifact type of the manifest. Only used for Generic artifacts.
	// +kubebuilder:validation:MaxLength=255
	ArtifactMediaType *string `json:"artifactMediaType,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.ociArtifactConfig.artifactType",description="The type of the OCI artifact. One of 'Helm', 'Image', or 'Generic'"
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest available version of the OCIArtifact"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the OCIArtifact has synced successfully"

// OCIArtifact is the Schema for the ociartifacts API.
type OCIArtifact struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OCIArtifactSpec   `json:"spec,omitempty"`
	Status OCIArtifactStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OCIArtifactList contains a list of OCIArtifact.
type OCIArtifactList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OCIArtifact `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OCIArtifact{}, &OCIArtifactList{})
}
