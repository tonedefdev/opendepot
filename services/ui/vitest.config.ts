import path from "node:path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  test: {
    include: ["src/**/*.{test,spec}.?(c|m)[jt]s?(x)"],
    setupFiles: ["./src/test-setup.ts"],
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      reporter: ["lcovonly", "text"],
      reportsDirectory: path.resolve(__dirname, "coverage"),
    },
  },
});