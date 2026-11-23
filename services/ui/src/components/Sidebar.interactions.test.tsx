// @vitest-environment jsdom

import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Sidebar from "./Sidebar";
import { TOGGLE_SIDEBAR_EVENT } from "@/components/MobileSidebarButton";
import theme from "@/theme";

const router = { push: vi.fn(), refresh: vi.fn() };
const colorScheme = { mode: "dark", systemMode: "dark", setMode: vi.fn() };
const SIDEBAR_COLLAPSED_STORAGE_KEY = "opendepot.sidebar.collapsed";
const SIDEBAR_COLLAPSED_COOKIE = "opendepot_sidebar_collapsed";
let pathname = "/";
let search = "";
let mobile = false;

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => pathname,
  useSearchParams: () => new URLSearchParams(search),
}));

vi.mock("@mui/material/useMediaQuery", () => ({
  default: () => mobile,
}));

vi.mock("@mui/material/styles", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@mui/material/styles")>();
  return {
    ...actual,
    useColorScheme: () => colorScheme,
  };
});

const namespaces = [
  { name: "team-a", public: false },
  { name: "team-b", public: true },
];

const user = { email: "dev@example.com", name: "Dev User" };

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function renderSidebar(props: React.ComponentProps<typeof Sidebar> = {}) {
  return render(
    <ThemeProvider theme={theme}>
      <Sidebar initialNamespaces={namespaces} {...props} />
    </ThemeProvider>,
  );
}

function searchInput() {
  return screen.getByRole("textbox");
}

function searchClearButton() {
  const button = searchInput().closest("form")?.querySelector("button");
  if (!button) {
    throw new Error("Search clear button not found");
  }
  return button;
}

function devTokenInput() {
  const input = document.querySelector<HTMLInputElement>(".MuiDrawer-docked [data-testid=\"dev-token-input\"]");
  if (!input) {
    throw new Error("Dev token input not found");
  }
  return input;
}

function desktopSearchInput() {
  const input = document.querySelector<HTMLInputElement>(".MuiDrawer-docked [data-sidebar-search]");
  if (!input) {
    throw new Error("Desktop search input not found");
  }
  return input;
}

function mobileDrawerRoot() {
  const root = document.querySelector<HTMLElement>(".MuiDrawer-modal");
  if (!root) {
    throw new Error("Mobile drawer not found");
  }
  return root;
}

function openMobileDrawer() {
  act(() => {
    window.dispatchEvent(new Event(TOGGLE_SIDEBAR_EVENT));
  });
}

beforeEach(() => {
  const store = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
    removeItem: (key: string) => {
      store.delete(key);
    },
    clear: () => {
      store.clear();
    },
    key: (index: number) => [...store.keys()][index] ?? null,
    get length() {
      return store.size;
    },
  });
  vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("not available in unit test"));
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  router.push.mockClear();
  router.refresh.mockClear();
  colorScheme.setMode.mockClear();
  colorScheme.mode = "dark";
  pathname = "/";
  search = "";
  mobile = false;
  document.cookie = `${SIDEBAR_COLLAPSED_COOKIE}=; path=/; max-age=0`;
});

describe("Sidebar search", () => {
  it("submits the typed query and drops the page parameter", () => {
    search = "page=3&kind=agent";
    renderSidebar();

    fireEvent.change(searchInput(), { target: { value: "widget" } });
    fireEvent.submit(searchInput().closest("form") as HTMLFormElement);

    expect(router.push).toHaveBeenCalledWith("/?kind=agent&q=widget");
  });

  it("clears the query and removes it from the URL", () => {
    search = "q=widget&kind=agent";
    renderSidebar();

    expect(searchInput().getAttribute("value")).toBe("widget");
    fireEvent.click(searchClearButton());

    expect((searchInput() as HTMLInputElement).value).toBe("");
    expect(router.push).toHaveBeenCalledWith("/?kind=agent");
  });
});

