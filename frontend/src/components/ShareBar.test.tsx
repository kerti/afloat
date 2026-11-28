import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ShareBar } from "./ShareBar";

// The track is 125% wide, so 100% sits at 80% of it.
describe("ShareBar", () => {
  it("fills the share of the allocation, with nothing over", () => {
    render(<ShareBar spent="750000.0000" allocation="1500000.0000" />);
    expect(screen.getByTestId("share-bar")).toHaveAttribute("aria-hidden", "true");
    expect(screen.getByTestId("share-bar-fill").style.width).toBe("40%");
    expect(screen.queryByTestId("share-bar-over")).toBeNull();
  });

  it("draws the share past 100% in the attention colour", () => {
    render(<ShareBar spent="1650000" allocation="1500000" />);
    expect(screen.getByTestId("share-bar-fill").style.width).toBe("80%");
    const over = screen.getByTestId("share-bar-over");
    expect(over.className).toContain("bg-attention");
    expect(over.style.left).toBe("80%");
    expect(parseFloat(over.style.width)).toBeCloseTo(8);
  });

  it("stops at the end of the track", () => {
    render(<ShareBar spent="9000000" allocation="1500000" />);
    expect(screen.getByTestId("share-bar-over").style.width).toBe("20%");
  });

  it("draws nothing against no allocation, and nothing below zero", () => {
    const { container, rerender } = render(<ShareBar spent="100" allocation="0" />);
    expect(container).toBeEmptyDOMElement();
    rerender(<ShareBar spent="-100" allocation="1000" />);
    expect(screen.getByTestId("share-bar-fill").style.width).toBe("0%");
  });
});
