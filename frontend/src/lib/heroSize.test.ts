import { describe, expect, it } from "vitest";
import { heroSize } from "./heroSize";
import { formatAllowance } from "./money";

describe("heroSize", () => {
  it("is largest for an everyday figure and steps down as the figure grows", () => {
    expect(heroSize("Rp0")).toBe("text-[9.9cqw]");
    expect(heroSize("Rp176.470")).toBe("text-[9.9cqw]");
    expect(heroSize("-Rp176.470")).toBe("text-[9.9cqw]"); // 10 characters
    expect(heroSize("Rp1.176.470")).toBe("text-[8.2cqw]");
    expect(heroSize("Rp999.999.999")).toBe("text-[8.2cqw]"); // 13
    expect(heroSize("Rp9.999.999.999")).toBe("text-[7cqw]");
    expect(heroSize("Rp99.999.999.999")).toBe("text-[7cqw]"); // 16
    expect(heroSize("Rp9.999.999.999.999")).toBe("text-[5.6cqw]"); // 19
    expect(heroSize("Rp99.999.999.999.999")).toBe("text-[4.6cqw]"); // 20
  });

  it("holds the worst case, 16 integer digits, and its negative, at one step", () => {
    const worst = formatAllowance("9999999999999999.9999", "IDR", "id-ID");
    expect(worst).toBe("Rp9.999.999.999.999.999");
    expect(worst).toHaveLength(23);
    expect(heroSize(worst)).toBe("text-[4.6cqw]");
    expect(heroSize("-" + worst)).toBe("text-[4.6cqw]");
  });

  it("counts a minus sign, since it takes room", () => {
    expect(heroSize("-Rp99.999.999")).toBe("text-[8.2cqw]");
    expect(heroSize("-Rp999.999.999")).toBe("text-[7cqw]");
  });

  it("falls to the smallest size for a figure wider than any rupiah one", () => {
    expect(heroSize("USD 9,999,999,999,999,999.00")).toBe("text-[4cqw]");
  });
});
