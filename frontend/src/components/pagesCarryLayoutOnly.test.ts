import { describe, expect, it } from "vitest";

// A page composes catalogue elements and adds layout only: flex, grid, gap,
// margin, width, order. It never carries its own type size, weight, colour,
// radius, padding or border, so changing an element is one edit and it changes
// everywhere (docs/agents/ui-conventions.md, Catalogue; Uruni web/src/
// components/cardClasses.ts, github.com/kerti/uruni).

const sources = import.meta.glob<string>("/src/pages/*.tsx", {
  query: "?raw",
  import: "default",
  eager: true,
});

// Utilities that carry a look. A variant prefix (hover:, md:, dark:) and an
// important marker do not hide one.
const LOOK = new RegExp(
  "^(?:" +
    [
      "text-(?!left$|right$|center$|start$|end$|wrap$|nowrap$|balance$|pretty$|ellipsis$|clip$)", // size, colour
      "font-",
      "leading-",
      "tracking-",
      "italic$",
      "underline$",
      "no-underline$",
      "uppercase$",
      "lowercase$",
      "capitalize$",
      "bg-",
      "border",
      "divide-",
      "ring",
      "outline",
      "shadow",
      "rounded",
      "opacity-",
      "p[xytrblse]?-",
      "fill-",
      "stroke-",
      "drop-shadow",
      "backdrop-",
      "transition",
      "animate-",
      "cursor-",
    ].join("|") +
    ")",
);

function tokenIsLook(token: string): boolean {
  const base = token.split(":").pop()!.replace(/^!/, "").replace(/!$/, "");
  return LOOK.test(base);
}

// The text of a {...} expression starting at `open`, braces balanced and
// string and template literals skipped over, so a nested ${...} or a "}" in a
// string does not end it early.
function expressionAt(source: string, open: number): string {
  let depth = 0;
  for (let i = open; i < source.length; i++) {
    const c = source[i];
    if (c === '"' || c === "'") {
      i = source.indexOf(c, i + 1);
      if (i < 0) break;
    } else if (c === "{") depth++;
    else if (c === "}" && --depth === 0) return source.slice(open + 1, i);
  }
  return source.slice(open + 1);
}

// The static text of every string literal in an expression: "…", '…', and the
// parts of a `…` template outside its ${…} holes.
function literalTexts(expr: string): string[] {
  const out: string[] = [];
  for (let i = 0; i < expr.length; i++) {
    const c = expr[i];
    if (c === '"' || c === "'") {
      const end = expr.indexOf(c, i + 1);
      if (end < 0) break;
      out.push(expr.slice(i + 1, end));
      i = end;
    } else if (c === "`") {
      let text = "";
      for (i++; i < expr.length && expr[i] !== "`"; i++) {
        if (expr[i] === "$" && expr[i + 1] === "{") {
          const hole = expressionAt(expr, i + 1);
          out.push(...literalTexts(hole));
          i += hole.length + 2;
          text += " ";
        } else text += expr[i];
      }
      out.push(text);
    }
  }
  return out;
}

// Every class token a className attribute can carry: a plain string, or the
// literals anywhere inside its expression (conditionals, cn(...), templates).
function classTokens(source: string): string[] {
  const tokens: string[] = [];
  for (const attr of source.matchAll(/className=(?:"([^"]*)"|\{)/g)) {
    const texts =
      attr[1] !== undefined ? [attr[1]] : literalTexts(expressionAt(source, attr.index! + 10));
    for (const text of texts) tokens.push(...text.split(/\s+/));
  }
  return tokens.filter(Boolean);
}

describe("pages carry layout classes only", () => {
  it("finds the pages, so the check below is not vacuously green", () => {
    expect(Object.keys(sources)).toContain("/src/pages/PocketsPage.tsx");
  });

  it("use no type, colour, radius, padding or border utility", () => {
    const offenders = Object.entries(sources).flatMap(([path, source]) =>
      classTokens(source)
        .filter(tokenIsLook)
        .map((token) => `${path}: ${token}`),
    );
    expect(offenders, "use a catalogue element (docs/agents/ui-conventions.md)").toEqual([]);
  });

  it("does match what it bans, and only that", () => {
    for (const banned of [
      "text-xl",
      "text-muted-foreground",
      "text-[0.8rem]",
      "font-semibold",
      "underline",
      "rounded-md",
      "px-3",
      "p-4",
      "pt-2",
      "border",
      "border-b",
      "divide-y",
      "bg-muted",
      "hover:bg-muted",
      "ring-2",
      "shadow-lg",
      "leading-none",
      "fill-primary",
      "stroke-muted",
      "drop-shadow-sm",
      "backdrop-blur",
      "transition-colors",
      "animate-spin",
      "cursor-pointer",
    ]) {
      expect(tokenIsLook(banned), banned).toBe(true);
    }
    for (const layout of [
      "flex",
      "flex-col",
      "grid",
      "gap-4",
      "w-full",
      "mr-auto",
      "mx-auto",
      "mt-2",
      "min-h-11",
      "items-center",
      "justify-end",
      "self-start",
      "order-last",
      "text-left",
      "text-center",
      "max-w-sm",
    ]) {
      expect(tokenIsLook(layout), layout).toBe(false);
    }
  });

  it("reads class strings from plain attributes and from expressions", () => {
    expect(classTokens('<p className="flex text-sm" />')).toEqual(["flex", "text-sm"]);
    expect(classTokens('<p className={a ? "gap-2" : "gap-2 font-medium"} />')).toEqual([
      "gap-2",
      "gap-2",
      "font-medium",
    ]);
  });

  it("reads class strings inside templates and nested expressions", () => {
    expect(classTokens('<p className={`flex ${x ? "font-bold" : ""}`} />')).toEqual([
      "font-bold",
      "flex",
    ]);
    expect(classTokens('<p className={a ? `p ${b}` : "font-bold"} />')).toEqual(["p", "font-bold"]);
    expect(classTokens('<p className={cn("gap-2", { "text-sm": on })} />')).toEqual([
      "gap-2",
      "text-sm",
    ]);
  });
});
