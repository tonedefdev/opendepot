// @vitest-environment jsdom

import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import NeedsReviewDrawer from "./NeedsReviewDrawer";
import theme from "@/theme";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function renderDrawer(open: boolean) {
  const onClose = vi.fn();
  render(
    <ThemeProvider theme={theme}>
      <NeedsReviewDrawer open={open} onClose={onClose} />
    </ThemeProvider>,
  );
  return { onClose };
}

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("NeedsReviewDrawer", () => {
  it("does not fetch the queue while closed", () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");

    renderDrawer(false);

    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.queryByText("Needs review")).toBeNull();
  });

  it("lists each Skill and Agent with its namespace, kind, risk, blocked and exempted state", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(jsonResponse({
      items: [
        { kind: "skill", namespace: "platform", name: "deploy-helper", riskLevel: "Critical", blocked: true, exempted: false },
        { kind: "agent", namespace: "research", name: "triage-bot", riskLevel: "Low", blocked: false, exempted: true },
      ],
    }));

    renderDrawer(true);

    await waitFor(() => expect(screen.getAllByText("deploy-helper").length).toBeGreaterThan(0));
    const table = within(screen.getByRole("table", { name: "Items that need review" }));
    const skillRow = within(table.getByRole("row", { name: /deploy-helper/ }));
    expect(skillRow.getByText("platform")).not.toBeNull();
    expect(skillRow.getByText("Skill")).not.toBeNull();
    expect(skillRow.getByText("Critical")).not.toBeNull();
    expect(skillRow.getByText("Blocked")).not.toBeNull();
    expect(skillRow.getByText("No")).not.toBeNull();
    const agentRow = within(table.getByRole("row", { name: /triage-bot/ }));
    expect(agentRow.getByText("research")).not.toBeNull();
    expect(agentRow.getByText("Agent")).not.toBeNull();
    expect(agentRow.getByText("Low")).not.toBeNull();
    expect(agentRow.getByText("Expired")).not.toBeNull();
    expect(screen.queryByText("Nothing needs review right now.")).toBeNull();
  });

  it("links each row to its detail page", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(jsonResponse({
      items: [{ kind: "agent", namespace: "research", name: "triage-bot", riskLevel: "High", blocked: false, exempted: false }],
    }));

    renderDrawer(true);

    const table = within(await screen.findByRole("table", { name: "Items that need review" }));
    expect(table.getByRole("link", { name: "triage-bot" }).getAttribute("href")).toBe("/research/agent/triage-bot");
  });

  it("shows the empty state when nothing needs review", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(jsonResponse({ items: [] }));

    renderDrawer(true);

    await waitFor(() => expect(screen.getByText("Nothing needs review right now.")).not.toBeNull());
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("explains what enters the queue and how exemptions affect it when empty", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(jsonResponse({ items: [] }));

    renderDrawer(true);

    await waitFor(() => expect(screen.getByText("Nothing needs review right now.")).not.toBeNull());
    expect(screen.getByText(/Critical verdict or cannot score a chunk/)).not.toBeNull();
    expect(screen.getByText(/returns when the exemption expires/)).not.toBeNull();
  });

  it("shows the server message when the queue cannot be loaded", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(jsonResponse({ code: "upstream_error", message: "server unavailable" }, 502));

    renderDrawer(true);

    await waitFor(() => expect(screen.getByText("server unavailable")).not.toBeNull());
    expect(screen.queryByText("Nothing needs review right now.")).toBeNull();
  });

  it("requests the queue through the same-origin route handler", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(jsonResponse({ items: [] }));

    renderDrawer(true);

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/review-queue", expect.objectContaining({ cache: "no-store" })));
  });
});
