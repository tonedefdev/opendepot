package v1alpha1

import (
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:generate stringer -type=OpenDepotType
type OpenDepotType int

const (
	TypeModule OpenDepotType = iota
	TypeProvider
)

const (
	OpenDepotFinalizer                       = "opendepot.defdev.io/finalizer"
	OpenDepotGithubSecretDataFieldAppID      = "githubAppID"
	OpenDepotGithubSecretDataFieldInstallID  = "githubInstallID"
	OpenDepotGithubSecretDataFieldPrivateKey = "githubPrivateKey"
	OpenDepotGithubSecretName                = "opendepot-github-application-secret"
	OpenDepotModule                          = "Module"
	OpenDepotProvider                        = "Provider"
	OpenDepotSkill                           = "Skill"
	OpenDepotAgent                           = "Agent"
	OpenTofuRegistryHost                     = "registry.opentofu.org"
	TerraformRegistryHost                    = "registry.terraform.io"
)

// DepotSpec defines the desired state of Depot.
type DepotSpec struct {
	// The configuration that should be applied to all modules that are part
	// of this Depot.
	GlobalConfig *GlobalConfig `json:"global,omitempty"`
	// The module configuration and version details for each module that should be managed by the Depot controller.
	ModuleConfigs []ModuleConfig `json:"moduleConfigs,omitempty"`
	// The provider configuration and version details for each provider that should be managed by the Depot controller.
	ProviderConfigs []ProviderConfig `json:"providerConfigs,omitempty"`
	// The skill configuration and version details for each skill that should be managed by the Depot controller.
	SkillConfigs []AgentSourceConfig `json:"skillConfigs,omitempty"`
	// The agent configuration and version details for each agent that should be managed by the Depot controller.
	AgentConfigs []AgentSourceConfig `json:"agentConfigs,omitempty"`
	// The polling interval in minutes for how often the Depot controller should check for new versions of the modules it manages.
	// If not specified, the default is 0.
	PollingIntervalMinutes *int `json:"pollingIntervalMinutes,omitempty"`
}

// Defines the desired config of all OpenDepot modules managed by the Depot controller.
type GlobalConfig struct {
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	ModuleConfig       *ModuleConfig       `json:"moduleConfig,omitempty"`
	StorageConfig      *StorageConfig      `json:"storageConfig"`
}

// DepotStatus defines the observed state of Depot.
type DepotStatus struct {
	// The list of Module resource names created and managed by this Depot.
	Modules []string `json:"modules,omitempty"`
	// The list of Provider resource names created and managed by this Depot.
	Providers []string `json:"providers,omitempty"`
	// The list of Skill resource names created and managed by this Depot.
	Skills []string `json:"skills,omitempty"`
	// The list of Agent resource names created and managed by this Depot.
	Agents []string `json:"agents,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="GlobalConfig",type="string",JSONPath=".spec.globalConfig",description="The global configuration applied to all modules managed by this Depot"

// Depot is the Schema for the depots API.
type Depot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DepotSpec   `json:"spec,omitempty"`
	Status DepotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DepotList contains a list of Depot.
type DepotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Depot `json:"items"`
}

// ModuleConfig is the configuration settings for the Module and for each
// Version created by the Module controller.
type ModuleConfig struct {
	// The file format of the module
	// This must be one of 'zip' or 'tar'.
	FileFormat *string `json:"fileFormat,omitempty"`
	// The Github client configuration settings.
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	// When true, enforces that the ChecksumSHA256 of the module archive
	// always matches the value stored in this field and in any destination storage config.
	Immutable *bool `json:"immutable,omitempty"`
	// The name of the module. If omitted, the name of the Module resource
	// is used in its place.
	Name *string `json:"name,omitempty"`
	// The main OpenTofu provider required for this module.
	Provider string `json:"provider,omitempty"`
	// Owner of the Github repository.
	RepoOwner string `json:"repoOwner,omitempty"`
	// The full URL of the Github repository.
	RepoUrl *string `json:"repoUrl,omitempty"`
	// The external storage configuration settings.
	StorageConfig *StorageConfig `json:"storageConfig,omitempty"`
	// A comma separated list of version constraints such as
	// '1.2.1' or '>= 1.0.0, < 2.0.0' or '~> 1.0.0, != 1.0.2'. This field is only
	// respected by the Depot controller.
	VersionConstraints string `json:"versionConstraints,omitempty"`
	// The number of versions to keep stored in the registry at any given time.
	VersionHistoryLimit *int `json:"versionHistoryLimit,omitempty"`
}

type GithubClientConfig struct {
	// This flag determines whether the GitHub client used to download modules
	// will be authenticated with a Github App. It's highly recommended
	// to enable this flag to avoid GitHub API rate limiting. When enabled, the namespace where the Module resource exists
	// must contain a Secret named 'opendepot-github-application-secret'. The secret must contain a githubAppID,
	// githubInstallID, and githubPrivateKey field. The private key must also be base64 encoded before being added
	// as data to the secret. When accessed, the controller will base64 decode the key to build an in-memory client
	// to authenticate with the Github API.
	UseAuthenticatedClient bool `json:"useAuthenticatedClient,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest version of the module"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.moduleConfig.provider",description="The provider of the module"
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.moduleConfig.repoUrl",description="The source repository URL of the module"
// +kubebuilder:printcolumn:name="StorageConfig",type="string",JSONPath=".spec.moduleConfig.storageConfig",description="The configuration for module storage"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Module has synced successfully"

// Module is the Schema for the Modules API.
type Module struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleSpec   `json:"spec,omitempty"`
	Status ModuleStatus `json:"status,omitempty"`
}

// ModuleSpec defines the desired state of a OpenDepot Module.
type ModuleSpec struct {
	// A flag to force a module to synchronize
	ForceSync bool `json:"forceSync,omitempty"`
	// The configuration details for the module that will be used to create each ModuleVersion
	ModuleConfig ModuleConfig `json:"moduleConfig"`
	// The version of the module. This should be a list of maps with semantic version tags. For example, 'version: v1.0.0', or 'version: 1.0.0'.
	// The version controller will automatically trim any leading 'v' character to make them compatible
	// with the registry protocol
	Versions []ModuleVersion `json:"versions"`
}

// ModuleStatus defines the observed state of a module.
type ModuleStatus struct {
	// The latest available version of the module
	LatestVersion *string `json:"latestVersion,omitempty"`
	// The randomly generated filename with its file extension.
	FileName string `json:"fileName,omitempty"`
	// A flag to determine if the module has successfully synced to its desired state
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled
	SyncStatus string `json:"syncStatus"`
	// A slice of the ModuleVersionRefs that have been successfully created by the controller
	ModuleVersionRefs map[string]*ModuleVersion `json:"moduleVersionRefs,omitempty"`
}

// +kubebuilder:object:root=true

// ModuleList contains a list of Module.
type ModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Module `json:"items"`
}

