import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import enGB from "./en-GB.json";
import idID from "./id-ID.json";
import { errorCodes } from "@/api/client";

function keys(obj: object, prefix = ""): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    v !== null && typeof v === "object" ? keys(v, `${prefix}${k}.`) : [`${prefix}${k}`],
  );
}

// The contract's ErrorCode enum, read from the YAML itself rather than the
// client's own list, so this catches the client and the catalogue together.
function contractErrorCodes(): string[] {
  const yaml = readFileSync(
    path.resolve(import.meta.dirname, "../../../contract/openapi.yaml"),
    "utf8",
  );
  const block = yaml.split(/\n {4}ErrorCode:\n/)[1]?.split(/\n {2}\S/)[0] ?? "";
  const enumPart = block.split(/\n {6}enum:\n/)[1] ?? "";
  return [...enumPart.matchAll(/^ {8}- ([A-Z_]+)$/gm)].map((m) => m[1]!);
}

// i18next picks a plural form by the language's own rules: English needs
// _one and _other, Indonesian has only _other. Compared without the suffix.
const pluralSuffix = /_(zero|one|two|few|many|other)$/;
const stems = (obj: object) =>
  [...new Set(keys(obj).map((k) => k.replace(pluralSuffix, "")))].sort();

describe("locale catalogues", () => {
  it("en-GB and id-ID carry exactly the same keys (PRD N12a)", () => {
    expect(stems(idID)).toEqual(stems(enGB));
  });

  it("the client knows every contract ErrorCode, and both locales word each one", () => {
    const codes = contractErrorCodes();
    expect(codes.length).toBeGreaterThan(0);
    expect([...errorCodes].sort()).toEqual([...codes].sort());
    for (const code of codes) {
      for (const catalogue of [enGB.errors, idID.errors]) {
        const worded = Object.keys(catalogue).some((k) => k.replace(pluralSuffix, "") === code);
        expect(worded, code).toBe(true);
      }
    }
  });

  // docs/agents/ui-conventions.md, Copy: no em dash in anything the user reads.
  // The raw files, so a key's name and a value are both covered.
  it.each(["en-GB", "id-ID"])("%s has no em dash", (locale) => {
    const raw = readFileSync(path.resolve(import.meta.dirname, `${locale}.json`), "utf8");
    expect(raw).not.toMatch(/\u2014|\\u2014|&mdash;/);
  });

  it.each(["en-GB", "id-ID"])("%s uses the single-character ellipsis", (locale) => {
    const raw = readFileSync(path.resolve(import.meta.dirname, `${locale}.json`), "utf8");
    expect(raw).not.toContain("...");
  });
});

// Every key the source can reach. A literal "a.b.c" counts; so does a template
// literal's static prefix (`errors.${code}` covers everything under errors.),
// because those keys are chosen at run time.
const sources = import.meta.glob<string>("/src/**/*.{ts,tsx}", {
  query: "?raw",
  import: "default",
  eager: true,
});

describe("locale keys in use", () => {
  const app = Object.entries(sources)
    .filter(
      ([p]) => !/\.test\.tsx?$/.test(p) && !p.startsWith("/src/test/") && !p.endsWith(".gen.ts"),
    )
    .map(([, source]) => source)
    .join("\n");
  const literals = new Set(
    [...app.matchAll(/["'`]([A-Za-z][\w]*(?:\.[\w]+)+)["'`]/g)].map((m) => m[1]!),
  );
  const prefixes = [...app.matchAll(/`([A-Za-z][\w.]*\.)\$\{/g)].map((m) => m[1]!);

  it("leaves no key in the catalogues that nothing reads", () => {
    const unused = stems(enGB).filter(
      (key) => !literals.has(key) && !prefixes.some((prefix) => key.startsWith(prefix)),
    );
    expect(unused, "delete the key, or use it").toEqual([]);
  });
});
