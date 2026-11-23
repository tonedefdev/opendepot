// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ScanPolicy, ScanPolicyCapabilities, ScanPolicyCatalog, ScanPolicyCatalogItem, ScanPolicyMutation } from "@/lib/api";
import SecurityPoliciesSurface from "./SecurityPoliciesSurface";
import { jevExemptionPrefill, type JevExemptionPrefill } from "./jevGovernance";

const router = { push: vi.fn(), refresh: vi.fn() };

vi.mock("next/navigation", () => ({
  useRouter: () => router,
}));

vi.mock("@/components/PageHeader", () => ({
  default: ({ title, actions }: { title: string; actions?: React.ReactNode }) => <header>{title}{actions}</header>,
}));

type FetchCall = Parameters<typeof fetch>;
type FetchResponse = Response | Error;

const capabilities: ScanPolicyCapabilities = {
  writesEnabled: true,
  canRead: true,
  canWrite: true,
  canManageNamespaceWidePolicies: true,
  methods: ["GET", "POST", "PUT", "DELETE"],
  supportsPreview: true,
};

const scopedCapabilities: ScanPolicyCapabilities = { ...capabilities, canManageNamespaceWidePolicies: false };

const defaultItem: ScanPolicyCatalogItem = {
  namespace: "platform",
  canRead: true,
  canWrite: true,
  canManageNamespaceWidePolicies: true,
  modules: [{ name: "terraform-aws-vpc" }],
  providers: [{ name: "aws" }],
  skills: [],
  agents: [],
};

const catalog: ScanPolicyCatalog = { writesEnabled: true, items: [defaultItem] };

const scopedCatalog: ScanPolicyCatalog = {
  writesEnabled: true,
  items: [{ ...defaultItem, canManageNamespaceWidePolicies: false }],
};

function catalogWith(resources: Partial<Pick<ScanPolicyCatalogItem, "modules" | "providers" | "skills" | "agents">>): ScanPolicyCatalog {
  return {
    writesEnabled: true,
    items: [{ ...defaultItem, modules: [], providers: [], skills: [], agents: [], ...resources }],
  };
}

const policy: ScanPolicy = {
  apiVersion: "opendepot.defdev.io/v1alpha1",
  kind: "ScanPolicy",
  metadata: { name: "baseline", namespace: "platform", resourceVersion: "v1" },
  spec: { priority: 0, severityThreshold: "HIGH", exemptions: [] },
};

const jevPrefill = jevExemptionPrefill({
  contentDigest: "f".repeat(64),
  versionName: "1.2.3",
  resource: { kind: "Module", name: "terraform-aws-vpc" },
  policies: [],
});

