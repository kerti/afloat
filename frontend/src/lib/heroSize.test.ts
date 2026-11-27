import { describe, expect, it } from "vitest";
import { heroSize } from "./heroSize";
import { formatAllowance } from "./money";

describe("heroSize", () => {
  it("is 36px for an everyday figure and steps down as the figure grows", () => {
    expect(heroSize("Rp0")).toBe("text-4xl");
    expect(heroSize("Rp176.470")).toBe("text-4xl");
    expect(heroSize("Rp999.999.999")).toBe("text-4xl");
    expect(heroSize("Rp9.999.999.999")).toBe("text-[32px]");
    expect(heroSize("Rp99.999.999.999")).toBe("text-[32px]");
    expect(heroSize("Rp999.999.999.999")).toBe("text-[28px]");
    expect(heroSize("Rp9.999.999.999.999")).toBe("text-[28px]"); // 19 characters
    expect(heroSize("Rp99.999.999.999.999")).toBe("text-2xl"); // 20
  });

  it("holds the worst case, 16 integer digits, and its negative, at 24px", () => {
    const worst = formatAllowance("9999999999999999.9999", "IDR", "id-ID");
    expect(worst).toBe("Rp9.999.999.999.999.999");
    expect(worst).toHaveLength(23);
    expect(heroSize(worst)).toBe("text-2xl");
    expect(heroSize("-" + worst)).toBe("text-xl");
  });

  it("counts a minus sign, since it takes room", () => {
    expect(heroSize("-Rp99.999.999")).toBe("text-4xl");
    expect(heroSize("-Rp999.999.999")).toBe("text-[32px]");
  });

  it("falls to the title's size for a figure wider than any rupiah one", () => {
    expect(heroSize("USD 9,999,999,999,999,999.00")).toBe("text-xl");
  });
});
