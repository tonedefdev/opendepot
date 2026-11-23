// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ContractTable from "./ContractTable";
import theme from "@/theme";
import type { BrowseContract, ContractOutput, ContractVariable } from "@/lib/api";

const variables: ContractVariable[] = [
  {
    name: "region",
    type: "string",
    required: true,
    description: "The **AWS** region.",
    default: undefined,
  },
  {
    name: "api_token",
    type: "string",
    required: false,
    sensitive: true,
    default: 'abc"def',
  },
  {
    name: "config",
    type: ["object", { enabled: "bool", size: "number" }],
    optionalAttributes: { size: true },
    required: true,
    default: null,
  },
  {
    name: "tags",
    type: ["map", "string"],
    required: false,
    default: { env: "dev", list: [1, 2], nested: {} },
  },
  {
    name: "zones",
    type: ["tuple", ["string", "number"]],
    required: false,
    default: [],
  },
  {
    name: "empty_object",
    type: ["object", {}],
    required: false,
    default: true,
  },
  {
    name: "empty_tuple",
    type: ["tuple", []],
    required: false,
    default: 3,
  },
  {
    name: "any_type",
    type: [],
    required: false,
  },
];

const outputs: ContractOutput[] = [
  { name: "vpc_id", type: "string", confidence: "exact", description: "The VPC id." },
  {
    name: "secret",
    type: ["list", "string"],
    confidence: "inferred",
    sensitive: true,
    reason: "Inferred from a for expression.",
  },
  { name: "unknown_output", type: "dynamic", confidence: "unknown" },
];

const networkAttributes = ["cidr", "a", "b", "c", "d", "e", "f", "g", "h"];

function makeContract(overrides: Partial<BrowseContract> = {}): BrowseContract {
  return {
    schemaVersion: "1",
    module: { namespace: "acme", name: "vpc", version: "1.2.3" },
    variables,
    outputs,
    compatibility: { grade: "full", warnings: [] },
    provenance: { derivedAt: "2026-01-01T00:00:00Z" },
    ...overrides,
  };
}

interface ResizeRecord {
  callback: (entries: ResizeObserverEntry[]) => void;
  observe: ReturnType<typeof vi.fn>;
  disconnect: ReturnType<typeof vi.fn>;
}

const resizeObservers: ResizeRecord[] = [];

class ResizeObserverStub {
  observe: ReturnType<typeof vi.fn>;
  disconnect: ReturnType<typeof vi.fn>;

  constructor(callback: (entries: ResizeObserverEntry[]) => void) {
    this.observe = vi.fn();
    this.disconnect = vi.fn();
    resizeObservers.push({ callback, observe: this.observe, disconnect: this.disconnect });
  }
}

function resizeTo(width: number) {
  const entry = { contentRect: { width } } as unknown as ResizeObserverEntry;
  for (const record of resizeObservers) {
    record.callback([entry]);
  }
}

