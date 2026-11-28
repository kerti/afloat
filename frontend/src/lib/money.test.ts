import { describe, expect, it } from "vitest";
import {
  canonicalMoney,
  compareMoney,
  currencyDecimals,
  displayDraft,
  draftFromWire,
  draftToWire,
  formatAllowance,
  formatMoney,
  formatMoneyCompact,
  formatPercent,
  percentOf,
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

describe("formatAllowance (toward minus infinity at the minor unit, #155 R11)", () => {
  it("cuts a positive amount, never rounds it up", () => {
    expect(formatAllowance("203448.2758", "IDR", "id-ID")).toBe("Rp203.448");
    expect(formatAllowance("203448.2758", "IDR", "en-GB")).toBe("Rp203,448");
    expect(formatAllowance("203448.9999", "IDR", "en-GB")).toBe("Rp203,448");
    expect(formatAllowance("0.9999", "IDR", "en-GB")).toBe("Rp0");
    // formatMoney is half-up and would say Rp1 here: the two differ on purpose.
    expect(formatMoney("0.9999", "IDR", "en-GB")).toBe("Rp1");
  });

  it("grows a negative amount away from zero, so it never reads as less than it is", () => {
    expect(formatAllowance("-0.5", "IDR", "en-GB")).toBe("-Rp1");
    expect(formatAllowance("-0.0001", "IDR", "en-GB")).toBe("-Rp1");
    expect(formatAllowance("-1.0001", "IDR", "id-ID")).toBe("-Rp2");
    expect(formatAllowance("-203448.2758", "IDR", "id-ID")).toBe("-Rp203.449");
    expect(formatAllowance("-2000.0000", "IDR", "id-ID")).toBe("-Rp2.000");
  });

  it("leaves an exact value exactly as it is", () => {
    expect(formatAllowance("200000.0000", "IDR", "id-ID")).toBe("Rp200.000");
    expect(formatAllowance("-200000.0000", "IDR", "id-ID")).toBe("-Rp200.000");
    expect(formatAllowance("5.0000", "IDR", "en-GB")).toBe("Rp5");
    expect(formatAllowance("7", "IDR", "en-GB")).toBe("Rp7");
  });

  it("never prints a negative zero", () => {
    for (const zero of ["0", "0.0000", "-0", "-0.0000"]) {
      expect(formatAllowance(zero, "IDR", "en-GB"), zero).toBe("Rp0");
    }
    expect(formatAllowance("-0.0000", "USD", "en-GB")).toBe("USD 0.00");
  });

  it("carries through the integer part", () => {
    expect(formatAllowance("-999.0001", "IDR", "en-GB")).toBe("-Rp1,000");
    expect(formatAllowance("-9999999999999999.5", "IDR", "en-GB")).toBe(
      "-Rp10,000,000,000,000,000",
    );
  });

  it("cuts at the currency's own minor unit: two decimals, three, none", () => {
    expect(formatAllowance("1234.5678", "USD", "en-GB")).toBe("USD 1,234.56");
    expect(formatAllowance("1234.5678", "USD", "id-ID")).toBe("USD 1.234,56");
    expect(formatAllowance("-1234.5601", "USD", "en-GB")).toBe("-USD 1,234.57");
    expect(formatAllowance("-1234.5600", "USD", "en-GB")).toBe("-USD 1,234.56");
    expect(formatAllowance("1.2349", "KWD", "en-GB")).toBe("KWD 1.234");
    expect(formatAllowance("-1.2341", "KWD", "en-GB")).toBe("-KWD 1.235");
    expect(formatAllowance("1234.9999", "JPY", "en-GB")).toBe("JPY 1,234");
    expect(formatAllowance("-1234.0001", "JPY", "en-GB")).toBe("-JPY 1,235");
  });

  it("holds 16 integer digits exactly, which a float cannot", () => {
    expect(formatAllowance("9999999999999999.9999", "IDR", "id-ID")).toBe(
      "Rp9.999.999.999.999.999",
    );
    expect(formatAllowance("9007199254740993.9999", "IDR", "en-GB")).toBe(
      "Rp9,007,199,254,740,993",
    );
    expect(formatAllowance("-9007199254740993.0001", "IDR", "en-GB")).toBe(
      "-Rp9,007,199,254,740,994",
    );
  });

  it("renders a bad input as given, never NaN", () => {
    for (const bad of ["", "abc", "1e6", "1,000", "NaN"]) {
      expect(formatAllowance(bad, "IDR", "en-GB")).toBe(bad);
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

describe("compareMoney", () => {
  it("compares on the digits, padding ignored", () => {
    expect(compareMoney("9000000.0000", "9000000")).toBe(0);
    expect(compareMoney("9000000.0001", "9000000")).toBe(1);
    expect(compareMoney("8999999.9999", "9000000.0000")).toBe(-1);
    expect(compareMoney("0", "0.0000")).toBe(0);
    expect(compareMoney("0.0000", "0.0001")).toBe(-1);
  });
  it("is exact where a float is not", () => {
    expect(compareMoney("9999999999999999.0001", "9999999999999999.0000")).toBe(1);
    expect(compareMoney("9999999999999999.0000", "9999999999999999")).toBe(0);
    expect(compareMoney("10", "9")).toBe(1);
  });
  it("handles signs and refuses what is not a decimal", () => {
    expect(compareMoney("-1.0000", "0")).toBe(-1);
    expect(compareMoney("-2", "-1")).toBe(-1);
    expect(compareMoney("1e3", "1")).toBeNull();
    expect(compareMoney("", "1")).toBeNull();
  });
});

describe("formatMoneyCompact", () => {
  it("is the symbol and the locale's compact words, for an axis tick", () => {
    expect(formatMoneyCompact(2400000, "IDR", "en-GB")).toBe("Rp2.4m");
    expect(formatMoneyCompact(2400000, "IDR", "id-ID")).toBe("Rp2,4\u00a0jt");
    expect(formatMoneyCompact(800000, "IDR", "en-GB")).toBe("Rp800k");
    expect(formatMoneyCompact(0, "IDR", "en-GB")).toBe("Rp0");
    expect(formatMoneyCompact(-1500, "IDR", "en-GB")).toBe("-Rp1.5k");
    expect(formatMoneyCompact(1200, "USD", "en-GB")).toBe("USD\u00a01.2k");
  });
});

describe("percentOf", () => {
  it.each([
    ["1500000.0000", "2000000.0000", 75],
    ["1999999.9999", "2000000", 99],
    ["2000000", "2000000.0000", 100],
    ["1200000", "1000000", 120],
    ["40000", "1500000", 2],
    ["0", "1000", 0],
    ["-5000", "1500000", 0],
    ["-50000", "100000", -50],
  ])("%s of %s is %d%%, toward zero, on the digits", (part, whole, pct) => {
    expect(percentOf(part, whole)).toBe(pct);
  });

  it.each([
    ["100", "0"],
    ["100", "-5"],
    ["x", "100"],
  ])("%s of %s has no percent", (part, whole) => {
    expect(percentOf(part, whole)).toBeNull();
  });
});

describe("formatPercent", () => {
  it("groups as the locale does", () => {
    expect(formatPercent(1234, "en-GB")).toBe("1,234%");
    expect(formatPercent(1234, "id-ID")).toBe("1.234%");
  });
});
