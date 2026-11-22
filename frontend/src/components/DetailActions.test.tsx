import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { DetailActions } from "@/components/DetailActions";
import { fullRow } from "@/lib/fullRow";

describe("fullRow", () => {
  it("is true only for the odd one out, the last of an odd count", () => {
    expect([0, 1].map((i) => fullRow(2, i))).toEqual([false, false]);
    expect([0, 1, 2].map((i) => fullRow(3, i))).toEqual([false, false, true]);
    expect(fullRow(1, 0)).toBe(true);
    expect([0, 1, 2, 3].map((i) => fullRow(4, i))).toEqual([false, false, false, false]);
  });
});

describe("DetailActions", () => {
  it("renders in the given order in a two-column grid, destructive last", () => {
    render(
      <DetailActions
        actions={[
          { key: "a", label: "Deactivate", onClick: () => {} },
          { key: "b", label: "Delete", tone: "destructive", onClick: () => {} },
        ]}
      />,
    );
    const buttons = screen.getAllByRole("button");
    expect(buttons.map((b) => b.textContent)).toEqual(["Deactivate", "Delete"]);
    expect(buttons[1]).toHaveClass("text-destructive");
    expect(buttons[0]!.parentElement).toHaveClass("grid-cols-2", "border-t");
  });

  it("lets a lone action span both columns, and renders nothing when empty", () => {
    const { container, rerender } = render(
      <DetailActions
        actions={[{ key: "d", label: "Delete", tone: "destructive", onClick: () => {} }]}
      />,
    );
    expect(screen.getByRole("button", { name: "Delete" })).toHaveClass("col-span-2");
    rerender(<DetailActions actions={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