// ModuleVersion holds details about the Version resource under management.
type ModuleVersion struct {
	// The randomly generated filename with its file extension.
	FileName *string `json:"fileName,omitempty"`
	// The name of the module.
	Name string `json:"name,omitempty"`
	// Whether the Version for the Module has synced or not.
	Synced bool `json:"synced,omitempty"`
	// The version of the module.
	Version string `json:"version,omitempty"`
}

// ProviderConfig is the configuration settings for the Provider and for each Version created by the Provider controller.
type ProviderConfig struct {
	// The name of the provider. If omitted, the name of the Provider resource
	// is used in its place.
	Name *string `json:"name,omitempty"`
	// The namespace (organization) of the provider in the configured upstream registry,
	// e.g. 'hashicorp', 'integrations', 'DataDog'. Defaults to 'hashicorp' when omitted,
	// preserving backwards compatibility for existing Provider resources.
	Namespace *string `json:"namespace,omitempty"`
	// The canonical registry used to discover and download this provider and exposed by the Provider Network Mirror Protocol.
	// Defaults to registry.opentofu.org when omitted.
	// +kubebuilder:validation:Enum=registry.opentofu.org;registry.terraform.io
	// +kubebuilder:default=registry.opentofu.org
	UpstreamRegistry *string `json:"upstreamRegistry,omitempty"`
	// The OS(s) that the provider supports. This is used to set the 'os' constraint in the provider's versions.
	OperatingSystems []string `json:"operatingSystems,omitempty"`
	// The architecture(s) that the provider supports. This is used to set the 'arch' constraint in the provider's versions.
	Architectures []string `json:"architectures,omitempty"`
	// The Github client configuration settings for source scanning. When set with
	// useAuthenticatedClient: true, the version controller will use a GitHub App to
	// authenticate requests when fetching the provider's go.mod for source scanning.
	// This is recommended for private source repositories and to avoid GitHub API rate limiting.
	// The namespace where the Version resource exists must contain a Secret named
	// 'opendepot-github-application-secret' with githubAppID, githubInstallID, and
	// githubPrivateKey fields (private key must be base64 encoded).
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	// The URL of the provider's source repository on GitHub, e.g. 'https://github.com/hashicorp/terraform-provider-aws'.
	// When omitted, OpenDepot looks up the repository from the OpenTofu registry (api.opentofu.org).
	// If the registry lookup fails, it falls back to 'github.com/{namespace}/terraform-provider-{name}'.
	// If the repository cannot be resolved, scanning falls back to binary-only mode.
	SourceRepository *string `json:"sourceRepository,omitempty"`
	// The external storage configuration settings.
	StorageConfig *StorageConfig `json:"storageConfig,omitempty"`
	// The version history limit for the provider.
	VersionHistoryLimit *int `json:"versionHistoryLimit,omitempty"`
	// A comma-separated list of version constraints such as
	// '1.2.1' or '>= 1.0.0, < 2.0.0' or '~> 1.0.0'. This field is only
	// respected by the Depot controller.
	VersionConstraints string `json:"versionConstraints,omitempty"`
}

// ProviderUpstreamRegistry returns the configured canonical provider registry.
func ProviderUpstreamRegistry(config *ProviderConfig) string {
	if config != nil && config.UpstreamRegistry != nil {
		upstreamRegistry := strings.TrimSpace(*config.UpstreamRegistry)

		if upstreamRegistry != "" {
			return upstreamRegistry
		}
	}

	return OpenTofuRegistryHost
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest version of the provider"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.providerConfig.name",description="The name of the provider"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Provider has synced successfully"

// Provider is the Schema for the Providers API.
type Provider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderSpec   `json:"spec,omitempty"`
	Status ProviderStatus `json:"status,omitempty"`
}

// ProviderSpec defines the desired state of a OpenDepot Provider.
type ProviderSpec struct {
	// A flag to force a provider to synchronize
	ForceSync bool `json:"forceSync,omitempty"`
	// The configuration details for the provider that will be used to create each ProviderVersion
	ProviderConfig ProviderConfig `json:"providerConfig"`
	// The version of the provider. This should be a list of maps with semantic version tags. For example, 'version: v1.0.0', or 'version: 1.0.0'.
	// The version controller will automatically trim any leading 'v' character to make them compatible
	// with the registry protocol
	Versions []ProviderVersion `json:"versions"`
}

// SecurityFinding represents a single vulnerability finding from a Trivy scan.
type SecurityFinding struct {
	// The CVE or GHSA identifier for the vulnerability.
	VulnerabilityID string `json:"vulnerabilityID"`
	// The name of the package containing the vulnerability.
	PkgName string `json:"pkgName"`
	// The version of the package currently in use.
	InstalledVersion string `json:"installedVersion"`
	// The minimum version of the package that resolves the vulnerability, if known.
	FixedVersion string `json:"fixedVersion,omitempty"`
	// The severity of the vulnerability: CRITICAL, HIGH, MEDIUM, LOW, or UNKNOWN.
	Severity string `json:"severity"`
	// A short description of the vulnerability.
	Title string `json:"title,omitempty"`
	// Whether a ScanPolicy exempted this finding from blocking reconciliation. Exempted
	// findings are still reported so that they remain visible and auditable.
	Exempted bool `json:"exempted,omitempty"`
	// The justification recorded on the ScanPolicy exemption that covered this finding.
	ExemptionReason string `json:"exemptionReason,omitempty"`
	// The name of the ScanPolicy that exempted this finding.
	ExemptedBy string `json:"exemptedBy,omitempty"`
}

// SourceScan holds the results of a Trivy source scan. Used for both module IaC (HCL filesystem)
// scans and provider go.mod dependency scans. Stored on VersionStatus so each Version CR carries
// its own result; the JSON key is "sourceScan".
type SourceScan struct {
	// The RFC3339 timestamp at which the source scan completed.
	ScannedAt string `json:"scannedAt"`
	// The list of findings produced by the scan.
	Findings []SecurityFinding `json:"findings,omitempty"`
}

// BinaryScan holds the results of a Trivy gobinary scan for a specific provider artifact.
// Stored on VersionStatus because each OS/arch binary may embed different Go stdlib versions
// or runtime dependencies; the JSON key is "binaryScan".
type BinaryScan struct {
	// The RFC3339 timestamp at which the binary scan completed.
	ScannedAt string `json:"scannedAt"`
	// The list of vulnerabilities found in the compiled provider binary.
	Findings []SecurityFinding `json:"findings,omitempty"`
}

