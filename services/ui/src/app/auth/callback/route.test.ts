import { NextRequest } from "next/server";
import { getIronSession } from "iron-session";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { verifyIDToken } from "@/lib/oidc";
import { GET } from "./route";

vi.mock("iron-session", () => ({
  getIronSession: vi.fn(),
}));

vi.mock("@/lib/oidc", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/oidc")>();
  return {
    ...actual,
    verifyIDToken: vi.fn(),
  };
});

const ENV_KEYS = [
  "OIDC_ISSUER_URL",
  "OIDC_CLIENT_ID",
  "OIDC_CLIENT_SECRET",
  "NEXT_PUBLIC_BASE_URL",
  "OIDC_CALLBACK_PATH",
  "OIDC_ALLOW_INSECURE_HTTP",
];

const issuer = "https://issuer.example.com";
const baseUrl = "https://registry.example.com";
const discovery = {
  issuer,
  token_endpoint: `${issuer}/token`,
  jwks_uri: `${issuer}/keys`,
};

type SessionStub = {
  idToken?: string;
  accessToken?: string;
  expiresAt?: string;
  save: ReturnType<typeof vi.fn>;
};

let session: SessionStub;

function configureEnv(overrides: Record<string, string> = {}) {
  const env: Record<string, string> = {
    OIDC_ISSUER_URL: issuer,
    OIDC_CLIENT_ID: "opendepot-ui",
    NEXT_PUBLIC_BASE_URL: baseUrl,
    ...overrides,
  };
  for (const [key, value] of Object.entries(env)) {
    vi.stubEnv(key, value);
  }
}

function callbackRequest(query: string, cookies: Record<string, string> = {}) {
  const cookieHeader = Object.entries(cookies)
    .map(([name, value]) => `${name}=${value}`)
    .join("; ");
  return new NextRequest(`${baseUrl}/auth/callback${query}`, {
    headers: cookieHeader ? { cookie: cookieHeader } : undefined,
  });
}

const validCookies = {
  oidc_state: "state-123",
  oidc_nonce: "nonce-456",
  oidc_cv: "verifier-789",
};

