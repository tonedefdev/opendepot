package v1alpha1

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestProviderUpstreamRegistry(t *testing.T) {
	terraformRegistry := TerraformRegistryHost
	blankRegistry := " "
	tests := []struct {
		name   string
		config *ProviderConfig
		want   string
	}{
		{name: "nil config", want: OpenTofuRegistryHost},
		{name: "omitted registry", config: &ProviderConfig{}, want: OpenTofuRegistryHost},
		{name: "blank registry", config: &ProviderConfig{UpstreamRegistry: &blankRegistry}, want: OpenTofuRegistryHost},
		{name: "Terraform registry", config: &ProviderConfig{UpstreamRegistry: &terraformRegistry}, want: TerraformRegistryHost},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ProviderUpstreamRegistry(test.config); got != test.want {
				t.Fatalf("ProviderUpstreamRegistry() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestOpenDepotNewKindsRegisteredInScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	tests := []struct {
		kind     string
		constant string
		obj      runtime.Object
		list     runtime.Object
	}{
		{kind: "OCIArtifact", constant: OpenDepotOCIArtifact, obj: &OCIArtifact{}, list: &OCIArtifactList{}},
		{kind: "MCPServer", constant: OpenDepotMCPServer, obj: &MCPServer{}, list: &MCPServerList{}},
		{kind: "Schematic", constant: OpenDepotSchematic, obj: &Schematic{}, list: &SchematicList{}},
	}

	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			if test.constant != test.kind {
				t.Fatalf("constant = %q, want %q", test.constant, test.kind)
			}

			gvks, _, err := scheme.ObjectKinds(test.obj)
			if err != nil {
				t.Fatalf("ObjectKinds(%s) error = %v", test.kind, err)
			}

			if len(gvks) != 1 || gvks[0] != GroupVersion.WithKind(test.kind) {
				t.Fatalf("ObjectKinds(%s) = %v, want %v", test.kind, gvks, GroupVersion.WithKind(test.kind))
			}

			if _, err := scheme.New(GroupVersion.WithKind(test.kind + "List")); err != nil {
				t.Fatalf("scheme.New(%sList) error = %v", test.kind, err)
			}
		})
	}
}

func jsonRoundTrip[T any](t *testing.T, in *T) (*T, []byte) {
	t.Helper()

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	return &out, data
}

func assertJSONKeys(t *testing.T, data []byte, keys ...string) {
	t.Helper()

	for _, key := range keys {
		if !strings.Contains(string(data), `"`+key+`":`) {
			t.Fatalf("marshaled JSON is missing key %q: %s", key, data)
		}
	}
}

func testOCIArtifacts() []*OCIArtifact {
	name := "charts/podinfo"
	versionLimit := 5
	immutable := true
	digest := "sha256:" + strings.Repeat("a", 64)
	latest := "6.5.0"
	repoURL := "https://github.com/example/tool"
	tagPrefix := "v"
	mediaType := "application/vnd.example.tool.v1"
	genericVersion := "1.2.3"

	helm := &OCIArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: "podinfo", Namespace: "opendepot"},
		Spec: OCIArtifactSpec{
			ForceSync: true,
			OCIArtifactConfig: OCIArtifactConfig{
				Name:         &name,
				ArtifactType: "Helm",
				Mirror: &OCIMirrorSource{
					Registry:             "ghcr.io",
					Repository:           "stefanprodan/charts/podinfo",
					CredentialsSecretRef: &corev1.LocalObjectReference{Name: "ghcr-credentials"},
					Verify: &OCIVerifyConfig{
						CosignPublicKeySecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "cosign-public-key"},
							Key:                  "cosign.pub",
						},
						Required: true,
					},
				},
				VersionConstraints:  ">= 6.0.0",
				VersionHistoryLimit: &versionLimit,
				Immutable:           &immutable,
			},
			Versions: []OCIArtifactVersion{{Name: "podinfo", Version: "6.5.0", Digest: digest, Synced: true}},
		},
		Status: OCIArtifactStatus{
			LatestVersion: &latest,
			Synced:        true,
			SyncStatus:    "Synced",
			VersionRefs: map[string]*OCIArtifactVersion{
				"6.5.0": {Name: "podinfo", Version: "6.5.0", Digest: digest, Synced: true},
			},
		},
	}

	generic := &OCIArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: "tool-bundle", Namespace: "opendepot"},
		Spec: OCIArtifactSpec{
			OCIArtifactConfig: OCIArtifactConfig{
				ArtifactType: "Generic",
				Github: &OCIGithubSource{
					RepoOwner:         "example",
					RepoUrl:           &repoURL,
					TagPrefix:         &tagPrefix,
					AssetPattern:      "*.tar.gz",
					MediaType:         &mediaType,
					ArtifactMediaType: &mediaType,
				},
			},
			Versions: []OCIArtifactVersion{{Name: "tool-bundle", Version: "1.2.3"}},
		},
		Status: OCIArtifactStatus{LatestVersion: &genericVersion, SyncStatus: "Pending"},
	}

	return []*OCIArtifact{helm, generic}
}

