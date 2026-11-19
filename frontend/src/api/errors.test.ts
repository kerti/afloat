import { describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import { ApiError } from "./client";
import { errorMessage } from "./errors";

describe("errorMessage", () => {
  const t = i18n.getFixedT("en-GB");
  const tID = i18n.getFixedT("id-ID");

  it("names the failing field in the user's language (VALIDATION carries {field, rule})", () => {
    const err = new ApiError(400, "VALIDATION", { field: "password", rule: "max" });
    expect(errorMessage(t, err)).toBe("Check the Password field.");
    expect(errorMessage(tID, err)).toBe("Periksa kolom Kata sandi.");
  });

  it("falls back to the raw field name for a field the catalogue does not know yet", () => {
    expect(
      errorMessage(t, new ApiError(400, "VALIDATION", { field: "amount", rule: "required" })),
    ).toBe("Check the amount field.");
  });

  it("words a 429 without Retry-After without inventing a number", () => {
    expect(errorMessage(t, new ApiError(429, "TOO_MANY_ATTEMPTS"))).toBe(
      "Too many attempts. Try again shortly.",
    );
  });

  it("never shows a code for an answer without the envelope", () => {
    expect(errorMessage(t, new ApiError(502, "UNKNOWN"))).toBe(
      "Something went wrong. Try again in a moment.",
    );
  });
});
