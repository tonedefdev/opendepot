import { type NextRequest, NextResponse } from "next/server";
import { getIronSession } from "iron-session";
import type { SessionData } from "@/lib/session";
import { sessionOptions } from "@/lib/session";

/**
 * parseJWTNonce decodes the payload segment of a JWT and returns the `nonce`
 * claim, or null if the token is malformed or the claim is absent.
 * The signature is NOT verified here — that is handled by the server's JWKS
 * verification. This is solely to read the plaintext nonce claim for CSRF
 * replay protection at the callback boundary.
 */
function parseJWTNonce(token: string): string | null {
  const parts = token.split(".");
  if (parts.length !== 3) return null;
  try {
    const paddedPayload = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const decoded = JSON.parse(
      Buffer.from(paddedPayload, "base64").toString("utf-8"),
    ) as { nonce?: string };
    return decoded.nonce ?? null;
  } catch {
    return null;
  }
}

export async function GET(req: NextRequest): Promise<NextResponse> {
  const issuer = process.env.OIDC_ISSUER_URL;
  const clientId = process.env.OIDC_CLIENT_ID;
  const clientSecret = process.env.OIDC_CLIENT_SECRET;
  const baseUrl = process.env.NEXT_PUBLIC_BASE_URL;
  const callbackPath = process.env.OIDC_CALLBACK_PATH ?? "/auth/callback";

  if (!issuer || !clientId || !baseUrl) {
    return new NextResponse("OIDC is not configured", { status: 503 });
  }

  const searchParams = req.nextUrl.searchParams;
  const code = searchParams.get("code");
  const state = searchParams.get("state");
  const errorParam = searchParams.get("error");

  if (errorParam) {
    const desc = searchParams.get("error_description") ?? errorParam;
    return NextResponse.redirect(new URL(`/?auth_error=${encodeURIComponent(desc)}`, baseUrl));
  }

  if (!code || !state) {
    return new NextResponse("Missing code or state", { status: 400 });
  }

  const storedState = req.cookies.get("oidc_state")?.value;
  const storedNonce = req.cookies.get("oidc_nonce")?.value;
  const codeVerifier = req.cookies.get("oidc_cv")?.value;

  if (!storedState || state !== storedState) {
    return new NextResponse("State mismatch — possible CSRF", { status: 400 });
  }
  if (!storedNonce || !codeVerifier) {
    return new NextResponse("Missing nonce or PKCE verifier", { status: 400 });
  }

  // Fetch OIDC discovery document.
  const discoveryRes = await fetch(`${issuer}/.well-known/openid-configuration`);
  if (!discoveryRes.ok) {
    return new NextResponse("Failed to fetch OIDC discovery document", { status: 502 });
  }
  const discovery = (await discoveryRes.json()) as { token_endpoint: string };

  // Exchange authorization code for token set.
  const tokenBody = new URLSearchParams({
    grant_type: "authorization_code",
    code,
    redirect_uri: `${baseUrl}${callbackPath}`,
    client_id: clientId,
    code_verifier: codeVerifier,
    ...(clientSecret ? { client_secret: clientSecret } : {}),
  });

  const tokenRes = await fetch(discovery.token_endpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: tokenBody.toString(),
  });

  if (!tokenRes.ok) {
    const body = await tokenRes.text();
    console.error("token exchange failed:", tokenRes.status, body);
    return new NextResponse("Token exchange failed", { status: 502 });
  }

  const tokenSet = (await tokenRes.json()) as {
    id_token?: string;
    access_token?: string;
    expires_in?: number;
  };

  if (!tokenSet.id_token) {
    return new NextResponse("No id_token in response", { status: 502 });
  }

  // Validate the nonce claim inside the id_token to prevent token replay attacks.
  // The id_token is a signed JWT: header.payload.signature — we only need the payload.
  const idTokenNonce = parseJWTNonce(tokenSet.id_token);
  if (!idTokenNonce || idTokenNonce !== storedNonce) {
    return new NextResponse("Nonce mismatch — possible token replay", { status: 400 });
  }

  // Store the token set in the session cookie.
  const response = NextResponse.redirect(new URL("/", baseUrl));
  const session = await getIronSession<SessionData>(req, response, sessionOptions);
  session.idToken = tokenSet.id_token;
  session.accessToken = tokenSet.access_token;
  if (tokenSet.expires_in) {
    session.expiresAt = new Date(Date.now() + tokenSet.expires_in * 1000).toISOString();
  }
  await session.save();

  // Clear PKCE/state cookies via headers.append to avoid ResponseCookies
  // overwriting the iron-session cookie that was set via headers.append above.
  for (const name of ["oidc_state", "oidc_nonce", "oidc_cv"]) {
    response.headers.append(
      "set-cookie",
      `${name}=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax`,
    );
  }

  return response;
}