func TestOpenDepotOCIArtifactJSONRoundTrip(t *testing.T) {
	for _, in := range testOCIArtifacts() {
		out, data := jsonRoundTrip(t, in)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("round trip mismatch for %s:\nin:  %+v\nout: %+v", in.Name, in, out)
		}

		assertJSONKeys(t, data, "ociArtifactConfig", "artifactType", "versions", "latestVersion", "syncStatus")
		if in.Spec.OCIArtifactConfig.ArtifactType == "Helm" {
			assertJSONKeys(t, data, "versionRefs", "mirror", "verify")
		}
	}
}

func testMCPServers() []*MCPServer {
	name := "filesystem"
	registry := "https://registry.npmjs.org"
	index := "https://pypi.org/simple"
	pythonVersion := "3.12"
	repoURL := "https://github.com/example/mcp-servers"
	imageRef := corev1.LocalObjectReference{Name: "filesystem-image"}
	sources := []MCPServerSource{
		{Type: "Image", Image: &MCPImageSource{OCIArtifactRef: imageRef}},
		{Type: "Npm", Npm: &MCPNpmSource{Package: "@modelcontextprotocol/server-filesystem", Registry: &registry}},
		{
			Type: "PyPI",
			PyPI: &MCPPyPISource{
				Package:       "mcp-server-fetch",
				Index:         &index,
				Platforms:     []string{"manylinux_2_28_x86_64"},
				PythonVersion: &pythonVersion,
			},
		},
		{Type: "Source", Source: &MCPRepoSource{RepoOwner: "example", RepoUrl: &repoURL, Path: "servers/filesystem"}},
	}

	servers := make([]*MCPServer, 0, len(sources))
	for _, source := range sources {
		limit := 3
		servers = append(servers, &MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "mcp-" + strings.ToLower(source.Type), Namespace: "opendepot"},
			Spec: MCPServerSpec{
				MCPServerSourceConfig: MCPServerSourceConfig{
					Name:                &name,
					Source:              source,
					Transport:           "stdio",
					Entrypoint:          &MCPEntrypoint{Command: "uvx", Args: []string{"mcp-server-fetch"}},
					VersionConstraints:  ">= 1.0.0",
					VersionHistoryLimit: &limit,
				},
				Versions: []MCPServerVersion{{Name: "filesystem", Version: "1.0.0", Synced: true}},
			},
			Status: MCPServerStatus{SyncStatus: "Synced", Synced: true, LatestVersion: &name},
		})
	}

	return servers
}

func TestOpenDepotMCPServerJSONRoundTrip(t *testing.T) {
	for _, in := range testMCPServers() {
		out, data := jsonRoundTrip(t, in)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("round trip mismatch for %s:\nin:  %+v\nout: %+v", in.Name, in, out)
		}

		assertJSONKeys(t, data, "mcpServerSourceConfig", "source", "transport", "entrypoint", "versions")
	}
}

