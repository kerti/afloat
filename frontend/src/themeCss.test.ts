import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// jsdom evaluates no CSS, so the rules that make light, dark and System work
// are checked as text (BOOTSTRAP §8; docs/agents/ui-conventions.md).
const css = readFileSync(path.resolve(import.meta.dirname, "styles/theme.css"), "utf8");
const noComments = css.replace(/\/\*[\s\S]*?\*\//g, "");
const rule = (selector: string) =>
  noComments.match(new RegExp(`${selector.replace(/[[\]"]/g, "\\$&")}\\s*\\{([^}]*)\\}`))?.[1];

describe("theme.css appearance rules", () => {
  it("lets the OS decide for System, and the attribute pick one side", () => {
    expect(rule(":root")).toMatch(/color-scheme:\s*light dark/);
    expect(rule(':root[data-appearance="light"]')).toMatch(/color-scheme:\s*light\s*;/);
    expect(rule(':root[data-appearance="dark"]')).toMatch(/color-scheme:\s*dark\s*;/);
  });

  it("writes every colour token as light-dark(), or as a scheme-independent value", () => {
    const tokens = [...noComments.matchAll(/^\s*(--[\w-]+):\s*([^;]+);/gm)]
      .map((m) => [m[1]!, m[2]!.trim()] as const)
      .filter(([name]) => !["--font-ui", "--radius"].includes(name));
    expect(tokens.length).toBeGreaterThan(10);
    // Colours that are the same in both appearances are the only exceptions.
    const same = ["--destructive-foreground", "--scrim"];
    for (const [name, value] of tokens) {
      if (same.includes(name)) continue;
      expect(value, name).toMatch(/^light-dark\(/);
    }
  });

  it("has no second dark block", () => {
    expect(noComments).not.toMatch(/prefers-color-scheme/);
  });
});
