import { type NextRequest, NextResponse } from "next/server";
import { getServerSessionToken } from "@/lib/session";

const serverHost = process.env.OPENDEPOT_SERVER_HOST ?? "localhost:80";
const BASE_URL = (process.env.OPENDEPOT_SERVER_URL ?? `http://${serverHost}`).replace(/\/$/, "");

export async function POST(request: NextRequest) {
  const token = await getServerSessionToken();
  const headers: Record<string, string> = {
    Accept: request.headers.get("Accept") ?? "application/zip, application/json",
    "Content-Type": "application/json",
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(`${BASE_URL}/opendepot/ui/v1/assembly/export`, {
    method: "POST",
    headers,
    body: await request.text(),
    cache: "no-store",
  });
  const responseHeaders = new Headers({ "Cache-Control": "no-store" });
  responseHeaders.set("Content-Type", response.headers.get("Content-Type") ?? "application/json");

  const disposition = response.headers.get("Content-Disposition");
  if (disposition) {
    responseHeaders.set("Content-Disposition", disposition);
  }

  const buffering = response.headers.get("X-Accel-Buffering");
  if (buffering) {
    responseHeaders.set("X-Accel-Buffering", buffering);
  }

  if (!response.ok) {
    const body = await response.text();
    let data: unknown;

    try {
      data = JSON.parse(body);
    } catch {
      data = {
        error: "upstream_error",
        message: body.trim() || `Assembly export failed with status ${response.status}.`,
      };
    }
    responseHeaders.set("Content-Type", "application/json");

    return NextResponse.json(data, { status: response.status, headers: responseHeaders });
  }

  if (response.headers.get("Content-Type")?.includes("application/x-ndjson")) {
    return new Response(response.body, { status: response.status, headers: responseHeaders });
  }

  return new Response(await response.arrayBuffer(), { status: response.status, headers: responseHeaders });
}
