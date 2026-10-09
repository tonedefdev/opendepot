// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import SecurityPoliciesSurface from "./SecurityPoliciesSurface";

const router = { push: vi.fn(), refresh: vi.fn() };

vi.mock("next/navigation", () => ({
  useRouter: () => router,
}));

vi.mock("@/components/PageHeader", () => ({
  default: ({ title }: { title: string }) => <header>{title}</header>,
}));

const capabilities = {
  writesEnabled: true,
  canRead: true,
  canWrite: true,
  canManageNamespaceWidePolicies: true,
  methods: ["GET", "POST", "PUT", "DELETE"],
  supportsPreview: true,
};

const catalog = {
  writesEnabled: true,
  items: [{
    namespace: "platform",
    canRead: true,
    canWrite: true,
    canManageNamespaceWidePolicies: true,
    modules: [{ name: "terraform-aws-vpc" }],
    providers: [{ name: "aws" }],
    skills: [],
    agents: [],
  }],
};

const policy = {
  apiVersion: "opendepot.defdev.io/v1alpha1" as const,
  kind: "ScanPolicy" as const,
  metadata: { name: "baseline", namespace: "platform", resourceVersion: "v1" },
  spec: { priority: 0, severityThreshold: "HIGH" as const, exemptions: [] },
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  vi.restoreAllMocks();
  router.push.mockReset();
  router.refresh.mockReset();
});

describe("SecurityPoliciesSurface", () => {
  it("shows policy skeleton cards while the list is loading", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise<Response>(() => {}));

    render(<SecurityPoliciesSurface mode="list" namespace="platform" catalog={catalog} />);

    expect(screen.getByRole("status", { name: "Loading policies" })).not.toBeNull();
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  it("paginates the policy list", async () => {
    const items = Array.from({ length: 13 }, (_, index) => ({ ...policy, metadata: { ...policy.metadata, name: `policy-${String(index + 1).padStart(2, "0")}` } }));
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items }))
      .mockResolvedValueOnce(jsonResponse(capabilities));

    render(<SecurityPoliciesSurface mode="list" namespace="platform" catalog={catalog} />);

    await waitFor(() => expect(screen.getByText("1–12 of 13 policies")).not.toBeNull());
    expect(screen.getByText("policy-12")).not.toBeNull();
    expect(screen.queryByText("policy-13")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Go to page 2" }));
    await waitFor(() => expect(screen.getByText("policy-13")).not.toBeNull());
    expect(screen.getByText("13–13 of 13 policies")).not.toBeNull();
  });

  it("shows the total number of expired exemptions", async () => {
    const items = [
      { ...policy, status: { expiredExemptions: 2 } },
      { ...policy, metadata: { ...policy.metadata, name: "restricted" }, status: { expiredExemptions: 1 } },
    ];
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items }))
      .mockResolvedValueOnce(jsonResponse(capabilities));

    render(<SecurityPoliciesSurface mode="list" namespace="platform" catalog={catalog} />);

    await waitFor(() => expect(screen.getByText("Expired exemptions")).not.toBeNull());
    expect(screen.getByText("3")).not.toBeNull();
  });

  it("does not show the read-only message while create capabilities are loading", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise<Response>(() => {}));

    render(<SecurityPoliciesSurface mode="create" namespace="platform" catalog={catalog} />);

    expect(screen.queryByText(/read-only access/)).toBeNull();
    expect(screen.queryByText(/This binding permits only policies targeted/)).toBeNull();
  });

  it("previews an unsaved create draft without issuing a write request", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse(capabilities))
      .mockResolvedValueOnce(jsonResponse({ valid: true, policy }));

    render(<SecurityPoliciesSurface mode="create" namespace="platform" catalog={catalog} />);
    const nameInput = screen.getAllByRole("textbox", { name: /Name/ })[0];
    fireEvent.change(nameInput, { target: { value: "baseline" } });
    await waitFor(() => expect((screen.getByRole("button", { name: "Validate policy" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Validate policy" }));

    await waitFor(() => expect(screen.getByText("Policy is valid. Nothing was saved.")).not.toBeNull());
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/security-policies/platform/preview",
      expect.objectContaining({ method: "POST" }),
    );
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "PUT" || init?.method === "POST" && String(init?.body).includes("security-policies/platform\""))).toBe(false);
  });

  it("shows invalid validation results and dismisses the alert", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse(capabilities))
      .mockResolvedValueOnce(jsonResponse({ valid: false, policy: { ...policy, status: { matchedVersions: 2 } } }));

    render(<SecurityPoliciesSurface mode="create" namespace="platform" catalog={catalog} />);
    await waitFor(() => expect((screen.getByRole("button", { name: "Validate policy" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Validate policy" }));

    await waitFor(() => expect(screen.getByText("Policy is invalid according to the server. Nothing was saved.")).not.toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByText("Policy is invalid according to the server. Nothing was saved.")).toBeNull());
  });

  it("hides create controls for a read-only capability response", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse({ ...capabilities, canWrite: false }));

    render(<SecurityPoliciesSurface mode="list" namespace="platform" catalog={catalog} />);
    await waitFor(() => expect(screen.getByText("No policies in platform")).not.toBeNull());
    expect(screen.queryByRole("link", { name: "Create policy" })).toBeNull();
  });

  it("preserves the edit draft and reports an optimistic-concurrency conflict", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [policy] }))
      .mockResolvedValueOnce(jsonResponse(capabilities))
      .mockResolvedValueOnce(jsonResponse(policy))
      .mockResolvedValueOnce(jsonResponse({ code: "conflict", message: "resource version is stale" }, 409));

    render(<SecurityPoliciesSurface mode="edit" namespace="platform" name="baseline" catalog={catalog} />);
    await waitFor(() => expect((screen.getByRole("button", { name: "Save changes" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(screen.getByText(/changed on the server/)).not.toBeNull());
    expect(router.push).not.toHaveBeenCalled();
  });

  it("only exposes catalog resources and hides selector editing for scoped operators", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse({ ...capabilities, canManageNamespaceWidePolicies: false }));

    render(<SecurityPoliciesSurface mode="create" namespace="platform" catalog={{
      ...catalog,
      items: [{ ...catalog.items[0], canManageNamespaceWidePolicies: false, modules: [{ name: "terraform-aws-vpc" }], providers: [], skills: [], agents: [] }],
    }} />);

    await waitFor(() => expect(screen.getByText(/only policies targeted/)).not.toBeNull());
    expect(screen.queryByLabelText("Target labels (JSON object)")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Add target" }));
    expect(screen.getByText("terraform-aws-vpc")).not.toBeNull();
    expect(screen.queryByRole("textbox", { name: "Resource name" })).toBeNull();
  });
});
