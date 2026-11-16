// Kind accents resolve to mode-aware CSS variables defined in the theme's CssBaseline, so they work in
// inline styles and color-mix() as well as sx.
export const ACCENT_COLOR = "var(--od-accent)";
export const NEUTRAL_COLOR = "var(--od-kind-version)";
export const VERSION_COLOR = NEUTRAL_COLOR;

// Every kind shares the accent, so icons and labels tell kinds apart rather than color.
export const DEPOT_COLOR = ACCENT_COLOR;
export const MODULE_COLOR = ACCENT_COLOR;
export const PROVIDER_COLOR = ACCENT_COLOR;
export const SKILL_COLOR = ACCENT_COLOR;
export const AGENT_COLOR = ACCENT_COLOR;

export function kindColor(kind: string): string {
  const k = kind.toLowerCase();
  return k === "version" || k === "versionoverflow" ? VERSION_COLOR : ACCENT_COLOR;
}

// Low-opacity tint of a kind color for node and panel backgrounds.
export function kindTint(kind: string, percent = 8): string {
  return `color-mix(in srgb, ${kindColor(kind)} ${percent}%, transparent)`;
}