describe("Sidebar namespace filter", () => {
  it("selects a namespace on the home page while keeping other filters", () => {
    search = "kind=agent&page=2";
    renderSidebar();

    fireEvent.click(screen.getByRole("button", { name: /^team-a/ }));

    expect(router.push).toHaveBeenCalledWith("/?kind=agent&namespace=team-a");
  });

  it("deselects the active namespace when it is clicked again", () => {
    search = "namespace=team-a";
    renderSidebar();

    fireEvent.click(screen.getByRole("button", { name: /^team-a/ }));

    expect(router.push).toHaveBeenCalledWith("/?");
  });

  it("does not filter by namespace from pages other than the registry", () => {
    pathname = "/depots";
    renderSidebar();

    fireEvent.click(screen.getByRole("button", { name: /^team-b/ }));

    expect(router.push).not.toHaveBeenCalled();
  });

  it("clears the namespace filter from the Clear filter action", () => {
    search = "namespace=team-a&kind=agent";
    renderSidebar();

    fireEvent.click(screen.getByRole("button", { name: "Clear filter" }));

    expect(router.push).toHaveBeenCalledWith("/?kind=agent");
  });

  it("shows namespaces returned by the client-side fallback fetch", async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(
      jsonResponse({ items: [{ name: "fetched-ns", public: true }] }),
    );
    renderSidebar({ initialNamespaces: [] });

    expect(await screen.findByRole("button", { name: /^fetched-ns/ })).not.toBeNull();
    expect(screen.getAllByText("pub").length).toBeGreaterThan(0);
  });

  it("leaves the namespace list empty when the fallback response has no items", async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse({}));
    renderSidebar({ initialNamespaces: [] });

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.queryByText("Namespace")).toBeNull();
  });

  it("keeps the namespace list empty when the fallback fetch fails", async () => {
    renderSidebar({ initialNamespaces: [] });

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.queryByText("Namespace")).toBeNull();
  });
});