func TestOpenDepotSchematicJSONRoundTrip(t *testing.T) {
	importedAt := metav1.NewTime(time.Date(2026, time.November, 26, 10, 0, 0, 0, time.Local))
	in := &Schematic{
		ObjectMeta: metav1.ObjectMeta{Name: "tofu-starter", Namespace: "opendepot"},
		Spec: SchematicSpec{
			AssemblyKind:  "Tofu",
			SchemaVersion: "opendepot.schematic.v1",
			DisplayName:   "Tofu starter",
			Description:   "A starter assembly",
			Tags:          []string{"starter", "tofu-1"},
			Document:      apiextensionsv1.JSON{Raw: []byte(`{"modules":[],"links":[]}`)},
			Tutorial:      true,
			Origin: &SchematicOrigin{
				Source:         "Import",
				ImportedFrom:   "bundle.json",
				ImportedDigest: "sha256:" + strings.Repeat("c", 64),
				ImportedAt:     &importedAt,
			},
		},
		Status: SchematicStatus{
			ObservedGeneration: 2,
			Digest:             "sha256:" + strings.Repeat("d", 64),
			Valid:              true,
			Diagnostics: []SchematicDiagnostic{
				{Code: "unused_input", Path: "modules[0].inputs", Message: "input is unused", Severity: "Warning"},
			},
			References: []SchematicReference{
				{Kind: "Module", Namespace: "opendepot", Name: "terraform-aws-vpc", Version: "1.0.0", State: "Available"},
				{Kind: "MCPServer", Namespace: "opendepot", Name: "filesystem", State: "Placeholder"},
			},
			Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Valid", Message: "ok"}},
		},
	}

	out, data := jsonRoundTrip(t, in)
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch:\nin:  %+v\nout: %+v", in, out)
	}

	assertJSONKeys(t, data, "assemblyKind", "schemaVersion", "displayName", "document", "tutorial", "origin", "importedAt", "valid", "diagnostics", "references", "observedGeneration")
}

