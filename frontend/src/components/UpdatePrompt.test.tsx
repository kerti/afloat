import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { clearOfTabBar } from "@/lib/tabBar";
import { UpdatePrompt } from "./UpdatePrompt";

vi.mock("virtual:pwa-register/react", () => ({
  useRegisterSW: () => ({
    needRefresh: [true, () => {}],
    offlineReady: [false, () => {}],
    updateServiceWorker: async () => {},
  }),
}));

// #153: the prompt sat under the fixed tab bar (z-10), so neither button could
// be tapped. It now clears the bar by the toasts' offset and stacks above it.
describe("UpdatePrompt", () => {
  it("sits clear of the tab bar and above it", () => {
    render(<UpdatePrompt />);
    const prompt = screen.getByRole("status");
    expect(prompt.style.bottom).toBe(clearOfTabBar);
    expect(prompt).toHaveClass("z-20");
  });
});