describe("Sidebar dev token", () => {
  it("hides the dev token input unless it is enabled", () => {
    renderSidebar();

    expect(document.querySelector("[data-testid=\"dev-token-input\"]")).toBeNull();
  });

  it("disables the save action until a token is entered", () => {
    renderSidebar({ devTokenEnabled: true });

    expect((screen.getByRole("button", { name: "Save token" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(devTokenInput(), { target: { value: "abc" } });

    expect((screen.getByRole("button", { name: "Save token" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("saves the trimmed token and refreshes the route", async () => {
    const fetchMock = vi.mocked(globalThis.fetch).mockResolvedValue(new Response("{}", { status: 200 }));
    renderSidebar({ devTokenEnabled: true });

    fireEvent.change(devTokenInput(), { target: { value: "  abc123  " } });
    fireEvent.click(screen.getByRole("button", { name: "Save token" }));

    expect(fetchMock).toHaveBeenCalledWith("/auth/dev-token", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: "abc123" }),
    });
    expect((await screen.findByRole("alert")).textContent).toBe("Token saved. Refreshing…");
    await waitFor(() => expect(router.refresh).toHaveBeenCalled(), { timeout: 2000 });
  });

  it("saves the token when Enter is pressed in the input", async () => {
    const fetchMock = vi.mocked(globalThis.fetch).mockResolvedValue(new Response("{}", { status: 200 }));
    renderSidebar({ devTokenEnabled: true });

    const input = devTokenInput();
    fireEvent.change(input, { target: { value: "enter-token" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/auth/dev-token", expect.objectContaining({ method: "POST" })));
    await waitFor(() => expect(router.refresh).toHaveBeenCalled(), { timeout: 2000 });
  });

  it("does not submit a blank token when Enter is pressed", () => {
    const fetchMock = vi.mocked(globalThis.fetch);
    renderSidebar({ devTokenEnabled: true });

    const input = devTokenInput();
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows the error message returned by the server", async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse({ error: "Token rejected" }, 401));
    renderSidebar({ devTokenEnabled: true });

    fireEvent.change(devTokenInput(), { target: { value: "bad" } });
    fireEvent.click(screen.getByRole("button", { name: "Save token" }));

    expect((await screen.findByRole("alert")).textContent).toBe("Token rejected");
    expect(router.refresh).not.toHaveBeenCalled();
  });

  it("shows a generic error when the server response has no message", async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse({}, 500));
    renderSidebar({ devTokenEnabled: true });

    fireEvent.change(devTokenInput(), { target: { value: "bad" } });
    fireEvent.click(screen.getByRole("button", { name: "Save token" }));

    expect((await screen.findByRole("alert")).textContent).toBe("Failed to save token.");
  });

  it("shows a network error when the request fails", async () => {
    renderSidebar({ devTokenEnabled: true });

    fireEvent.change(devTokenInput(), { target: { value: "abc" } });
    fireEvent.click(screen.getByRole("button", { name: "Save token" }));

    expect((await screen.findByRole("alert")).textContent).toBe("Network error.");
  });

  it("shows a progress indicator in place of the save action while saving", () => {
    vi.mocked(globalThis.fetch).mockReturnValue(new Promise<Response>(() => undefined));
    renderSidebar({ devTokenEnabled: true });

    fireEvent.change(devTokenInput(), { target: { value: "abc" } });
    fireEvent.click(screen.getByRole("button", { name: "Save token" }));

    expect(screen.queryByRole("button", { name: "Save token" })).toBeNull();
    expect(screen.getByRole("progressbar")).not.toBeNull();
  });
});

describe("Sidebar sign out", () => {
  it("signs out, confirms with a toast, and returns home", async () => {
    const fetchMock = vi.mocked(globalThis.fetch).mockResolvedValue(new Response(null, { status: 200 }));
    renderSidebar({ userInfo: user });

    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));

    expect(fetchMock).toHaveBeenCalledWith("/auth/logout");
    expect(await screen.findByText("Successfully signed out")).not.toBeNull();
    await waitFor(() => expect(router.push).toHaveBeenCalledWith("/"), { timeout: 2000 });
    expect(router.refresh).toHaveBeenCalled();
  });

  it("still clears the session client-side when the logout request fails", async () => {
    renderSidebar({ userInfo: user });

    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));

    expect(await screen.findByText("Successfully signed out")).not.toBeNull();
    await waitFor(() => expect(router.push).toHaveBeenCalledWith("/"), { timeout: 2000 });
  });

  it("dismisses the sign-out toast from its close action", async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(new Response(null, { status: 200 }));
    renderSidebar({ userInfo: user });

    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(await screen.findByText("Successfully signed out")).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    await waitFor(() => expect(screen.queryByText("Successfully signed out")).toBeNull());
  });

  it("dismisses the sign-out toast when clicking away from it", async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(new Response(null, { status: 200 }));
    renderSidebar({ userInfo: user });

    fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(await screen.findByText("Successfully signed out")).not.toBeNull();

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    fireEvent.click(document.body);

    await waitFor(() => expect(screen.queryByText("Successfully signed out")).toBeNull());
  });
});

describe("Sidebar collapse preference", () => {
  it("collapses to the icon rail and persists the preference", () => {
    renderSidebar({ userInfo: user });

    fireEvent.click(screen.getByRole("button", { name: "Collapse sidebar" }));

    expect(screen.getByRole("button", { name: "Expand sidebar" })).not.toBeNull();
    expect(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY)).toBe("true");
    expect(document.cookie).toContain(`${SIDEBAR_COLLAPSED_COOKIE}=true`);
  });

  it("expands from the icon rail and persists the preference", () => {
    renderSidebar({ initialCollapsed: true });

    fireEvent.click(screen.getByRole("button", { name: "Expand sidebar" }));

    expect(screen.getByRole("button", { name: "Collapse sidebar" })).not.toBeNull();
    expect(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY)).toBe("false");
    expect(document.cookie).toContain(`${SIDEBAR_COLLAPSED_COOKIE}=false`);
  });

  it("restores a saved collapsed preference after mount", () => {
    localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, "true");
    renderSidebar({ initialCollapsed: false });

    expect(screen.getByRole("button", { name: "Expand sidebar" })).not.toBeNull();
  });

  it("restores a saved expanded preference over the initial collapsed state", () => {
    localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, "false");
    renderSidebar({ initialCollapsed: true });

    expect(screen.getByRole("button", { name: "Collapse sidebar" })).not.toBeNull();
  });

  it("ignores an unrecognised saved preference", () => {
    localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, "maybe");
    renderSidebar({ initialCollapsed: true });

    expect(screen.getByRole("button", { name: "Expand sidebar" })).not.toBeNull();
  });

  it("expands the rail and focuses the search field from the rail search button", async () => {
    renderSidebar({ initialCollapsed: true });

    fireEvent.click(screen.getByRole("button", { name: "Search resources" }));

    expect(screen.getByRole("button", { name: "Collapse sidebar" })).not.toBeNull();
    await waitFor(() => {
      expect(document.activeElement).toBe(desktopSearchInput());
    });
  });

  it("closes the mobile drawer without persisting when collapse is pressed on mobile", () => {
    mobile = true;
    renderSidebar({ userInfo: user });

    openMobileDrawer();
    fireEvent.click(screen.getByRole("button", { name: "Collapse sidebar" }));

    expect(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY)).toBeNull();
    expect(document.cookie).not.toContain(SIDEBAR_COLLAPSED_COOKIE);
  });
});

