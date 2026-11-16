import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  devIndicators: false,
  // The layout test's dev server sets its own build directory so it never shares .next with a developer's server.
  distDir: process.env.OPENDEPOT_UI_DIST_DIR ?? ".next",
  // The UI communicates with the server API through NGINX proxying — no
  // direct CORS or rewrites needed inside Next.js itself
};

export default nextConfig;
