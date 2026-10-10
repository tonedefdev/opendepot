export const MODULE_COLOR = "#03deb8";
export const PROVIDER_COLOR = "#047df1";
export const SKILL_COLOR = "#a371f7";
export const AGENT_COLOR = "#db61a2";

export function kindColor(kind: string): string {
  switch (kind.toLowerCase()) {
    case "provider":
      return PROVIDER_COLOR;
    case "skill":
      return SKILL_COLOR;
    case "agent":
      return AGENT_COLOR;
    default:
      return MODULE_COLOR;
  }
}
