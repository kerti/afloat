import { describe, expect, it } from "vitest";

// Recharts is about 120 kB gzipped and Home's first paint must not wait for it
// (#155 R17, T8). It stays out of the entry chunk only while its one importer,
// ChartCardImpl.tsx, is itself reached only through a dynamic import(): a single
// static import of either, anywhere, pulls the library into the entry graph.
// This reads the module graph's edges from the source; the built chunk sizes are
// reported in the PR.

const sources = import.meta.glob<string>("/src/**/*.{ts,tsx}", {
  query: "?raw",
  import: "default",
  eager: true,
});

const app = Object.entries(sources).filter(
  ([path]) => !/\.test\.tsx?$/.test(path) && !path.startsWith("/src/test/"),
);

// Comments removed, so one that names the library is not an import.
function code(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|\s)\/\/.*$/gm, "$1");
}

const RECHARTS = /(?:from|import\s*\(|import)\s*\(?\s*["']recharts(?:\/[^"']*)?["']/;
const STATIC_IMPL = /(?:from|import)\s+["'][^"']*ChartCardImpl(?:\.tsx?)?["']/;
const DYNAMIC_IMPL = /import\s*\(\s*["'][^"']*ChartCardImpl(?:\.tsx?)?["']\s*\)/;

describe("the chart's lazy boundary", () => {
  it("finds the sources, so the checks below are not vacuously green", () => {
    const paths = app.map(([path]) => path);
    expect(paths).toContain("/src/components/ChartCard.tsx");
    expect(paths).toContain("/src/components/ChartCardImpl.tsx");
  });

  it("imports recharts in ChartCardImpl.tsx and nowhere else", () => {
    const importers = app.filter(([, source]) => RECHARTS.test(code(source))).map(([path]) => path);
    expect(importers).toEqual(["/src/components/ChartCardImpl.tsx"]);
  });

  it("reaches ChartCardImpl only through a dynamic import, from ChartCard.tsx alone", () => {
    const statics = app
      .filter(([, source]) => STATIC_IMPL.test(code(source)))
      .map(([path]) => path);
    expect(statics, "a static import pulls recharts into the entry chunk").toEqual([]);
    const dynamics = app
      .filter(([, source]) => DYNAMIC_IMPL.test(code(source)))
      .map(([path]) => path);
    expect(dynamics).toEqual(["/src/components/ChartCard.tsx"]);
  });

  it("does match what it bans, and only that", () => {
    for (const banned of [
      'import { Line } from "recharts";',
      "import { Line } from 'recharts'",
      'import * as R from "recharts/es6"',
      'const m = await import("recharts")',
      'import "recharts"',
    ]) {
      expect(RECHARTS.test(banned), banned).toBe(true);
    }
    for (const fine of [
      'import { x } from "recharts-like"',
      "// import from recharts",
      'const s = "recharts"',
    ]) {
      expect(RECHARTS.test(code(fine)), fine).toBe(false);
    }
    expect(STATIC_IMPL.test('import Impl from "./ChartCardImpl";')).toBe(true);
    expect(STATIC_IMPL.test('lazy(() => import("./ChartCardImpl"))')).toBe(false);
    expect(DYNAMIC_IMPL.test('lazy(() => import("./ChartCardImpl"))')).toBe(true);
  });
});
