import { useState } from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SearchSelect } from "@/components/ui/search-select";

const options = [
  { value: "IDR", label: "IDR · Indonesian Rupiah" },
  { value: "USD", label: "USD · US Dollar" },
  { value: "EUR", label: "EUR · Euro" },
];

function Harness() {
  const [value, setValue] = useState("IDR");
  return (
    <>
      <label htmlFor="c">Currency</label>
      <SearchSelect
        id="c"
        value={value}
        onValueChange={setValue}
        options={options}
        searchLabel="Search currencies"
        noMatch="No matching currency"
      />
    </>
  );
}

describe("SearchSelect", () => {
  it("shows the current label on a trigger named by its label", () => {
    render(<Harness />);
    expect(screen.getByRole("button", { name: "Currency" })).toHaveTextContent(
      "IDR · Indonesian Rupiah",
    );
  });

  it("filters as you type, on the code or the name, ignoring case and accents", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "Currency" }));
    expect(screen.getAllByRole("option")).toHaveLength(3);
    await user.type(screen.getByRole("combobox", { name: "Search currencies" }), "dOLLAR");

    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["USD · US Dollar"]);
  });

  it("says when nothing matches", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "Currency" }));
    await user.type(screen.getByRole("combobox", { name: "Search currencies" }), "zzz");

    expect(screen.queryByRole("option")).not.toBeInTheDocument();
    expect(screen.getByText("No matching currency")).toBeInTheDocument();
  });

  it("chooses with the arrow keys and Enter, and closes", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "Currency" }));
    await user.keyboard("{ArrowDown}{Enter}");

    expect(screen.getByRole("button", { name: "Currency" })).toHaveTextContent("USD · US Dollar");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("opens on the current value, marks it, and Esc leaves it unchanged", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "Currency" }));
    expect(screen.getByRole("option", { name: /IDR/ })).toHaveAttribute("aria-selected", "true");
    await user.keyboard("{ArrowDown}{Escape}");

    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Currency" })).toHaveTextContent("IDR");
  });

  it("chooses by tap", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "Currency" }));
    await user.click(screen.getByRole("option", { name: /EUR/ }));

    expect(screen.getByRole("button", { name: "Currency" })).toHaveTextContent("EUR · Euro");
  });
});
