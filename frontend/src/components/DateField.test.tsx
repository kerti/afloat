import { useState } from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DateField } from "@/components/DateField";

function Harness({ initial = "" }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <DateField id="d" value={value} onChange={setValue} today="2026-11-25" />
      <output data-testid="value">{value}</output>
    </>
  );
}

describe("DateField", () => {
  it("reads Today while untouched, and sends nothing", () => {
    render(<Harness />);
    expect(screen.getByRole("button")).toHaveTextContent("Today");
    expect(screen.getByTestId("value")).toHaveTextContent("");
  });

  it("reads Today when the value is today's date", () => {
    render(<Harness initial="2026-11-25" />);
    expect(screen.getByRole("button")).toHaveTextContent("Today");
  });

  it("reads a picked date long-style and closes on the pick", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button"));
    await user.click(await screen.findByRole("button", { name: /20(th)? November/ }));

    expect(screen.getByTestId("value")).toHaveTextContent("2026-11-20");
    expect(screen.getByRole("button")).toHaveTextContent("20 November 2026");
    expect(screen.queryByRole("grid")).not.toBeInTheDocument();
  });

  it("marks today from the prop, not the device", async () => {
    const user = userEvent.setup();
    render(<Harness initial="2026-11-20" />);

    await user.click(screen.getByRole("button"));

    const grid = await screen.findByRole("grid");
    expect(grid.querySelector("[data-today]")).toHaveTextContent("25");
    expect(grid.querySelector("[data-selected]")).toHaveTextContent("20");
  });
});
