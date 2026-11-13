// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, describe, expect, it } from "vitest";
import JevPanel from "./JevPanel";
import { buildAgentConfigSnippet } from "./AgentDetail";
import theme from "@/theme";
import type { JevAssessment } from "@/lib/api";
import type { JevUnblockView } from "./jevUnblock";

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

  it("offers a prefilled Create Jev exemption link when blocked and the caller can edit", () => {
    const digest = "a".repeat(64);
    renderPanel({
      assessment: { ...assessment, blocked: true, contentDigest: digest },
      exemption: { namespace: "platform", versionName: "1.2.3", canEdit: true },
    });

    const link = screen.getByRole("link", { name: "Create Jev exemption" });
    const href = new URL(link.getAttribute("href") ?? "", "http://localhost");
    expect(href.pathname).toBe("/security-policies/new");
    expect(href.searchParams.get("namespace")).toBe("platform");
    expect(href.searchParams.get("jevDigest")).toBe(digest);
    expect(href.searchParams.get("jevVersion")).toBe("1.2.3");
  });

  it("disables the Create Jev exemption action with an explanation when the caller cannot edit", () => {
    renderPanel({
      assessment: { ...assessment, blocked: true, contentDigest: "b".repeat(64) },
      exemption: { namespace: "platform", canEdit: false },
    });

    expect((screen.getByRole("button", { name: "Create Jev exemption" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Only namespace-wide policy managers can create Jev exemptions in platform.")).not.toBeNull();
  });

  it("hides the Create Jev exemption action when the version is not blocked or has no digest", () => {
    const { unmount } = renderPanel({
      assessment: { ...assessment, blocked: false, contentDigest: "c".repeat(64) },
      exemption: { namespace: "platform", canEdit: true },
    });
    expect(screen.queryByText("Create Jev exemption")).toBeNull();
    unmount();

    renderPanel({
      assessment: { ...assessment, blocked: true },
      exemption: { namespace: "platform", canEdit: true },
    });
    expect(screen.queryByText("Create Jev exemption")).toBeNull();
  });

  it("hides the Create Jev exemption action when no exemption context is supplied", () => {
    renderPanel({ assessment: { ...assessment, blocked: true, contentDigest: "d".repeat(64) } });
    expect(screen.queryByText("Create Jev exemption")).toBeNull();
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
    expect(tooltip.textContent).toContain("It is set when the risk level is Critical");
    expect(tooltip.textContent).toContain("confidence falls below the policy's minConfidence, which also blocks the version");
    expect(tooltip.textContent).toContain("An exemption does not clear it");
  });

  it("shows the needs-review flag for a Critical assessment", () => {
    renderPanel({ assessment: { ...assessment, riskLevel: "Critical", riskScore: 4, needsReview: true, blocked: true } });

    expect(screen.getByText("Needs review")).not.toBeNull();
    expect(screen.getByText("Risk: Critical")).not.toBeNull();
  });

  it("shows the content digest with a copy button", () => {
    renderPanel({ assessment: { ...assessment, contentDigest: "sha256:abc123" } });

    expect(screen.getByText("sha256:abc123")).not.toBeNull();
    expect(screen.getByRole("button", { name: "Copy to clipboard" })).not.toBeNull();
  });

  it("omits the digest row when the assessment has no content digest", () => {
    renderPanel({ assessment });

    expect(screen.queryByText("Digest")).toBeNull();
    expect(screen.queryByRole("button", { name: "Copy to clipboard" })).toBeNull();
  });

  it("shows an exemption chip naming the policy and a tooltip with the reason and expiry", async () => {
    renderPanel({
      assessment: {
        ...assessment,
        exempted: true,
        exemptedBy: "platform-baseline",
        exemptionReason: "Reviewed by security",
        exemptionExpires: "2027-01-01T00:00:00Z",
      },
    });

    fireEvent.mouseOver(screen.getByText("Exempted by platform-baseline"));
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toContain("Reason: Reviewed by security");
    expect(tooltip.textContent).toContain("Expires ");
    expect(tooltip.textContent).not.toContain("No expiry");
  });

  it("says No expiry in the exemption tooltip when the exemption has no expires", async () => {
    renderPanel({
      assessment: {
        ...assessment,
        exempted: true,
        exemptedBy: "platform-baseline",
        exemptionReason: "Vendor attested",
      },
    });

    fireEvent.mouseOver(screen.getByText("Exempted by platform-baseline"));
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toContain("Reason: Vendor attested");
    expect(tooltip.textContent).toContain("No expiry");
  });

  it("shows exempted block reasons under their own label and not as active blocks", () => {
    renderPanel({
      assessment: {
        ...assessment,
        exempted: true,
        exemptedBy: "platform-baseline",
        blockReasons: ["injection above limit"],
      },
    });

    expect(screen.getByText("Exempted block reasons: injection above limit")).not.toBeNull();
    expect(screen.queryByText(/Flags:/)).toBeNull();
    expect(screen.queryByText(/Blocked by Jev/)).toBeNull();
  });

  it("does not show the exempted label or chip when the assessment is not exempted", () => {
    renderPanel({ assessment: { ...assessment, blockReasons: ["scope mismatch"] } });

    expect(screen.queryByText(/Exempted/)).toBeNull();
    expect(screen.getByText("Flags: scope mismatch")).not.toBeNull();
  });

  it("does not list exempted block reasons inside a blocked banner", () => {
    renderPanel({
      assessment: {
        ...assessment,
        blocked: true,
        exempted: true,
        exemptedBy: "platform-baseline",
        blockReasons: ["injection above limit"],
      },
    });

    expect(screen.getByText("Blocked by Jev")).not.toBeNull();
    expect(screen.queryByText(/Blocked by Jev: injection above limit/)).toBeNull();
    expect(screen.getByText("Exempted block reasons: injection above limit")).not.toBeNull();
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

describe("JevPanel unblock section", () => {
  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  const blocked: JevAssessment = { ...assessment, blocked: true, contentDigest: "d".repeat(64) };

  it("tells the user to set forceSync and copies the kubectl command with the assessment's Version name", () => {
    const unblock: JevUnblockView = { state: "no-digest", namespace: "platform" };
    renderPanel({ assessment: { ...blocked, contentDigest: undefined, versionName: "lint-helper-1-0-0" }, unblock });
    expect(screen.getByText(/This assessment has no digest, so no exemption can match it. Set forceSync: true on this Version, then return here./)).not.toBeNull();
    expect(screen.getByText(`kubectl patch version lint-helper-1-0-0 -n platform --type merge -p '{"spec":{"forceSync":true}}'`)).not.toBeNull();
  });

  it("uses a placeholder Version name when the server did not return versionName", () => {
    renderPanel({ assessment: { ...blocked, contentDigest: undefined }, unblock: { state: "no-digest", namespace: "platform" } });
    expect(screen.getByText(`kubectl patch version <name> -n platform --type merge -p '{"spec":{"forceSync":true}}'`)).not.toBeNull();
  });

  it("uses the assessment's Version name in the forceSync command for a pending exemption", () => {
    renderPanel({
      assessment: { ...blocked, versionName: "lint-helper-2-0-0" },
      unblock: { state: "pending", namespace: "platform", digest: "d".repeat(64), policy: "platform-skills" },
    });
    expect(screen.getByText(`kubectl patch version lint-helper-2-0-0 -n platform --type merge -p '{"spec":{"forceSync":true}}'`)).not.toBeNull();
  });

  it("shows the digest to use and names the governing policy when no entry matches", () => {
    renderPanel({ assessment: blocked, unblock: { state: "needs-entry", digest: "d".repeat(64), policy: "platform-skills" } });
    expect(screen.getByText(/to ScanPolicy platform-skills/)).not.toBeNull();
    expect(screen.getAllByText("d".repeat(64)).length).toBeGreaterThan(1);
  });

  it("names the winner and the shadowed policy with the winner's priority", () => {
    renderPanel({
      assessment: blocked,
      unblock: { state: "superseded", digest: "d".repeat(64), policy: "older", winner: "newer", policyPriority: 2, winnerPriority: 9 },
    });
    expect(screen.getByText(/newer \(priority 9\) wins this Version over older \(priority 2\)/)).not.toBeNull();
  });

  it("shows the expiry for an expired entry", () => {
    renderPanel({ assessment: blocked, unblock: { state: "expired", digest: "d".repeat(64), policy: "platform", expires: "2026-01-01T00:00:00Z" } });
    expect(screen.getByText(/The Jev exemption for this digest in ScanPolicy platform expired at /)).not.toBeNull();
  });

  it("refers to the policy without a name when the expired entry's name is hidden", () => {
    renderPanel({ assessment: blocked, unblock: { state: "expired", digest: "d".repeat(64), expires: "2026-01-01T00:00:00Z" } });
    expect(screen.getByText(/The Jev exemption for this digest in a ScanPolicy expired at /)).not.toBeNull();
    expect(screen.queryByText(/ScanPolicy undefined/)).toBeNull();
  });

  it("shows the exempting policy when the entry is active", () => {
    renderPanel({ assessment: { ...blocked, blocked: false, exempted: true, exemptedBy: "platform" }, unblock: { state: "exempted", policy: "platform" } });
    expect(screen.getByText(/Exempted by platform. No expiry./)).not.toBeNull();
  });

  it("says the exempting policy is unnamed when the name is hidden", () => {
    renderPanel({ assessment: { ...blocked, blocked: false, exempted: true }, unblock: { state: "exempted", expires: "2026-02-01T00:00:00Z" } });
    expect(screen.getByText(/Exempted by the governing ScanPolicy. Expires /)).not.toBeNull();
    expect(screen.queryByText(/Exempted by undefined/)).toBeNull();
  });

  it("describes the unknown state without a reason or candidates", () => {
    renderPanel({ assessment: blocked, unblock: { state: "unknown", digest: "d".repeat(64) } });
    expect(screen.getByText(/The unblock state for this Version cannot be determined/)).not.toBeNull();
    expect(screen.getAllByText("d".repeat(64)).length).toBeGreaterThan(0);
  });

  it("shows the expiry for an unknown state", () => {
    renderPanel({ assessment: blocked, unblock: { state: "unknown", digest: "d".repeat(64), expires: "2026-02-01T00:00:00Z" } });
    expect(screen.getByText(/Expiry: /)).not.toBeNull();
  });

  it("names the winner and the shadowed policy with the winner's priority", () => {
    renderPanel({
      assessment: blocked,
      unblock: { state: "superseded", digest: "d".repeat(64), policy: "older", winner: "newer", policyPriority: 2, winnerPriority: 9 },
    });
    expect(screen.getByText(/newer \(priority 9\) wins this Version over older \(priority 2\)/)).not.toBeNull();
  });

  it("describes a superseded exemption without names when they are hidden", () => {
    renderPanel({
      assessment: blocked,
      unblock: { state: "superseded", digest: "d".repeat(64), policyPriority: 2, winnerPriority: 9 },
    });
    expect(screen.getByText(/Policy names are hidden for this account. The governing policy has priority 9 and the policy with the exemption has priority 2./)).not.toBeNull();
    expect(screen.queryByText(/wins this Version over/)).toBeNull();
  });

  it("does not claim no policy governs the Version when the governing name is hidden", () => {
    renderPanel({ assessment: blocked, unblock: { state: "needs-entry", digest: "d".repeat(64) } });
    expect(screen.queryByText(/No ScanPolicy governs this Version/)).toBeNull();
    expect(screen.getByText(/The governing policy's name is not shown here/)).not.toBeNull();
  });

  it("marks the create link when the governing policy name is hidden", () => {
    renderPanel({
      assessment: blocked,
      exemption: { namespace: "platform", targetKind: "Skill", targetName: "lint-helper", canEdit: true, governingPolicyHidden: true },
      unblock: { state: "needs-entry", digest: "d".repeat(64) },
    });
    const link = screen.getByRole("link", { name: /Create Jev exemption/ });
    expect(link.getAttribute("href")).toContain("jevGoverningHidden=true");
  });

  it("puts the create link's target into the prefill", () => {
    renderPanel({
      assessment: blocked,
      exemption: { namespace: "platform", versionName: "lint-helper-1-0-0", targetKind: "Skill", targetName: "lint-helper", canEdit: true },
      unblock: { state: "needs-entry", digest: "d".repeat(64) },
    });
    const link = screen.getByRole("link", { name: /Create Jev exemption/ });
    expect(link.getAttribute("href")).toContain("jevKind=Skill");
    expect(link.getAttribute("href")).toContain("jevName=lint-helper");
  });
});
