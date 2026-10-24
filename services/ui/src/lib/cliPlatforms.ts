export const CLI_PLATFORMS = [
  { os: "darwin", arch: "arm64", label: "macOS (Apple silicon)" },
  { os: "darwin", arch: "amd64", label: "macOS (Intel)" },
  { os: "linux", arch: "amd64", label: "Linux (x86-64)" },
  { os: "linux", arch: "arm64", label: "Linux (ARM64)" },
  { os: "windows", arch: "amd64", label: "Windows (x86-64)" },
] as const;

export function cliBinaryName(os: string, arch: string): string {
  return `opendepot_${os}_${arch}${os === "windows" ? ".exe" : ""}`;
}

export function isCLIPlatform(os: string, arch: string): boolean {
  return CLI_PLATFORMS.some((platform) => platform.os === os && platform.arch === arch);
}

export type CLIOS = (typeof CLI_PLATFORMS)[number]["os"];

export const CLI_OS_NAMES: Record<CLIOS, string> = {
  darwin: "macOS",
  linux: "Linux",
  windows: "Windows",
};

export function detectCLIOS(userAgent: string): CLIOS | null {
  if (/windows/i.test(userAgent)) return "windows";
  if (/mac os x|macintosh/i.test(userAgent)) return "darwin";
  if (/linux|x11/i.test(userAgent)) return "linux";
  return null;
}
