package mcp

import (
	"slices"
	"strings"
	"testing"
)

func TestLaunchDescriptorRoundTrip(t *testing.T) {
	desc := LaunchDescriptor{
		Command:   testNode,
		Args:      []string{"vendor/filesystem/dist/index.js", "--stdio"},
		Transport: TransportStdio,
		Runtime:   RuntimeNode,
	}

	data, err := desc.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	parsed, err := ParseLaunchDescriptor(data)
	if err != nil {
		t.Fatalf("ParseLaunchDescriptor(Marshal()) error = %v", err)
	}

	if parsed.Command != desc.Command || parsed.Runtime != desc.Runtime || !slices.Equal(parsed.Args, desc.Args) {
		t.Errorf("round trip changed the descriptor: got %+v, want %+v", parsed, desc)
	}

	again, err := parsed.Marshal()
	if err != nil {
		t.Fatalf("second Marshal() error = %v", err)
	}

	if string(again) != string(data) {
		t.Errorf("Marshal is not stable across a round trip:\n%s\nvs\n%s", again, data)
	}
}

func TestLaunchDescriptorValidate(t *testing.T) {
	tests := []struct {
		name    string
		desc    LaunchDescriptor
		wantErr string
	}{
		{
			name: "node stdio",
			desc: LaunchDescriptor{
				Command: testNode, Args: []string{testEntryFile}, Transport: TransportStdio, Runtime: RuntimeNode,
			},
		},
		{
			name: "server flag -p after the script is not an inline flag",
			desc: LaunchDescriptor{
				Command:   testNode,
				Args:      []string{testEntryFile, "-p", "8080"},
				Transport: TransportStdio,
				Runtime:   RuntimeNode,
			},
		},
		{
			name: "python stdio with venv path",
			desc: LaunchDescriptor{
				Command: testPython, Args: []string{"-m", "server"}, Transport: TransportStdio, Runtime: RuntimePython,
			},
		},
		{
			name: "oci pinned image",
			desc: LaunchDescriptor{
				Transport: TransportStdio, Runtime: RuntimeOCI, ImageRef: testImageRef, ImageDigest: testDigest,
			},
		},
		{
			name: "streamable-http with url",
			desc: LaunchDescriptor{
				Transport: TransportStreamableHTTP, Runtime: RuntimeNode, Command: testNode, URL: testRemoteURL,
			},
		},
		{
			name:    "node without command",
			desc:    LaunchDescriptor{Transport: TransportStdio, Runtime: RuntimeNode},
			wantErr: "command is required",
		},
		{
			name:    "python without command",
			desc:    LaunchDescriptor{Transport: TransportStdio, Runtime: RuntimePython},
			wantErr: "command is required",
		},
		{
			name:    "unknown runtime",
			desc:    LaunchDescriptor{Command: testNode, Transport: TransportStdio, Runtime: "deno"},
			wantErr: "runtime \"deno\"",
		},
		{
			name:    "unknown transport",
			desc:    LaunchDescriptor{Command: testNode, Transport: "sse", Runtime: RuntimeNode},
			wantErr: "transport \"sse\"",
		},
		{
			name:    "streamable-http without url",
			desc:    LaunchDescriptor{Command: testNode, Transport: TransportStreamableHTTP, Runtime: RuntimeNode},
			wantErr: "url \"\" must be an http(s) URL",
		},
		{
			name: "shell wrapper sh -c",
			desc: LaunchDescriptor{
				Command: "sh", Args: []string{"-c", "node server.js"}, Transport: TransportStdio, Runtime: RuntimeNode,
			},
			wantErr: testShellError,
		},
		{
			name: "shell by absolute path",
			desc: LaunchDescriptor{
				Command: "/bin/bash", Args: []string{"run.sh"}, Transport: TransportStdio, Runtime: RuntimeNode,
			},
			wantErr: testShellError,
		},
		{
			name: "windows cmd.exe",
			desc: LaunchDescriptor{
				Command: `C:\Windows\System32\cmd.exe`, Transport: TransportStdio, Runtime: RuntimeNode,
			},
			wantErr: testShellError,
		},
		{
			name: "npx fetches at launch",
			desc: LaunchDescriptor{
				Command: "npx", Args: []string{"-y", "some-server"}, Transport: TransportStdio, Runtime: RuntimeNode,
			},
			wantErr: "fetches packages",
		},
		{
			name: "uvx fetches at launch",
			desc: LaunchDescriptor{
				Command: "uvx", Args: []string{"some-server"}, Transport: TransportStdio, Runtime: RuntimePython,
			},
			wantErr: "fetches packages",
		},
		{
			name: "inline code flag",
			desc: LaunchDescriptor{
				Command: testNode, Args: []string{"-e", "require('x')"}, Transport: TransportStdio, Runtime: RuntimeNode,
			},
			wantErr: "runs inline code",
		},
		{
			name:    "command with whitespace",
			desc:    LaunchDescriptor{Command: "node server", Transport: TransportStdio, Runtime: RuntimeNode},
			wantErr: "whitespace",
		},
		{
			name: "argument with newline",
			desc: LaunchDescriptor{
				Command: testNode, Args: []string{"a\nb"}, Transport: TransportStdio, Runtime: RuntimeNode,
			},
			wantErr: "control character",
		},
		{
			name:    "oci without digest",
			desc:    LaunchDescriptor{Transport: TransportStdio, Runtime: RuntimeOCI, ImageRef: testImageRef},
			wantErr: "imageDigest",
		},
		{
			name: "oci ref with tag",
			desc: LaunchDescriptor{
				Transport: TransportStdio, Runtime: RuntimeOCI, ImageRef: testImageRef + ":latest", ImageDigest: testDigest,
			},
			wantErr: "without a tag or digest",
		},
		{
			name: "oci digest not sha256 hex",
			desc: LaunchDescriptor{
				Transport: TransportStdio, Runtime: RuntimeOCI, ImageRef: testImageRef, ImageDigest: "sha256:short",
			},
			wantErr: "imageDigest \"sha256:short\"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.desc.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLaunchDescriptorEntry(t *testing.T) {
	t.Run("stdio node", func(t *testing.T) {
		entry, err := LaunchDescriptor{
			Command: testNode, Args: []string{testEntryFile}, Transport: TransportStdio, Runtime: RuntimeNode,
		}.Entry()
		if err != nil {
			t.Fatalf("Entry() error = %v", err)
		}

		if entry.Type != "stdio" || entry.Command != testNode || !slices.Equal(entry.Args, []string{testEntryFile}) {
			t.Errorf("Entry() = %+v, want a stdio node entry", entry)
		}
	})

	t.Run("oci pins the digest", func(t *testing.T) {
		entry, err := LaunchDescriptor{
			Transport:   TransportStdio,
			Runtime:     RuntimeOCI,
			ImageRef:    testImageRef,
			ImageDigest: testDigest,
			Args:        []string{"--readonly"},
		}.Entry()
		if err != nil {
			t.Fatalf("Entry() error = %v", err)
		}

		wantArgs := []string{"run", "--rm", "-i", testImageRef + "@" + testDigest, "--readonly"}
		if entry.Command != "docker" || !slices.Equal(entry.Args, wantArgs) {
			t.Errorf("Entry() = %s %v, want docker %v", entry.Command, entry.Args, wantArgs)
		}
	})

	t.Run("streamable-http uses url", func(t *testing.T) {
		entry, err := LaunchDescriptor{
			Command: testNode, Transport: TransportStreamableHTTP, Runtime: RuntimeNode, URL: testRemoteURL,
		}.Entry()
		if err != nil {
			t.Fatalf("Entry() error = %v", err)
		}

		if entry.Type != "http" || entry.URL != testRemoteURL || entry.Command != "" {
			t.Errorf("Entry() = %+v, want an http entry with the url", entry)
		}
	})

	t.Run("invalid descriptor is refused", func(t *testing.T) {
		if _, err := (LaunchDescriptor{Command: "sh", Transport: TransportStdio, Runtime: RuntimeNode}).Entry(); err == nil {
			t.Error("Entry() accepted a shell command")
		}
	})
}
