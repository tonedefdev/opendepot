package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SchematicSpec defines the desired state of Schematic.
type SchematicSpec struct {
	// The kind of assembly that the document describes. One of 'Tofu' or 'Agent'. It can't change after the Schematic is created.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Tofu;Agent
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="assemblyKind is immutable"
	AssemblyKind string `json:"assemblyKind"`
	// The version of the Schematic document format.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=opendepot.schematic.v1
	SchemaVersion string `json:"schemaVersion"`
	// The human readable name of the Schematic.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	DisplayName string `json:"displayName"`
	// A short description of the Schematic.
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	Description string `json:"description,omitempty"`
	// The tags that describe the Schematic.
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:MaxLength=32
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9][a-z0-9-]*$`
	// +optional
	Tags []string `json:"tags,omitempty"`
	// The assembly document. Its structure depends on assemblyKind. The server and the reconciler limit it to 256 KiB.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	Document apiextensionsv1.JSON `json:"document"`
	// Whether the Schematic is a tutorial. Tutorial documents can contain placeholder nodes and can't be exported
	// to OpenTofu or agent files.
	// +optional
	Tutorial bool `json:"tutorial,omitempty"`
	// The origin of the Schematic.
	// +optional
	Origin *SchematicOrigin `json:"origin,omitempty"`
}

// SchematicOrigin records how a Schematic was created.
type SchematicOrigin struct {
	// How the Schematic was created. One of 'Builtin', 'Import', 'Duplicate', or 'Canvas'.
	// +kubebuilder:validation:Enum=Builtin;Import;Duplicate;Canvas
	Source string `json:"source"`
	// The location that the Schematic bundle was imported from.
	// +kubebuilder:validation:MaxLength=512
	// +optional
	ImportedFrom string `json:"importedFrom,omitempty"`
	// The sha256 digest of the bundle document that was imported.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	// +optional
	ImportedDigest string `json:"importedDigest,omitempty"`
	// The time at which the Schematic bundle was imported.
	// +optional
	ImportedAt *metav1.Time `json:"importedAt,omitempty"`
}

// SchematicStatus defines the observed state of Schematic.
type SchematicStatus struct {
	// The most recent generation of the Schematic that the controller has observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// The sha256 digest of the canonical document.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	// +optional
	Digest string `json:"digest,omitempty"`
	// Whether the document is valid.
	Valid bool `json:"valid"`
	// The problems found while validating the document.
	// +kubebuilder:validation:MaxItems=50
	// +optional
	Diagnostics []SchematicDiagnostic `json:"diagnostics,omitempty"`
	// The resources that the document refers to, and whether each one can currently be used.
	// +kubebuilder:validation:MaxItems=256
	// +optional
	References []SchematicReference `json:"references,omitempty"`
	// The observed conditions of the Schematic. The condition types are 'Ready' and 'ReferencesResolved'.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// SchematicDiagnostic is a problem found while validating a Schematic document.
type SchematicDiagnostic struct {
	// A stable code that identifies the problem.
	// +kubebuilder:validation:MaxLength=64
	Code string `json:"code"`
	// The location of the problem in the document.
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Path string `json:"path,omitempty"`
	// A human readable description of the problem.
	// +kubebuilder:validation:MaxLength=512
	Message string `json:"message"`
	// How serious the problem is. One of 'Error' or 'Warning'.
	// +kubebuilder:validation:Enum=Error;Warning
	Severity string `json:"severity"`
}

// SchematicReference is a resource that a Schematic document refers to.
type SchematicReference struct {
	// The kind of the referenced resource. One of 'Module', 'Provider', 'Skill', 'Agent', or 'MCPServer'.
	// +kubebuilder:validation:Enum=Module;Provider;Skill;Agent;MCPServer
	Kind string `json:"kind"`
	// The namespace of the referenced resource.
	Namespace string `json:"namespace"`
	// The name of the referenced resource.
	Name string `json:"name"`
	// The version constraint of the referenced resource.
	// +optional
	Version string `json:"version,omitempty"`
	// Whether the referenced resource can currently be used.
	// +kubebuilder:validation:Enum=Available;Missing;Yanked;Blocked;Hidden;Placeholder
	State string `json:"state"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Kind",type="string",JSONPath=".spec.assemblyKind",description="The kind of assembly that the Schematic describes. One of 'Tofu' or 'Agent'"
// +kubebuilder:printcolumn:name="Valid",type="boolean",JSONPath=".status.valid",description="Whether the Schematic document is valid"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Schematic is the Schema for the schematics API.
type Schematic struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SchematicSpec   `json:"spec,omitempty"`
	Status SchematicStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SchematicList contains a list of Schematic.
type SchematicList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Schematic `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Schematic{}, &SchematicList{})
}
