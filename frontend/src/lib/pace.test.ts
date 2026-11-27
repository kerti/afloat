import { describe, expect, it } from "vitest";
import { paceOf } from "@/lib/pace";

describe("paceOf (#155 R8)", () => {
  it("reads a positive variance as ahead, with the amount half-up and unsigned", () => {
    expect(paceOf("420000.0000", "IDR", "en-GB")).toEqual({
      direction: "ahead",
      amount: "Rp420,000",
    });
  });

  it("reads a negative variance as behind, with no sign in the amount", () => {
    expect(paceOf("-420000.0000", "IDR", "id-ID")).toEqual({
      direction: "behind",
      amount: "Rp420.000",
    });
  });

  it("is on pace whenever the shown amount is zero, from either side", () => {
    for (const v of ["0", "0.0000", "-0.0000", "0.4999", "-0.4999", "0.0001", "-0.0001"]) {
      expect(paceOf(v, "IDR", "en-GB"), v).toEqual({ direction: "on" });
    }
  });

  it("is not on pace at the half, which rounds away from zero", () => {
    expect(paceOf("0.5", "IDR", "en-GB")).toEqual({ direction: "ahead", amount: "Rp1" });
    expect(paceOf("-0.5", "IDR", "en-GB")).toEqual({ direction: "behind", amount: "Rp1" });
  });

  it("decides on the currency's own decimals, not the rupiah's", () => {
    expect(paceOf("0.0049", "USD", "en-GB")).toEqual({ direction: "on" });
    expect(paceOf("-0.005", "USD", "en-GB")).toEqual({
      direction: "behind",
      amount: "USD 0.01",
    });
  });

  it("holds 16 integer digits exactly", () => {
    expect(paceOf("-9999999999999999.0000", "IDR", "en-GB")).toEqual({
      direction: "behind",
      amount: "Rp9,999,999,999,999,999",
    });
  });

  it("says nothing for what is not a plain decimal", () => {
    for (const v of ["", "abc", "1e6", "+5", "--5", "1,000"]) {
      expect(paceOf(v, "IDR", "en-GB"), v).toBeNull();
    }
  });
});
