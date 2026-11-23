import type { ReactNode } from "react";

// A read-only figure: a label and its amount, in a ListCard. The amount is
// tabular-nums and medium weight, the name regular (docs/agents/ui-conventions.md,
// Weight marks the figure; Uruni). ListRow is a tappable row; this is not.
// PROPOSAL pending the phone preview, #130 C15.
export function FigureRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <li className="flex min-h-11 items-center gap-3 px-3 py-2">
      <span className="mr-auto">{label}</span>
      <span className="font-medium tabular-nums">{children}</span>
    </li>
  );
}
