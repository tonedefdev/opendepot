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

  const upstreamUrl = `${BASE_URL}/opendepot/ui/v1/resources/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}/scan-findings`;

  const headers: Record<string, string> = { Accept: "application/json" };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(upstreamUrl, { headers, cache: "no-store" });
  const data: unknown = await res.json();
  return NextResponse.json(data, { status: res.status });
}
