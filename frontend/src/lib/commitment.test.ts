import { describe, expect, it } from "vitest";
import { stillDueOf } from "@/lib/commitment";

describe("stillDueOf (#155 R16)", () => {
  it("reads a positive still due as due, with the amount half-up and unsigned", () => {
    expect(stillDueOf("500000.0000", "IDR", "en-GB")).toEqual({ kind: "due", amount: "Rp500,000" });
  });

  it("reads a negative still due as more than was set aside, with no sign in the amount", () => {
    expect(stillDueOf("-200000.0000", "IDR", "id-ID")).toEqual({
      kind: "over",
      amount: "Rp200.000",
    });
  });

  it("is paid whenever the shown amount is zero, from either side", () => {
    for (const v of ["0", "0.0000", "-0.0000", "0.4999", "-0.4999", "0.0001", "-0.0001"]) {
      expect(stillDueOf(v, "IDR", "en-GB"), v).toEqual({ kind: "paid" });
    }
  });

  it("is not paid at the half, which rounds away from zero", () => {
    expect(stillDueOf("0.5", "IDR", "en-GB")).toEqual({ kind: "due", amount: "Rp1" });
    expect(stillDueOf("-0.5", "IDR", "en-GB")).toEqual({ kind: "over", amount: "Rp1" });
  });

  it("decides on the currency's own decimals, not the rupiah's", () => {
    expect(stillDueOf("0.0049", "USD", "en-GB")).toEqual({ kind: "paid" });
    expect(stillDueOf("0.005", "USD", "en-GB")).toEqual({ kind: "due", amount: "USD\u00a00.01" });
  });

  it("never abbreviates a large figure", () => {
    expect(stillDueOf("9999999999999999.0000", "IDR", "en-GB")).toEqual({
      kind: "due",
      amount: "Rp9,999,999,999,999,999",
    });
  });

  it("says nothing for a figure that is not a plain decimal", () => {
    for (const v of ["", "abc", "1e5", "NaN"]) expect(stillDueOf(v, "IDR", "en-GB"), v).toBeNull();
  });
});
