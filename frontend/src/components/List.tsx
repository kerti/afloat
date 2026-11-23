import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// A divided list of tappable rows. Tapping a row opens its edit dialog, so a
// row has no chevron (docs/agents/ui-conventions.md, Editing surfaces).

export function ListCard({ children }: { children: ReactNode }) {
  return <ul className="flex flex-col divide-y rounded-md border">{children}</ul>;
}

/** One full-width button, at least 44px, naming its record. Its title is
 * regular weight (muted when the record is inactive); a badge sits inside it.
 * `trailing` is an amount at the row's end, as FigureRow shows one: tabular-nums
 * and medium, or, with `trailingMuted`, the words that stand where an amount
 * would, muted and regular. PROPOSAL pending the phone preview, #130 PR 3. */
export function ListRow({
  label,
  onClick,
  muted,
  badge,
  trailing,
  trailingMuted,
  children,
}: {
  label: string;
  onClick: () => void;
  muted?: boolean;
  badge?: ReactNode;
  trailing?: ReactNode;
  trailingMuted?: boolean;
  children: ReactNode;
}) {
  const hasTrailing = trailing !== undefined;
  return (
    <li>
      <button
        type="button"
        aria-label={label}
        onClick={onClick}
        className="flex min-h-11 w-full items-center gap-3 px-3 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span
          className={cn(muted && "text-muted-foreground", !(hasTrailing && badge) && "mr-auto")}
        >
          {children}
        </span>
        {hasTrailing && badge ? <span className="mr-auto">{badge}</span> : badge}
        {hasTrailing && (
          <span className={trailingMuted ? "text-muted-foreground" : "font-medium tabular-nums"}>
            {trailing}
          </span>
        )}
      </button>
    </li>
  );
}
