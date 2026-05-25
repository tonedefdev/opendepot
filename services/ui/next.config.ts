import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  // The UI communicates with the server API through NGINX proxying — no
  // direct CORS or rewrites needed inside Next.js itself.
};

export default nextConfig;
