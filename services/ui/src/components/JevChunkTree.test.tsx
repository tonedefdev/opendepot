// @vitest-environment jsdom

import { fireEvent, render, screen, within } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { describe, expect, it } from "vitest";
import JevChunkTree from "./JevChunkTree";
import theme from "@/theme";
import type { JevAssessment, JevChunkResult } from "@/lib/api";

// The longest file list here is 34 entries, so the chunk "4" row lists 32 and shows "+2 more".
const manyFiles = Array.from({ length: 32 }, (_, i) => `docs/file-${String(i).padStart(2, "0")}.md`);

const chunks: JevChunkResult[] = [
  { index: 1, label: "1", files: ["SKILL.md", "skills/review/index.md"], fileCount: 2, status: "Scored", riskLevel: "Low", riskScore: 0.12, safeProbability: 0.95, riskConfidence: 0.9 },
  { index: 2, label: "2", files: ["SKILL.md"], fileCount: 1, status: "Scored", riskLevel: "Moderate", riskScore: 0.4, safeProbability: 0.7, riskConfidence: 0.8 },
  { index: 3, label: "3a", files: ["skills/review/tools/run.sh"], fileCount: 1, status: "Scored", riskLevel: "High", riskScore: 0.8, safeProbability: 0.3, riskConfidence: 0.6 },
  { index: 3, label: "3b", files: ["skills/review/tools/fetch.sh"], fileCount: 1, status: "Scored", riskLevel: "Low", riskScore: 0.1, safeProbability: 0.97, riskConfidence: 0.95 },
  { index: 4, label: "4", files: manyFiles, fileCount: 34, status: "Scored", riskLevel: "Low", riskScore: 0.05, safeProbability: 0.99, riskConfidence: 0.9 },
  { index: 5, label: "5", files: ["notes/huge.md"], fileCount: 1, status: "TokenLimitExceeded", error: "content still exceeds the token limit after two halvings" },
  { index: 6, label: "6", files: ["notes/other.md"], fileCount: 1, status: "Failed", error: "Jev returned HTTP 503 after retries" },
];

const skippedFiles = [
  { path: "assets/logo.png", reason: "Binary" as const },
  { path: "scripts/deploy.sh", reason: "Withheld" as const },
];

const assessment: JevAssessment = {
  evaluatedAt: "2026-01-01T00:00:00Z",
  needsReview: false,
  blocked: true,
  chunkCount: 7,
  assessedChunks: 6,
  chunks,
  skippedFiles,
  skippedFileCount: 2,
};

function renderTree(value: JevAssessment = assessment) {
  return render(
    <ThemeProvider theme={theme}>
      <JevChunkTree assessment={value} />
    </ThemeProvider>,
  );
}

// rowOf returns the list row that contains the given text, so chips can be checked within it.
function rowOf(text: string): HTMLElement {
  const row = screen.getByText(text).closest("li, [role='button']");
  if (!(row instanceof HTMLElement)) {
    throw new Error(`no row contains ${text}`);
  }
  return row;
}