// ProviderStatus defines the observed state of a provider.
type ProviderStatus struct {
	// The latest available version of the provider
	LatestVersion *string `json:"latestVersion,omitempty"`
	// The randomly generated filename with its file extension.
	FileName string `json:"fileName,omitempty"`
	// A flag to determine if the provider has successfully synced to its desired state
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled
	SyncStatus string `json:"syncStatus"`
	// A slice of the ProviderVersionRefs that have been successfully created by the controller
	ProviderVersionRefs map[string]*ProviderVersion `json:"providerVersionRefs,omitempty"`
	// ResolvedSourceRepository is the VCS source URL discovered by the version controller
	// from the OpenTofu registry (api.opentofu.org). Populated automatically on first scan;
	// spec.providerConfig.sourceRepository takes precedence if set.
	ResolvedSourceRepository string `json:"resolvedSourceRepository,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderList contains a list of Provider.
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Provider `json:"items"`
}

// ProviderVersion holds details about the Version resource under management.
type ProviderVersion struct {
	// The system architecture this Version of the Provider supports.
	Architecture string `json:"architecture,omitempty"`
	// The name of the provider.
	Name string `json:"name,omitempty"`
	// The operating system this Version of the Provider supports.
	OperatingSystem string `json:"operatingSystem,omitempty"`
	// Whether the Version for the Provider has synced or not.
	Synced bool `json:"synced,omitempty"`
	// The version of the provider.
	Version string `json:"version,omitempty"`
}

// AgentSourceConfig is the configuration settings for a Skill or Agent and for each Version created
// by the agent controller. The source is a directory within a Github repository so that several skills
// or agents can be published from one monorepo.
// +kubebuilder:validation:XValidation:rule="!has(self.ref) || !has(self.versionConstraints) || size(self.versionConstraints) == 0",message="ref and versionConstraints are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!has(self.ref) || !has(self.tagPrefix)",message="tagPrefix is not used with ref"
// +kubebuilder:validation:XValidation:rule="!has(self.branchPolicy) || has(self.ref)",message="branchPolicy requires ref"
type AgentSourceConfig struct {
	// The name of the skill or agent. If omitted, the name of the Skill or Agent resource
	// is used in its place.
	Name *string `json:"name,omitempty"`
	// Owner of the Github repository.
	RepoOwner string `json:"repoOwner,omitempty"`
	// The full URL of the Github repository.
	RepoUrl *string `json:"repoUrl,omitempty"`
	// The path within the repository to the directory that contains the skill or agent.
	// If omitted, the repository root is used.
	Path string `json:"path,omitempty"`
	// The prefix of the Git tags that version this source, such as 'my-skill/'. The prefix is
	// stripped from each tag to produce the version. If omitted, tags are used as-is.
	TagPrefix *string `json:"tagPrefix,omitempty"`
	// The Github client configuration settings.
	GithubClientConfig *GithubClientConfig `json:"githubClientConfig,omitempty"`
	// The external storage configuration settings.
	StorageConfig *StorageConfig `json:"storageConfig,omitempty"`
	// When true, enforces that the ChecksumSHA256 of the archive always matches the value stored in this field
	// and in any destination storage config.
	Immutable *bool `json:"immutable,omitempty"`
	// A comma separated list of version constraints such as '1.2.1' or '>= 1.0.0, < 2.0.0' or '~> 1.0.0, != 1.0.2'.
	// This field is only respected by the Depot controller and selects tag mode. It cannot be set with ref.
	VersionConstraints string `json:"versionConstraints,omitempty"`
	// The number of versions to keep stored in the registry at any given time.
	VersionHistoryLimit *int `json:"versionHistoryLimit,omitempty"`
	// The agent platform that the skill or agent targets. The frontmatter keys are validated against
	// that platform's known keys. When omitted, the keys of every supported platform are accepted.
	// +kubebuilder:validation:Enum=agentskills;claude;copilot;codex;opencode;pi
	Platform *string `json:"platform,omitempty"`
	// A reference to the key in a Secret in the same namespace that holds the TypeSafe Jev token.
	// The key defaults to 'jevToken' when omitted. Jev only runs when the version controller has
	// Jev enabled and this reference is set.
	JevSecretRef *corev1.SecretKeySelector `json:"jevSecretRef,omitempty"`
	// The thresholds that gate a Version on its Jev assessment. When omitted, or when every threshold
	// is unset, Jev results are informational only.
	JevPolicy *JevPolicy `json:"jevPolicy,omitempty"`
	// The name of a Git branch to follow instead of tags. Setting ref selects branch mode, where each new
	// commit that touches the source path becomes a Version. Setting ref together with versionConstraints
	// is rejected.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9._+/-]+$`
	Ref *string `json:"ref,omitempty"`
	// The policy that governs how branch commits are classified into versions. Only valid with ref.
	// The policy classifies changes and never blocks a Version.
	// +optional
	BranchPolicy *BranchPolicy `json:"branchPolicy,omitempty"`
}

// BranchPolicy holds the settings that control branch-mode versioning. Thresholds decide the bump size
// from the Jev bump classification. The policy never blocks a Version.
type BranchPolicy struct {
	// The minimum time between two branch versions created from the same source. Commits pushed within
	// this interval collapse into one version. Zero disables the interval. Negative values are rejected.
	// +kubebuilder:default="10m"
	// +optional
	MinVersionInterval *metav1.Duration `json:"minVersionInterval,omitempty"`
	// The probability of a MAJOR change at or above which the bump is MAJOR.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:default=0.5
	// +optional
	MajorThreshold *float64 `json:"majorThreshold,omitempty"`
	// The combined probability of MAJOR and MINOR changes at or above which the bump is at least MINOR.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:default=0.5
	// +optional
	MinorThreshold *float64 `json:"minorThreshold,omitempty"`
	// The minimum classification confidence. A bump below this confidence is flagged for review.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:default=0.5
	// +optional
	MinConfidence *float64 `json:"minConfidence,omitempty"`
}

const (
	// DefaultMinVersionInterval is the minimum time between two branch versions when a source omits minVersionInterval.
	DefaultMinVersionInterval = 10 * time.Minute
	// DefaultBranchThreshold is the bump threshold used when a source omits a branch threshold.
	DefaultBranchThreshold = 0.5
	// DefaultBranchMinConfidence is the confidence below which a branch bump is flagged for review.
	DefaultBranchMinConfidence = 0.5
)

// EffectiveMinVersionInterval returns the effective minimum interval between branch versions.
func (p *BranchPolicy) EffectiveMinVersionInterval() time.Duration {
	if p == nil || p.MinVersionInterval == nil {
		return DefaultMinVersionInterval
	}

	return p.MinVersionInterval.Duration
}

