package mcp

import (
	"slices"
	"strings"
	"testing"
)

func TestLaunchDescriptorRoundTrip(t *testing.T) {
	desc := LaunchDescriptor{
		Command:   testNode,
		Args:      []string{"vendor/filesystem/dist/index.js", testStdioFlag},
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
			name: "inline flag -p after the script is refused at any index",
			desc: LaunchDescriptor{
				Command:   testNode,
				Args:      []string{testEntryFile, "-p", "8080"},
				Transport: TransportStdio,
				Runtime:   RuntimeNode,
			},
			wantErr: testInlineError,
		},
		{
			name: "python stdio",
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
			wantErr: testShellError,
		},
		{
			name: "uvx fetches at launch",
			desc: LaunchDescriptor{
				Command: "uvx", Args: []string{"some-server"}, Transport: TransportStdio, Runtime: RuntimePython,
			},
			wantErr: testShellError,
		},
		{
			name: "inline code flag",
			desc: LaunchDescriptor{
				Command: testNode, Args: []string{"-e", "require('x')"}, Transport: TransportStdio, Runtime: RuntimeNode,
			},
			wantErr: testInlineError,
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

// bypassCase is one launch shape that must be refused.
type bypassCase struct {
	name    string
	desc    LaunchDescriptor
	wantErr string
}

// TestLaunchDescriptorRejectsBypasses covers launch shapes that would run a shell, a wrapper, a package fetch, or
// inline code while still naming an allowed interpreter.
func TestLaunchDescriptorRejectsBypasses(t *testing.T) {
	wrappers := []string{"env", "xargs", "nohup", "timeout", "nice", "stdbuf", "setsid", "sudo", "busybox", "exec"}

	tests := make([]bypassCase, 0, 64)
	for _, w := range wrappers {
		tests = append(tests, bypassCase{
			name: "wrapper " + w + " for node",
			desc: LaunchDescriptor{
				Command: w, Args: []string{testEntryFile}, Transport: TransportStdio, Runtime: RuntimeNode,
			},
			wantErr: testShellError,
		})
	}

	commands := []struct {
		name    string
		command string
		runtime string
	}{
		{"absolute node path", "/usr/bin/node", RuntimeNode},
		{"parent escape to node", "../node", RuntimeNode},
		{"parent escape to sh", "../../bin/sh", RuntimeNode},
		{"parent escape to python", "../bin/python", RuntimePython},
		{"nested parent escape to python", ".opendepot/../../bin/python", RuntimePython},
		{"node with exe suffix", "node.exe", RuntimeNode},
		{"node with different case", "Node", RuntimeNode},
		{"python3 under node runtime", "python3", RuntimeNode},
		{"node under python runtime", "node", RuntimePython},
		{"absolute python path", "/usr/bin/python3", RuntimePython},
		{"windows drive python path", `C:\Python\python.exe`, RuntimePython},
		{"shell under python runtime", "sh", RuntimePython},
		{"node under oci runtime", "node", RuntimeOCI},
		{"python under oci runtime", "python", RuntimeOCI},
		{"absolute docker path", "/usr/bin/docker", RuntimeOCI},
		{"sh under oci runtime", "sh", RuntimeOCI},
		{"npx fetcher under node runtime", "npx", RuntimeNode},
	}

	for _, c := range commands {
		tests = append(tests, bypassCase{
			name:    c.name,
			desc:    LaunchDescriptor{Command: c.command, Args: []string{"x"}, Transport: TransportStdio, Runtime: c.runtime},
			wantErr: testShellError,
		})
	}

	inline := []struct {
		name    string
		runtime string
		args    []string
	}{
		{"node -e", RuntimeNode, []string{"-e", "x"}},
		{"node -p", RuntimeNode, []string{"-p", "x"}},
		{"node --eval", RuntimeNode, []string{"--eval", "x"}},
		{"node --eval=", RuntimeNode, []string{"--eval=x"}},
		{"node --print=", RuntimeNode, []string{"--print=x"}},
		{"node -e attached", RuntimeNode, []string{"-erequire('x')"}},
		{"node -p attached", RuntimeNode, []string{"-p8080"}},
		{"node -e after script", RuntimeNode, []string{testEntryFile, "-e", "x"}},
		{"python -c", RuntimePython, []string{"-c", "print(1)"}},
		{"python -c attached", RuntimePython, []string{"-cprint(1)"}},
		{"python combined -Ic", RuntimePython, []string{"-Ic", "x"}},
		{"python combined -uc", RuntimePython, []string{"-uc", "x"}},
		{"python -c after module", RuntimePython, []string{"-m", "pytest", "-c", "x"}},
	}

	for _, c := range inline {
		tests = append(tests, bypassCase{
			name: c.name,
			desc: LaunchDescriptor{
				Command: testNodeOrPython(c.runtime), Args: c.args, Transport: TransportStdio, Runtime: c.runtime,
			},
			wantErr: testInlineError,
		})
	}

	fetch := []struct {
		name string
		args []string
	}{
		{"python -m pip", []string{"-m", "pip", "install", "x"}},
		{"python -mpip attached", []string{"-mpip", "install", "x"}},
		{"python -m ensurepip", []string{"-m", "ensurepip"}},
		{"python -m uv", []string{"-m", "uv", "run"}},
		{"python -m pip submodule", []string{"-m", "pip.__main__"}},
		{"python combined -Sm pip", []string{"-Sm", "pip"}},
	}

	for _, c := range fetch {
		tests = append(tests, bypassCase{
			name:    c.name,
			desc:    LaunchDescriptor{Command: testPython, Args: c.args, Transport: TransportStdio, Runtime: RuntimePython},
			wantErr: testFetchError,
		})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.desc.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, tc.wantErr)
			}

			if _, err := tc.desc.Entry(); err == nil {
				t.Error("Entry() accepted a bypass descriptor")
			}
		})
	}
}

