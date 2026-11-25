import {
  CalendarDots,
  CaretDoubleLeft,
  CaretDoubleRight,
  CaretLeft,
  CaretRight,
} from "@phosphor-icons/react";
import { useState, type CSSProperties } from "react";
import { Popover } from "radix-ui";
import { DayPicker, useDayPicker, type MonthCaptionProps } from "react-day-picker";
import { enGB, id as idID } from "react-day-picker/locale";
import "react-day-picker/style.css";
import { useTranslation } from "react-i18next";
import { pickerPopover, pickerTrigger } from "@/components/pickerStyles";
import { Button } from "@/components/ui/button";
import { formatDate } from "@/lib/dates";

// Ported from Uruni web/src/components/DateField.tsx:
// the trigger reads the date as the app writes it, a tap opens a calendar in a
// popover. Today is the server's (`me.today`), never the device's.

const calendarStyle = {
  "--rdp-accent-color": "var(--primary)",
  "--rdp-accent-background-color": "var(--muted)",
  "--rdp-day-height": "44px",
  "--rdp-day-width": "44px",
  "--rdp-day_button-height": "42px",
  "--rdp-day_button-width": "42px",
} as CSSProperties;

function toDate(iso: string): Date | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  return m ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])) : undefined;
}

function toIso(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

// The calendar's header: "November 2026" between month arrows, with year arrows
// outside them. No native <select> anywhere (ui-conventions, Pickers).
function MonthCaption({ calendarMonth }: MonthCaptionProps) {
  const { t, i18n } = useTranslation();
  const { goToMonth, dayPickerProps } = useDayPicker();
  const date = calendarMonth.date;
  const shift = (months: number) =>
    goToMonth(new Date(date.getFullYear(), date.getMonth() + months, 1));
  const first = dayPickerProps.startMonth;
  const last = dayPickerProps.endMonth;
  const before = (months: number) =>
    first !== undefined && new Date(date.getFullYear(), date.getMonth() + months, 1) < first;
  const after = (months: number) =>
    last !== undefined && new Date(date.getFullYear(), date.getMonth() + months, 1) > last;
  const arrows: [string, number, typeof CaretLeft][] = [
    [t("dates.previousYear"), -12, CaretDoubleLeft],
    [t("dates.previousMonth"), -1, CaretLeft],
  ];
  const arrowsAfter: [string, number, typeof CaretLeft][] = [
    [t("dates.nextMonth"), 1, CaretRight],
    [t("dates.nextYear"), 12, CaretDoubleRight],
  ];
  const arrow = ([name, by, Arrow]: [string, number, typeof CaretLeft]) => (
    <Button
      key={by}
      type="button"
      variant="ghost"
      className="w-11 px-0"
      aria-label={name}
      disabled={by < 0 ? before(by) : after(by)}
      onClick={() => shift(by)}
    >
      <Arrow aria-hidden="true" className="size-4" weight="regular" />
    </Button>
  );
  return (
    <div className="flex items-center justify-between">
      <div className="flex">{arrows.map(arrow)}</div>
      <span
        role="status"
        aria-live="polite"
        className="min-w-0 truncate text-center text-base font-medium"
      >
        {new Intl.DateTimeFormat(i18n.language, { month: "long", year: "numeric" }).format(date)}
      </span>
      <div className="flex">{arrowsAfter.map(arrow)}</div>
    </div>
  );
}

export function DateField({
  id,
  value,
  onChange,
  today,
  error,
  disabled,
}: {
  disabled?: boolean;
  id?: string;
  /** The id of the field's error message, when it has one. */
  error?: string;
  /** "YYYY-MM-DD", or "" for untouched: the server dates it (R5). */
  value: string;
  onChange: (iso: string) => void;
  today: string;
}) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const shown = value || today;
  const label = shown === today ? t("dates.today") : formatDate(shown, i18n.language);
  const name = t("fields.date");
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        id={id}
        disabled={disabled}
        aria-label={`${name}, ${label}`}
        aria-invalid={error ? true : undefined}
        aria-describedby={error}
        className={pickerTrigger}
      >
        <span className="truncate">{label}</span>
        <CalendarDots aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="start"
          sideOffset={6}
          collisionPadding={16}
          aria-label={name}
          className={pickerPopover}
        >
          <DayPicker
            mode="single"
            required
            locale={i18n.language === "id-ID" ? idID : enGB}
            hideNavigation
            components={{ MonthCaption }}
            selected={toDate(shown)}
            today={toDate(today)}
            defaultMonth={toDate(shown)}
            startMonth={new Date(2000, 0)}
            endMonth={new Date(toDate(today)!.getFullYear() + 5, 11)}
            onSelect={(d) => {
              onChange(toIso(d));
              setOpen(false);
            }}
            className="text-base"
            style={calendarStyle}
          />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