const savedJevPolicy: ScanPolicy = {
  ...policy,
  spec: {
    priority: 5,
    severityThreshold: "HIGH",
    targetRefs: [{ kind: "Module", name: "terraform-aws-vpc" }],
    jevExemptions: [{ contentDigest: "f".repeat(64), reason: "Jev exemption for version 1.2.3" }],
  },
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function queueFetch(...responses: FetchResponse[]) {
  const fetchMock = vi.spyOn(globalThis, "fetch");
  for (const response of responses) {
    if (response instanceof Error) {
      fetchMock.mockRejectedValueOnce(response);
    } else {
      fetchMock.mockResolvedValueOnce(response);
    }
  }
  return fetchMock;
}

function requestBody(fetchMock: { mock: { calls: FetchCall[] } }, method: string) {
  const call = fetchMock.mock.calls.find(([, init]) => init?.method === method);
  return JSON.parse(String(call?.[1]?.body)) as ScanPolicyMutation;
}

function callCount(fetchMock: { mock: { calls: FetchCall[] } }, method: string) {
  return fetchMock.mock.calls.filter(([, init]) => init?.method === method).length;
}

interface RenderOptions {
  catalog?: ScanPolicyCatalog;
  capabilities?: ScanPolicyCapabilities;
  jevPrefill?: JevExemptionPrefill;
}

function renderCreate(options: RenderOptions = {}, ...responses: FetchResponse[]) {
  const fetchMock = queueFetch(jsonResponse({ items: [] }), jsonResponse(options.capabilities ?? capabilities), ...responses);
  render(<SecurityPoliciesSurface mode="create" namespace="platform" catalog={options.catalog ?? catalog} jevPrefill={options.jevPrefill} />);
  return fetchMock;
}

function renderEdit(existing: ScanPolicy, options: RenderOptions = {}, ...responses: FetchResponse[]) {
  const fetchMock = queueFetch(
    jsonResponse({ items: [existing] }),
    jsonResponse(options.capabilities ?? capabilities),
    jsonResponse(existing),
    ...responses,
  );
  render(<SecurityPoliciesSurface mode="edit" namespace="platform" name={existing.metadata.name} catalog={options.catalog ?? catalog} />);
  return fetchMock;
}

async function expectEnabled(name: string) {
  await waitFor(() => expect((screen.getByRole("button", { name }) as HTMLButtonElement).disabled).toBe(false));
}

function typeName(value = "baseline") {
  fireEvent.change(screen.getAllByRole("textbox", { name: /Name/ })[0], { target: { value } });
}

function closeListbox() {
  fireEvent.keyDown(screen.getByRole("listbox"), { key: "Escape" });
  return waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}

afterEach(() => {
  vi.restoreAllMocks();
  router.push.mockReset();
  router.refresh.mockReset();
});

describe("SecurityPoliciesSurface interactions", () => {
  describe("policy list", () => {
    it("filters policies by search text and reports when nothing matches", async () => {
      const items = [policy, { ...policy, metadata: { ...policy.metadata, name: "restricted" } }];
      queueFetch(jsonResponse({ items }), jsonResponse(capabilities));
      render(<SecurityPoliciesSurface mode="list" namespace="platform" catalog={catalog} />);

      await waitFor(() => expect(screen.getByText("restricted")).not.toBeNull());
      fireEvent.change(screen.getByLabelText("Search policies"), { target: { value: "RESTRICTED" } });
      expect(screen.getByText("restricted")).not.toBeNull();
      expect(screen.queryByText("baseline")).toBeNull();

      fireEvent.change(screen.getByLabelText("Search policies"), { target: { value: "no-such-policy" } });
      expect(screen.getByText(/No policies match/)).not.toBeNull();
    });

    it("reloads the policy list from the refresh button", async () => {
      const fetchMock = queueFetch(
        jsonResponse({ items: [policy] }),
        jsonResponse(capabilities),
        jsonResponse({ items: [policy] }),
        jsonResponse(capabilities),
      );
      render(<SecurityPoliciesSurface mode="list" namespace="platform" catalog={catalog} />);

      await waitFor(() => expect(screen.getByText("baseline")).not.toBeNull());
      fireEvent.click(screen.getByRole("button", { name: "Refresh policies" }));

      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));
    });

    it("asks for a namespace and does not fetch when the catalog has no namespaces", () => {
      const fetchMock = queueFetch();
      render(<SecurityPoliciesSurface mode="list" namespace="platform" catalog={{ writesEnabled: true, items: [] }} />);

      expect(screen.getByText("Select a namespace to view its policies.")).not.toBeNull();
      expect(fetchMock).not.toHaveBeenCalled();
    });

    it("shows the server message when the policy list cannot be loaded", async () => {
      queueFetch(jsonResponse({ message: "Policy service unavailable" }, 500), jsonResponse(capabilities));
      render(<SecurityPoliciesSurface mode="list" namespace="platform" catalog={catalog} />);

      await waitFor(() => expect(screen.getByText("Policy service unavailable")).not.toBeNull());
    });
  });

  describe("create validation and save errors", () => {
    it("requires an authorized target when a scoped operator saves without one", async () => {
      const fetchMock = renderCreate({ catalog: scopedCatalog, capabilities: scopedCapabilities });
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      expect(screen.getByText("At least one authorized target is required for this namespace.")).not.toBeNull();
      expect(callCount(fetchMock, "POST")).toBe(0);
    });

    it("blocks Jev exemption changes from scoped operators without sending a write", async () => {
      const fetchMock = renderCreate({ catalog: scopedCatalog, capabilities: scopedCapabilities, jevPrefill });
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      expect(screen.getByText("Only namespace-wide policy managers can add, change, or remove Jev exemptions.")).not.toBeNull();
      expect(callCount(fetchMock, "POST")).toBe(0);
    });

    it("explains a forbidden save and disables writes", async () => {
      renderCreate({}, jsonResponse({ message: "forbidden" }, 403));
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Add target" }));
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      await waitFor(() => expect(screen.getByText("Your account is not authorized to manage ScanPolicies.")).not.toBeNull());
      expect((screen.getByRole("button", { name: "Create policy" }) as HTMLButtonElement).disabled).toBe(true);
      expect(router.push).not.toHaveBeenCalled();
    });

    it("shows the server message when a save is rejected", async () => {
      renderCreate({}, jsonResponse({ message: "priority conflicts with an existing policy" }, 400));
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Add target" }));
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      await waitFor(() => expect(screen.getByText("priority conflicts with an existing policy")).not.toBeNull());
    });

    it("shows a message when the preview request fails", async () => {
      renderCreate({}, new Error("Preview service unreachable"));
      await expectEnabled("Validate policy");
      fireEvent.click(screen.getByRole("button", { name: "Validate policy" }));

      await waitFor(() => expect(screen.getByText("Preview service unreachable")).not.toBeNull());
    });
  });

  describe("finding exemptions and targets", () => {
    it("requires a reason for a new finding exemption and removes rows", async () => {
      renderCreate();
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Add target" }));
      fireEvent.click(screen.getByRole("button", { name: "Add exemption" }));
      fireEvent.change(screen.getByLabelText(/^Reason/), { target: { value: "   " } });
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      expect(screen.getByText("A reason is required for every exemption.")).not.toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Remove exemption 1" }));
      expect(screen.getByText("No exemptions configured.")).not.toBeNull();
    });

    it("submits vulnerability IDs, severities, and scan types for a finding exemption", async () => {
      const fetchMock = renderCreate({}, jsonResponse(savedJevPolicy));
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Add target" }));
      fireEvent.click(screen.getByRole("button", { name: "Add exemption" }));
      fireEvent.change(screen.getByLabelText(/Vulnerability IDs/), { target: { value: "CVE-2026-0001, aws-0057" } });
      fireEvent.change(screen.getByLabelText(/^Reason/), { target: { value: "Accepted risk" } });

      fireEvent.mouseDown(screen.getByRole("combobox", { name: /Severities/ }));
      fireEvent.click(screen.getByRole("option", { name: "CRITICAL" }));
      await closeListbox();
      fireEvent.mouseDown(screen.getByRole("combobox", { name: /Scan types/ }));
      fireEvent.click(screen.getByRole("option", { name: "module" }));
      await closeListbox();

      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      await waitFor(() => expect(callCount(fetchMock, "POST")).toBe(1));
      expect(requestBody(fetchMock, "POST").spec.exemptions).toEqual([
        { vulnerabilityIDs: ["CVE-2026-0001", "aws-0057"], reason: "Accepted risk", severities: ["CRITICAL"], scanTypes: ["module"] },
      ]);
    });

    it.each([
      ["skills", "Skill", "skill-a"],
      ["agents", "Agent", "agent-a"],
      ["providers", "Provider", "aws"],
    ] as const)("adds a %s target when it is the only onboarded resource kind", async (resourceKey, kind, resourceName) => {
      renderCreate({ catalog: catalogWith({ [resourceKey]: [{ name: resourceName }] }) });
      await expectEnabled("Create policy");
      fireEvent.click(screen.getByRole("button", { name: "Add target" }));

      expect(document.body.textContent).toContain(resourceName);
      expect(document.body.textContent).toContain(kind);
    });

    it("disables Add target when no authorized resources exist", async () => {
      renderCreate({ catalog: catalogWith({}) });
      await expectEnabled("Create policy");

      expect((screen.getByRole("button", { name: "Add target" }) as HTMLButtonElement).disabled).toBe(true);
    });
  });

  describe("edit flows", () => {
    it("saves edited vulnerability and package lists with the resource version", async () => {
      const existing: ScanPolicy = {
        ...policy,
        spec: { ...policy.spec, exemptions: [{ reason: "Accepted risk", vulnerabilityIDs: ["CVE-2026-0001"] }] },
      };
      const fetchMock = renderEdit(existing, {}, jsonResponse(existing));
      await expectEnabled("Save changes");
      fireEvent.change(screen.getByLabelText(/Vulnerability IDs/), { target: { value: "CVE-2026-0001, aws-0057" } });
      fireEvent.change(screen.getByLabelText(/Package names/), { target: { value: "stdlib" } });
      fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

      await waitFor(() => expect(router.push).toHaveBeenCalledWith("/security-policies/platform/baseline"));
      const put = fetchMock.mock.calls.find(([, init]) => init?.method === "PUT");
      expect((put?.[1]?.headers as Record<string, string>)["If-Match"]).toBe("v1");
      expect(requestBody(fetchMock, "PUT").spec.exemptions).toEqual([
        { reason: "Accepted risk", vulnerabilityIDs: ["CVE-2026-0001", "aws-0057"], pkgNames: ["stdlib"] },
      ]);
    });

    it("deletes the policy after confirmation and returns to the policy list", async () => {
      const fetchMock = renderEdit(policy, {}, jsonResponse({}));
      await expectEnabled("Save changes");
      fireEvent.click(screen.getByRole("button", { name: "Delete policy" }));
      const dialog = screen.getByRole("dialog");
      expect(within(dialog).getByText("Delete baseline?")).not.toBeNull();
      fireEvent.click(within(dialog).getByRole("button", { name: "Delete policy" }));

      await waitFor(() => expect(router.push).toHaveBeenCalledWith("/security-policies"));
      expect(router.refresh).toHaveBeenCalled();
      const del = fetchMock.mock.calls.find(([, init]) => init?.method === "DELETE");
      expect(del?.[0]).toBe("/api/security-policies/platform/baseline");
      expect((del?.[1]?.headers as Record<string, string>)["If-Match"]).toBe("v1");
    });

    it("keeps the policy when the delete dialog is cancelled", async () => {
      const fetchMock = renderEdit(policy);
      await expectEnabled("Save changes");
      fireEvent.click(screen.getByRole("button", { name: "Delete policy" }));
      fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }));

      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(callCount(fetchMock, "DELETE")).toBe(0);
      expect(router.push).not.toHaveBeenCalled();
    });

    it("shows an authorization warning and closes the dialog when delete is forbidden", async () => {
      renderEdit(policy, {}, jsonResponse({ message: "forbidden" }, 403));
      await expectEnabled("Save changes");
      fireEvent.click(screen.getByRole("button", { name: "Delete policy" }));
      fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete policy" }));

      await waitFor(() => expect(screen.getByText("Your account is not authorized to manage ScanPolicies.")).not.toBeNull());
      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(router.push).not.toHaveBeenCalled();
    });
  });

  describe("Jev exemption prefill", () => {
    it("explains the default priority when no existing policy governs the Version", () => {
      renderCreate({ jevPrefill });

      expect(screen.getByText("No existing policy governs this Version, so the exemption uses the default priority.")).not.toBeNull();
    });

    it("explains that the default priority may not outrank a governing policy whose name is withheld", () => {
      const prefill = jevExemptionPrefill({
        contentDigest: "f".repeat(64),
        resource: { kind: "Module", name: "terraform-aws-vpc" },
        policies: [],
        governingNameHidden: true,
      });
      renderCreate({ jevPrefill: prefill });

      expect(screen.getByText("The governing policy could not be named, so the default priority may not outrank it.")).not.toBeNull();
    });

    it("explains that the priority is unset when existing policies could not be read", () => {
      const prefill = jevExemptionPrefill({
        contentDigest: "f".repeat(64),
        resource: { kind: "Module", name: "terraform-aws-vpc" },
        policies: null,
      });
      renderCreate({ jevPrefill: prefill });

      expect(screen.getByText("Not set above the existing policies because they could not be read.")).not.toBeNull();
    });

    it("raises the priority one above the governing policy", () => {
      const governing: ScanPolicy = {
        ...policy,
        metadata: { name: "platform-wide", namespace: "platform" },
        spec: { priority: 4, severityThreshold: "HIGH", targetRefs: [{ kind: "Module", name: "terraform-aws-vpc" }] },
      };
      const prefill = jevExemptionPrefill({
        contentDigest: "f".repeat(64),
        resource: { kind: "Module", name: "terraform-aws-vpc" },
        policies: [governing],
      });
      renderCreate({ jevPrefill: prefill });

      expect(screen.getByText("One above the highest priority that governs this Version (4), so this exemption wins. You can change it.")).not.toBeNull();
      expect((screen.getByLabelText("Priority") as HTMLInputElement).value).toBe("5");
    });

    it("reports a Jev exemption that wins at the saved priority", async () => {
      const refreshed = { ...savedJevPolicy, status: { matchedVersions: 1 } };
      renderCreate({ jevPrefill }, jsonResponse(savedJevPolicy), jsonResponse(refreshed), jsonResponse({ items: [refreshed] }));
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      await waitFor(() => expect(screen.getByText(/governs this Version and wins at priority 5/)).not.toBeNull());
      expect((screen.getByRole("button", { name: "Create policy" }) as HTMLButtonElement).disabled).toBe(true);
    });

    it("reports a superseded Jev exemption with a link to edit the policy", async () => {
      const superseded = { ...savedJevPolicy, status: { supersededBy: "platform-wide" } };
      const winner: ScanPolicy = { ...policy, metadata: { name: "platform-wide", namespace: "platform" }, spec: { priority: 3, severityThreshold: "HIGH" } };
      renderCreate({ jevPrefill }, jsonResponse(savedJevPolicy), jsonResponse(superseded), jsonResponse({ items: [winner] }));
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      await waitFor(() => expect(screen.getByText(/is superseded: platform-wide \(priority 3\) wins this Version/)).not.toBeNull());
      expect(screen.getByRole("link", { name: "Edit policy" }).getAttribute("href")).toBe("/security-policies/platform/baseline");
    });

    it("reports a Jev exemption that matches no Version", async () => {
      const noMatch = { ...savedJevPolicy, status: { matchedVersions: 0 } };
      renderCreate({ jevPrefill }, jsonResponse(savedJevPolicy), jsonResponse(noMatch), jsonResponse({ items: [noMatch] }));
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      await waitFor(() => expect(screen.getByText(/matches no Version, so its Jev exemption cannot apply/)).not.toBeNull());
    });

    it("keeps the pending result when the saved policy cannot be read back", async () => {
      renderCreate({ jevPrefill }, jsonResponse(savedJevPolicy), jsonResponse({ message: "read failed" }, 500), jsonResponse({ items: [] }));
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      await waitFor(() => expect(screen.getByText(/has not reported whether it governs this Version yet/)).not.toBeNull());
    });

    it("refreshes a pending Jev result into a final state", async () => {
      renderCreate(
        { jevPrefill },
        jsonResponse(savedJevPolicy),
        jsonResponse(savedJevPolicy),
        jsonResponse({ items: [savedJevPolicy] }),
        jsonResponse({ ...savedJevPolicy, status: { matchedVersions: 2 } }),
        jsonResponse({ items: [savedJevPolicy] }),
      );
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));
      await waitFor(() => expect(screen.getByRole("button", { name: "Refresh" })).not.toBeNull());

      fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

      await waitFor(() => expect(screen.getByText(/governs this Version and wins at priority 5/)).not.toBeNull());
    });

    it("shows the server message when refreshing the Jev result fails", async () => {
      renderCreate(
        { jevPrefill },
        jsonResponse(savedJevPolicy),
        jsonResponse(savedJevPolicy),
        jsonResponse({ items: [savedJevPolicy] }),
        jsonResponse({ message: "Status unavailable" }, 500),
        jsonResponse({ items: [] }),
      );
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));
      await waitFor(() => expect(screen.getByRole("button", { name: "Refresh" })).not.toBeNull());

      fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

      await waitFor(() => expect(screen.getByText("Status unavailable")).not.toBeNull());
    });

    it("sends the prefilled Jev exemption and target with the new policy", async () => {
      const fetchMock = renderCreate({ jevPrefill }, jsonResponse(savedJevPolicy), jsonResponse(savedJevPolicy), jsonResponse({ items: [] }));
      await expectEnabled("Create policy");
      typeName();
      fireEvent.click(screen.getByRole("button", { name: "Create policy" }));

      await waitFor(() => expect(callCount(fetchMock, "POST")).toBe(1));
      const body = requestBody(fetchMock, "POST");
      expect(body.spec.targetRefs).toEqual([{ kind: "Module", name: "terraform-aws-vpc" }]);
      expect(body.spec.jevExemptions).toEqual([{ contentDigest: "f".repeat(64), reason: "Jev exemption for version 1.2.3" }]);
      expect(body.spec.priority).toBe(0);
    });
  });
});