function queueFetch(...responses: Array<Response | Error>) {
  const fetchMock = vi.fn();
  for (const response of responses) {
    if (response instanceof Error) {
      fetchMock.mockRejectedValueOnce(response);
    } else {
      fetchMock.mockResolvedValueOnce(response);
    }
  }
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function jsonResponse(body: object, status = 200) {
  return new Response(JSON.stringify(body), { status });
}

beforeEach(() => {
  for (const key of ENV_KEYS) {
    delete process.env[key];
  }
  session = { save: vi.fn().mockResolvedValue(undefined) };
  vi.mocked(getIronSession).mockResolvedValue(session as never);
  vi.mocked(verifyIDToken).mockResolvedValue({ sub: "user-1" });
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("OIDC callback configuration and parameter checks", () => {
  it("returns 503 when OIDC is not configured", async () => {
    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(503);
    expect(await response.text()).toBe("OIDC is not configured");
  });

  it("returns 503 when the base URL is invalid", async () => {
    configureEnv({ NEXT_PUBLIC_BASE_URL: "http://registry.example.com" });

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(503);
    expect(await response.text()).toBe("Invalid OIDC base URL");
  });

  it("returns 503 when the issuer URL is invalid", async () => {
    configureEnv({ OIDC_ISSUER_URL: "::bad::" });

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(503);
    expect(await response.text()).toBe("Invalid OIDC issuer URL");
  });

  it("redirects back to the login page with the provider error description", async () => {
    configureEnv();

    const response = await GET(
      callbackRequest("?error=access_denied&error_description=User%20cancelled", validCookies),
    );

    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe(`${baseUrl}/?auth_error=User%20cancelled`);
  });

  it("falls back to the error code when no description is provided", async () => {
    configureEnv();

    const response = await GET(callbackRequest("?error=login_required", validCookies));

    expect(response.headers.get("location")).toBe(`${baseUrl}/?auth_error=login_required`);
  });

  it("returns 400 when code or state is missing", async () => {
    configureEnv();

    const response = await GET(callbackRequest("?code=abc", validCookies));

    expect(response.status).toBe(400);
    expect(await response.text()).toBe("Missing code or state");
  });

  it("returns 400 when the state does not match the stored cookie", async () => {
    configureEnv();

    const response = await GET(callbackRequest("?code=abc&state=forged", validCookies));

    expect(response.status).toBe(400);
    expect(await response.text()).toBe("State mismatch — possible CSRF");
  });

  it("returns 400 when the nonce or PKCE verifier cookie is missing", async () => {
    configureEnv();

    const response = await GET(
      callbackRequest("?code=abc&state=state-123", { oidc_state: "state-123", oidc_nonce: "nonce-456" }),
    );

    expect(response.status).toBe(400);
    expect(await response.text()).toBe("Missing nonce or PKCE verifier");
  });
});

describe("OIDC callback discovery and token exchange", () => {
  it("returns 502 when the provider is unreachable", async () => {
    configureEnv();
    queueFetch(new Error("network down"));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("OIDC provider is unreachable");
  });

  it("returns 502 when the discovery document request fails", async () => {
    configureEnv();
    queueFetch(new Response("error", { status: 500 }));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Failed to fetch OIDC discovery document");
  });

  it("returns 502 when the discovery issuer does not match the configured issuer", async () => {
    configureEnv();
    queueFetch(jsonResponse({ ...discovery, issuer: "https://other.example.com" }));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Invalid OIDC discovery document");
  });

  it("returns 502 when the discovery document is missing the JWKS URI", async () => {
    configureEnv();
    queueFetch(jsonResponse({ ...discovery, jwks_uri: "" }));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Invalid OIDC discovery document");
  });

  it("returns 502 when a discovered endpoint leaves the issuer origin", async () => {
    configureEnv();
    queueFetch(jsonResponse({ ...discovery, token_endpoint: "https://evil.example.com/token" }));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Invalid OIDC discovery endpoints");
  });

  it("returns 502 when the token exchange cannot be reached", async () => {
    configureEnv();
    queueFetch(jsonResponse(discovery), new Error("token endpoint down"));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Token exchange failed");
  });

  it("returns 502 when the token endpoint rejects the code", async () => {
    configureEnv();
    queueFetch(jsonResponse(discovery), new Response("invalid_grant", { status: 400 }));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("Token exchange failed");
  });

  it("sends the authorization code with PKCE verifier and client secret", async () => {
    configureEnv({ OIDC_CLIENT_SECRET: "shh", NEXT_PUBLIC_BASE_URL: baseUrl });
    const fetchMock = queueFetch(
      jsonResponse(discovery),
      jsonResponse({ id_token: "header.payload.signature" }),
    );

    await GET(callbackRequest("?code=auth-code&state=state-123", validCookies));

    const [tokenUrl, tokenInit] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(tokenUrl).toBe(`${issuer}/token`);
    expect(tokenInit.method).toBe("POST");
    const body = new URLSearchParams(String(tokenInit.body));
    expect(body.get("grant_type")).toBe("authorization_code");
    expect(body.get("code")).toBe("auth-code");
    expect(body.get("redirect_uri")).toBe(`${baseUrl}/auth/callback`);
    expect(body.get("client_id")).toBe("opendepot-ui");
    expect(body.get("code_verifier")).toBe("verifier-789");
    expect(body.get("client_secret")).toBe("shh");
  });

  it("returns 502 when the token response has no id_token", async () => {
    configureEnv();
    queueFetch(jsonResponse(discovery), jsonResponse({ access_token: "only-access" }));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(502);
    expect(await response.text()).toBe("No id_token in response");
  });

  it("returns 400 when the ID token fails verification", async () => {
    configureEnv();
    queueFetch(jsonResponse(discovery), jsonResponse({ id_token: "header.payload.signature" }));
    vi.mocked(verifyIDToken).mockRejectedValue(new Error("bad signature"));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(400);
    expect(await response.text()).toBe("Invalid ID token");
  });
});

describe("OIDC callback success", () => {
  it("stores the token set in the session, clears PKCE cookies, and redirects home", async () => {
    configureEnv();
    queueFetch(
      jsonResponse(discovery),
      jsonResponse({ id_token: "header.payload.signature", access_token: "access-1", expires_in: 3600 }),
    );
    const now = new Date("2026-01-01T00:00:00.000Z");
    vi.useFakeTimers();
    vi.setSystemTime(now);

    try {
      const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

      expect(verifyIDToken).toHaveBeenCalledWith("header.payload.signature", {
        issuer,
        clientId: "opendepot-ui",
        nonce: "nonce-456",
        jwksUri: `${issuer}/keys`,
      });
      expect(session.idToken).toBe("header.payload.signature");
      expect(session.accessToken).toBe("access-1");
      expect(session.expiresAt).toBe("2026-01-01T01:00:00.000Z");
      expect(session.save).toHaveBeenCalledTimes(1);

      expect(response.status).toBe(307);
      expect(response.headers.get("location")).toBe(`${baseUrl}/`);
      const setCookies = response.headers.getSetCookie();
      expect(setCookies).toEqual(
        expect.arrayContaining([
          "oidc_state=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax",
          "oidc_nonce=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax",
          "oidc_cv=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax",
        ]),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("omits expiresAt when the provider does not return expires_in", async () => {
    configureEnv();
    queueFetch(jsonResponse(discovery), jsonResponse({ id_token: "header.payload.signature" }));

    const response = await GET(callbackRequest("?code=abc&state=state-123", validCookies));

    expect(response.status).toBe(307);
    expect(session.idToken).toBe("header.payload.signature");
    expect(session.expiresAt).toBeUndefined();
  });
});
