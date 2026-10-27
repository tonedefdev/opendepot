// Kind accents resolve to mode-aware CSS variables defined in the theme's CssBaseline, so they work in
// inline styles and color-mix() as well as sx.
export const ACCENT_COLOR = "var(--od-accent)";
export const NEUTRAL_COLOR = "var(--od-kind-version)";
export const VERSION_COLOR = NEUTRAL_COLOR;

// Everywhere except Stats and Depots, every kind shares the accent and icons tell kinds apart.
export const DEPOT_COLOR = ACCENT_COLOR;
export const MODULE_COLOR = ACCENT_COLOR;
export const PROVIDER_COLOR = ACCENT_COLOR;
export const SKILL_COLOR = ACCENT_COLOR;
export const AGENT_COLOR = ACCENT_COLOR;

export function kindColor(kind: string): string {
  const k = kind.toLowerCase();
  return k === "version" || k === "versionoverflow" ? VERSION_COLOR : ACCENT_COLOR;
}

// Stats and Depots show every kind side by side, so they separate Terraform kinds (module, provider) from
// AI kinds (skill, agent) and depots with the bridge color.
const IAC_COLOR = "var(--od-kind-iac)";
const AI_COLOR = "var(--od-kind-ai)";

export function graphKindColor(kind: string): string {
  switch (kind.toLowerCase()) {
    case "module":
    case "provider":
      return IAC_COLOR;
    case "skill":
    case "agent":
      return AI_COLOR;
    case "depot":
      return ACCENT_COLOR;
    default:
      return VERSION_COLOR;
  }
}

// Low-opacity tint of a graph kind color for node and panel backgrounds.
export function graphKindTint(kind: string, percent = 8): string {
  return `color-mix(in srgb, ${graphKindColor(kind)} ${percent}%, transparent)`;
}
