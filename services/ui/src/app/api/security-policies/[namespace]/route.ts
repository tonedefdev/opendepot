import { NextResponse } from "next/server";
import {
  ApiRequestError,
  createScanPolicy,
  listScanPolicies,
  type ScanPolicyMutation,
} from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

interface RouteContext {
  params: Promise<{ namespace: string }>;
}

function errorResponse(error: unknown) {
  if (error instanceof ApiRequestError) {
    return NextResponse.json(
      { code: error.status === 409 ? "conflict" : "upstream_error", message: error.message, details: error.body },
      { status: error.status },
    );
  }
  return NextResponse.json({ code: "internal_error", message: "The policy service is unavailable." }, { status: 502 });
}

export async function GET(_request: Request, context: RouteContext) {
  try {
    const { namespace } = await context.params;
    return NextResponse.json(await listScanPolicies(namespace, await getServerSessionToken()));
  } catch (error) {
    return errorResponse(error);
  }
}

export async function POST(request: Request, context: RouteContext) {
  try {
    const { namespace } = await context.params;
    const mutation = (await request.json()) as ScanPolicyMutation;
    return NextResponse.json(await createScanPolicy(namespace, mutation, await getServerSessionToken()), { status: 201 });
  } catch (error) {
    return errorResponse(error);
  }
}
