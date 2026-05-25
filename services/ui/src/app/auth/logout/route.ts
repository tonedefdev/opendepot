import { type NextRequest, NextResponse } from "next/server";
import { getIronSession } from "iron-session";
import type { SessionData } from "@/lib/session";
import { sessionOptions } from "@/lib/session";

export async function GET(req: NextRequest): Promise<NextResponse> {
  const response = NextResponse.redirect(new URL("/", req.url));
  const session = await getIronSession<SessionData>(req, response, sessionOptions);
  session.destroy();
  return response;
}
