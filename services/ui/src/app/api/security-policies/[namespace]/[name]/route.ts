import { NextResponse } from "next/server";
import {
  ApiRequestError,
  deleteScanPolicy,
  getScanPolicy,
  updateScanPolicy,
  type ScanPolicyMutation,
} from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

interface RouteContext {
  params: Promise<{ namespace: string; name: string }>;
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

async function routeParams(context: RouteContext) {
  return context.params;
}

export async function GET(_request: Request, context: RouteContext) {
  try {
    const { namespace, name } = await routeParams(context);
    return NextResponse.json(await getScanPolicy(namespace, name, await getServerSessionToken()));
  } catch (error) {
    return errorResponse(error);
  }
}

export async function PUT(request: Request, context: RouteContext) {
  try {
    const { namespace, name } = await routeParams(context);
    const resourceVersion = request.headers.get("If-Match");
    if (!resourceVersion) {
      return NextResponse.json({ code: "missing_if_match", message: "If-Match is required." }, { status: 428 });
    }
    const mutation = (await request.json()) as ScanPolicyMutation;
    return NextResponse.json(
      await updateScanPolicy(namespace, name, mutation, resourceVersion, await getServerSessionToken()),
    );
  } catch (error) {
    return errorResponse(error);
  }
}

export async function DELETE(request: Request, context: RouteContext) {
  try {
    const { namespace, name } = await routeParams(context);
    const resourceVersion = request.headers.get("If-Match");
    if (!resourceVersion) {
      return NextResponse.json({ code: "missing_if_match", message: "If-Match is required." }, { status: 428 });
    }
    await deleteScanPolicy(namespace, name, resourceVersion, await getServerSessionToken());
    return new NextResponse(null, { status: 204 });
  } catch (error) {
    return errorResponse(error);
  }
}
