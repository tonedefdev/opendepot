import type { SessionOptions } from "iron-session";
import { getIronSession } from "iron-session";
import { cookies } from "next/headers";

export interface SessionData {
  idToken?: string;
  accessToken?: string;
  // ISO-8601 expiry timestamp from the token set.
  expiresAt?: string;
  // Developer-only override token; only populated when DEV_TOKEN_INPUT_ENABLED=true.
  devToken?: string;
}

export const sessionOptions: SessionOptions = {
  password: process.env.SESSION_PASSWORD as string,
  cookieName: "opendepot_session",
  cookieOptions: {
    // Require HTTPS only when the configured base URL uses HTTPS.
    // Using NODE_ENV would set secure=true even on HTTP port-forwards.
    secure: (process.env.NEXT_PUBLIC_BASE_URL ?? "").startsWith("https://"),
    httpOnly: true,
    sameSite: "lax",
  },
};

/**
 * Server-only helper. Reads the iron-session from the incoming request cookies
 * and returns the effective bearer token for upstream API calls.
 *
 * Priority: devToken (local dev override) > idToken (OIDC session) > undefined.
 * Must only be called from Server Components or Route Handlers.
 */
export async function getServerSessionToken(): Promise<string | undefined> {
  const cookieStore = await cookies();
  const session = await getIronSession<SessionData>(cookieStore, sessionOptions);
  return session.devToken ?? session.idToken ?? undefined;
}

/**
 * Decodes the payload of a JWT without verifying the signature and returns
 * the parsed claims object, or null on any error.
 * Used server-side only to extract display claims (name, email) from the id_token.
 */
export function parseJWTClaims(token: string): Record<string, unknown> | null {
  const parts = token.split(".");
  if (parts.length !== 3) return null;
  try {
    const paddedPayload = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    return JSON.parse(Buffer.from(paddedPayload, "base64").toString("utf-8")) as Record<string, unknown>;
  } catch {
    return null;
  }
}