describe("Sidebar mobile drawer", () => {
  it("closes the mobile drawer from its backdrop", async () => {
    mobile = true;
    renderSidebar();

    openMobileDrawer();
    const backdrop = document.querySelector(".MuiDrawer-root .MuiBackdrop-root");
    if (!backdrop) {
      throw new Error("Mobile drawer backdrop not found");
    }
    fireEvent.click(backdrop);

    await waitFor(() => expect(getComputedStyle(mobileDrawerRoot()).visibility).toBe("hidden"));
  });
});

describe("Sidebar color mode", () => {
  it("switches to light mode from dark mode", () => {
    renderSidebar();

    fireEvent.click(screen.getByRole("button", { name: "Toggle color mode" }));

    expect(colorScheme.setMode).toHaveBeenCalledWith("light");
  });

  it("switches to dark mode from light mode", () => {
    colorScheme.mode = "light";
    renderSidebar();

    fireEvent.click(screen.getByRole("button", { name: "Toggle color mode" }));

    expect(colorScheme.setMode).toHaveBeenCalledWith("dark");
  });
});

describe("Sidebar needs review drawer", () => {
  it("closes the needs review drawer from its close action", async () => {
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse({ items: [] }));
    renderSidebar({ securityPoliciesEnabled: true });

    fireEvent.click(screen.getByRole("button", { name: "Needs review" }));
    expect(await screen.findByText("Nothing needs review right now.")).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Close needs review" }));

    await waitFor(() => expect(screen.queryByText("Nothing needs review right now.")).toBeNull());
  });

  it("closes the mobile drawer when needs review is opened on mobile", async () => {
    mobile = true;
    vi.mocked(globalThis.fetch).mockResolvedValue(jsonResponse({ items: [] }));
    renderSidebar({ securityPoliciesEnabled: true });

    openMobileDrawer();
    fireEvent.click(screen.getByRole("button", { name: "Needs review" }));

    await waitFor(() => expect(getComputedStyle(mobileDrawerRoot()).visibility).toBe("hidden"));
    expect(await screen.findByText("Nothing needs review right now.")).not.toBeNull();
  });
});

describe("Sidebar login page", () => {
  it("renders nothing on the login page", () => {
    pathname = "/login";
    const { container } = renderSidebar();

    expect(container.innerHTML).toBe("");
  });
});
