import crypto from "crypto";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GET } from "./route";

const ENV_KEYS = [
  "OIDC_ISSUER_URL",
  "OIDC_CLIENT_ID",
  "NEXT_PUBLIC_BASE_URL",
  "OIDC_CALLBACK_PATH",
  "OIDC_SCOPES",
  "OIDC_ALLOW_INSECURE_HTTP",
  "OIDC_AUTHZ_URL",
];

const issuer = "https://issuer.example.com";
const discoveryDocument = {
  issuer,
  authorization_endpoint: `${issuer}/authorize`,
};

function configureEnv(overrides: Record<string, string> = {}) {
  const env: Record<string, string> = {
    OIDC_ISSUER_URL: issuer,
    OIDC_CLIENT_ID: "opendepot-ui",
    NEXT_PUBLIC_BASE_URL: "https://registry.example.com",
    ...overrides,
  };
  for (const [key, value] of Object.entries(env)) {
    vi.stubEnv(key, value);
  }
}

function discoveryResponse(document: object = discoveryDocument, status = 200) {
  return new Response(JSON.stringify(document), { status });
}

beforeEach(() => {
  for (const key of ENV_KEYS) {
    delete process.env[key];
  }
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("OIDC login route configuration", () => {
  it("returns 503 when required OIDC settings are missing", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await GET();

    expect(response.status).toBe(503);
    expect(await response.text()).toBe("OIDC is not configured");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("returns 503 when the base URL is not HTTPS and insecure HTTP is disabled", async () => {
    configureEnv({ NEXT_PUBLIC_BASE_URL: "http://registry.example.com" });
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await GET();

    expect(response.status).toBe(503);
    expect(await response.text()).toBe("Invalid OIDC base URL");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("returns 503 when the issuer URL is invalid", async () => {
    configureEnv({ OIDC_ISSUER_URL: "not a url" });
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await GET();

    expect(response.status).toBe(503);
    expect(await response.text()).toBe("Invalid OIDC issuer URL");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("returns 503 when the discovery document cannot be reached", async () => {
    configureEnv();
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("connection refused")));

    const response = await GET();

    expect(response.status).toBe(503);
    expect(await response.text()).toBe("OIDC provider is unreachable");
  });

  it("returns 502 when the discovery document request fails", async () => {
    configureEnv();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("boom", { status: 500 })));

    const response = await GET();

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Failed to fetch OIDC discovery document");
  });

  it("returns 502 when the discovery authorization endpoint is outside the issuer origin", async () => {
    configureEnv();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(discoveryResponse({ authorization_endpoint: "https://evil.example.com/authorize" })),
    );

    const response = await GET();

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Invalid OIDC discovery endpoint");
  });

  it("returns 502 when the authorization endpoint override is invalid", async () => {
    configureEnv({ OIDC_AUTHZ_URL: "ftp://issuer.example.com/authorize" });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(discoveryResponse()));

    const response = await GET();

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Invalid OIDC authorization endpoint");
  });
});

describe("OIDC login redirect", () => {
  it("redirects to the authorization endpoint with PKCE, state, and nonce", async () => {
    configureEnv();
    const fetchMock = vi.fn().mockResolvedValue(discoveryResponse());
    vi.stubGlobal("fetch", fetchMock);

    const response = await GET();

    expect(fetchMock).toHaveBeenCalledWith(
      `${issuer}/.well-known/openid-configuration`,
      expect.objectContaining({ redirect: "error" }),
    );
    expect(response.status).toBe(307);

    const location = new URL(response.headers.get("location") ?? "");
    expect(`${location.origin}${location.pathname}`).toBe(`${issuer}/authorize`);
    expect(location.searchParams.get("client_id")).toBe("opendepot-ui");
    expect(location.searchParams.get("redirect_uri")).toBe("https://registry.example.com/auth/callback");
    expect(location.searchParams.get("response_type")).toBe("code");
    expect(location.searchParams.get("scope")).toBe("openid profile email groups");
    expect(location.searchParams.get("code_challenge_method")).toBe("S256");

    const state = location.searchParams.get("state");
    const nonce = location.searchParams.get("nonce");
    const codeChallenge = location.searchParams.get("code_challenge");
    expect(state).toMatch(/^[0-9a-f]{32}$/);
    expect(nonce).toMatch(/^[0-9a-f]{32}$/);

    const cookieState = response.cookies.get("oidc_state");
    const cookieNonce = response.cookies.get("oidc_nonce");
    const cookieVerifier = response.cookies.get("oidc_cv");
    expect(cookieState?.value).toBe(state);
    expect(cookieNonce?.value).toBe(nonce);
    expect(cookieVerifier?.value).toBeTruthy();

    const expectedChallenge = crypto
      .createHash("sha256")
      .update(cookieVerifier?.value ?? "")
      .digest("base64url");
    expect(codeChallenge).toBe(expectedChallenge);

    for (const cookie of [cookieState, cookieNonce, cookieVerifier]) {
      expect(cookie).toMatchObject({ httpOnly: true, sameSite: "lax", maxAge: 300, path: "/", secure: true });
    }
  });

  it("uses configured callback path and scopes", async () => {
    configureEnv({ OIDC_CALLBACK_PATH: "/sso/callback", OIDC_SCOPES: "openid email" });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(discoveryResponse()));

    const response = await GET();

    const location = new URL(response.headers.get("location") ?? "");
    expect(location.searchParams.get("redirect_uri")).toBe("https://registry.example.com/sso/callback");
    expect(location.searchParams.get("scope")).toBe("openid email");
  });

  it("marks PKCE cookies as insecure when the base URL is HTTP with insecure mode enabled", async () => {
    configureEnv({
      NEXT_PUBLIC_BASE_URL: "http://localhost:3000",
      OIDC_ISSUER_URL: "http://localhost:8080",
      OIDC_ALLOW_INSECURE_HTTP: "true",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        discoveryResponse({ authorization_endpoint: "http://localhost:8080/authorize" }),
      ),
    );

    const response = await GET();

    expect(response.status).toBe(307);
    expect(response.cookies.get("oidc_state")).toMatchObject({ secure: false });
  });

  it("uses OIDC_AUTHZ_URL as a cross-origin authorization endpoint when set", async () => {
    configureEnv({ OIDC_AUTHZ_URL: "https://browser.example.net/authorize" });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(discoveryResponse()));

    const response = await GET();

    expect(response.status).toBe(307);
    const location = new URL(response.headers.get("location") ?? "");
    expect(`${location.origin}${location.pathname}`).toBe("https://browser.example.net/authorize");
  });
});
