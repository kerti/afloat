import { describe, expect, it } from "vitest";

// Components use the theme tokens in styles/theme.css, never a raw Tailwind
// palette class, so light and dark both stay real (PRD §9) and nothing is
// coloured red or green by hand (docs/agents/ui-conventions.md, Colour and
// type; Balances frontend/src, github.com/kerti/balances-v2).

const sources = import.meta.glob<string>("/src/**/*.{ts,tsx}", {
  query: "?raw",
  import: "default",
  eager: true,
});

const PALETTE =
  "red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|slate|gray|zinc|neutral|stone";
const UTILITY =
  "bg|text|border|ring|outline|fill|stroke|from|via|to|divide|decoration|accent|caret|shadow|placeholder";
// text-red-500, bg-green-50/60, dark:border-blue-300; bg-white and text-black;
// and an arbitrary colour such as bg-[#fff] or text-[rgb(0,0,0)].
const RAW_COLOUR = new RegExp(
  `(?<![\\w-])(?:${UTILITY})-(?:(?:${PALETTE})-\\d{2,3}|white|black|\\[(?:#|rgb|hsl|oklch))`,
);

describe("raw colours", () => {
  it("finds the source files, so the check below is not vacuously green", () => {
    expect(Object.keys(sources)).toContain("/src/components/ui/button.tsx");
  });

  it("appear in no component or page", () => {
    const offenders = Object.entries(sources)
      .filter(([path]) => !/\.test\.tsx?$/.test(path) && !path.endsWith("schema.gen.ts"))
      .filter(([, source]) => RAW_COLOUR.test(source))
      .map(([path]) => path);
    expect(offenders, "use a token from styles/theme.css (text-attention, bg-muted, ...)").toEqual(
      [],
    );
  });

  it("does match what it bans, and only that", () => {
    for (const banned of [
      "text-red-500",
      "bg-green-50",
      "dark:border-blue-300",
      "bg-white",
      "text-[#a34a28]",
      "bg-[rgb(0,0,0)]",
    ]) {
      expect(RAW_COLOUR.test(banned), banned).toBe(true);
    }
    for (const fine of [
      "text-attention",
      "bg-muted",
      "border-border",
      "text-muted-foreground",
      "bg-background/80",
      "ring-ring",
      "text-primary-foreground",
      "bg-card!",
    ]) {
      expect(RAW_COLOUR.test(fine), fine).toBe(false);
    }
  });
});
