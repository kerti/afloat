import { describe, expect, it } from "vitest";

// CLAUDE.md rule 5: Today and the Period Day come from the server, so the device
// clock never decides a business date; and `new Date("2026-09-03")` parses as UTC
// and shifts the day (docs/agents/ui-conventions.md, Money and dates; after Uruni
// web/src/lib/noDeviceClock.test.ts, github.com/kerti/uruni). Outside the
// allowlist and test files, src may not read the clock or build a Date from a
// date-only string.

const sources = import.meta.glob<string>("/src/**/*.{ts,tsx}", {
  query: "?raw",
  import: "default",
  eager: true,
});

// Not business dates. uuid.ts stamps a UUIDv7's 48-bit creation time (RFC 9562),
// which is a moment, not a calendar day.
const allowed = new Set(["/src/lib/uuid.ts"]);

function mayReadTheClock(path: string): boolean {
  return allowed.has(path) || /\.test\.tsx?$/.test(path) || path.startsWith("/src/test/");
}

// Comments removed, so one that mentions `new Date()` is not a call. A `//` is a
// comment only at the start of a line or after whitespace, so a URL survives.
function withoutComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|\s)\/\/.*$/gm, "$1");
}

const DEVICE_CLOCK = /\bnew\s+Date\s*\(\s*\)|\bDate\s*\.\s*now\s*\(/;
const DATE_ONLY = /\bnew\s+Date\s*\(\s*(["'`])\d{4}-\d{2}-\d{2}\1\s*\)/;
const BANNED = [DEVICE_CLOCK, DATE_ONLY];

describe("the device clock and date-only strings", () => {
  it("finds the source files, so the checks below are not vacuously green", () => {
    const paths = Object.keys(sources);
    expect(paths).toContain("/src/lib/uuid.ts");
    expect(paths).toContain("/src/pages/SettingsPage.tsx");
    expect(paths.length).toBeGreaterThan(30);
  });

  it("appear nowhere in src outside the allowlist and the tests", () => {
    const offenders = Object.entries(sources)
      .filter(([path]) => !mayReadTheClock(path))
      .filter(([, source]) => BANNED.some((re) => re.test(withoutComments(source))))
      .map(([path]) => path);
    expect(
      offenders,
      "dates come from the server; never new Date(), Date.now() or new Date('YYYY-MM-DD')",
    ).toEqual([]);
  });

  it("does match what it bans, and only that", () => {
    for (const banned of [
      "const now = new Date()",
      "new  Date( )",
      "const t = Date.now()",
      'new Date("2026-09-03")',
      "new Date('2026-09-03')",
      "new Date(`2026-09-03`)",
    ]) {
      expect(
        BANNED.some((re) => re.test(banned)),
        banned,
      ).toBe(true);
    }
    for (const fine of ["new Date(2026, 8, 3)", "new Date(seconds * 1000)", "new Date(iso)"]) {
      expect(
        BANNED.some((re) => re.test(fine)),
        fine,
      ).toBe(false);
    }
    expect(DEVICE_CLOCK.test(withoutComments("// new Date()\n/* Date.now() */"))).toBe(false);
  });
});
