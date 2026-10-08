import { NextResponse } from "next/server";
import { ApiRequestError, previewScanPolicy, type ScanPolicyMutation } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

interface RouteContext {
  params: Promise<{ namespace: string }>;
}

export async function POST(request: Request, context: RouteContext) {
  try {
    const { namespace } = await context.params;
    return NextResponse.json(
      await previewScanPolicy(namespace, (await request.json()) as ScanPolicyMutation, await getServerSessionToken()),
    );
  } catch (error) {
    const status = error instanceof ApiRequestError ? error.status : 502;
    return NextResponse.json({ code: "preview_unavailable", message: "Policy preview failed." }, { status });
  }
}
