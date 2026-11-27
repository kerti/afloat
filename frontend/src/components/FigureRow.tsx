import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// A read-only figure: a label and its amount, in a ListCard. The amount is
// tabular-nums and medium weight, the name regular (docs/agents/ui-conventions.md,
// Weight marks the figure; Uruni). ListRow is a tappable row; this is not. A
// `badge` (Inactive) sits beside the label, with `labelMuted` for the row of
// something inactive, as every list row mutes its title; `muted` is for the
// words that stand where an amount would ("No allocation"), regular weight and
// not a figure. A `detail` is a second, muted line right-aligned under the
// amount ("of Rp1.500.000"), so the figure and what it is of read together.
// Confirmed on the phone preview, #155 PR 7 (D7).
export function FigureRow({
  label,
  labelMuted,
  badge,
  detail,
  muted,
  children,
}: {
  label: string;
  labelMuted?: boolean;
  badge?: ReactNode;
  detail?: string;
  muted?: boolean;
  children?: ReactNode;
}) {
  const title = labelMuted ? "text-muted-foreground" : "";
  const amount = (
    <span className={muted ? "text-muted-foreground" : "font-medium tabular-nums"}>{children}</span>
  );
  if (detail !== undefined) {
    return (
      <li className="flex min-h-11 items-center gap-3 px-3 py-2">
        <span className={cn(badge ? "" : "mr-auto", title)}>{label}</span>
        {badge && <span className="mr-auto">{badge}</span>}
        <span className="flex flex-col items-end">
          {amount}
          <span className="text-sm text-muted-foreground tabular-nums">{detail}</span>
        </span>
      </li>
    );
  }
  return (
    <li className="flex min-h-11 items-center gap-3 px-3 py-2">
      <span className={cn(badge ? "" : "mr-auto", title)}>{label}</span>
      {badge && <span className="mr-auto">{badge}</span>}
      {amount}
    </li>
  );
}
