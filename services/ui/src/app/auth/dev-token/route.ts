import { type NextRequest, NextResponse } from "next/server";
import { getIronSession } from "iron-session";
import { cookies } from "next/headers";
import { sessionOptions, type SessionData } from "@/lib/session";

/**
 * POST /auth/dev-token
 * Stores a developer-supplied bearer token in the iron-session.
 * Only available when DEV_TOKEN_INPUT_ENABLED=true (local dev only).
 * Body: { token: string }
 */
export async function POST(request: NextRequest): Promise<NextResponse> {
  if (process.env.DEV_TOKEN_INPUT_ENABLED !== "true") {
    return NextResponse.json({ error: "Not available" }, { status: 403 });
  }

  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "Invalid JSON body" }, { status: 400 });
  }

  if (
    typeof body !== "object" ||
    body === null ||
    !("token" in body) ||
    typeof (body as Record<string, unknown>)["token"] !== "string"
  ) {
    return NextResponse.json({ error: "Missing or invalid 'token' field" }, { status: 400 });
  }

  const token = (body as { token: string }).token.trim();
  if (!token) {
    return NextResponse.json({ error: "'token' must not be empty" }, { status: 400 });
  }

  const cookieStore = await cookies();
  const session = await getIronSession<SessionData>(cookieStore, sessionOptions);
  session.devToken = token;
  await session.save();

  return NextResponse.json({ ok: true });
}

/**
 * DELETE /auth/dev-token
 * Clears the dev token from the session.
 */
export async function DELETE(): Promise<NextResponse> {
  if (process.env.DEV_TOKEN_INPUT_ENABLED !== "true") {
    return NextResponse.json({ error: "Not available" }, { status: 403 });
  }

  const cookieStore = await cookies();
  const session = await getIronSession<SessionData>(cookieStore, sessionOptions);
  session.devToken = undefined;
  await session.save();

  return NextResponse.json({ ok: true });
}
