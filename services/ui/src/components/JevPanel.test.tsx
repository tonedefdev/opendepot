// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, describe, expect, it } from "vitest";
import JevPanel from "./JevPanel";
import { buildAgentConfigSnippet } from "./AgentDetail";
import theme from "@/theme";
import type { JevAssessment } from "@/lib/api";

const assessment: JevAssessment = {
  evaluatedAt: "2026-01-01T00:00:00Z",
  model: "jev-1",
  safeProbability: 0.92,
  injectionProbability: 0.04,
  exfiltrationProbability: 0.01,
  destructiveProbability: 0.02,
  hiddenInstructionsProbability: 0.03,
  scopeMismatchProbability: 0.05,
  remoteExecutionProbability: 0.01,
  riskScore: 0.12,
  riskLevel: "Low",
  riskConfidence: 0.88,
  needsReview: false,
  blocked: false,
};

function renderPanel(props: React.ComponentProps<typeof JevPanel>) {
  return render(
    <ThemeProvider theme={theme}>
      <JevPanel {...props} />
    </ThemeProvider>,
  );
}

describe("JevPanel", () => {
  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  it("discloses the data flow to TypeSafe and states the scores are model probabilities", () => {
    renderPanel({ assessment });

    expect(screen.getByText(/Skill or agent content was sent to TypeSafe/)).not.toBeNull();
    expect(screen.getByText(/model probabilities, not a certification or an OpenDepot verdict/)).not.toBeNull();
  });

  it("shows each probability and hides limits when no thresholds are configured", () => {
    renderPanel({ assessment });

    expect(screen.getByText("Injection")).not.toBeNull();
    expect(screen.getByText("4.0%")).not.toBeNull();
    expect(screen.getByText("Risk: Low")).not.toBeNull();
    expect(screen.queryByText(/max /)).toBeNull();
    expect(screen.queryByText(/min /)).toBeNull();
  });

  it("shows configured thresholds and flags breached probabilities", () => {
    renderPanel({ assessment, thresholds: { maxInjectionProbability: 0.02, minSafeProbability: 0.5 } });

    expect(screen.getByText("(max 2.0%)")).not.toBeNull();
    expect(screen.getByText("(min 50.0%)")).not.toBeNull();
    expect(screen.queryByText("(max 1.0%)")).toBeNull();
  });

  it("colors each bar by severity and lets a breached threshold force red", () => {
    const bars = () => screen.getAllByRole("progressbar");
    const colored = { ...assessment, safeProbability: 0.4, injectionProbability: 0.3, exfiltrationProbability: 0.6 };

    renderPanel({ assessment: colored, thresholds: { maxInjectionProbability: 0.2 } });

    const classes = bars().map((bar) => bar.className);
    expect(classes[0]).toContain("MuiLinearProgress-colorError");
    expect(classes[1]).toContain("MuiLinearProgress-colorError");
    expect(classes[2]).toContain("MuiLinearProgress-colorError");
    expect(classes[3]).toContain("MuiLinearProgress-colorSuccess");
  });

  it("colors low-band bars green and mid-band bars yellow", () => {
    renderPanel({ assessment: { ...assessment, safeProbability: 0.9, injectionProbability: 0.3 } });

    const classes = screen.getAllByRole("progressbar").map((bar) => bar.className);
    expect(classes[0]).toContain("MuiLinearProgress-colorSuccess");
    expect(classes[1]).toContain("MuiLinearProgress-colorWarning");
  });

  it("renders a blocked banner with its reasons", () => {
    renderPanel({ assessment: { ...assessment, blocked: true, blockReasons: ["injection above limit"] } });

    expect(screen.getByText(/Blocked by Jev: injection above limit/)).not.toBeNull();
    expect(screen.queryByText(/Blocked by Jev policy/)).toBeNull();
  });

  it("shows the over-ceiling alert when content needed more than 16 parts and none were sent", () => {
    renderPanel({
      assessment: { ...assessment, blocked: true, chunkCount: 23, chunks: [], skippedFiles: [] },
    });

    expect(
      screen.getByText("Content needs at least 23 parts; the limit is 16; no Jev calls were made"),
    ).not.toBeNull();
  });

  it("does not show the over-ceiling alert within the limit or when parts were sent", () => {
    renderPanel({ assessment: { ...assessment, chunkCount: 16, chunks: [] } });
    expect(screen.queryByText(/no Jev calls were made/)).toBeNull();

    cleanup();
    renderPanel({
      assessment: {
        ...assessment,
        chunkCount: 17,
        chunks: [{ index: 1, label: "1", fileCount: 0, status: "Scored" }],
      },
    });
    expect(screen.queryByText(/no Jev calls were made/)).toBeNull();
  });

  it("renders the assessed-content tree below the bars", () => {
    renderPanel({
      assessment: {
        ...assessment,
        chunkCount: 1,
        chunks: [{ index: 1, label: "1", files: ["SKILL.md"], fileCount: 1, status: "Scored", riskLevel: "Low" }],
      },
    });

    expect(screen.getByRole("button", { name: "Files" })).not.toBeNull();
    expect(screen.getByRole("button", { name: "Chunks" })).not.toBeNull();
  });

  it("renders no tree for a legacy assessment", () => {
    renderPanel({ assessment });

    expect(screen.queryByRole("button", { name: "Files" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Chunks" })).toBeNull();
  });

  it("gives every probability an info button and shows what it assesses on hover", async () => {
    renderPanel({ assessment });

    for (const label of ["Safe", "Injection", "Exfiltration", "Destructive actions", "Hidden instructions", "Scope mismatch", "Remote execution"]) {
      expect(screen.getByRole("button", { name: `About ${label}` })).not.toBeNull();
    }

    fireEvent.mouseOver(screen.getByRole("button", { name: "About Exfiltration" }));
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toContain("send data, files, or credentials to an external destination");
  });

  it("explains the risk chips and the review flag on hover", async () => {
    renderPanel({ assessment: { ...assessment, needsReview: true } });

    expect(screen.queryByRole("button", { name: "About Confidence" })).toBeNull();

    fireEvent.mouseOver(screen.getByText(/^Confidence:/));
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toContain("How certain the model is about the risk level");
    expect(tooltip.textContent).toContain("When the policy sets minConfidence, a part below it blocks the version");
  });

  it("says that low confidence blocks the version when the review flag is shown", async () => {
    renderPanel({ assessment: { ...assessment, needsReview: true } });

    fireEvent.mouseOver(screen.getByText("Needs review"));
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toContain("confidence falls below the policy's minConfidence, and that low confidence also blocks the version");
  });
});

describe("JevPanel input cost", () => {
  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  it("prices reported input tokens at the Jev rate per million tokens", () => {
    renderPanel({ assessment: { ...assessment, inputTokens: 1_000_000 } });

    expect(screen.getByText("Cost: $0.042")).not.toBeNull();
  });

  it("keeps small evaluations visible instead of rounding them to zero", () => {
    renderPanel({ assessment: { ...assessment, inputTokens: 1234 } });

    expect(screen.getByText("Cost: $0.000052")).not.toBeNull();
  });

  it("hides the cost chip for legacy assessments and when no tokens were billed", () => {
    const { unmount } = renderPanel({ assessment });
    expect(screen.queryByText(/^Cost:/)).toBeNull();
    unmount();

    renderPanel({ assessment: { ...assessment, inputTokens: 0 } });
    expect(screen.queryByText(/^Cost:/)).toBeNull();
  });

  it("explains the estimate and the rate on hover", async () => {
    renderPanel({ assessment: { ...assessment, inputTokens: 1_234_567 } });

    fireEvent.mouseOver(screen.getByText(/^Cost:/));
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toContain("1,234,567 in total, at $0.042 per million input tokens");
    expect(tooltip.textContent).toContain("Jev does not charge for output tokens");
  });
});

describe("buildAgentConfigSnippet", () => {
  it("uses agent_skill for skills and agent for agents", () => {
    const skill = buildAgentConfigSnippet({ kind: "skill", namespace: "acme", name: "code-review", registryHost: "registry.example.com", latestVersion: "v1.2.0" });
    const agent = buildAgentConfigSnippet({ kind: "agent", namespace: "acme", name: "reviewer", registryHost: "registry.example.com" });

    expect(skill).toContain('agent_skill "code-review" {');
    expect(skill).toContain('source = "registry.example.com/acme/code-review"');
    expect(skill).toContain('version = "1.2.0"');
    expect(agent).toContain('agent "reviewer" {');
    expect(agent).not.toContain("version =");
  });
});
