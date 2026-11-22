import type { ReactNode } from "react";

// A divided list of tappable rows. Tapping a row opens its edit dialog, so a
// row has no chevron (docs/agents/ui-conventions.md, Editing surfaces).

export function ListCard({ children }: { children: ReactNode }) {
  return <ul className="flex flex-col divide-y rounded-md border">{children}</ul>;
}

/** One full-width button, at least 44px, naming its record. Its title is
 * regular weight (muted when the record is inactive); a badge sits inside it. */
export function ListRow({
  label,
  onClick,
  muted,
  badge,
  children,
}: {
  label: string;
  onClick: () => void;
  muted?: boolean;
  badge?: ReactNode;
  children: ReactNode;
}) {
  return (
    <li>
      <button
        type="button"
        aria-label={label}
        onClick={onClick}
        className="flex min-h-11 w-full items-center gap-3 px-3 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className={muted ? "mr-auto text-muted-foreground" : "mr-auto"}>{children}</span>
        {badge}
      </button>
    </li>
  );
}
