import { NextResponse } from "next/server";
import { ApiRequestError, listJevReviewQueue } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

function errorResponse(error: unknown) {
  if (error instanceof ApiRequestError) {
    return NextResponse.json(
      { code: "upstream_error", message: error.message, details: error.body },
      { status: error.status },
    );
  }
  return NextResponse.json({ code: "internal_error", message: "The review queue is unavailable." }, { status: 502 });
}

export async function GET() {
  try {
    return NextResponse.json(await listJevReviewQueue(await getServerSessionToken()));
  } catch (error) {
    return errorResponse(error);
  }
}
