import { describe, expect, it } from "vitest";

// Every toast goes through lib/plainToast.ts or lib/undoToast.ts, which give it
// the id tap-to-dismiss needs (lib/tapToDismiss.ts) and the one duration for
// its kind. A direct sonner call would raise a toast a tap cannot hide.
const sources = import.meta.glob<string>("/src/**/*.{ts,tsx}", {
  query: "?raw",
  import: "default",
  eager: true,
});

const ALLOWED = new Set([
  "/src/lib/plainToast.ts",
  "/src/lib/undoToast.ts",
  "/src/components/ui/sonner.tsx",
]);

describe("toasts", () => {
  it("finds the source files, so the check below is not vacuously green", () => {
    expect(Object.keys(sources)).toContain("/src/lib/plainToast.ts");
  });

  it("are raised only through the helpers, never sonner directly", () => {
    const offenders = Object.entries(sources)
      .filter(([path]) => !ALLOWED.has(path) && !/\.test\.tsx?$|\/test\//.test(path))
      .filter(([, text]) => /from\s+["']sonner["']/.test(text))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });
});
