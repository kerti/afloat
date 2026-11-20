import { describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import { ApiError } from "./client";
import { errorMessage } from "./errors";

describe("errorMessage", () => {
  const t = i18n.getFixedT("en-GB");
  const tID = i18n.getFixedT("id-ID");

  it("names the failing field in the user's language (VALIDATION carries {field, rule})", () => {
    const err = new ApiError(400, "VALIDATION", { field: "password", rule: "max" });
    expect(errorMessage(t, err)).toBe("Check the password field.");
    expect(errorMessage(tID, err)).toBe("Periksa kolom kata sandi.");
  });

  it("falls back to the raw field name for a field the catalogue does not know yet", () => {
    expect(
      errorMessage(t, new ApiError(400, "VALIDATION", { field: "amount", rule: "required" })),
    ).toBe("Check the amount field.");
  });

  it("pluralises the backoff window by the language's own rules", () => {
    const wait = (s: number) => new ApiError(429, "TOO_MANY_ATTEMPTS", {}, s);
    expect(errorMessage(t, wait(1))).toBe("Too many attempts. Try again in 1 second.");
    expect(errorMessage(t, wait(4))).toBe("Too many attempts. Try again in 4 seconds.");
    expect(errorMessage(tID, wait(1))).toBe("Terlalu banyak percobaan. Coba lagi dalam 1 detik.");
    expect(errorMessage(t, wait(0))).toBe("Too many attempts. Try again shortly.");
  });

  it("words a 429 without Retry-After without inventing a number", () => {
    expect(errorMessage(t, new ApiError(429, "TOO_MANY_ATTEMPTS"))).toBe(
      "Too many attempts. Try again shortly.",
    );
  });

  it("words each WEAK_PASSWORD reason, and EMAIL_TAKEN and FOUNDING_DISABLED, in both languages", () => {
    const weak = (reason: string) => new ApiError(400, "WEAK_PASSWORD", { reason });
    expect(errorMessage(t, weak("min"))).toBe("Use at least 10 characters.");
    expect(errorMessage(t, weak("breached"))).toBe("That password is too common. Pick another.");
    expect(errorMessage(tID, weak("min"))).toBe("Gunakan minimal 10 karakter.");
    expect(errorMessage(tID, weak("breached"))).toBe(
      "Kata sandi itu terlalu umum. Pilih yang lain.",
    );
    // A reason the client does not know reads as the floor, never as a raw key.
    expect(errorMessage(t, weak("future-reason"))).toBe("Use at least 10 characters.");

    expect(errorMessage(t, new ApiError(409, "EMAIL_TAKEN"))).toContain("already exists");
    expect(errorMessage(tID, new ApiError(409, "EMAIL_TAKEN"))).toContain("Sudah ada akun");
    expect(errorMessage(t, new ApiError(403, "FOUNDING_DISABLED"))).toContain("isn't accepting");
    expect(errorMessage(tID, new ApiError(403, "FOUNDING_DISABLED"))).toContain("tidak menerima");
  });

  it("never shows a code for an answer without the envelope", () => {
    expect(errorMessage(t, new ApiError(502, "UNKNOWN"))).toBe(
      "Something went wrong. Try again in a moment.",
    );
  });
});
