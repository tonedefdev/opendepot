import type { BrowseResource, BrowseScanCounts } from "@/lib/api";
import { isAgentKind } from "./resourceDisplay";

export const RISK_COLUMN_MIN_PX = 172;

const CHIP_FONT_PX = 13;
const LABEL_FONT_PX = 14;
// Small chips use 8px label padding on each side plus a 1px border on each side.
const CHIP_CHROME_PX = 18;
const CHIP_GAP_PX = 4;
// Five 3px bars with 2px gaps, followed by the meter's 8px gap to its label.
const METER_BARS_PX = 23;
const METER_LABEL_GAP_PX = 8;
// TableCell default padding is 16px on each side.
const CELL_PADDING_PX = 32;
const HEADER_PX = 84;

type MeasureText = (text: string, font: string) => number;

const SEVERITY_LABELS: Array<[keyof BrowseScanCounts, string]> = [
  ["critical", "C"],
  ["high", "H"],
  ["medium", "M"],
  ["low", "L"],
  ["unknown", "U"],
  ["exempted", "E"],
];

function rowChipWidths(resource: BrowseResource, measure: MeasureText, chipFont: string, labelFont: string): number[] {
  const widths: number[] = [];
  if (isAgentKind(resource.kind) && resource.jevRiskLevel) {
    widths.push(METER_BARS_PX + METER_LABEL_GAP_PX + measure(resource.jevRiskLevel, labelFont));
  }
  const counts = resource.scanCounts;
  if (!counts) return widths;
  for (const [key, letter] of SEVERITY_LABELS) {
    const count = counts[key];
    if (count > 0) widths.push(measure(`${letter} ${count}`, chipFont) + CHIP_CHROME_PX);
  }
  return widths;
}

// Returns the Risk header width needed so the widest row on the page fits without clipping.
export function riskColumnWidth(items: BrowseResource[], measure: MeasureText, fontFamily: string): number {
  const chipFont = `500 ${CHIP_FONT_PX}px ${fontFamily}`;
  const labelFont = `500 ${LABEL_FONT_PX}px ${fontFamily}`;
  let widest = 0;
  for (const item of items) {
    const widths = rowChipWidths(item, measure, chipFont, labelFont);
    if (widths.length === 0) continue;
    const rowWidth = widths.reduce((sum, w) => sum + w, 0) + CHIP_GAP_PX * (widths.length - 1);
    widest = Math.max(widest, rowWidth);
  }
  return Math.max(RISK_COLUMN_MIN_PX, Math.ceil(widest + CELL_PADDING_PX), HEADER_PX);
}

export function canvasMeasureText(): MeasureText | null {
  const ctx = document.createElement("canvas").getContext("2d");
  if (!ctx) return null;
  return (text, font) => {
    ctx.font = font;
    return ctx.measureText(text).width;
  };
}
