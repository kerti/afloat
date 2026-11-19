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
});
