import path from "node:path";
import { defineConfig } from "vitest/config";

// virtual:pwa-register/react exists only once vite-plugin-pwa builds the
// worker; tests get a stub so a component that uses it can render.
const alias = {
  "@": path.resolve(import.meta.dirname, "./src"),
  "virtual:pwa-register/react": path.resolve(
    import.meta.dirname,
    "./src/test/pwa-register-stub.ts",
  ),
};

export default defineConfig({
  resolve: { alias },
  // A fixed build name, so a test never depends on the checkout's git state.
  define: { __APP_VERSION__: JSON.stringify("dev (test)") },
  test: {
    environment: "jsdom",
    // Above setup.ts's asyncUtilTimeout, so a starved wait fails as its own error.
    testTimeout: 15_000,
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test/setup.ts"],
    coverage: {
      provider: "v8",
      reporter: ["text", "lcov"],
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.test.{ts,tsx}", "src/test/**", "src/api/schema.gen.ts", "src/main.tsx"],
    },
  },
});
