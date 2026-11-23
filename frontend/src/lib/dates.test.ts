import { describe, expect, it } from "vitest";
import { formatDate, formatDateRange } from "./dates";

describe("formatDate", () => {
  it("is long style with the month in full", () => {
    expect(formatDate("2026-09-03", "en-GB")).toBe("3 September 2026");
    expect(formatDate("2026-08-17", "id-ID")).toBe("17 Agustus 2026");
    expect(formatDate("2026-11-01", "en-GB", false)).toBe("1 November");
  });

  it("knows month lengths and leap years", () => {
    expect(formatDate("2028-02-29", "en-GB")).toBe("29 February 2028");
    expect(formatDate("2026-02-29", "en-GB")).toBe("2026-02-29");
  });

  it("renders anything that is not a real date as given", () => {
    for (const bad of ["", "soon", "2026-13-01", "2026-00-10", "2026-04-31", "2026-9-3"]) {
      expect(formatDate(bad, "en-GB")).toBe(bad);
    }
  });

  it("falls back to en-GB for a locale it does not know", () => {
    expect(formatDate("2026-09-03", "fr-FR")).toBe("3 September 2026");
  });
});

describe("formatDateRange", () => {
  it("says the year once when both ends share it", () => {
    expect(formatDateRange("2026-11-01", "2026-11-30", "en-GB")).toEqual({
      start: "1 November",
      end: "30 November 2026",
    });
  });

  it("says it twice across a year boundary", () => {
    expect(formatDateRange("2026-12-25", "2027-01-24", "en-GB")).toEqual({
      start: "25 December 2026",
      end: "24 January 2027",
    });
  });
});
