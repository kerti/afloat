import "@testing-library/jest-dom/vitest";
import { afterAll, afterEach, beforeAll, expect } from "vitest";
import { cleanup } from "@testing-library/react";
import { server } from "./server";
import { i18n, initI18n } from "@/i18n";
// The boot script, as index.html loads it: it sets window.afloatAppearance,
// which the AppearanceProvider calls.
import "../../public/appearance-init.js";

initI18n("en-GB");

// sonner captures the pointer on a toast press; jsdom has no pointer capture.
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};
// Radix Select (components/ui/select.tsx) reads both when it opens.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.scrollIntoView ??= () => {};

// jsdom has no matchMedia; sonner reads it to follow the OS colour scheme.
window.matchMedia ??= (query: string) =>
  ({
    matches: false,
    media: query,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  }) as unknown as MediaQueryList;

// MSW's onUnhandledFrame: "error" only prints: the request then rejects, and
// the API client reports any rejected fetch as NETWORK, so a test missing a
// handler would pass on a quiet "Can't reach Afloat". Recording the request
// and failing in afterEach makes a missing handler a failed test.
const unhandled: string[] = [];
server.events.on("request:unhandled", ({ request }) => {
  unhandled.push(`${request.method} ${new URL(request.url).pathname}`);
});

beforeAll(() => server.listen({ onUnhandledFrame: "error" }));
afterEach(async () => {
  cleanup();
  // The appearance cache and attribute are device state; no test inherits them.
  window.localStorage.clear();
  document.documentElement.removeAttribute("data-appearance");
  server.resetHandlers();
  // A test that switched locale must not leak it into the next.
  await i18n.changeLanguage("en-GB");
  const missed = unhandled.splice(0);
  expect(missed, "requests with no MSW handler").toEqual([]);
});
afterAll(() => server.close());