func TestOpenDepotNewFieldsJSONRoundTrip(t *testing.T) {
	schematicManagement := false
	jevSecret := &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "jev"}, Key: "jevToken"}
	ociArtifact := testOCIArtifacts()[0].Spec.OCIArtifactConfig
	mcpSource := testMCPServers()[1].Spec.MCPServerSourceConfig
	imageDigest := "sha256:" + strings.Repeat("e", 64)
	signatureVerified := true
	platforms := []string{"linux/amd64"}

	t.Run("Depot", func(t *testing.T) {
		in := &Depot{
			ObjectMeta: metav1.ObjectMeta{Name: "depot", Namespace: "opendepot"},
			Spec: DepotSpec{
				OCIArtifactConfigs: []OCIArtifactConfig{ociArtifact},
				MCPServerConfigs:   []MCPServerSourceConfig{mcpSource},
			},
			Status: DepotStatus{
				OCIArtifacts: []string{"podinfo"},
				MCPServers:   []string{"mcp-npm"},
			},
		}

		out, data := jsonRoundTrip(t, in)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("round trip mismatch:\nin:  %+v\nout: %+v", in, out)
		}

		assertJSONKeys(t, data, "ociArtifactConfigs", "mcpServerConfigs", "ociArtifacts", "mcpServers")
	})

	t.Run("Version", func(t *testing.T) {
		in := &Version{
			ObjectMeta: metav1.ObjectMeta{Name: "podinfo-6.5.0", Namespace: "opendepot"},
			Spec: VersionSpec{
				Type:                 OpenDepotOCIArtifact,
				Version:              "6.5.0",
				OCIArtifactConfigRef: &ociArtifact,
			},
			Status: VersionStatus{
				Synced:     true,
				SyncStatus: "Synced",
				OCIManifest: &OCIManifestStatus{
					Digest:            imageDigest,
					MediaType:         "application/vnd.oci.image.index.v1+json",
					ArtifactType:      "application/vnd.cncf.helm.config.v1+json",
					UpstreamDigest:    imageDigest,
					TagDrift:          true,
					Platforms:         platforms,
					TotalSizeBytes:    4096,
					SignatureVerified: &signatureVerified,
				},
				HelmChart: &HelmChartMetadata{
					Name:        "podinfo",
					Version:     "6.5.0",
					AppVersion:  "6.5.0",
					Description: "Podinfo Helm chart",
					KubeVersion: ">=1.25.0",
					Keywords:    []string{"podinfo"},
					Dependencies: []HelmChartDependency{
						{Name: "redis", Version: "17.0.0", Repository: "https://charts.bitnami.com/bitnami"},
					},
				},
				OCIScan: &SourceScan{},
			},
		}

		out, data := jsonRoundTrip(t, in)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("round trip mismatch:\nin:  %+v\nout: %+v", in, out)
		}

		assertJSONKeys(t, data, "ociArtifactConfigRef", "ociManifest", "helmChart", "ociScan", "tagDrift", "dependencies")
	})

	t.Run("MCP Version", func(t *testing.T) {
		in := &Version{
			ObjectMeta: metav1.ObjectMeta{Name: "mcp-npm-1.0.0", Namespace: "opendepot"},
			Spec: VersionSpec{
				Type:               OpenDepotAgent,
				Version:            "1.0.0",
				MCPServerSourceRef: &mcpSource,
			},
			Status: VersionStatus{
				Synced:     true,
				SyncStatus: "Synced",
				MCPMetadata: &MCPMetadata{
					ServerName:    "filesystem",
					Description:   "Filesystem access",
					Transport:     "stdio",
					Runnable:      true,
					Platforms:     []string{"manylinux_2_28_x86_64"},
					PackageDigest: imageDigest,
				},
			},
		}

		out, data := jsonRoundTrip(t, in)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("round trip mismatch:\nin:  %+v\nout: %+v", in, out)
		}

		assertJSONKeys(t, data, "mcpServerSourceRef", "mcpMetadata", "runnable", "packageDigest")
	})

	t.Run("GroupBinding", func(t *testing.T) {
		in := &GroupBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "platform", Namespace: "opendepot"},
			Spec: GroupBindingSpec{
				Expression:           `"platform" in groups`,
				OCIArtifactResources: []string{"podinfo*"},
				MCPServerResources:   []string{"mcp-*"},
				SchematicResources:   []string{"*"},
				SchematicManagement:  &schematicManagement,
				SchematicNamespaces:  []string{"team-a"},
				JevSecretRef:         jevSecret,
			},
		}

		out, data := jsonRoundTrip(t, in)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("round trip mismatch:\nin:  %+v\nout: %+v", in, out)
		}

		assertJSONKeys(t, data, "ociArtifactResources", "mcpServerResources", "schematicResources", "schematicManagement", "schematicNamespaces", "jevSecretRef")
	})

	t.Run("GroupBinding unset SchematicManagement", func(t *testing.T) {
		in := &GroupBinding{Spec: GroupBindingSpec{Expression: `"platform" in groups`}}

		_, data := jsonRoundTrip(t, in)
		if strings.Contains(string(data), `"schematicManagement"`) {
			t.Fatalf("unset schematicManagement must be omitted so that saving stays enabled by default: %s", data)
		}
	})

	t.Run("SecurityGroupBinding", func(t *testing.T) {
		in := &SecurityGroupBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "platform-security", Namespace: "opendepot"},
			Spec: SecurityGroupBindingSpec{
				Expression:           `"security" in groups`,
				OCIArtifactResources: []string{"*"},
				MCPServerResources:   []string{"mcp-*"},
				SchematicResources:   []string{"tutorial-*"},
			},
		}

		out, data := jsonRoundTrip(t, in)
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("round trip mismatch:\nin:  %+v\nout: %+v", in, out)
		}

		assertJSONKeys(t, data, "ociArtifactResources", "mcpServerResources", "schematicResources")
	})
}

// markerText returns the comment text that the kubebuilder markers are written in, for either a type
// (fieldName is empty) or one of its fields.
func markerText(t *testing.T, file, typeName, fieldName string) string {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}

		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != typeName {
				continue
			}

			if fieldName == "" {
				doc := typeSpec.Doc
				if doc == nil {
					doc = gen.Doc
				}

				return commentText(doc)
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s is not a struct", typeName)
			}

			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					if name.Name == fieldName {
						return commentText(field.Doc)
					}
				}
			}
		}
	}

	t.Fatalf("%s.%s was not found in %s", typeName, fieldName, file)

	return ""
}

func commentText(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}

	lines := make([]string, 0, len(group.List))
	for _, comment := range group.List {
		lines = append(lines, comment.Text)
	}

	return strings.Join(lines, "\n")
}

func assertMarkers(t *testing.T, text string, markers ...string) {
	t.Helper()

	for _, marker := range markers {
		if !strings.Contains(text, marker) {
			t.Fatalf("missing marker %q", marker)
		}
	}
}

