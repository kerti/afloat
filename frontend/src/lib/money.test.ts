import { describe, expect, it } from "vitest";
import {
  canonicalMoney,
  currencyDecimals,
  displayDraft,
  draftFromWire,
  draftToWire,
  formatMoney,
  parseMoney,
  roundMoney,
  currencySymbol,
  typeInto,
} from "./money";

describe("formatMoney", () => {
  it("shows IDR with no decimals and an Rp prefix, grouped in the locale", () => {
    expect(formatMoney("9000000.0000", "IDR", "id-ID")).toBe("Rp9.000.000");
    expect(formatMoney("9000000.0000", "IDR", "en-GB")).toBe("Rp9,000,000");
    expect(formatMoney("0.0000", "IDR", "en-GB")).toBe("Rp0");
  });

  it("shows a decimal currency with its decimals and its code", () => {
    expect(formatMoney("1234.5000", "USD", "en-GB")).toBe("USD 1,234.50");
    expect(formatMoney("1234.5000", "USD", "id-ID")).toBe("USD 1.234,50");
    expect(formatMoney("1.2345", "KWD", "en-GB")).toBe("KWD 1.235");
    expect(currencyDecimals("JPY")).toBe(0);
  });

  it("rounds half up on the digits, carrying through the integer part", () => {
    expect(formatMoney("999.9950", "USD", "en-GB")).toBe("USD 1,000.00");
    expect(formatMoney("1000.5000", "IDR", "en-GB")).toBe("Rp1,001");
    expect(formatMoney("1000.4999", "IDR", "en-GB")).toBe("Rp1,000");
    expect(formatMoney("0.5000", "IDR", "en-GB")).toBe("Rp1");
  });

  it("keeps a negative sign, and drops it when the figure rounds to zero", () => {
    expect(formatMoney("-1500000.0000", "IDR", "id-ID")).toBe("-Rp1.500.000");
    expect(formatMoney("-0.0004", "IDR", "en-GB")).toBe("Rp0");
  });

  it("renders a bad input as given, never NaN", () => {
    for (const bad of ["", "abc", "1e6", "1,000", " 5", "NaN", "1.2.3"]) {
      expect(formatMoney(bad, "IDR", "en-GB")).toBe(bad);
    }
  });
});

describe("parseMoney (strict paste)", () => {
  it("reads the locale's own grouping and decimal point", () => {
    expect(parseMoney("9.000.000", "id-ID", "IDR")).toBe("9000000");
    expect(parseMoney("9,000,000", "en-GB", "IDR")).toBe("9000000");
    expect(parseMoney("1.234,50", "id-ID", "USD")).toBe("1234.5");
    expect(parseMoney(" 1,234.50 ", "en-GB", "USD")).toBe("1234.5");
    expect(parseMoney("007", "en-GB", "USD")).toBe("7");
  });

  it("refuses anything else", () => {
    for (const bad of [
      "",
      "Rp9.000.000",
      "-5",
      "1e6",
      "9,00,000",
      "9,000.000,5",
      "1.5.5",
      "12 345",
      "1,000.123",
      "99999999999999999",
    ]) {
      expect(parseMoney(bad, "en-GB", "USD"), bad).toBeNull();
    }
    // The other locale's separators are not this one's.
    expect(parseMoney("1.000", "en-GB", "IDR")).toBeNull();
    expect(parseMoney("1,5", "en-GB", "USD")).toBeNull();
    // A zero-decimal currency takes no decimal part.
    expect(parseMoney("1,5", "id-ID", "IDR")).toBeNull();
  });
});

describe("canonicalMoney and drafts", () => {
  it("makes one spelling of an amount", () => {
    expect(canonicalMoney("9000000.0000")).toBe("9000000");
    expect(canonicalMoney("0150.5000")).toBe("150.5");
    expect(canonicalMoney("oops")).toBe("oops");
    expect(draftFromWire("9000000.0000")).toBe("9000000");
    expect(draftFromWire("-1")).toBe("");
    expect(draftToWire("12.")).toBe("12");
    expect(displayDraft("1234567.5", "id-ID")).toBe("1.234.567,5");
    expect(displayDraft("1234.", "en-GB")).toBe("1,234.");
  });
});

describe("typeInto", () => {
  it("regroups and keeps the caret after the same digit", () => {
    // "999" then a 9 typed at the end.
    expect(typeInto("999", "9999", 4, "en-GB", "IDR")).toEqual({ draft: "9999", significant: 4 });
    // A 5 typed after the first digit of 1,000: "1|,000" → "15,000".
    expect(typeInto("1,000", "15,000", 2, "en-GB", "IDR")).toEqual({
      draft: "15000",
      significant: 2,
    });
  });

  it("takes a typed . or , as the decimal point, once", () => {
    expect(typeInto("1.000", "1.000,", 6, "id-ID", "USD").draft).toBe("1000.");
    expect(typeInto("1,000", "1,000.", 6, "en-GB", "USD").draft).toBe("1000.");
    expect(typeInto("1,000.", "1,000..", 7, "en-GB", "USD").draft).toBe("1000.");
    expect(typeInto("", ".", 1, "en-GB", "USD")).toEqual({ draft: "0.", significant: 2 });
  });

  it("reads the locale's decimal separator already on screen", () => {
    expect(typeInto("1.000,5", "1.000,56", 7, "id-ID", "USD").draft).toBe("1000.56");
  });

  it("drops what is not a digit, extra decimals and any decimal point for IDR", () => {
    expect(typeInto("", "12ab", 4, "en-GB", "USD").draft).toBe("12");
    expect(typeInto("1.23", "1.234", 5, "en-GB", "USD").draft).toBe("1.23");
    expect(typeInto("12", "12.", 3, "en-GB", "IDR").draft).toBe("12");
    expect(typeInto("", "9".repeat(17), 17, "en-GB", "IDR").draft).toBe("9".repeat(16));
  });

  it("collapses leading zeros and moves the caret with them", () => {
    expect(typeInto("0", "07", 2, "en-GB", "IDR")).toEqual({ draft: "7", significant: 1 });
  });
});

describe("roundMoney", () => {
  it("rounds half up to the currency's decimals, as formatMoney does", () => {
    expect(roundMoney("1000.5", "IDR")).toBe("1001");
    expect(roundMoney("1000.4", "IDR")).toBe("1000");
    expect(roundMoney("12.345", "USD")).toBe("12.35");
    expect(roundMoney("9000000.0000", "IDR")).toBe("9000000");
    expect(roundMoney("999.995", "USD")).toBe("1000");
    expect(roundMoney("-0.0004", "IDR")).toBe("0");
  });

  it("returns what is not a plain decimal as given", () => {
    expect(roundMoney("oops", "IDR")).toBe("oops");
  });
});

describe("currencySymbol", () => {
  it("is Rp for IDR and the code otherwise", () => {
    expect(currencySymbol("IDR")).toBe("Rp");
    expect(currencySymbol("USD")).toBe("USD");
  });
});