// EffectiveMajorThreshold returns the effective MAJOR probability threshold.
func (p *BranchPolicy) EffectiveMajorThreshold() float64 {
	if p == nil || p.MajorThreshold == nil {
		return DefaultBranchThreshold
	}

	return *p.MajorThreshold
}

// EffectiveMinorThreshold returns the effective combined MAJOR and MINOR probability threshold.
func (p *BranchPolicy) EffectiveMinorThreshold() float64 {
	if p == nil || p.MinorThreshold == nil {
		return DefaultBranchThreshold
	}

	return *p.MinorThreshold
}

// EffectiveMinConfidence returns the effective minimum classification confidence.
func (p *BranchPolicy) EffectiveMinConfidence() float64 {
	if p == nil || p.MinConfidence == nil {
		return DefaultBranchMinConfidence
	}

	return *p.MinConfidence
}

// JevPolicy holds the optional thresholds that gate a Version on its Jev assessment. Each threshold
// is evaluated separately. A threshold that is not set is informational only.
type JevPolicy struct {
	// The minimum safe probability. A Version is blocked when its safe probability is below this value.
	MinSafeProbability *float64 `json:"minSafeProbability,omitempty"`
	// The maximum prompt injection probability. A Version is blocked when its probability is above this value.
	MaxInjectionProbability *float64 `json:"maxInjectionProbability,omitempty"`
	// The maximum data exfiltration probability. A Version is blocked when its probability is above this value.
	MaxExfiltrationProbability *float64 `json:"maxExfiltrationProbability,omitempty"`
	// The maximum destructive action probability. A Version is blocked when its probability is above this value.
	MaxDestructiveProbability *float64 `json:"maxDestructiveProbability,omitempty"`
	// The maximum hidden instructions probability. A Version is blocked when its probability is above this value.
	MaxHiddenInstructionsProbability *float64 `json:"maxHiddenInstructionsProbability,omitempty"`
	// The maximum scope mismatch probability. A Version is blocked when its probability is above this value.
	MaxScopeMismatchProbability *float64 `json:"maxScopeMismatchProbability,omitempty"`
	// The maximum remote execution probability. A Version is blocked when its probability is above this value.
	MaxRemoteExecutionProbability *float64 `json:"maxRemoteExecutionProbability,omitempty"`
	// The maximum risk score. A Version is blocked when its risk score is above this value.
	MaxRiskScore *float64 `json:"maxRiskScore,omitempty"`
	// The minimum risk confidence. A Version whose risk confidence is below this value is flagged
	// for review but is never blocked.
	MinConfidence *float64 `json:"minConfidence,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest version of the skill"
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.agentSourceConfig.repoUrl",description="The source repository URL of the skill"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Skill has synced successfully"

// Skill is the Schema for the Skills API.
type Skill struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkillSpec   `json:"spec,omitempty"`
	Status SkillStatus `json:"status,omitempty"`
}

// SkillSpec defines the desired state of a OpenDepot Skill.
type SkillSpec struct {
	// A flag to force a skill to synchronize
	ForceSync bool `json:"forceSync,omitempty"`
	// The configuration details for the skill that will be used to create each SkillVersion
	AgentSourceConfig AgentSourceConfig `json:"agentSourceConfig"`
	// The version of the skill. This should be a list of maps with semantic version tags. For example, 'version: v1.0.0', or 'version: 1.0.0'.
	// The version controller will automatically trim any leading 'v' character to make them compatible
	// with the registry protocol. Entries for a branch source (ref) hold a commit and a state, and their
	// version stays empty until the Version controller assigns one.
	Versions []SkillVersion `json:"versions"`
}

// SkillStatus defines the observed state of a skill.
type SkillStatus struct {
	// The latest available version of the skill
	LatestVersion *string `json:"latestVersion,omitempty"`
	// A flag to determine if the skill has successfully synced to its desired state
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled
	SyncStatus string `json:"syncStatus"`
	// A slice of the SkillVersionRefs that have been successfully created by the controller
	VersionRefs map[string]*SkillVersion `json:"versionRefs,omitempty"`
}

// +kubebuilder:object:root=true

// SkillList contains a list of Skill.
type SkillList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Skill `json:"items"`
}

// SkillVersion holds details about the Version resource under management.
type SkillVersion struct {
	// The randomly generated filename with its file extension.
	FileName *string `json:"fileName,omitempty"`
	// The name of the Version resource that holds this entry. Set only for branch entries.
	Name string `json:"name,omitempty"`
	// Whether the Version for the Skill has synced or not.
	Synced bool `json:"synced,omitempty"`
	// The version of the skill. Branch entries have an empty version until the Version controller assigns one.
	Version string `json:"version,omitempty"`
	// The full commit SHA of a branch entry. Set only when the source uses ref.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{40}$`
	// +optional
	Commit string `json:"commit,omitempty"`
	// The time the controller first saw the commit. This is the ordering key for branch entries.
	// +optional
	DiscoveredAt *metav1.Time `json:"discoveredAt,omitempty"`
	// The lifecycle state of a branch entry, set by the Agent controller. Only Pending, Assigned, and Rejected are valid.
	// +kubebuilder:validation:Enum=Pending;Assigned;Rejected
	// +optional
	State string `json:"state,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="LatestVersion",type="string",JSONPath=".status.latestVersion",description="The latest version of the agent"
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.agentSourceConfig.repoUrl",description="The source repository URL of the agent"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Agent has synced successfully"

// Agent is the Schema for the Agents API.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// AgentSpec defines the desired state of a OpenDepot Agent.
type AgentSpec struct {
	// A flag to force an agent to synchronize
	ForceSync bool `json:"forceSync,omitempty"`
	// The configuration details for the agent that will be used to create each AgentVersion
	AgentSourceConfig AgentSourceConfig `json:"agentSourceConfig"`
	// The version of the agent. This should be a list of maps with semantic version tags. For example, 'version: v1.0.0', or 'version: 1.0.0'.
	// The version controller will automatically trim any leading 'v' character to make them compatible
	// with the registry protocol. Entries for a branch source (ref) hold a commit and a state, and their
	// version stays empty until the Version controller assigns one.
	Versions []AgentVersion `json:"versions"`
}

// AgentStatus defines the observed state of an agent.
type AgentStatus struct {
	// The latest available version of the agent
	LatestVersion *string `json:"latestVersion,omitempty"`
	// A flag to determine if the agent has successfully synced to its desired state
	Synced bool `json:"synced"`
	// A field for declaring current status information about how the resource is being reconciled
	SyncStatus string `json:"syncStatus"`
	// A slice of the AgentVersionRefs that have been successfully created by the controller
	VersionRefs map[string]*AgentVersion `json:"versionRefs,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList contains a list of Agent.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

// AgentVersion holds details about the Version resource under management.
type AgentVersion struct {
	// The randomly generated filename with its file extension.
	FileName *string `json:"fileName,omitempty"`
	// The name of the Version resource that holds this entry. Set only for branch entries.
	Name string `json:"name,omitempty"`
	// Whether the Version for the Agent has synced or not.
	Synced bool `json:"synced,omitempty"`
	// The version of the agent. Branch entries have an empty version until the Version controller assigns one.
	Version string `json:"version,omitempty"`
	// The full commit SHA of a branch entry. Set only when the source uses ref.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{40}$`
	// +optional
	Commit string `json:"commit,omitempty"`
	// The time the controller first saw the commit. This is the ordering key for branch entries.
	// +optional
	DiscoveredAt *metav1.Time `json:"discoveredAt,omitempty"`
	// The lifecycle state of a branch entry, set by the Agent controller. Only Pending, Assigned, and Rejected are valid.
	// +kubebuilder:validation:Enum=Pending;Assigned;Rejected
	// +optional
	State string `json:"state,omitempty"`
}

// AgentMetadata holds the frontmatter fields parsed from a skill or agent definition.
type AgentMetadata struct {
	// The name declared in the definition's frontmatter.
	Name string `json:"name,omitempty"`
	// The description declared in the definition's frontmatter.
	Description string `json:"description,omitempty"`
	// The tools declared in the definition's frontmatter.
	Tools []string `json:"tools,omitempty"`
	// The model declared in the definition's frontmatter.
	Model string `json:"model,omitempty"`
}

// JevAssessment holds the result of a TypeSafe Jev assessment of a Version. Every value is a model
// probability or score, not a certification. The Jev token and the prompt content are never stored.
type JevAssessment struct {
	// The RFC3339 timestamp at which the assessment was evaluated.
	EvaluatedAt string `json:"evaluatedAt"`
	// The Jev model that produced the assessment, as returned by the API.
	Model string `json:"model,omitempty"`
	// The probability that the Version is safe to use.
	// +optional
	SafeProbability *float64 `json:"safeProbability,omitempty"`
	// The probability that the Version is susceptible to prompt injection.
	// +optional
	InjectionProbability *float64 `json:"injectionProbability,omitempty"`
	// The probability that the Version directs data, files, or credentials to an external destination.
	// +optional
	ExfiltrationProbability *float64 `json:"exfiltrationProbability,omitempty"`
	// The probability that the Version directs destructive or irreversible actions.
	// +optional
	DestructiveProbability *float64 `json:"destructiveProbability,omitempty"`
	// The probability that the Version contains hidden or obfuscated instructions.
	// +optional
	HiddenInstructionsProbability *float64 `json:"hiddenInstructionsProbability,omitempty"`
	// The probability that the Version asks the agent to do things beyond its description.
	// +optional
	ScopeMismatchProbability *float64 `json:"scopeMismatchProbability,omitempty"`
	// The probability that the Version directs the agent to download and run remote code.
	// +optional
	RemoteExecutionProbability *float64 `json:"remoteExecutionProbability,omitempty"`
	// The risk score assigned to the Version.
	// +optional
	RiskScore *float64 `json:"riskScore,omitempty"`
	// The risk level assigned to the Version.
	// +kubebuilder:validation:Enum=Minimal;Low;Moderate;High;Critical
	// +optional
	RiskLevel string `json:"riskLevel,omitempty"`
	// The confidence of the risk assessment. This describes answer concentration, not correctness.
	// +optional
	RiskConfidence *float64 `json:"riskConfidence,omitempty"`
	// Whether the assessment requires a manual review.
	NeedsReview bool `json:"needsReview"`
	// Whether a configured JevPolicy threshold blocks the Version.
	Blocked bool `json:"blocked"`
	// The JevPolicy thresholds that were crossed when the Version is blocked.
	// +optional
	BlockReasons []string `json:"blockReasons,omitempty"`
	// A redacted description of the error that prevented the assessment from completing.
	// +optional
	Error string `json:"error,omitempty"`
}

// ScanPolicyTargetRef identifies the Module, Provider, Skill, or Agent resources a ScanPolicy applies to.
type ScanPolicyTargetRef struct {
	// The kind of resource this reference targets. One of 'Module', 'Provider', 'Skill', or 'Agent'.
	// +kubebuilder:validation:Enum=Module;Provider;Skill;Agent
	Kind string `json:"kind"`
	// The name of the Module, Provider, Skill, or Agent resource. Matching is exact; the single
	// literal '*' matches every resource of the given kind in the namespace.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// A comma-separated list of version constraints such as '1.2.1' or '>= 1.0.0, < 2.0.0'
	// or '~> 1.0.0'. Constraints use AND semantics, so a version must satisfy the full
	// expression. When omitted the reference matches every version of the resource.
	// +optional
	Versions string `json:"versions,omitempty"`
}

// ScanExemption declares a set of scan findings that must not block reconciliation.
// A finding is exempted when it matches every populated field of the exemption.
type ScanExemption struct {
	// The vulnerability or misconfiguration identifiers this exemption covers, such as
	// 'CVE-2024-1234' or 'aws-0057'. Matching is exact; the single literal '*' matches
	// every identifier. When omitted every identifier is covered.
	// +optional
	VulnerabilityIDs []string `json:"vulnerabilityIDs,omitempty"`
	// The package names this exemption covers, such as 'stdlib' or 'golang.org/x/net'.
	// Matching is exact; the single literal '*' matches every package. When omitted
	// every package is covered.
	// +optional
	PkgNames []string `json:"pkgNames,omitempty"`
	// The scan types this exemption applies to. When omitted every scan type is covered.
	// +kubebuilder:validation:items:Enum=binary;source;module;agent
	// +optional
	ScanTypes []string `json:"scanTypes,omitempty"`
	// The severities this exemption covers. Matching is exact and case insensitive; the
	// single literal '*' matches every severity. When omitted every severity is covered.
	// +optional
	Severities []string `json:"severities,omitempty"`
	// The justification for this exemption, so that every exemption remains auditable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Reason string `json:"reason"`
	// The time at which this exemption stops applying. Once it has passed the covered
	// findings block again. When omitted the exemption never expires.
	// +optional
	Expires *metav1.Time `json:"expires,omitempty"`
}

// ScanPolicySpec defines the desired state of ScanPolicy.
type ScanPolicySpec struct {
	// The precedence of this policy. When more than one ScanPolicy matches a Version the
	// policy with the highest priority wins outright and supplies both the severity
	// threshold and the exemption set; lower priority policies are ignored entirely rather
	// than merged. Ties are broken by the oldest creation timestamp, then by name ascending.
	// +kubebuilder:default=0
	// +optional
	Priority int `json:"priority,omitempty"`
	// A label selector matched against the labels of the Version resources this policy
	// applies to. A Version matches when the selector matches it or when any entry in
	// targetRefs matches it. When both selector and targetRefs are omitted the policy
	// applies to every Version in its namespace.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
	// An explicit list of Module, Provider, Skill, or Agent resources this policy applies to.
	// +optional
	TargetRefs []ScanPolicyTargetRef `json:"targetRefs,omitempty"`
	// The minimum severity that blocks reconciliation for the matched Versions. This
	// overrides the version controller's --scan-block-on-critical and --scan-block-on-high
	// flags. 'NONE' disables blocking entirely for the matched Versions. When omitted the
	// controller flags remain in effect as the baseline.
	// +kubebuilder:validation:Enum=CRITICAL;HIGH;MEDIUM;LOW;NONE
	// +optional
	SeverityThreshold string `json:"severityThreshold,omitempty"`
	// The findings that must not block reconciliation for the matched Versions.
	// +optional
	Exemptions []ScanExemption `json:"exemptions,omitempty"`
}

// ScanPolicyStatus defines the observed state of ScanPolicy.
type ScanPolicyStatus struct {
	// The number of Version resources in this namespace currently matched by this policy.
	MatchedVersions int `json:"matchedVersions"`
	// The number of exemptions that are currently in effect.
	ActiveExemptions int `json:"activeExemptions"`
	// The number of exemptions whose expiry has passed and no longer apply.
	ExpiredExemptions int `json:"expiredExemptions"`
	// The name of the higher priority ScanPolicy that shadows this policy for every Version
	// it matches. Empty when this policy wins for at least one Version, or when it matches
	// no Versions at all.
	SupersededBy string `json:"supersededBy,omitempty"`
	// The observed conditions of the ScanPolicy.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Priority",type="integer",JSONPath=".spec.priority",description="The precedence of this policy; highest wins"
// +kubebuilder:printcolumn:name="Threshold",type="string",JSONPath=".spec.severityThreshold",description="The minimum severity that blocks reconciliation"
// +kubebuilder:printcolumn:name="Matched",type="integer",JSONPath=".status.matchedVersions",description="The number of Versions matched by this policy"
// +kubebuilder:printcolumn:name="Active",type="integer",JSONPath=".status.activeExemptions",description="The number of exemptions currently in effect"
// +kubebuilder:printcolumn:name="SupersededBy",type="string",JSONPath=".status.supersededBy",description="The higher priority policy shadowing this one"

// ScanPolicy is the Schema for the scanpolicies API.
type ScanPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScanPolicySpec   `json:"spec,omitempty"`
	Status ScanPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ScanPolicyList contains a list of ScanPolicy.
type ScanPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScanPolicy `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="The type of resource. Either 'Module' or 'Provider'"
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.synced",description="Whether the Version has synced successfully"
// +kubebuilder:printcolumn:name="Checksum",type="string",JSONPath=".status.checksum",description="The base64 encoded SHA256 checksum of the file Version"

// Version is the Schema for the Version API.
type Version struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VersionSpec   `json:"spec,omitempty"`
	Status VersionStatus `json:"status,omitempty"`
}

// VersionSpec defines a specific version of a OpenDepot Module, Provider, Skill, or Agent.
// +kubebuilder:validation:XValidation:rule="size(self.version) > 0 || has(self.sourceCommit)",message="version is required unless sourceCommit is set"
// +kubebuilder:validation:XValidation:rule="!has(self.sourceCommit) || (has(self.agentSourceRef) && has(self.agentSourceRef.ref))",message="sourceCommit requires agentSourceRef.ref"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.sourceCommit) || size(oldSelf.version) == 0 || self.version == oldSelf.version",message="version is write-once for branch Versions"
type VersionSpec struct {
	// The system architecture this Version of the Provider supports.
	Architecture string `json:"architecture,omitempty"`
	// The name of the file with its extension.
	// For a Module the file extension must be one of .zip or .tar.gz
	// since OpenTofu currently only supports these two
	// extension types.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._-]*$`
	FileName *string `json:"fileName,omitempty"`
	// A flag to force a module version to synchronize.
	ForceSync bool `json:"forceSync,omitempty"`
	// The reference to the Skill or Agent resource's config.
	AgentSourceRef *AgentSourceConfig `json:"agentSourceRef,omitempty"`
	// The reference to the Module resource's config.
	ModuleConfigRef *ModuleConfig `json:"moduleConfigRef,omitempty"`
	// The reference to the Provider resource's config.
	ProviderConfigRef *ProviderConfig `json:"providerConfigRef,omitempty"`
	// The operating system this Version of the Provider supports.
	OperatingSystem string `json:"operatingSystem,omitempty"`
	// The type of resource. One of 'Module', 'Provider', 'Skill', or 'Agent'
	Type string `json:"type"`
	// The version of the Module, Provider, Skill, or Agent. For a branch Version of a Skill or Agent this
	// is empty until the Version controller assigns a semantic version, and it can be set only once.
	Version string `json:"version"`
	// Whether the Version has been yanked. A yanked Version stays listed but is skipped
	// when resolving version constraints.
	Yanked bool `json:"yanked,omitempty"`
	// The full commit SHA of the branch that this Skill or Agent Version was created from. Set only for
	// branch Versions. It can't change after it is set.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{40}$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="sourceCommit is immutable"
	// +optional
	SourceCommit string `json:"sourceCommit,omitempty"`
}

// VersionStatus defines the current status of the resource.
type VersionStatus struct {
	// The SHA256 checksum of the module as a base64 encoded string.
	Checksum *string `json:"checksum,omitempty"`
	// A flag that determines whether the Version has been successfully reconciled.
	Synced bool `json:"synced"`
	// The Version's reconciliation status.
	SyncStatus string `json:"syncStatus"`
	// ArchiveSizeBytes is the size in bytes of the stored archive, set by the Version
	// controller after a successful PutObject. Nil when the archive has not yet been uploaded.
	// +optional
	ArchiveSizeBytes *int64 `json:"archiveSizeBytes,omitempty"`
	// The binary vulnerability scan result for this specific provider artifact.
	// Only populated for provider Version resources when scanning is enabled.
	BinaryScan *BinaryScan `json:"binaryScan,omitempty"`
	// The source vulnerability scan result for this Version. Populated for provider Versions
	// (go.mod scan) and module Versions (IaC filesystem scan) when scanning is enabled.
	SourceScan *SourceScan `json:"sourceScan,omitempty"`
	// ReadmeConfigMapRef references the ConfigMap holding this module Version's base64
	// encoded README.md content. Only populated for module Version resources when a
	// README could be resolved from Github or the module archive.
	ReadmeConfigMapRef *ReadmeConfigMapRef `json:"readmeConfigMapRef,omitempty"`
	// ContractConfigMapRef references the ConfigMap holding this module Version's
	// Assembly Line contract JSON. Only populated for module Version resources when
	// contract derivation is enabled.
	// +optional
	ContractConfigMapRef *ContractConfigMapRef `json:"contractConfigMapRef,omitempty"`
	// ProviderSchemaRef references the reduced provider schema stored in the object
	// storage backend for this provider Version. Only populated for provider Version
	// resources whose OS/arch matches the controller's own platform.
	// +optional
	ProviderSchemaRef *ProviderSchemaRef `json:"providerSchemaRef,omitempty"`
	// ProviderSchemaStatus records the outcome of the most recent provider schema
	// extraction attempt. Unlike ProviderSchemaRef, which is only ever set on success,
	// this field is also set when an extraction attempt fails, so the failure is
	// visible instead of the Version silently having no schema. Only populated for
	// provider Version resources whose OS/arch matches the controller's own platform.
	// +optional
	ProviderSchemaStatus *ProviderSchemaStatus `json:"providerSchemaStatus,omitempty"`
	// AgentMetadata holds the frontmatter fields parsed from a skill or agent Version.
	// +optional
	AgentMetadata *AgentMetadata `json:"agentMetadata,omitempty"`
	// ShaSums is the SHA256SUMS file for this Version, produced and signed once at sync.
	// +optional
	ShaSums string `json:"shaSums,omitempty"`
	// ShaSumsSignature is the detached signature over ShaSums, produced at sync with the
	// provider GPG signing key.
	// +optional
	ShaSumsSignature string `json:"shaSumsSignature,omitempty"`
	// SigningKeyFingerprint is the fingerprint of the key that produced ShaSumsSignature.
	// +optional
	SigningKeyFingerprint string `json:"signingKeyFingerprint,omitempty"`
	// JevAssessment holds the TypeSafe Jev assessment of this Version. Only populated when Jev
	// is enabled and the source opts in with jevSecretRef.
	// +optional
	JevAssessment *JevAssessment `json:"jevAssessment,omitempty"`
	// SourceCommit is the full commit SHA that was downloaded for this branch Version.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{40}$`
	// +optional
	SourceCommit string `json:"sourceCommit,omitempty"`
	// BumpAssessment records how the Version controller chose the semantic version bump for a branch Version.
	// +optional
	BumpAssessment *BumpAssessment `json:"bumpAssessment,omitempty"`
	// BranchPhase is the lifecycle phase of a branch Version. Only set for branch Versions.
	// +kubebuilder:validation:Enum=Pending;Held;Assigned;Rejected
	// +optional
	BranchPhase string `json:"branchPhase,omitempty"`
}

// BumpAssessment records how the semantic version bump of a branch Version was chosen. It never
// holds the Jev token or any prompt content.
type BumpAssessment struct {
	// The full commit SHA that was assessed.
	// +optional
	Commit string `json:"commit,omitempty"`
	// The full commit SHA of the previous branch Version. Empty for the first Version.
	// +optional
	PreviousCommit string `json:"previousCommit,omitempty"`
	// The semantic version of the previous Version. Empty for the first Version.
	// +optional
	PreviousVersion string `json:"previousVersion,omitempty"`
	// The bump size applied to the previous version.
	// +kubebuilder:validation:Enum=Major;Minor;Patch
	// +optional
	Category string `json:"category,omitempty"`
	// Where the bump came from. Initial is the first version, Default is a PATCH without Jev, Jev is a
	// Jev classification, Withheld is a MAJOR chosen because a flagged file was found, and Manual is an
	// operator override.
	// +kubebuilder:validation:Enum=Initial;Default;Jev;Withheld;Manual
	// +optional
	Source string `json:"source,omitempty"`
	// A short reason for the bump decision.
	// +optional
	Reason string `json:"reason,omitempty"`
	// The Jev model that produced the classification.
	// +optional
	Model string `json:"model,omitempty"`
	// The RFC3339 time of the evaluation.
	// +optional
	EvaluatedAt string `json:"evaluatedAt,omitempty"`
	// The probability of a MAJOR change.
	// +optional
	MajorProbability *float64 `json:"majorProbability,omitempty"`
	// The probability of a MINOR change.
	// +optional
	MinorProbability *float64 `json:"minorProbability,omitempty"`
	// The probability of a PATCH change.
	// +optional
	PatchProbability *float64 `json:"patchProbability,omitempty"`
	// The confidence of the classification.
	// +optional
	Confidence *float64 `json:"confidence,omitempty"`
	// Whether the bump should be reviewed by a person.
	// +optional
	NeedsReview bool `json:"needsReview,omitempty"`
	// The number of classification attempts made while the Version was held.
	// +optional
	Attempts int `json:"attempts,omitempty"`
	// A redacted description of the last classification error.
	// +optional
	Error string `json:"error,omitempty"`
}

// ReadmeConfigMapRef references the ConfigMap and data key holding a module Version's
// base64 encoded README.md content.
type ReadmeConfigMapRef struct {
	// The name of the ConfigMap holding the README content.
	Name string `json:"name"`
	// The key within the ConfigMap's data holding the base64 encoded README content.
	Key string `json:"key"`
}

// ContractConfigMapRef references the ConfigMap and data key holding a module
// Version's Assembly Line contract JSON.
type ContractConfigMapRef struct {
	// The name of the ConfigMap holding the contract.
	Name string `json:"name"`
	// The key within the ConfigMap's data holding the contract JSON.
	Key string `json:"key"`
	// The compatibility grade of this module for Assembly Line.
	// +kubebuilder:validation:Enum=full;partial;unsupported
	Grade string `json:"grade"`
	// The RFC3339 timestamp at which the contract was derived.
	DerivedAt string `json:"derivedAt"`
}

// ProviderSchemaRef references the reduced provider schema object stored in the
// configured storage backend.
type ProviderSchemaRef struct {
	// The storage key of the gzipped reduced schema object.
	Key string `json:"key"`
	// The SHA256 digest of the uncompressed reduced schema, as a hex string.
	Digest string `json:"digest"`
	// The RFC3339 timestamp at which the schema was extracted.
	ExtractedAt string `json:"extractedAt"`
	// The size in bytes of the stored gzipped object.
	SizeBytes int64 `json:"sizeBytes"`
}

// ProviderSchemaStatus records the outcome of the most recent provider schema
// extraction attempt for a provider Version.
type ProviderSchemaStatus struct {
	// The outcome of the most recent extraction attempt.
	// +kubebuilder:validation:Enum=Succeeded;Failed
	State string `json:"state"`
	// A human readable description of the failure. Only set when state is Failed.
	// +optional
	Message string `json:"message,omitempty"`
	// The RFC3339 timestamp of the most recent extraction attempt.
	AttemptedAt string `json:"attemptedAt"`
}

// +kubebuilder:object:root=true

// VersionList contains a list of Version.
type VersionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Version `json:"items"`
}

