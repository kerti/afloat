import { afterEach, describe, expect, it, vi } from "vitest";
import { uuidv7 } from "./uuid";

// The pattern the contract puts on CreatePocketRequest.id.
const contractPattern =
  /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$/;

afterEach(() => vi.useRealTimers());

describe("uuidv7", () => {
  it("is version 7 with the RFC variant and matches the contract's pattern", () => {
    for (let i = 0; i < 200; i++) {
      const id = uuidv7();
      expect(id).toMatch(contractPattern);
      expect(id[14]).toBe("7");
      expect("89ab").toContain(id[19]);
    }
  });

  it("is never a v4", () => {
    expect(uuidv7()[14]).not.toBe("4");
    expect(crypto.randomUUID()).not.toMatch(contractPattern);
  });

  it("carries the millisecond timestamp in its first 48 bits", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-22T10:00:00.123Z"));
    const id = uuidv7();
    expect(Number.parseInt(id.replace(/-/g, "").slice(0, 12), 16)).toBe(Date.now());
  });

  it("sorts by time across calls", () => {
    vi.useFakeTimers();
    const ids: string[] = [];
    for (let i = 0; i < 20; i++) {
      vi.setSystemTime(new Date(1_800_000_000_000 + i * 1000));
      ids.push(uuidv7());
    }
    expect([...ids].sort()).toEqual(ids);
  });

  it("differs between calls in the same millisecond", () => {
    vi.useFakeTimers();
    expect(new Set(Array.from({ length: 50 }, uuidv7)).size).toBe(50);
  });
});
