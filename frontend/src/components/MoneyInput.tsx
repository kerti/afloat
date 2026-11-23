import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ComponentProps,
  type ClipboardEvent,
} from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import {
  caretAfter,
  currencyDecimals,
  currencySymbol,
  displayDraft,
  draftFromWire,
  draftToWire,
  parseMoney,
  typeInto,
} from "@/lib/money";

// A money field. It groups digits live in the app locale and keeps the caret
// after the same digit when separators come and go; a typed "." or "," is the
// decimal point; paste goes through a strict locale parser and is ignored when
// it does not parse. The value it holds and reports is the wire string
// ("9000000", "1500.5"), never a float; "" is empty. (docs/agents/
// ui-conventions.md, Money input; Uruni web AmountInput #749, Balances
// NumericInput ADR-0056.) PROPOSAL pending the phone preview, #130 C15.
export function MoneyInput({
  value,
  onChange,
  currency,
  className,
  ...props
}: Omit<ComponentProps<typeof Input>, "value" | "onChange" | "type" | "inputMode"> & {
  value: string;
  onChange: (wire: string) => void;
  currency: string;
}) {
  const { i18n } = useTranslation();
  const locale = i18n.language;
  const generated = useId();
  const symbol = currencySymbol(currency);
  const symbolId = `${props.id ?? generated}-currency`;
  const ref = useRef<HTMLInputElement>(null);
  // The draft keeps a typed decimal point the wire value drops; a value that
  // changed from outside (Esc, a reset) wins over it.
  const [local, setLocal] = useState(() => draftFromWire(value));
  const [caret, setCaret] = useState<{ at: number } | null>(null);
  const draft = draftToWire(local) === value ? local : draftFromWire(value);
  const shown = displayDraft(draft, locale);

  useLayoutEffect(() => {
    if (caret && document.activeElement === ref.current) {
      ref.current!.setSelectionRange(caret.at, caret.at);
    }
  }, [caret]);

  function commit(next: string, at: number | "end") {
    setLocal(next);
    onChange(draftToWire(next));
    const display = displayDraft(next, locale);
    setCaret({ at: at === "end" ? display.length : at });
  }

  function onInput(e: React.ChangeEvent<HTMLInputElement>) {
    const { draft: next, significant } = typeInto(
      shown,
      e.target.value,
      e.target.selectionStart ?? e.target.value.length,
      locale,
      currency,
    );
    commit(next, caretAfter(displayDraft(next, locale), significant, locale));
  }

  function onPaste(e: ClipboardEvent<HTMLInputElement>) {
    e.preventDefault();
    const wire = parseMoney(e.clipboardData.getData("text"), locale, currency);
    if (wire !== null) commit(draftFromWire(wire), "end");
  }

  // The currency sits inside the field as an adornment, not part of the value.
  // It is hidden from the accessibility tree as a visual, and named instead as
  // the field's description (aria-describedby), so the label stays the field's
  // name and a screen reader still hears the currency.
  return (
    <div className={cn("relative", className)}>
      <span
        id={symbolId}
        aria-hidden="true"
        className="pointer-events-none absolute inset-y-0 left-3 flex items-center text-muted-foreground"
      >
        {symbol}
      </span>
      <Input
        {...props}
        ref={ref}
        type="text"
        inputMode={currencyDecimals(currency) === 0 ? "numeric" : "decimal"}
        autoComplete="off"
        value={shown}
        onChange={onInput}
        onPaste={onPaste}
        className="tabular-nums"
        style={{ paddingLeft: `calc(1.25rem + ${symbol.length}ch)` }}
        aria-describedby={[symbolId, props["aria-describedby"]].filter(Boolean).join(" ")}
      />
    </div>
  );
}