// The configuration settings for storing the module in an Amazon S3 bucket.
type AmazonS3Config struct {
	// The S3 bucket name.
	Bucket string `json:"bucket"`
	// The S3 bucket key, ie: 'my/bucket/prefix'
	// The file name will be automatically generated by the opendepot-module-controller.
	Key *string `json:"key,omitempty"`
	// The AWS region for the bucket.
	Region string `json:"region"`
}

type AzureStorageConfig struct {
	// The Azure Storage Account name.
	AccountName string `json:"accountName"`
	// The Azure Storage Account URL.
	AccountUrl string `json:"accountUrl"`
	// The Azure subscription ID where the Azure Storage Account is located.
	SubscriptionID string `json:"subscriptionID"`
	// The Azure Resource Group where the Azure Storage Account is located.
	ResourceGroup string `json:"resourceGroup"`
}

type GoogleCloudStorageConfig struct {
	// The GCS bucket name.
	Bucket string `json:"bucket"`
}

// PresignConfig controls pre-signed URL generation for provider and module downloads.
type PresignConfig struct {
	// When true, download requests are redirected to the storage backend via a pre-signed URL.
	Enabled *bool `json:"enabled,omitempty"`
	// TTL controls how long the pre-signed URL remains valid (e.g. "15m", "1h").
	// If omitted, defaults to 15 minutes.
	// +kubebuilder:default="15m"
	TTL *metav1.Duration `json:"ttl,omitempty"`
	// When true, if pre-sign generation fails the server falls back to proxying the download.
	// Defaults to true.
	// +kubebuilder:default=true
	FallbackToProxy *bool `json:"fallbackToProxy,omitempty"`
}

