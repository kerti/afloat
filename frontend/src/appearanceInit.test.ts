import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import path from "node:path";

// public/appearance-init.js is a classic script (CSP, #99), so it is run the
// way a browser would run it, with `module` supplied to hand its engine back.
const source = readFileSync(
  path.resolve(import.meta.dirname, "../public/appearance-init.js"),
  "utf8",
);
const html = readFileSync(path.resolve(import.meta.dirname, "../index.html"), "utf8");

type Engine = {
  keys: { name: string; attribute: string; values: string[] }[];
  read: (s: Pick<Storage, "getItem">, name: string) => string | null;
  write: (s: Pick<Storage, "setItem">, name: string, value: string) => void;
  apply: (doc: Document, name: string, value: string | null) => void;
  boot: (doc: Document, s: Pick<Storage, "getItem">) => void;
};

function load(): Engine {
  const mod = { exports: {} as Engine };
  new Function("module", "window", source).call(undefined, mod, window);
  return mod.exports;
}

function freshDoc(): Document {
  return new DOMParser().parseFromString(
    `<!doctype html><html><head>
      <meta name="theme-color" media="(prefers-color-scheme: light)" content="#f0f3f5">
      <meta name="theme-color" media="(prefers-color-scheme: dark)" content="#0c1820">
    </head><body></body></html>`,
    "text/html",
  );
}

const store = (value: string | null) => ({ getItem: () => value });
const colours = (doc: Document) =>
  [...doc.querySelectorAll('meta[name="theme-color"]')].map((m) => m.getAttribute("content"));

describe("appearance boot script", () => {
  const engine = load();

  it("is driven by a key table with one row today", () => {
    expect(engine.keys).toEqual([
      { name: "appearance", attribute: "data-appearance", values: ["light", "dark"] },
    ]);
  });

  it.each(["light", "dark"])("sets data-appearance for a cached %s", (value) => {
    const doc = freshDoc();
    engine.boot(doc, store(value));
    expect(doc.documentElement.getAttribute("data-appearance")).toBe(value);
  });

  it.each(["system", "auto", "", null])("sets nothing for %j", (value) => {
    const doc = freshDoc();
    engine.boot(doc, store(value));
    expect(doc.documentElement.hasAttribute("data-appearance")).toBe(false);
    expect(colours(doc)).toEqual(["#f0f3f5", "#0c1820"]);
  });

  it("survives storage that throws", () => {
    const doc = freshDoc();
    engine.boot(doc, {
      getItem: () => {
        throw new Error("blocked");
      },
    });
    expect(doc.documentElement.hasAttribute("data-appearance")).toBe(false);
  });

  it("points theme-color at the chosen scheme, and restores both for system", () => {
    const doc = freshDoc();
    engine.apply(doc, "appearance", "dark");
    expect(colours(doc)).toEqual(["#0c1820", "#0c1820"]);
    engine.apply(doc, "appearance", "light");
    expect(colours(doc)).toEqual(["#f0f3f5", "#f0f3f5"]);
    engine.apply(doc, "appearance", "system");
    expect(colours(doc)).toEqual(["#f0f3f5", "#0c1820"]);
    expect(doc.documentElement.hasAttribute("data-appearance")).toBe(false);
  });

  it("caches under afloat.appearance and ignores a key it has no row for", () => {
    const seen: string[] = [];
    engine.write({ setItem: (k) => void seen.push(k) }, "appearance", "dark");
    expect(seen).toEqual(["afloat.appearance"]);
    const doc = freshDoc();
    engine.apply(doc, "palette", "dark");
    expect(doc.documentElement.hasAttribute("data-appearance")).toBe(false);
  });

  it("is loaded by index.html as an external script before the bundle", () => {
    const init = html.indexOf('src="/appearance-init.js"');
    expect(init).toBeGreaterThan(-1);
    expect(init).toBeLessThan(html.indexOf("/src/main.tsx"));
    expect(html).not.toMatch(/<script(?![^>]*\ssrc=)/);
  });
});