func TestOpenDepotOCIArtifactConfigValidationRules(t *testing.T) {
	text := markerText(t, "ociartifact_types.go", "OCIArtifactConfig", "")
	assertMarkers(t, text,
		`+kubebuilder:validation:XValidation:rule="has(self.mirror) != has(self.github)",message="exactly one of mirror or github is required"`,
		`+kubebuilder:validation:XValidation:rule="!has(self.github) || self.artifactType != 'Image'",message="images can only be mirrored"`,
		`+kubebuilder:validation:XValidation:rule="!has(self.platforms) || self.artifactType == 'Image'",message="platforms is only valid for Image"`,
		`+kubebuilder:validation:XValidation:rule="!has(self.tagPattern) || !has(self.versionConstraints) || size(self.versionConstraints) == 0",message="tagPattern and versionConstraints are mutually exclusive"`,
	)

	artifactType := markerText(t, "ociartifact_types.go", "OCIArtifactConfig", "ArtifactType")
	assertMarkers(t, artifactType, "+kubebuilder:validation:Required", "+kubebuilder:validation:Enum=Helm;Image;Generic")

	platforms := markerText(t, "ociartifact_types.go", "OCIArtifactConfig", "Platforms")
	assertMarkers(t, platforms, "+kubebuilder:validation:MaxItems=16")
}

func TestOpenDepotMCPServerSourceValidationRules(t *testing.T) {
	text := markerText(t, "mcpserver_types.go", "MCPServerSource", "")
	assertMarkers(t, text,
		`+kubebuilder:validation:XValidation:rule="(self.type == 'Image') == has(self.image) && (self.type == 'Npm') == has(self.npm) && (self.type == 'PyPI') == has(self.pypi) && (self.type == 'Source') == has(self.source)",message="set exactly the block that matches type"`,
	)

	sourceType := markerText(t, "mcpserver_types.go", "MCPServerSource", "Type")
	assertMarkers(t, sourceType, "+kubebuilder:validation:Required", "+kubebuilder:validation:Enum=Image;Npm;PyPI;Source")

	transport := markerText(t, "mcpserver_types.go", "MCPServerSourceConfig", "Transport")
	assertMarkers(t, transport, "+kubebuilder:validation:Enum=stdio;streamable-http", "+kubebuilder:default=stdio")
}

func TestOpenDepotSchematicValidationRules(t *testing.T) {
	assemblyKind := markerText(t, "schematic_types.go", "SchematicSpec", "AssemblyKind")
	assertMarkers(t, assemblyKind,
		"+kubebuilder:validation:Enum=Tofu;Agent",
		`+kubebuilder:validation:XValidation:rule="self == oldSelf",message="assemblyKind is immutable"`,
	)

	schemaVersion := markerText(t, "schematic_types.go", "SchematicSpec", "SchemaVersion")
	assertMarkers(t, schemaVersion, "+kubebuilder:validation:Enum=opendepot.schematic.v1")

	document := markerText(t, "schematic_types.go", "SchematicSpec", "Document")
	assertMarkers(t, document,
		"+kubebuilder:pruning:PreserveUnknownFields",
		"+kubebuilder:validation:Schemaless",
		"+kubebuilder:validation:Type=object",
	)

	diagnostics := markerText(t, "schematic_types.go", "SchematicStatus", "Diagnostics")
	assertMarkers(t, diagnostics, "+kubebuilder:validation:MaxItems=50")

	references := markerText(t, "schematic_types.go", "SchematicStatus", "References")
	assertMarkers(t, references, "+kubebuilder:validation:MaxItems=256")

	state := markerText(t, "schematic_types.go", "SchematicReference", "State")
	assertMarkers(t, state, "+kubebuilder:validation:Enum=Available;Missing;Yanked;Blocked;Hidden;Placeholder")
}

func TestOpenDepotSharedEnumMarkers(t *testing.T) {
	targetKind := markerText(t, "types.go", "ScanPolicyTargetRef", "Kind")
	assertMarkers(t, targetKind, "+kubebuilder:validation:Enum=Module;Provider;Skill;Agent;OCIArtifact;MCPServer")

	scanTypes := markerText(t, "types.go", "ScanExemption", "ScanTypes")
	assertMarkers(t, scanTypes, "+kubebuilder:validation:items:Enum=binary;source;module;agent;oci;mcp")
}