// StorageConfig holds details about how to store a Version.
type StorageConfig struct {
	AzureStorage *AzureStorageConfig `json:"azureStorage,omitempty"`
	// The configuration settings for storing Versions on a local filesystem.
	FileSystem *FileSystemConfig `json:"fileSystem,omitempty"`
	// The configuration settings for storing Versions in an Amazon S3 bucket.
	S3 *AmazonS3Config `json:"s3,omitempty"`
	// The configuration settings for storing Versions in a Google Cloud Storage bucket.
	GCS *GoogleCloudStorageConfig `json:"gcs,omitempty"`
	// Presign is the optional configuration for pre-signed URL generation.
	Presign *PresignConfig `json:"presign,omitempty"`
}

// The configuration settings for storing Versions on a local filesystem.
type FileSystemConfig struct {
	// The directory path on the file system where the Version will be stored.
	DirectoryPath *string `json:"directoryPath,omitempty"`
}

// GroupBindingExprEnv is the variable environment exposed to GroupBinding expressions.
type GroupBindingExprEnv struct {
	Groups []string `expr:"groups"`
}

// GroupBindingSpec defines the desired state of GroupBinding.
type GroupBindingSpec struct {
	// Expression is an expr-lang boolean expression evaluated against the OIDC JWT groups claim.
	// The only available variable is `groups` ([]string).
	// Examples:
	//   '"platform" in groups'
	//   '"platform" in groups || "platform-readonly" in groups'
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Expression string `json:"expression"`

	// ModuleResources is the list of glob patterns for Module resource names this binding grants access to.
	// The * wildcard is supported (e.g. "terraform-aws-*").
	// Empty or omitted means no modules are accessible.
	// +optional
	ModuleResources []string `json:"moduleResources,omitempty"`

	// ProviderResources is the list of exact Provider resource names this binding grants access to.
	// Empty or omitted means no providers are accessible.
	// +optional
	ProviderResources []string `json:"providerResources,omitempty"`

	// SkillResources is the list of glob patterns for Skill resource names this binding grants access to.
	// The * wildcard is supported (e.g. "code-review-*").
	// Empty or omitted means no skills are accessible.
	// +optional
	SkillResources []string `json:"skillResources,omitempty"`

	// AgentResources is the list of glob patterns for Agent resource names this binding grants access to.
	// The * wildcard is supported (e.g. "release-*").
	// Empty or omitted means no agents are accessible.
	// +optional
	AgentResources []string `json:"agentResources,omitempty"`

	// ScanPolicyManagement grants permission to create, update, and delete
	// ScanPolicy resources through the server policy-management API.
	// This is disabled when omitted.
	// +optional
	ScanPolicyManagement bool `json:"scanPolicyManagement,omitempty"`

	// ScanPolicyNamespaces is the allow-list of namespaces where this binding
	// may read or manage ScanPolicy resources. An empty list denies access.
	// The literal "*" allows every namespace reachable by the server.
	// +optional
	ScanPolicyNamespaces []string `json:"scanPolicyNamespaces,omitempty"`
}

