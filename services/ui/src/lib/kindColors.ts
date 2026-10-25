export const DEPOT_COLOR = "#04cfd0";
export const MODULE_COLOR = "#03deb8";
export const PROVIDER_COLOR = "#047df1";
export const SKILL_COLOR = "#f0883e";
export const AGENT_COLOR = "#a371f7";
export const VERSION_COLOR = "#8b949e";

export const KIND_CHIP_TEXT = "#ffffff";

// Darker fills for solid chips so white text meets WCAG 4.5:1 (about 4.6:1 on each).
export const KIND_CHIP_COLORS: Record<string, string> = {
  depot: "#038282",
  module: "#02836d",
  provider: "#0473dd",
  skill: "#bc570f",
  agent: "#8c4df5",
  version: "#6b7580",
};

export function kindChipColor(kind: string): string {
  return KIND_CHIP_COLORS[kind.toLowerCase()] ?? KIND_CHIP_COLORS.module;
}

export function kindColor(kind: string): string {
  switch (kind.toLowerCase()) {
    case "depot":
      return DEPOT_COLOR;
    case "provider":
      return PROVIDER_COLOR;
    case "skill":
      return SKILL_COLOR;
    case "agent":
      return AGENT_COLOR;
    case "version":
      return VERSION_COLOR;
    default:
      return MODULE_COLOR;
  }
}
