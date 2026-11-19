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

describe("locale catalogues", () => {
  it("en-GB and id-ID carry exactly the same keys (PRD N12a)", () => {
    expect(keys(idID).sort()).toEqual(keys(enGB).sort());
  });

  it("the client knows every contract ErrorCode, and both locales word each one", () => {
    const codes = contractErrorCodes();
    expect(codes.length).toBeGreaterThan(0);
    expect([...errorCodes].sort()).toEqual([...codes].sort());
    for (const code of codes) {
      expect(enGB.errors).toHaveProperty(code);
      expect(idID.errors).toHaveProperty(code);
    }
  });
});
