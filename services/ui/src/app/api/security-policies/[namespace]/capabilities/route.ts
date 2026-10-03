import { NextResponse } from "next/server";
import { ApiRequestError, getScanPolicyCapabilities } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

interface RouteContext {
  params: Promise<{ namespace: string }>;
}

export async function GET(_request: Request, context: RouteContext) {
  try {
    const { namespace } = await context.params;
    return NextResponse.json(await getScanPolicyCapabilities(namespace, await getServerSessionToken()));
  } catch (error) {
    const status = error instanceof ApiRequestError ? error.status : 502;
    return NextResponse.json({ code: "capabilities_unavailable", message: "Policy capabilities could not be loaded." }, { status });
  }
}
