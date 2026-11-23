import type { CLIOS } from "@/lib/cliPlatforms";

function buildUnixInstallScript(os: "darwin" | "linux", origin: string): string {
  return `set -eu

case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  arm64 | aarch64) ARCH=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

BIN="opendepot_${os}_$ARCH"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "${origin}/api/cli/${os}/$ARCH" -o "$TMP/$BIN"
curl -fsSL "${origin}/api/cli/SHA256SUMS" -o "$TMP/SHA256SUMS"

EXPECTED="$(awk -v f="$BIN" '$2 == f { print $1 }' "$TMP/SHA256SUMS")"
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "$TMP/$BIN" | awk '{ print $1 }')"
else
  ACTUAL="$(shasum -a 256 "$TMP/$BIN" | awk '{ print $1 }')"
fi

if [ -z "$EXPECTED" ] || [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "Checksum verification failed for $BIN" >&2
  exit 1
fi

chmod +x "$TMP/$BIN"
sudo mkdir -p /usr/local/bin
sudo install -m 0755 "$TMP/$BIN" /usr/local/bin/opendepot

opendepot --version`;
}

function buildWindowsInstallScript(origin: string): string {
  return `$ErrorActionPreference = "Stop"

$bin = "opendepot_windows_amd64.exe"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
  Invoke-WebRequest -UseBasicParsing -Uri "${origin}/api/cli/windows/amd64" -OutFile (Join-Path $tmp $bin)
  Invoke-WebRequest -UseBasicParsing -Uri "${origin}/api/cli/SHA256SUMS" -OutFile (Join-Path $tmp "SHA256SUMS")

  $expected = Get-Content (Join-Path $tmp "SHA256SUMS") | ForEach-Object {
    $parts = $_ -split " +"
    if ($parts.Count -ge 2 -and $parts[1] -eq $bin) { $parts[0] }
  }
  $actual = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $bin)).Hash
  if (-not $expected -or $expected -ine $actual) { throw "Checksum verification failed for $bin" }

  $installDir = Join-Path (Join-Path $env:LOCALAPPDATA "OpenDepot") "bin"
  New-Item -ItemType Directory -Force -Path $installDir | Out-Null
  $target = Join-Path $installDir "opendepot.exe"
  Copy-Item -Force -Path (Join-Path $tmp $bin) -Destination $target

  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $entries = @($userPath -split ";" | Where-Object { $_ })
  if ($entries -notcontains $installDir) {
    [Environment]::SetEnvironmentVariable("Path", (@($entries + $installDir) -join ";"), "User")
  }

  & $target --version
} finally {
  Remove-Item -Recurse -Force $tmp
}`;
}

export function buildCLIInstallScript(os: CLIOS, origin: string): string {
  if (os === "windows") {
    return buildWindowsInstallScript(origin);
  }
  return buildUnixInstallScript(os, origin);
}

export function buildCLIVerifyCommand(os: CLIOS): string {
  if (os === "windows") {
    return `$bin = "opendepot_windows_amd64.exe"

$expected = Get-Content SHA256SUMS | Where-Object { $_ -like "* $bin" } | ForEach-Object { ($_ -split " +")[0] }
$actual = (Get-FileHash -Algorithm SHA256 $bin).Hash

if ($expected -ieq $actual) { "OK: $bin" } else { "FAILED: $bin" }`;
  }
  if (os === "linux") {
    return "sha256sum -c SHA256SUMS --ignore-missing";
  }
  return "shasum -a 256 -c SHA256SUMS --ignore-missing";
}