describe("JevChunkTree", () => {
  it("renders nothing for a legacy assessment", () => {
    const { container } = renderTree({ evaluatedAt: "2026-01-01T00:00:00Z", needsReview: false, blocked: false });

    expect(container.innerHTML).toBe("");
  });

  it("expands nested directories down to the files", () => {
    renderTree();

    fireEvent.click(screen.getByRole("button", { name: /^skills\// }));
    fireEvent.click(screen.getByRole("button", { name: /^review\// }));
    fireEvent.click(screen.getByRole("button", { name: /^tools\// }));

    expect(screen.getByText("run.sh")).not.toBeNull();
    expect(screen.getByText("fetch.sh")).not.toBeNull();
    expect(screen.getByText("index.md")).not.toBeNull();
  });

  it("gives a file in several parts the worst risk level and lists its part labels", () => {
    renderTree();

    const skill = rowOf("SKILL.md");
    expect(within(skill).getByText("Moderate")).not.toBeNull();
    expect(within(skill).queryByText("Low")).toBeNull();
    expect(within(skill).getByText("1")).not.toBeNull();
    expect(within(skill).getByText("2")).not.toBeNull();
  });

  it("shows the part labels 3a and 3b on the same file", () => {
    renderTree();

    fireEvent.click(screen.getByRole("button", { name: /^skills\// }));
    fireEvent.click(screen.getByRole("button", { name: /^review\// }));
    fireEvent.click(screen.getByRole("button", { name: /^tools\// }));

    expect(within(rowOf("run.sh")).getByText("3a")).not.toBeNull();
    expect(within(rowOf("fetch.sh")).getByText("3b")).not.toBeNull();
  });

  it("rolls up the worst risk level into a directory", () => {
    renderTree();

    expect(within(screen.getByRole("button", { name: /^skills\// })).getByText("High")).not.toBeNull();
  });

  it("shows the reason for each skipped file", () => {
    renderTree();

    fireEvent.click(screen.getByRole("button", { name: /^assets\// }));
    fireEvent.click(screen.getByRole("button", { name: /^scripts\// }));

    expect(within(rowOf("logo.png")).getByText("Binary, not read")).not.toBeNull();
    expect(within(rowOf("deploy.sh")).getByText("Withheld by scan")).not.toBeNull();
  });

  it("shows how many files in the parts were not listed", () => {
    renderTree();

    expect(screen.getByText("+2 more files not listed")).not.toBeNull();
  });

  it("shows +N more inside a chunk row when its file list was cut", () => {
    renderTree();
    fireEvent.click(screen.getByRole("button", { name: "Chunks" }));
    fireEvent.click(screen.getByRole("button", { name: /^Part 4 / }));

    expect(screen.getByText("+2 more")).not.toBeNull();
    expect(screen.getByText("docs/file-00.md")).not.toBeNull();
  });

  it("switches to one row per part with status, level, score, safe and confidence", () => {
    renderTree();
    fireEvent.click(screen.getByRole("button", { name: "Chunks" }));

    const row = screen.getByRole("button", { name: /^Part 3a / });
    expect(within(row).getByText("Scored")).not.toBeNull();
    expect(within(row).getByText("High")).not.toBeNull();
    expect(within(row).getByText("Score 0.80")).not.toBeNull();
    expect(within(row).getByText("Safe 30.0%")).not.toBeNull();
    expect(within(row).getByText("Confidence 60.0%")).not.toBeNull();
  });

  it("shows the error text for a TokenLimitExceeded row and a Failed row in red", () => {
    renderTree();
    fireEvent.click(screen.getByRole("button", { name: "Chunks" }));

    const limit = screen.getByText("content still exceeds the token limit after two halvings");
    const failed = screen.getByText("Jev returned HTTP 503 after retries");
    // jsdom reports the theme variable rather than a resolved RGB value.
    expect(getComputedStyle(limit).color).toBe("var(--mui-palette-error-main)");
    expect(getComputedStyle(failed).color).toBe("var(--mui-palette-error-main)");
  });

  it("expands a chunk row to its files from the keyboard", () => {
    renderTree();
    fireEvent.click(screen.getByRole("button", { name: "Chunks" }));

    const row = screen.getByRole("button", { name: /^Part 1 / });
    expect(row.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("skills/review/index.md")).toBeNull();

    row.focus();
    expect(document.activeElement).toBe(row);
    fireEvent.keyDown(row, { key: "Enter" });

    expect(row.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("skills/review/index.md")).not.toBeNull();
  });

  it("expands a directory from the keyboard", () => {
    renderTree();

    const dir = screen.getByRole("button", { name: /^skills\// });
    expect(dir.getAttribute("aria-expanded")).toBe("false");

    fireEvent.keyDown(dir, { key: "Enter" });

    expect(dir.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByRole("button", { name: /^review\// })).not.toBeNull();
  });
});
