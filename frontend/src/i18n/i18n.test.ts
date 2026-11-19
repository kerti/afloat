import { describe, expect, it } from "vitest";
import { browserLocale } from "./index";

describe("browserLocale", () => {
  it.each([
    [["id-ID"], "id-ID"],
    [["id"], "id-ID"],
    [["en-US", "id-ID"], "en-GB"],
    [["fr-FR", "id"], "id-ID"],
    [["fr-FR"], "en-GB"],
    [[], "en-GB"],
  ])("%j -> %s", (langs, want) => {
    expect(browserLocale(langs)).toBe(want);
  });
});
