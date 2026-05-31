import { type NextRequest, NextResponse } from "next/server";
import { getServerSessionToken } from "@/lib/session";

const serverHost = process.env.OPENDEPOT_SERVER_HOST ?? "localhost:80";
const BASE_URL = (
  process.env.OPENDEPOT_SERVER_URL ?? `http://${serverHost}`
).replace(/\/$/, "");

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ namespace: string; kind: string; name: string }> },
) {
  const { namespace, kind, name } = await params;
  const token = await getServerSessionToken();

  const upstreamUrl = new URL(
    `${BASE_URL}/opendepot/ui/v1/resources/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}/versions`,
  );

  for (const [key, value] of request.nextUrl.searchParams.entries()) {
    upstreamUrl.searchParams.set(key, value);
  }

  const headers: Record<string, string> = { Accept: "application/json" };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(upstreamUrl.toString(), { headers, cache: "no-store" });
  const data: unknown = await res.json();
  return NextResponse.json(data, { status: res.status });
}
