import { type NextRequest, NextResponse } from "next/server";
import crypto from "crypto";

// PKCE + state + nonce authorization redirect.
export async function GET(_req: NextRequest): Promise<NextResponse> {
  const issuer = process.env.OIDC_ISSUER_URL;
  const clientId = process.env.OIDC_CLIENT_ID;
  const baseUrl = process.env.NEXT_PUBLIC_BASE_URL;
  const callbackPath = process.env.OIDC_CALLBACK_PATH ?? "/auth/callback";
  const scopes = process.env.OIDC_SCOPES ?? "openid profile email groups";

  if (!issuer || !clientId || !baseUrl) {
    return new NextResponse("OIDC is not configured", { status: 503 });
  }

  // Fetch OIDC discovery document to get the authorization endpoint.
  const discoveryRes = await fetch(
    `${issuer}/.well-known/openid-configuration`,
  );
  if (!discoveryRes.ok) {
    return new NextResponse("Failed to fetch OIDC discovery document", {
      status: 502,
    });
  }
  const discovery = (await discoveryRes.json()) as {
    authorization_endpoint: string;
  };

  // Generate PKCE code verifier + challenge.
  const codeVerifier = crypto.randomBytes(32).toString("base64url");
  const codeChallenge = crypto
    .createHash("sha256")
    .update(codeVerifier)
    .digest("base64url");

  const state = crypto.randomBytes(16).toString("hex");
  const nonce = crypto.randomBytes(16).toString("hex");

  const redirectUri = `${baseUrl}${callbackPath}`;
  // OIDC_AUTHZ_URL overrides the authorization endpoint from discovery.
  // Use this in local Kind environments where the issuerUrl is an in-cluster
  // address that is unreachable from browsers (e.g. port-forward dev setups).
  const authzUrlOverride = process.env.OIDC_AUTHZ_URL;
  const authUrl = new URL(authzUrlOverride ?? discovery.authorization_endpoint);
  authUrl.searchParams.set("client_id", clientId);
  authUrl.searchParams.set("redirect_uri", redirectUri);
  authUrl.searchParams.set("response_type", "code");
  authUrl.searchParams.set("scope", scopes);
  authUrl.searchParams.set("state", state);
  authUrl.searchParams.set("nonce", nonce);
  authUrl.searchParams.set("code_challenge", codeChallenge);
  authUrl.searchParams.set("code_challenge_method", "S256");

  // Store state, nonce, and verifier in short-lived SameSite=Lax cookies.
  const response = NextResponse.redirect(authUrl.toString());
  const cookieOpts = {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax" as const,
    maxAge: 300,
    path: "/",
  };
  response.cookies.set("oidc_state", state, cookieOpts);
  response.cookies.set("oidc_nonce", nonce, cookieOpts);
  response.cookies.set("oidc_cv", codeVerifier, cookieOpts);

  return response;
}
