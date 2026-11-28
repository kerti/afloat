import { execFileSync } from "node:child_process";
import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";

// The dev server on 5181 forwards the API to the backend on 5182 (BOOTSTRAP
// §1), so the browser sees one origin, as it does in production where the Go
// binary serves both (§8). AFLOAT_API_TARGET moves it.
const apiTarget = process.env.AFLOAT_API_TARGET ?? "http://localhost:5182";

// The installable PWA (PRD N14). The service worker precaches the built shell
// and nothing else: no runtimeCaching, so no /api answer ever sits in a cache
// where a stale screen could read as current data, and writes fail visibly
// offline rather than silently queueing.
const pwa = VitePWA({
  // 'prompt', not 'autoUpdate': a reload on deploy would discard whatever the
  // user was typing. UpdatePrompt offers it instead.
  registerType: "prompt",
  // UpdatePrompt registers the worker; no second registration script.
  injectRegister: null,
  manifest: {
    name: "Afloat",
    short_name: "Afloat",
    description: "How much is left to spend today.",
    start_url: "/",
    scope: "/",
    display: "standalone",
    // Neutral until the brand session (PRD §9); kept in step with the light
    // --background token in src/styles/theme.css. A manifest takes one value,
    // so the install splash stays light in dark mode; the running app's bar
    // follows the scheme through the theme-color metas in index.html.
    background_color: "#f0f3f5",
    theme_color: "#f0f3f5",
    // Placeholder until the brand session; PNG sizes for iOS come with it.
    icons: [
      { src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "any" },
      { src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "maskable" },
    ],
  },
  workbox: {
    globPatterns: ["**/*.{js,css,html,svg,woff2}"],
    navigateFallback: "/index.html",
    // The Go binary owns /api; an installed worker must never answer a
    // navigation there with the cached shell.
    navigateFallbackDenylist: [/^\/api(\/|$)/],
  },
});

// The build the header names (Uruni's AppVersion). The SPA is embedded in the
// Go binary it ships with, so the version can be fixed here at build time:
// AFLOAT_VERSION when a release sets it, else a tag on HEAD, else "dev" and
// the short commit, as Uruni shows an untagged build. No git, plain "dev".
function git(...args: string[]): string | null {
  try {
    return execFileSync("git", args, { stdio: ["ignore", "pipe", "ignore"] })
      .toString()
      .trim();
  } catch {
    return null;
  }
}

function appVersion(): string {
  if (process.env.AFLOAT_VERSION) return process.env.AFLOAT_VERSION;
  const tag = git("describe", "--tags", "--exact-match");
  if (tag) return tag;
  const commit = git("rev-parse", "--short=7", "HEAD");
  return commit ? `dev (${commit})` : "dev";
}

export default defineConfig({
  plugins: [react(), tailwindcss(), pwa],
  define: {
    __APP_VERSION__: JSON.stringify(appVersion()),
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    port: 5181,
    strictPort: true,
    proxy: {
      "/api": apiTarget,
    },
  },
});
