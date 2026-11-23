import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { i18n } from "@/i18n";
import { MoneyInput } from "./MoneyInput";

function Harness({
  currency = "IDR",
  initial = "",
  onWire,
}: {
  currency?: string;
  initial?: string;
  onWire?: (w: string) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <MoneyInput
      aria-label="Amount"
      currency={currency}
      value={value}
      onChange={(w) => {
        setValue(w);
        onWire?.(w);
      }}
    />
  );
}

const field = () => screen.getByLabelText("Amount") as HTMLInputElement;

describe("MoneyInput", () => {
  it("groups digits as they are typed and reports the wire string", async () => {
    const onWire = vi.fn();
    const user = userEvent.setup();
    render(<Harness onWire={onWire} />);
    await user.type(field(), "9000000");
    expect(field()).toHaveValue("9,000,000");
    expect(onWire).toHaveBeenLastCalledWith("9000000");
  });

  it("groups in the app locale", async () => {
    await i18n.changeLanguage("id-ID");
    const user = userEvent.setup();
    render(<Harness currency="USD" />);
    await user.type(field(), "1234,5");
    expect(field()).toHaveValue("1.234,5");
  });

  it("takes a typed . as the decimal point, keeping it while nothing follows", async () => {
    const onWire = vi.fn();
    const user = userEvent.setup();
    render(<Harness currency="USD" onWire={onWire} />);
    await user.type(field(), "1234.");
    expect(field()).toHaveValue("1,234.");
    expect(onWire).toHaveBeenLastCalledWith("1234");
    await user.type(field(), "567");
    expect(field()).toHaveValue("1,234.56");
    expect(onWire).toHaveBeenLastCalledWith("1234.56");
  });

  it("keeps the caret after the same digit when separators come and go", async () => {
    const user = userEvent.setup();
    render(<Harness initial="999" />);
    expect(field()).toHaveValue("999");
    field().focus();
    // Between the first and second 9: "9|99".
    field().setSelectionRange(1, 1);
    await user.keyboard("9");
    expect(field()).toHaveValue("9,999");
    expect(field().selectionStart).toBe(3);
    await user.keyboard("1");
    expect(field()).toHaveValue("99,199");
    expect(field().selectionStart).toBe(4);
  });

  it("accepts a strict paste and ignores one that does not parse", async () => {
    const user = userEvent.setup();
    render(<Harness currency="USD" initial="5" />);
    field().focus();
    await user.paste("1,234.50");
    expect(field()).toHaveValue("1,234.5");
    await user.paste("1.234,50");
    expect(field()).toHaveValue("1,234.5");
    await user.paste("Rp 9,000");
    expect(field()).toHaveValue("1,234.5");
  });

  it("is numeric for a zero-decimal currency and decimal otherwise", () => {
    const { rerender } = render(
      <MoneyInput aria-label="Amount" currency="IDR" value="" onChange={() => {}} />,
    );
    expect(field()).toHaveAttribute("inputmode", "numeric");
    rerender(<MoneyInput aria-label="Amount" currency="USD" value="" onChange={() => {}} />);
    expect(field()).toHaveAttribute("inputmode", "decimal");
  });

  it("refuses a decimal point for IDR and extra decimals for USD", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<Harness currency="IDR" />);
    await user.type(field(), "12.5");
    expect(field()).toHaveValue("125");
    unmount();
    render(<Harness currency="USD" />);
    await user.type(field(), "1.234");
    expect(field()).toHaveValue("1.23");
  });

  it("follows a value changed from outside", () => {
    const { rerender } = render(
      <MoneyInput aria-label="Amount" currency="IDR" value="5000" onChange={() => {}} />,
    );
    expect(field()).toHaveValue("5,000");
    rerender(<MoneyInput aria-label="Amount" currency="IDR" value="" onChange={() => {}} />);
    expect(field()).toHaveValue("");
    fireEvent.change(field(), { target: { value: "7" } });
  });
});
