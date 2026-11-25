import { useState } from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TimeField } from "@/components/TimeField";
import enGB from "@/locales/en-GB.json";

function Harness({ initial = "" }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <TimeField value={value} onChange={setValue} />
      <output data-testid="value">{value}</output>
    </>
  );
}

describe("TimeField", () => {
  it("is a quiet 'Add time' trigger while unset, with no clear button", () => {
    render(<Harness />);
    expect(screen.getByRole("button", { name: enGB.dates.addTime })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: enGB.dates.clearTime })).not.toBeInTheDocument();
  });

  it("offers the 24 hours, then 5-minute steps, and sets HH:MM", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: enGB.dates.addTime }));
    expect(screen.getByText(enGB.dates.hour)).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /^\d\d$/ })).toHaveLength(24);
    await user.click(screen.getByRole("button", { name: "07" }));
    expect(screen.getAllByRole("button", { name: /^:\d\d$/ })).toHaveLength(12);
    await user.click(screen.getByRole("button", { name: ":05" }));

    expect(screen.getByTestId("value")).toHaveTextContent("07:05");
    expect(screen.getByRole("button", { name: "Time, 07:05" })).toHaveTextContent("07:05");
  });

  it("goes back from the minutes to the hours", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: enGB.dates.addTime }));
    await user.click(screen.getByRole("button", { name: "19" }));
    const back = screen.getByRole("button", { name: enGB.dates.backToHours });
    expect(back).toHaveTextContent("19");
    await user.click(back);

    expect(screen.getAllByRole("button", { name: /^\d\d$/ })).toHaveLength(24);
  });

  it("clears back to unset", async () => {
    const user = userEvent.setup();
    render(<Harness initial="19:05" />);

    await user.click(screen.getByRole("button", { name: enGB.dates.clearTime }));

    expect(screen.getByTestId("value")).toHaveTextContent("");
    expect(screen.getByRole("button", { name: enGB.dates.addTime })).toBeInTheDocument();
  });
});