func TestLaunchDescriptorAllowsListedCommands(t *testing.T) {
	allowed := []LaunchDescriptor{
		{Command: testNode, Args: []string{testEntryFile, "--verbose"}, Transport: TransportStdio, Runtime: RuntimeNode},
		{Command: "python", Args: []string{"-m", "pytest"}, Transport: TransportStdio, Runtime: RuntimePython},
		{Command: "python3", Args: []string{"-u", testServerScript}, Transport: TransportStdio, Runtime: RuntimePython},
		{Command: "podman", Transport: TransportStdio, Runtime: RuntimeOCI, ImageRef: testImageRef, ImageDigest: testDigest},
		{Command: "nerdctl", Transport: TransportStdio, Runtime: RuntimeOCI, ImageRef: testImageRef, ImageDigest: testDigest},
	}

	for _, desc := range allowed {
		if err := desc.Validate(); err != nil {
			t.Errorf("Validate(%s %v) error = %v, want nil", desc.Command, desc.Args, err)
		}
	}
}

// TestLaunchDescriptorVendoredInterpreterPath covers the vendored relative interpreter paths that node and python
// may launch, and the absolute and escaping paths that must still be refused.
func TestLaunchDescriptorVendoredInterpreterPath(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		runtime string
		wantErr string
	}{
		{
			"vendored venv python", ".opendepot/mcp/summarizer/.venv/bin/python", []string{"-m", "summarizer"},
			RuntimePython, "",
		},
		{
			"vendored venv python3", ".opendepot/mcp/summarizer/.venv/bin/python3", []string{testServerScript},
			RuntimePython, "",
		},
		{"vendored relative node", "./node", []string{testEntryFile}, RuntimeNode, ""},
		{"vendored node in package", ".opendepot/mcp/filesystem/bin/node", []string{testEntryFile}, RuntimeNode, ""},
		{"absolute venv python", "/usr/bin/python3", []string{testServerScript}, RuntimePython, testShellError},
		{"escaping venv python", "../bin/python", []string{testServerScript}, RuntimePython, testShellError},
		{
			"escape hidden in venv path", ".opendepot/../../bin/python", []string{testServerScript},
			RuntimePython, testShellError,
		},
		{
			"vendored path with wrong basename", ".opendepot/mcp/bin/sh", []string{testServerScript},
			RuntimePython, testShellError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			desc := LaunchDescriptor{Command: tc.command, Args: tc.args, Transport: TransportStdio, Runtime: tc.runtime}
			err := desc.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}

				if _, err := desc.Entry(); err != nil {
					t.Fatalf("Entry() error = %v, want nil", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestLaunchDescriptorRejectsLoaderDataAndEscapes covers node loader flags, node --run, data: and :// arguments, and
// script paths that leave the vendored tree. Each case must be refused by Validate and Entry.
func TestLaunchDescriptorRejectsLoaderDataAndEscapes(t *testing.T) {
	const (
		loaderError = "loads code before the verified file"
		runError    = "runs a package script"
		remoteError = "remote or data: code"
		scriptError = "vendored tree"
	)

	tests := []struct {
		name    string
		runtime string
		args    []string
		wantErr string
	}{
		{
			"node --import data url", RuntimeNode,
			[]string{"--import", "data:text/javascript,console.log(1)", testEntryFile}, loaderError,
		},
		{
			"node --import=data url", RuntimeNode,
			[]string{"--import=data:text/javascript,console.log(1)", testEntryFile}, loaderError,
		},
		{"node --import local file", RuntimeNode, []string{"--import", "./hook.mjs", testEntryFile}, loaderError},
		{"node --import=local file", RuntimeNode, []string{"--import=./hook.mjs", testEntryFile}, loaderError},
		{
			"node --experimental-loader", RuntimeNode,
			[]string{"--experimental-loader", "./loader.mjs", testEntryFile}, loaderError,
		},
		{
			"node --experimental-loader=", RuntimeNode,
			[]string{"--experimental-loader=./loader.mjs", testEntryFile}, loaderError,
		},
		{"node --loader", RuntimeNode, []string{"--loader", "./loader.mjs", testEntryFile}, loaderError},
		{"node --loader=", RuntimeNode, []string{"--loader=./loader.mjs", testEntryFile}, loaderError},
		{"node --require", RuntimeNode, []string{"--require", testPreloadScript, testEntryFile}, loaderError},
		{"node --require=", RuntimeNode, []string{"--require=" + testPreloadScript, testEntryFile}, loaderError},
		{"node -r separate", RuntimeNode, []string{"-r", testPreloadScript, testEntryFile}, loaderError},
		{"node -r attached", RuntimeNode, []string{"-rdist/preload.js", testEntryFile}, loaderError},
		{"node -r in a cluster", RuntimeNode, []string{"-pr", testPreloadScript, testEntryFile}, loaderError},
		{"node --run", RuntimeNode, []string{"--run", "build"}, runError},
		{"node --run=", RuntimeNode, []string{"--run=build"}, runError},
		{"node data argument", RuntimeNode, []string{"data:text/javascript,console.log(1)", testEntryFile}, remoteError},
		{
			"node DATA argument is case-insensitive", RuntimeNode,
			[]string{"DATA:text/javascript,x", testEntryFile}, remoteError,
		},
		{"node file url argument", RuntimeNode, []string{"file:///tmp/x.js"}, remoteError},
		{"node https argument", RuntimeNode, []string{"https://example.com/x.js"}, remoteError},
		{"python data argument", RuntimePython, []string{"data:text/x-python,print(1)"}, remoteError},
		{"python https argument", RuntimePython, []string{"-m", "https://example.com/x.py"}, remoteError},
		{"python http argument", RuntimePython, []string{"http://example.com/server.py"}, remoteError},
		{"node script escapes the tree", RuntimeNode, []string{"../outside.js"}, scriptError},
		{"node script with nested escape", RuntimeNode, []string{"dist/../../outside.js"}, scriptError},
		{"node absolute script", RuntimeNode, []string{"/opt/outside.js"}, scriptError},
		{"node windows script", RuntimeNode, []string{`C:\outside.js`}, scriptError},
		{"node without a script path", RuntimeNode, []string{testStdioFlag}, scriptError},
		{"python script escapes the tree", RuntimePython, []string{"../server.py"}, scriptError},
		{"python absolute script", RuntimePython, []string{"/opt/server.py"}, scriptError},
		{"python without a script path", RuntimePython, []string{"-u"}, scriptError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command := testNodeOrPython(tc.runtime)
			desc := LaunchDescriptor{Command: command, Args: tc.args, Transport: TransportStdio, Runtime: tc.runtime}
			err := desc.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, tc.wantErr)
			}

			if _, err := desc.Entry(); err == nil {
				t.Error("Entry() accepted a refused descriptor")
			}
		})
	}
}

// TestLaunchDescriptorStreamableHTTPSkipsScriptPath keeps the remote transport valid without a local script path.
func TestLaunchDescriptorStreamableHTTPSkipsScriptPath(t *testing.T) {
	desc := LaunchDescriptor{
		Command: testNode, Transport: TransportStreamableHTTP, Runtime: RuntimeNode, URL: testRemoteURL,
	}
	if err := desc.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

// testNodeOrPython returns a command that is valid for runtime, so the inline-code cases fail only on their arguments.
func testNodeOrPython(runtime string) string {
	if runtime == RuntimePython {
		return testPython
	}

	return testNode
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
