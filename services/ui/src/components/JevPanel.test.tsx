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

  it("renders a blocked banner with its reasons", () => {
    renderPanel({ assessment: { ...assessment, blocked: true, blockReasons: ["injection above limit"] } });

    expect(screen.getByText(/Blocked by Jev policy: injection above limit/)).not.toBeNull();
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
