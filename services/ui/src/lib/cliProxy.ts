import { NextResponse } from "next/server";

const serverHost = process.env.OPENDEPOT_SERVER_HOST ?? "localhost:80";
const BASE_URL = (process.env.OPENDEPOT_SERVER_URL ?? `http://${serverHost}`).replace(/\/$/, "");

export async function proxyCLIArtifact(path: string): Promise<NextResponse> {
  let upstream: Response;
  try {
    upstream = await fetch(`${BASE_URL}${path}`, { cache: "no-store" });
  } catch {
    return NextResponse.json({ error: "CLI artifact unavailable" }, { status: 502 });
  }

  if (!upstream.ok || !upstream.body) {
    const status = upstream.status === 404 ? 404 : 502;
    return NextResponse.json({ error: "CLI artifact unavailable" }, { status });
  }

  const headers = new Headers({
    "Content-Type": upstream.headers.get("content-type") ?? "application/octet-stream",
    "Content-Disposition": upstream.headers.get("content-disposition") ?? "attachment",
    "X-Content-Type-Options": "nosniff",
    "Cache-Control": "no-store",
  });
  const contentLength = upstream.headers.get("content-length");
  if (contentLength) {
    headers.set("Content-Length", contentLength);
  }

  return new NextResponse(upstream.body, { status: 200, headers });
}
