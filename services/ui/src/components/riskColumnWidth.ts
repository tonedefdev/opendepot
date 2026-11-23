import type { BrowseResource, BrowseScanCounts } from "@/lib/api";
import { isAgentKind } from "./resourceDisplay";

// Header floor so the "Risk" label and sort control fit when no row has findings.
export const RISK_COLUMN_MIN_PX = 84;

// Chip text is 0.75rem at weight 600. Dense chips use 4px label padding on each side plus a 1px border on each side.
const CHIP_FONT_PX = 12;
const CHIP_FONT_WEIGHT = 600;
const LABEL_FONT_PX = 14;
// MUI body2 letter-spacing (0.01071em), which canvas measurement does not include.
const LABEL_LETTER_SPACING_PX = 0.15;
const CHIP_CHROME_PX = 10;
const CHIP_GAP_PX = 4;
// The meter row sits 4px (gap 0.5) left of the chips.
const METER_CHIP_GAP_PX = 4;
// Five 3px bars with 2px gaps, followed by the meter's 8px gap to its label.
const METER_BARS_PX = 23;
const METER_LABEL_GAP_PX = 8;
// Left and right padding are both 16px on the Risk column.
const CELL_PADDING_PX = 32;

type MeasureText = (text: string, font: string) => number;

const SEVERITY_LABELS: Array<[keyof BrowseScanCounts, string]> = [
  ["critical", "C"],
  ["high", "H"],
  ["medium", "M"],
  ["low", "L"],
  ["unknown", "U"],
  ["exempted", "E"],
];

function rowRiskWidth(resource: BrowseResource, measure: MeasureText, chipFont: string, labelFont: string): number {
  const meterWidth =
    isAgentKind(resource.kind) && resource.jevRiskLevel
      ? METER_BARS_PX +
        METER_LABEL_GAP_PX +
        measure(resource.jevRiskLevel, labelFont) +
        resource.jevRiskLevel.length * LABEL_LETTER_SPACING_PX
      : 0;
  const chipWidths: number[] = [];
  const counts = resource.scanCounts;
  if (counts) {
    for (const [key, letter] of SEVERITY_LABELS) {
      const count = counts[key];
      if (count > 0) chipWidths.push(measure(`${letter} ${count}`, chipFont) + CHIP_CHROME_PX);
    }
  }
  const chipsWidth = chipWidths.reduce((sum, w) => sum + w, 0) + CHIP_GAP_PX * Math.max(0, chipWidths.length - 1);
  if (meterWidth > 0 && chipWidths.length > 0) return meterWidth + METER_CHIP_GAP_PX + chipsWidth;
  return meterWidth || chipsWidth;
}

// Returns the Risk header width needed so the widest row on the page fits without clipping.
export function riskColumnWidth(items: BrowseResource[], measure: MeasureText, fontFamily: string): number {
  const chipFont = `${CHIP_FONT_WEIGHT} ${CHIP_FONT_PX}px ${fontFamily}`;
  const labelFont = `500 ${LABEL_FONT_PX}px ${fontFamily}`;
  let widest = 0;
  for (const item of items) {
    widest = Math.max(widest, rowRiskWidth(item, measure, chipFont, labelFont));
  }
  return Math.max(RISK_COLUMN_MIN_PX, Math.ceil(widest + CELL_PADDING_PX));
}

export function canvasMeasureText(): MeasureText | null {
  const ctx = document.createElement("canvas").getContext("2d");
  if (!ctx) return null;
  return (text, font) => {
    ctx.font = font;
    return ctx.measureText(text).width;
  };
}
