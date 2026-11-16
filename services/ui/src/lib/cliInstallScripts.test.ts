import { describe, expect, it } from "vitest";
import { buildCLIInstallScript } from "./cliInstallScripts";
import { detectCLIOS } from "./cliPlatforms";

const ORIGIN = "https://registry.example.com";

describe("buildCLIInstallScript", () => {
  it.each(["darwin", "linux"] as const)("builds a verified %s install script", (os) => {
    const script = buildCLIInstallScript(os, ORIGIN);

    expect(script).toContain(`curl -fsSL "${ORIGIN}/api/cli/${os}/$ARCH"`);
    expect(script).toContain(`curl -fsSL "${ORIGIN}/api/cli/SHA256SUMS"`);
    expect(script).toContain('"$EXPECTED" != "$ACTUAL"');
    expect(script).toContain("sudo install -m 0755");
    expect(script).toContain("opendepot --version");
    expect(script).not.toContain("${");
  });

  it("builds a PowerShell script for Windows", () => {
    const script = buildCLIInstallScript("windows", ORIGIN);

    expect(script).toContain(`Uri "${ORIGIN}/api/cli/windows/amd64"`);
    expect(script).toContain(`Uri "${ORIGIN}/api/cli/SHA256SUMS"`);
    expect(script).toContain("Get-FileHash -Algorithm SHA256");
    expect(script).toContain('"Path", (@($entries + $installDir) -join ";"), "User"');
    expect(script).not.toContain("sudo");
    expect(script).not.toContain("${");
  });

  it("never emits a backslash that a template literal would swallow", () => {
    expect(buildCLIInstallScript("windows", ORIGIN)).not.toMatch(/\\/);
    expect(buildCLIInstallScript("linux", ORIGIN)).not.toMatch(/\\/);
  });
});

describe("detectCLIOS", () => {
  it.each([
    ["Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15", "darwin"],
    ["Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", "windows"],
    ["Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36", "linux"],
    ["curl/8.4.0", null],
  ])("detects the OS for %s", (userAgent, expected) => {
    expect(detectCLIOS(userAgent)).toBe(expected);
  });
});
