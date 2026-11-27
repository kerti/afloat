import "@testing-library/jest-dom/vitest";
import { afterAll, afterEach, beforeAll, expect } from "vitest";
import { cleanup, configure } from "@testing-library/react";
import { server } from "./server";
import { i18n, initI18n } from "@/i18n";
// The boot script, as index.html loads it: it sets window.afloatAppearance,
// which the AppearanceProvider calls.
import "../../public/appearance-init.js";

initI18n("en-GB");

// findBy* and waitFor wait for the fake network and a render, never for a clock,
// so the limit only matters when the machine is starved. Idle, the slowest
// first render here (the Settings screen, a cold first test in a file) takes
// about 0.2 s; with CPU contention (full suite beside other work, load above the
// core count) the same step took 1.3 to 2.4 s and tripped the 1 s default, with
// no change to the code under test. A passing test never waits this long.
configure({ asyncUtilTimeout: 8000 });

// sonner captures the pointer on a toast press; jsdom has no pointer capture.
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};
// Radix Select (components/ui/select.tsx) reads both when it opens.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.scrollIntoView ??= () => {};

// jsdom has no ResizeObserver and no layout; Recharts' ResponsiveContainer reads
// its box from both. The stub reports one phone-width box when it starts
// observing, as a real observer does, so a chart draws: tests read the data and
// the document's structure, never pixels.
globalThis.ResizeObserver ??= class {
  private readonly callback: ResizeObserverCallback;
  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
  }
  observe(target: Element) {
    queueMicrotask(() =>
      this.callback(
        [{ target, contentRect: { width: 320, height: 220 } } as unknown as ResizeObserverEntry],
        this,
      ),
    );
  }
  unobserve() {}
  disconnect() {}
};

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
