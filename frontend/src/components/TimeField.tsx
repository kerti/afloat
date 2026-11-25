import { ArrowLeft, Clock, X } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Popover } from "radix-ui";
import { Button } from "@/components/ui/button";
import { pickerPopover } from "@/components/pickerStyles";

// R15. No precedent in Uruni or Balances. An hour
// grid (00-23, 4 x 6), then a minute grid in 5-minute steps (4 x 3), 24-hour in
// both locales. Never pre-filled; untouched, the Expense has no time.

const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
const MINUTES = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, "0"));

export function TimeField({
  value,
  onChange,
  disabled,
}: {
  disabled?: boolean;
  value: string;
  onChange: (hhmm: string) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [hour, setHour] = useState<string | null>(null);

  function openChange(next: boolean) {
    setOpen(next);
    if (next) setHour(null);
  }

  return (
    <div className="flex shrink-0 items-center">
      <Popover.Root open={open} onOpenChange={openChange}>
        <Popover.Trigger
          disabled={disabled}
          aria-label={value ? t("dates.time", { time: value }) : undefined}
          className={
            value
              ? "flex h-11 items-center gap-2 rounded-md border border-input px-3 text-base tabular-nums outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
              : "inline-flex min-h-11 items-center gap-1.5 rounded-md px-2 text-sm text-muted-foreground underline underline-offset-4 outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          }
        >
          {value ? (
            <>
              <Clock aria-hidden="true" className="size-4 text-muted-foreground" />
              {value}
            </>
          ) : (
            t("dates.addTime")
          )}
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            align="end"
            aria-label={t("dates.timePicker")}
            sideOffset={6}
            collisionPadding={16}
            className={pickerPopover}
          >
            <div className="mb-2 flex min-h-11 items-center justify-between text-sm text-muted-foreground">
              {hour === null ? (
                <span>{t("dates.hour")}</span>
              ) : (
                <Button
                  type="button"
                  variant="ghost"
                  aria-label={t("dates.backToHours")}
                  onClick={() => setHour(null)}
                >
                  <ArrowLeft aria-hidden="true" className="size-4" weight="regular" />
                  <span className="tabular-nums">{hour}</span>
                </Button>
              )}
            </div>
            <div className="grid grid-cols-4 gap-1">
              {(hour === null ? HOURS : MINUTES).map((v) => (
                <Button
                  key={v}
                  type="button"
                  variant="ghost"
                  className="w-14 tabular-nums"
                  onClick={() => {
                    if (hour === null) setHour(v);
                    else {
                      onChange(`${hour}:${v}`);
                      setOpen(false);
                    }
                  }}
                >
                  {hour === null ? v : `:${v}`}
                </Button>
              ))}
            </div>
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
      {value && (
        <Button
          type="button"
          variant="ghost"
          className="w-11 px-0"
          aria-label={t("dates.clearTime")}
          disabled={disabled}
          onClick={() => onChange("")}
        >
          <X aria-hidden="true" className="size-4" weight="regular" />
        </Button>
      )}
    </div>
  );
}
