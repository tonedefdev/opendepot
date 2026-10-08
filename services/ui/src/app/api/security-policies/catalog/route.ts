import { NextResponse } from "next/server";
import { ApiRequestError, getScanPolicyCatalog } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

function errorResponse(error: unknown) {
  if (error instanceof ApiRequestError) {
    return NextResponse.json(
      { code: "upstream_error", message: "The policy catalog could not be loaded." },
      { status: error.status },
    );
  }
  return NextResponse.json(
    { code: "internal_error", message: "The policy catalog is unavailable." },
    { status: 502 },
  );
}

export async function GET() {
  try {
    return NextResponse.json(await getScanPolicyCatalog(await getServerSessionToken()));
  } catch (error) {
    return errorResponse(error);
  }
}