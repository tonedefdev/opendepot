import { NextResponse } from "next/server";
import { ApiRequestError, listNamespaces } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

function errorResponse(error: unknown) {
  if (error instanceof ApiRequestError) {
    return NextResponse.json(
      { code: "upstream_error", message: error.message, details: error.body },
      { status: error.status },
    );
  }
  return NextResponse.json({ code: "internal_error", message: "Namespaces are unavailable." }, { status: 502 });
}

export async function GET() {
  try {
    return NextResponse.json(await listNamespaces(await getServerSessionToken()));
  } catch (error) {
    return errorResponse(error);
  }
}
