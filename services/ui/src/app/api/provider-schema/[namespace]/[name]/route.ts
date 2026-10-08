import { type NextRequest, NextResponse } from "next/server";
import { getServerSessionToken } from "@/lib/session";

const serverHost = process.env.OPENDEPOT_SERVER_HOST ?? "localhost:80";
const BASE_URL = (process.env.OPENDEPOT_SERVER_URL ?? `http://${serverHost}`).replace(/\/$/, "");

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ namespace: string; name: string }> },
) {
  const { namespace, name } = await params;
  const token = await getServerSessionToken();
  const upstreamUrl = new URL(
    `${BASE_URL}/opendepot/ui/v1/resources/${encodeURIComponent(namespace)}/provider/${encodeURIComponent(name)}/provider-schema`,
  );

  for (const [key, value] of request.nextUrl.searchParams.entries()) {
    upstreamUrl.searchParams.set(key, value);
  }

  const headers: Record<string, string> = { Accept: "application/json" };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(upstreamUrl, { headers, cache: "no-store" });
  const data: unknown = await response.json();

  return NextResponse.json(data, { status: response.status, headers: { "Cache-Control": "no-store" } });
}