function matchMediaStub(matches: boolean) {
  return vi.fn((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
}

function renderTable(contract: BrowseContract = makeContract()) {
  return render(
    <ThemeProvider theme={theme}>
      <ContractTable contract={contract} />
    </ThemeProvider>,
  );
}

beforeEach(() => {
  resizeObservers.length = 0;
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("ContractTable header", () => {
  it.each([
    { grade: "full" as const, help: "Every variable is typed and every output type was resolved." },
    {
      grade: "partial" as const,
      help: "Some types could not be resolved. Untyped variables, child modules, or missing provider schemas degrade a contract.",
    },
    { grade: "unsupported" as const, help: "This module could not be parsed, so no contract could be derived." },
  ])("shows the $grade grade with its explanation on hover", async ({ grade, help }) => {
    renderTable(makeContract({ compatibility: { grade, warnings: [] } }));

    expect(screen.getByText(grade)).toBeTruthy();
    fireEvent.mouseOver(screen.getByText(grade));

    expect(await screen.findByText(help)).toBeTruthy();
  });

  it("prefixes the module version with v when it is missing", () => {
    renderTable(makeContract({ module: { namespace: "acme", name: "vpc", version: "1.2.3" } }));

    expect(screen.getByText("Derived from v1.2.3")).toBeTruthy();
  });

  it("keeps an existing v prefix on the module version", () => {
    renderTable(makeContract({ module: { namespace: "acme", name: "vpc", version: "v2.0.0" } }));

    expect(screen.getByText("Derived from v2.0.0")).toBeTruthy();
    expect(screen.queryByText("Derived from vv2.0.0")).toBeNull();
  });

  it("lists compatibility warnings in an info alert", () => {
    renderTable(makeContract({ compatibility: { grade: "partial", warnings: ["Child module x is unresolved", "Provider schema missing"] } }));

    const alert = screen.getByRole("alert");
    expect(within(alert).getByText("Child module x is unresolved")).toBeTruthy();
    expect(within(alert).getByText("Provider schema missing")).toBeTruthy();
    expect(alert.className).toContain("MuiAlert-standardInfo");
  });

  it("omits the warnings alert when there are no warnings", () => {
    renderTable(makeContract({ compatibility: { grade: "full" } }));

    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows the input and output counts on the tabs", () => {
    renderTable();

    expect(screen.getByRole("tab", { name: "Inputs (8)" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Outputs (3)" })).toBeTruthy();
  });

  it("treats missing variables and outputs as empty lists", () => {
    renderTable(makeContract({ variables: undefined, outputs: undefined }));

    expect(screen.getByRole("tab", { name: "Inputs (0)" })).toBeTruthy();
    expect(screen.getByText("This module declares no input variables.")).toBeTruthy();
  });
});

describe("ContractTable tabs and empty states", () => {
  it("shows the no-outputs message when the outputs tab is empty", () => {
    renderTable(makeContract({ outputs: [] }));

    fireEvent.click(screen.getByRole("tab", { name: "Outputs (0)" }));

    expect(screen.getByText("This module declares no outputs.")).toBeTruthy();
  });

  it("shows the no-inputs message when the inputs tab is empty", () => {
    renderTable(makeContract({ variables: [] }));

    expect(screen.getByText("This module declares no input variables.")).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("switches between the inputs and outputs tables", () => {
    renderTable();

    expect(screen.getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "Name",
      "Type",
      "Required",
      "Default",
      "Description",
    ]);

    fireEvent.click(screen.getByRole("tab", { name: "Outputs (3)" }));

    expect(screen.getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "Name",
      "Type",
      "Confidence",
      "Description",
    ]);
    expect(screen.getAllByText("vpc_id").length).toBeGreaterThan(0);
    expect(screen.queryAllByText("region")).toHaveLength(0);
  });
});

describe("ContractTable input rows", () => {
  it("shows required and optional chips and the sensitive marker", () => {
    renderTable();

    expect(screen.getAllByText("required").length).toBeGreaterThan(0);
    expect(screen.getAllByText("optional").length).toBeGreaterThan(0);
    expect(screen.getAllByText("sensitive").length).toBeGreaterThan(0);
  });

  it("renders a markdown description and an em dash for a missing one", () => {
    renderTable();

    const region = screen.getAllByText("region")[0].closest("tr");
    expect(region).not.toBeNull();
    expect(within(region as HTMLElement).getByText("AWS").tagName).toBe("STRONG");
    expect(within(region as HTMLElement).getByText("—")).toBeTruthy();
  });

  it("renders primitive and optional object types in OpenTofu form", () => {
    renderTable();

    expect(document.body.textContent).toContain("object({");
    expect(document.body.textContent).toContain("enabled = bool");
    expect(document.body.textContent).toContain("size = optional(number)");
    expect(document.body.textContent).toContain("map(string)");
  });

  it("renders tuple, empty object, empty tuple, and dynamic types", () => {
    renderTable();

    expect(document.body.textContent).toContain("tuple([");
    expect(document.body.textContent).toContain("object({})");
    expect(document.body.textContent).toContain("tuple([])");
    expect(document.body.textContent).toContain("dynamic");
  });

  it("renders defaults in HCL form with escaped strings and nulls", () => {
    renderTable();

    expect(document.body.textContent).toContain('"abc\\"def"');
    expect(document.body.textContent).toContain("null");
    expect(document.body.textContent).toContain("env = ");
    expect(document.body.textContent).toContain("list = [");
    expect(document.body.textContent).toContain("nested = {}");
    expect(document.body.textContent).toContain("true");
    expect(document.body.textContent).toContain("3");
    expect(document.body.textContent).toContain("[]");
  });

  it("shows an em dash for a variable with an undefined default", () => {
    renderTable(makeContract({ variables: [{ name: "plain", type: "string", required: true }], outputs: [] }));

    const row = screen.getAllByText("plain")[0].closest("tr") as HTMLElement;
    expect(within(row).getAllByText("—")).toHaveLength(2);
  });

  it("collapses a long type and toggles it open and closed", () => {
    const attributes = Object.fromEntries(networkAttributes.map((key) => [key, "string"]));
    renderTable(
      makeContract({
        variables: [{ name: "network", type: ["object", attributes], required: true }],
        outputs: [],
      }),
    );

    fireEvent.click(screen.getAllByRole("button", { name: /Show full type \(11 lines\)/ })[0]);

    expect(screen.getAllByRole("button", { name: "Collapse type" }).length).toBeGreaterThan(0);

    fireEvent.click(screen.getAllByRole("button", { name: "Collapse type" })[0]);

    expect(screen.getAllByRole("button", { name: "Show full type (11 lines)" }).length).toBeGreaterThan(0);
  });

  it("does not offer a collapse toggle for a short type", () => {
    renderTable(makeContract({ variables: [{ name: "short", type: "string", required: true }], outputs: [] }));

    expect(screen.queryByRole("button", { name: /Show full type/ })).toBeNull();
  });
});

describe("ContractTable output rows", () => {
  it("shows confidence chips and the output description", () => {
    renderTable();
    fireEvent.click(screen.getByRole("tab", { name: "Outputs (3)" }));

    expect(screen.getAllByText("exact").length).toBeGreaterThan(0);
    expect(screen.getAllByText("inferred").length).toBeGreaterThan(0);
    expect(screen.getAllByText("unknown").length).toBeGreaterThan(0);
    expect(screen.getAllByText("The VPC id.").length).toBeGreaterThan(0);
    expect(screen.getAllByText("sensitive").length).toBeGreaterThan(0);
  });

  it("explains a non-exact confidence in a tooltip", async () => {
    renderTable();
    fireEvent.click(screen.getByRole("tab", { name: "Outputs (3)" }));

    fireEvent.mouseOver(screen.getAllByText("inferred")[0]);

    expect(await screen.findByText("Inferred from a for expression.")).toBeTruthy();
  });

  it("shows an em dash for an output with no description", () => {
    renderTable(makeContract({ variables: [], outputs: [{ name: "bare", type: "string", confidence: "exact" }] }));

    fireEvent.click(screen.getByRole("tab", { name: "Outputs (1)" }));

    const row = screen.getAllByText("bare")[0].closest("tr") as HTMLElement;
    expect(within(row).getByText("—")).toBeTruthy();
  });
});

describe("ContractTable mobile cards", () => {
  it("renders an input card with its fields", () => {
    renderTable();

    expect(screen.getAllByText("Type").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Default").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Description").length).toBeGreaterThan(0);
  });

  it("renders output cards with confidence and description fields", () => {
    renderTable();
    fireEvent.click(screen.getByRole("tab", { name: "Outputs (3)" }));

    expect(screen.getAllByText("Confidence").length).toBeGreaterThan(0);
    expect(screen.getAllByText("secret").length).toBeGreaterThan(0);
  });
});

describe("ContractTable overflow and scrolling", () => {
  it("toggles the table height with a show more button when content overflows", () => {
    vi.spyOn(Element.prototype, "scrollHeight", "get").mockReturnValue(2000);
    vi.spyOn(Element.prototype, "clientHeight", "get").mockReturnValue(420);
    renderTable();

    const tableToggle = screen.getAllByRole("button", { name: "Show more" }).at(-1) as HTMLElement;
    fireEvent.click(tableToggle);

    expect(screen.getAllByRole("button", { name: "Show less" }).length).toBeGreaterThan(0);
  });

  it("toggles a long description when its content overflows", () => {
    vi.spyOn(Element.prototype, "scrollHeight", "get").mockReturnValue(500);
    renderTable(makeContract({ variables: [{ name: "long", type: "string", required: true, description: "Long text" }], outputs: [] }));

    const [firstToggle] = screen.getAllByRole("button", { name: "Show more" });
    fireEvent.click(firstToggle);

    expect(screen.getAllByRole("button", { name: "Show less" })).toHaveLength(1);
  });

  it("hides the table overflow toggle in compact layouts", () => {
    vi.stubGlobal("matchMedia", matchMediaStub(true));
    vi.spyOn(Element.prototype, "scrollHeight", "get").mockReturnValue(2000);
    vi.spyOn(Element.prototype, "clientHeight", "get").mockReturnValue(420);
    renderTable(makeContract({ variables: [{ name: "long", type: "string", required: true, description: "Long text" }], outputs: [] }));

    expect(screen.getAllByRole("button", { name: "Show more" })).toHaveLength(2);
  });

  it("mirrors the header scroll position to the body scroll position", () => {
    renderTable();

    const [headTable, bodyTable] = screen.getAllByRole("table", { hidden: true });
    const headScroll = headTable.parentElement as HTMLElement;
    const bodyScroll = bodyTable.parentElement as HTMLElement;

    fireEvent.scroll(bodyScroll, { target: { scrollLeft: 42 } });

    expect(headScroll.scrollLeft).toBe(42);
  });

  it("re-measures columns after a resize and pins them to the measured widths", () => {
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(800);
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(() => ({ width: 120 }) as DOMRect);
    renderTable();

    const [headTable, bodyTable] = screen.getAllByRole("table", { hidden: true });
    expect(bodyTable.querySelectorAll("col")).toHaveLength(5);
    expect(headTable.querySelectorAll("col")).toHaveLength(5);
    expect(window.getComputedStyle(bodyTable).tableLayout).toBe("fixed");

    resizeTo(900);
    resizeTo(900);

    expect(bodyTable.querySelectorAll("col")).toHaveLength(5);

    fireEvent.click(screen.getByRole("tab", { name: "Outputs (3)" }));

    expect(bodyTable.querySelectorAll("col")).toHaveLength(4);
  });

  it("keeps the natural layout when the body has no measurable width", () => {
    renderTable();

    const [, bodyTable] = screen.getAllByRole("table", { hidden: true });
    expect(bodyTable.querySelectorAll("col")).toHaveLength(0);
    expect(window.getComputedStyle(bodyTable).tableLayout).toBe("auto");
  });

  it("disconnects the resize observer on unmount", () => {
    const { unmount } = renderTable();

    expect(resizeObservers.some((record) => record.observe.mock.calls.length > 0)).toBe(true);

    unmount();

    expect(resizeObservers.every((record) => record.disconnect.mock.calls.length > 0)).toBe(true);
  });
});
