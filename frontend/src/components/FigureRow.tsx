import type { ReactNode } from "react";

// A read-only figure: a label and its amount, in a ListCard. The amount is
// tabular-nums and medium weight, the name regular (docs/agents/ui-conventions.md,
// Weight marks the figure; Uruni). ListRow is a tappable row; this is not. A
// `badge` (Inactive) sits beside the label; `muted` is for the words that stand
// where an amount would ("No allocation"), regular weight and not a figure.
export function FigureRow({
  label,
  badge,
  muted,
  children,
}: {
  label: string;
  badge?: ReactNode;
  muted?: boolean;
  children?: ReactNode;
}) {
  return (
    <li className="flex min-h-11 items-center gap-3 px-3 py-2">
      <span className={badge ? "" : "mr-auto"}>{label}</span>
      {badge && <span className="mr-auto">{badge}</span>}
      <span className={muted ? "text-muted-foreground" : "font-medium tabular-nums"}>
        {children}
      </span>
    </li>
  );
}
