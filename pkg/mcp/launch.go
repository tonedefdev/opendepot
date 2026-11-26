package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// LaunchDescriptorFile is the name of the launch descriptor generated into a packaged MCP archive.
const LaunchDescriptorFile = "opendepot-mcp.json"

// Runtime values for LaunchDescriptor.Runtime.
const (
	RuntimeNode   = "node"
	RuntimePython = "python"
	RuntimeOCI    = "oci"
)

// defaultContainerCommand is the container engine used by OCI entries when the descriptor names none.
const defaultContainerCommand = "docker"

var (
	imageRefPattern    = regexp.MustCompile(`^[a-z0-9.-]+(:[0-9]{1,5})?(/[a-z0-9._-]+)+$`)
	imageDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

	// shellCommands wrap their arguments in a shell. A launch through one of them is not a direct exec.
	shellCommands = []string{"sh", "bash", "dash", "zsh", "ksh", "fish", "csh", "tcsh", "cmd", "powershell", "pwsh"}

	// fetchCommands download packages from the network when they run. Launches must use the vendored copy.
	fetchCommands = []string{"npx", "bunx", "pnpx", "uvx", "uv", "pipx", "pip", "pip3", "npm", "pnpm", "yarn"}

	// inlineFlags make an interpreter run code from its arguments instead of the verified script.
	inlineFlags = []string{"-c", "-e", "-p", "--eval", "--print"}
)

// LaunchDescriptor is the opendepot-mcp.json file that describes how to start a vendored MCP server.
//
// Command and Args name the locally installed, verified copy, relative to the project root for node and python.
// For the oci runtime, Command is the container engine (docker when empty), ImageRef and ImageDigest pin the image,
// and Args are passed to the container after the image. URL is required for streamable-http.
type LaunchDescriptor struct {
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	Transport   string   `json:"transport"`
	Runtime     string   `json:"runtime"`
	URL         string   `json:"url,omitempty"`
	ImageRef    string   `json:"imageRef,omitempty"`
	ImageDigest string   `json:"imageDigest,omitempty"`
}

// ServerEntry is the client-neutral launch definition that each renderer writes into its config format.
type ServerEntry struct {
	Type    string
	Command string
	Args    []string
	URL     string
}

// ParseLaunchDescriptor decodes an opendepot-mcp.json document and validates it.
func ParseLaunchDescriptor(data []byte) (LaunchDescriptor, error) {
	var desc LaunchDescriptor
	if err := json.Unmarshal(data, &desc); err != nil {
		return LaunchDescriptor{}, fmt.Errorf("mcp: parse %s: %w", LaunchDescriptorFile, err)
	}

	if err := desc.Validate(); err != nil {
		return LaunchDescriptor{}, fmt.Errorf("mcp: invalid %s: %w", LaunchDescriptorFile, err)
	}

	return desc, nil
}

// Marshal encodes the descriptor as indented JSON with a trailing newline after validating it.
func (d LaunchDescriptor) Marshal() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("mcp: invalid %s: %w", LaunchDescriptorFile, err)
	}

	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("mcp: encode %s: %w", LaunchDescriptorFile, err)
	}

	return append(data, '\n'), nil
}

// Validate checks the transport, the runtime, and the launch arguments. Every problem is reported.
func (d LaunchDescriptor) Validate() error {
	var errs []error
	switch d.Transport {
	case TransportStdio:
	case TransportStreamableHTTP:
		if !httpURLPattern.MatchString(d.URL) {
			errs = append(errs, fmt.Errorf("url %q must be an http(s) URL for transport %q", d.URL, d.Transport))
		}
	default:
		errs = append(errs, fmt.Errorf("transport %q must be %q or %q", d.Transport, TransportStdio, TransportStreamableHTTP))
	}

	switch d.Runtime {
	case RuntimeNode, RuntimePython:
		if d.Command == "" {
			errs = append(errs, fmt.Errorf("command is required for runtime %q", d.Runtime))
		}
	case RuntimeOCI:
		errs = append(errs, validateImage(d.ImageRef, d.ImageDigest)...)
	default:
		errs = append(errs, fmt.Errorf("runtime %q must be %q, %q, or %q", d.Runtime, RuntimeNode, RuntimePython, RuntimeOCI))
	}

	if d.Command != "" {
		if err := validateCommand(d.Command); err != nil {
			errs = append(errs, err)
		}
	}

	interpreter := d.Runtime == RuntimeNode || d.Runtime == RuntimePython
	if interpreter && len(d.Args) > 0 && slices.Contains(inlineFlags, d.Args[0]) {
		errs = append(errs, fmt.Errorf("argument %q runs inline code; launch the verified file instead", d.Args[0]))
	}

	for _, arg := range d.Args {
		if strings.ContainsFunc(arg, isControl) {
			errs = append(errs, fmt.Errorf("argument %q contains a control character", arg))
		}
	}

	return errors.Join(errs...)
}

// Entry converts the descriptor into a launch definition for the client renderers. It validates the descriptor first.
func (d LaunchDescriptor) Entry() (ServerEntry, error) {
	if err := d.Validate(); err != nil {
		return ServerEntry{}, err
	}

	switch {
	case d.Transport == TransportStreamableHTTP:
		return ServerEntry{Type: "http", URL: d.URL}, nil
	case d.Runtime == RuntimeOCI:
		command := d.Command
		if command == "" {
			command = defaultContainerCommand
		}

		args := make([]string, 0, 4+len(d.Args))
		args = append(args, "run", "--rm", "-i", d.ImageRef+"@"+d.ImageDigest)
		args = append(args, d.Args...)

		return ServerEntry{Type: "stdio", Command: command, Args: args}, nil
	default:
		return ServerEntry{Type: "stdio", Command: d.Command, Args: slices.Clone(d.Args)}, nil
	}
}

// validateImage checks the pinned image reference and digest of the oci runtime. Both are required.
func validateImage(ref, digest string) []error {
	var errs []error
	if ref == "" || digest == "" {
		errs = append(errs, errors.New("imageRef and imageDigest are required for runtime \"oci\""))
	}

	if ref != "" && !imageRefPattern.MatchString(ref) {
		errs = append(errs, fmt.Errorf("imageRef %q must be a registry host and repository without a tag or digest", ref))
	}

	if digest != "" && !imageDigestPattern.MatchString(digest) {
		errs = append(errs, fmt.Errorf("imageDigest %q must be sha256:<64 lowercase hex characters>", digest))
	}

	return errs
}

// validateCommand rejects commands that are not a direct exec of a file: shells, package fetchers, and any value
// that contains whitespace or control characters.
func validateCommand(command string) error {
	if strings.ContainsFunc(command, func(r rune) bool { return isControl(r) || r == ' ' }) {
		return fmt.Errorf("command %q must not contain whitespace or control characters", command)
	}

	name := commandName(command)
	if slices.Contains(shellCommands, name) {
		return fmt.Errorf("command %q is a shell; launch the verified file directly", command)
	}

	if slices.Contains(fetchCommands, name) {
		return fmt.Errorf("command %q fetches packages at launch; use the locally installed copy", command)
	}

	return nil
}

// commandName returns the lowercase base name of command without a directory or a .exe or .cmd suffix.
func commandName(command string) string {
	if i := strings.LastIndexAny(command, `/\`); i >= 0 {
		command = command[i+1:]
	}

	name := strings.ToLower(command)
	name = strings.TrimSuffix(name, ".exe")

	return strings.TrimSuffix(name, ".cmd")
}

// isControl reports whether r is an ASCII control character, including NUL, tab, and newline.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}
