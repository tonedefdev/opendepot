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

  it("keeps Jev exemptions, including entries without expires, when saving an edited policy", async () => {
    const jevPolicy = {
      ...policy,
      spec: {
        ...policy.spec,
        jevExemptions: [
          { contentDigest: "a".repeat(64), reason: "Reviewed by security", expires: "2027-01-01T00:00:00Z" },
          { contentDigest: "b".repeat(64), reason: "Vendor attested" },
        ],
      },
    };
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [jevPolicy] }))
      .mockResolvedValueOnce(jsonResponse(capabilities))
      .mockResolvedValueOnce(jsonResponse(jevPolicy))
      .mockResolvedValueOnce(jsonResponse(jevPolicy));

    render(<SecurityPoliciesSurface mode="edit" namespace="platform" name="baseline" catalog={catalog} />);
    await waitFor(() => expect((screen.getByRole("button", { name: "Save changes" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(true));
    const put = fetchMock.mock.calls.find(([, init]) => init?.method === "PUT");
    expect(JSON.parse(String(put?.[1]?.body)).spec.jevExemptions).toEqual([
      { contentDigest: "a".repeat(64), reason: "Reviewed by security", expires: "2027-01-01T00:00:00Z" },
      { contentDigest: "b".repeat(64), reason: "Vendor attested" },
    ]);
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

  it("disables Jev editing with an explanation for callers who are not namespace-wide managers", async () => {
    const existing = { ...policy, spec: { ...policy.spec, jevExemptions: [{ contentDigest: "a".repeat(64), reason: "Vendor attested" }] } };
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [existing] }))
      .mockResolvedValueOnce(jsonResponse({ ...capabilities, canManageNamespaceWidePolicies: false }))
      .mockResolvedValueOnce(jsonResponse(existing));

    render(<SecurityPoliciesSurface mode="edit" namespace="platform" name="baseline" catalog={{
      ...catalog,
      items: [{ ...catalog.items[0], canManageNamespaceWidePolicies: false }],
    }} />);

    await waitFor(() => expect(screen.getByText(/Only namespace-wide policy managers can add, change, or remove Jev exemptions/)).not.toBeNull());
    expect((screen.getByRole("button", { name: "Add Jev exemption" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Remove Jev exemption 1" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByLabelText(/Content digest/) as HTMLInputElement).disabled).toBe(true);
  });

  it("keeps unchanged Jev exemptions when a scoped operator saves another field", async () => {
    const existing = {
      ...policy,
      spec: {
        ...policy.spec,
        targetRefs: [{ kind: "Module" as const, name: "terraform-aws-vpc" }],
        jevExemptions: [{ contentDigest: "a".repeat(64), reason: "Vendor attested" }],
      },
    };
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [existing] }))
      .mockResolvedValueOnce(jsonResponse({ ...capabilities, canManageNamespaceWidePolicies: false }))
      .mockResolvedValueOnce(jsonResponse(existing))
      .mockResolvedValueOnce(jsonResponse(existing));

    render(<SecurityPoliciesSurface mode="edit" namespace="platform" name="baseline" catalog={{
      ...catalog,
      items: [{ ...catalog.items[0], canManageNamespaceWidePolicies: false }],
    }} />);
    await waitFor(() => expect((screen.getByRole("button", { name: "Save changes" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(true));
    const put = fetchMock.mock.calls.find(([, init]) => init?.method === "PUT");
    expect(JSON.parse(String(put?.[1]?.body)).spec.jevExemptions).toEqual(existing.spec.jevExemptions);
  });

  it("creates a Jev exemption entry and keeps the Live YAML in sync", async () => {
    renderCreate();
    await waitFor(() => expect((screen.getByRole("button", { name: "Add Jev exemption" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Add Jev exemption" }));
    fireEvent.change(screen.getByLabelText(/Content digest/), { target: { value: "c".repeat(64) } });
    fireEvent.change(screen.getByLabelText(/^Reason/), { target: { value: "Reviewed the Jev assessment" } });

    expect(document.body.textContent).toContain(`contentDigest: ${"c".repeat(64)}`);
    expect(document.body.textContent).toContain("reason: Reviewed the Jev assessment");
  });

  it("validates the Jev content digest as 64 lowercase hex characters", async () => {
    renderCreate();
    await waitFor(() => expect((screen.getByRole("button", { name: "Add Jev exemption" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Add Jev exemption" }));
    const digest = screen.getByLabelText(/Content digest/);
    const digestError = "Enter 64 lowercase hex characters (a SHA-256 digest without a prefix).";

    fireEvent.change(digest, { target: { value: "A".repeat(64) } });
    expect(screen.getByText(digestError)).not.toBeNull();
    fireEvent.change(digest, { target: { value: `sha256:${"a".repeat(64)}` } });
    expect(screen.getByText(digestError)).not.toBeNull();
    fireEvent.change(digest, { target: { value: "a".repeat(63) } });
    expect(screen.getByText(digestError)).not.toBeNull();
    fireEvent.change(digest, { target: { value: "a".repeat(64) } });
    expect(screen.queryByText(digestError)).toBeNull();
  });

  it("flags a digest that already has a Jev exemption in the same policy", async () => {
    renderCreate();
    await waitFor(() => expect((screen.getByRole("button", { name: "Add Jev exemption" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Add Jev exemption" }));
    fireEvent.click(screen.getByRole("button", { name: "Add Jev exemption" }));
    const [first, second] = screen.getAllByLabelText(/Content digest/);
    fireEvent.change(first, { target: { value: "d".repeat(64) } });
    fireEvent.change(second, { target: { value: "d".repeat(64) } });

    expect(screen.getAllByText("This digest already has a Jev exemption in this policy.")).toHaveLength(1);
  });

  it("requires a non-blank reason for every Jev exemption", async () => {
    renderCreate();
    await waitFor(() => expect((screen.getByRole("button", { name: "Add Jev exemption" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Add Jev exemption" }));
    fireEvent.change(screen.getByLabelText(/Content digest/), { target: { value: "e".repeat(64) } });
    fireEvent.change(screen.getByLabelText(/^Reason/), { target: { value: "   " } });
    expect(screen.getByText("A reason is required for every Jev exemption.")).not.toBeNull();

    fireEvent.change(screen.getByLabelText(/^Reason/), { target: { value: "Reviewed" } });
    expect(screen.queryByText("A reason is required for every Jev exemption.")).toBeNull();
  });

  it("does not submit a policy with invalid Jev exemptions", async () => {
    const fetchMock = renderCreate();
    await waitFor(() => expect((screen.getByRole("button", { name: "Add Jev exemption" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Add Jev exemption" }));
    fireEvent.change(screen.getByLabelText(/Content digest/), { target: { value: "not-a-digest" } });
    fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

    await waitFor(() => expect(screen.getByText("Enter 64 lowercase hex characters (a SHA-256 digest without a prefix).")).not.toBeNull());
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
  });

  it("prefills a Jev exemption draft from the Security Policy route", async () => {
    renderCreate({ initialJevExemption: { contentDigest: "f".repeat(64), reason: "Jev exemption for version 1.2.3" } });
    await waitFor(() => expect((screen.getByRole("button", { name: "Add Jev exemption" }) as HTMLButtonElement).disabled).toBe(false));

    expect((screen.getByLabelText(/Content digest/) as HTMLInputElement).value).toBe("f".repeat(64));
    expect((screen.getByLabelText(/^Reason/) as HTMLInputElement).value).toBe("Jev exemption for version 1.2.3");
    expect(document.body.textContent).toContain(`contentDigest: ${"f".repeat(64)}`);
  });
});

function renderCreate(props: Partial<React.ComponentProps<typeof SecurityPoliciesSurface>> = {}) {
  const fetchMock = vi.spyOn(globalThis, "fetch")
    .mockResolvedValueOnce(jsonResponse({ items: [] }))
    .mockResolvedValueOnce(jsonResponse(capabilities));
  render(<SecurityPoliciesSurface mode="create" namespace="platform" catalog={catalog} {...props} />);
  return fetchMock;
}