// GroupBindingStatus defines the observed state of GroupBinding.
type GroupBindingStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Expression",type="string",JSONPath=".spec.expression"

// GroupBinding is the Schema for the groupbindings API.
type GroupBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GroupBindingSpec   `json:"spec,omitempty"`
	Status GroupBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GroupBindingList contains a list of GroupBinding.
type GroupBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GroupBinding `json:"items"`
}

type SecurityGroupBindingSpec struct {
	// +kubebuilder:validation:MinLength=1
	Expression                    string   `json:"expression"`
	Namespaces                    []string `json:"namespaces,omitempty"`
	ModuleResources               []string `json:"moduleResources,omitempty"`
	ProviderResources             []string `json:"providerResources,omitempty"`
	SkillResources                []string `json:"skillResources,omitempty"`
	AgentResources                []string `json:"agentResources,omitempty"`
	NamespaceWidePolicyManagement bool     `json:"namespaceWidePolicyManagement,omitempty"`
}

type SecurityGroupBindingStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Expression",type="string",JSONPath=".spec.expression"
type SecurityGroupBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecurityGroupBindingSpec   `json:"spec,omitempty"`
	Status SecurityGroupBindingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SecurityGroupBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecurityGroupBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
	SchemeBuilder.Register(&Depot{}, &DepotList{})
	SchemeBuilder.Register(&GroupBinding{}, &GroupBindingList{})
	SchemeBuilder.Register(&Module{}, &ModuleList{})
	SchemeBuilder.Register(&Provider{}, &ProviderList{})
	SchemeBuilder.Register(&ScanPolicy{}, &ScanPolicyList{})
	SchemeBuilder.Register(&SecurityGroupBinding{}, &SecurityGroupBindingList{})
	SchemeBuilder.Register(&Skill{}, &SkillList{})
	SchemeBuilder.Register(&Version{}, &VersionList{})
}
