import { type NextRequest, NextResponse } from "next/server";
import { getServerSessionToken } from "@/lib/session";

const serverHost = process.env.OPENDEPOT_SERVER_HOST ?? "localhost:80";
const BASE_URL = (
  process.env.OPENDEPOT_SERVER_URL ?? `http://${serverHost}`
).replace(/\/$/, "");

export async function GET(
  _request: NextRequest,
  { params }: { params: Promise<{ namespace: string; kind: string; name: string }> },
) {
  const { namespace, kind, name } = await params;
  const token = await getServerSessionToken();

  const version = _request.nextUrl.searchParams.get("version");
  const upstreamBase = `${BASE_URL}/opendepot/ui/v1/resources/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}/scan-findings`;
  const upstreamUrl = version ? `${upstreamBase}?version=${encodeURIComponent(version)}` : upstreamBase;

  const headers: Record<string, string> = { Accept: "application/json" };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(upstreamUrl, { headers, cache: "no-store" });
  let data: unknown;
  try {
    data = await res.json();
  } catch {
    return NextResponse.json({ error: "upstream error" }, { status: res.status });
  }
  return NextResponse.json(data, { status: res.status });
}
