import { proxyCLIArtifact } from "@/lib/cliProxy";

export async function GET() {
  return proxyCLIArtifact("/opendepot/cli/v1/SHA256SUMS");
}
