import { type NextRequest, NextResponse } from "next/server";
import { isCLIPlatform } from "@/lib/cliPlatforms";
import { proxyCLIArtifact } from "@/lib/cliProxy";

export async function GET(
  _request: NextRequest,
  { params }: { params: Promise<{ os: string; arch: string }> },
) {
  const { os, arch } = await params;
  if (!isCLIPlatform(os, arch)) {
    return NextResponse.json({ error: "unsupported platform" }, { status: 404 });
  }

  return proxyCLIArtifact(`/opendepot/cli/v1/${os}/${arch}`);
}
