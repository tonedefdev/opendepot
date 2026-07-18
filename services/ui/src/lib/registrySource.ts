// Shared source-string builders for Terraform registry addresses, used by
// both UsageSnippet.tsx (the "Usage" section) and ResourceReadme.tsx (README
// rewriting) so the two always agree on what "our" registry source looks like.

export function stripV(v: string): string {
  return v.startsWith("v") ? v.slice(1) : v;
}

export function buildProviderSource(registryHost: string, namespace: string, name: string): string {
  return `${registryHost}/${namespace}/${name}`;
}

export function buildModuleSource(
  registryHost: string,
  namespace: string,
  name: string,
  provider?: string,
): string {
  return provider
    ? `${registryHost}/${namespace}/${name}/${provider}`
    : `${registryHost}/${namespace}/${name}`;
}
