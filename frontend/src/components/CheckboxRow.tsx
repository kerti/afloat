import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// A checkbox is 20px inside a label at least 44px tall; "Show inactive …" is a
// checkbox, never a switch (Balances ShowInactiveToggle, github.com/kerti/balances-v2).
export function CheckboxRow({
  checked,
  onChange,
  className,
  children,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  /** Layout only: "justify-start" for a row under a field (Capture's Refund). */
  className?: string;
  children: ReactNode;
}) {
  return (
    <label
      className={cn(
        "flex min-h-11 items-center justify-end gap-2 text-sm text-muted-foreground",
        className,
      )}
    >
      <input
        type="checkbox"
        className="size-5"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      {children}
    </label>
  );
}
