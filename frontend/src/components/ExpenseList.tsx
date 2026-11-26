import { CaretRight } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { SubsectionLabel } from "@/components/Text";
import { Button } from "@/components/ui/button";

// The day heading is the SubsectionLabel over a bordered divided list; the row
// is two lines (Uruni web/src/screens/Home.tsx purpose row); Load more is Uruni
// web/src/components/LoadMore.tsx. Ruled 25 Nov 2026, #140 PR 3b. A row that
// opens another screen is a full-width link ending in a chevron (Uruni
// TransactionList; ruled 25 Nov 2026, #140 PR 4).

const rowLink =
  "flex min-h-11 w-full items-center gap-3 px-3 py-2 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

function Chevron() {
  return (
    <CaretRight
      aria-hidden="true"
      weight="regular"
      className="size-4 shrink-0 text-muted-foreground"
    />
  );
}

export function DayGroup({ heading, children }: { heading: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <SubsectionLabel>{heading}</SubsectionLabel>
      <ul className="flex flex-col divide-y rounded-md border">{children}</ul>
    </section>
  );
}

/** Opens the Expense's edit route (`to`), with a chevron, or, with `onOpen`, a
 * dialog (the Recycle Bin's read-only one), without. Its accessible name is
 * `label`: the title and the amount ("Edit Rent, Rp3,000,000"). */
export function ExpenseRow({
  to,
  onOpen,
  label,
  title,
  detail,
  amount,
  muted,
}: {
  to?: string;
  onOpen?: () => void;
  label: string;
  title: string;
  detail: string;
  amount: string;
  muted?: boolean;
}) {
  const body = (
    <>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={muted ? "truncate text-muted-foreground" : "truncate"}>{title}</span>
        <span className="truncate text-sm text-muted-foreground">{detail}</span>
      </span>
      <span className="shrink-0 font-medium tabular-nums">{amount}</span>
    </>
  );
  return (
    <li>
      {to !== undefined ? (
        <Link to={to} aria-label={label} className={rowLink}>
          {body}
          <Chevron />
        </Link>
      ) : (
        <button type="button" aria-label={label} onClick={onOpen} className={rowLink}>
          {body}
        </button>
      )}
    </li>
  );
}

/** A bordered link row ending in a chevron, above the list: the one that
 * narrows it ("Needs a category (3)"; no badge, no colour, #140 R8). */
export function FilterRow({ to, children }: { to: string; children: ReactNode }) {
  return (
    <ul className="flex flex-col rounded-md border">
      <li>
        <Link to={to} className={rowLink}>
          <span className="min-w-0 flex-1">{children}</span>
          <Chevron />
        </Link>
      </li>
    </ul>
  );
}

export function LoadMore({ loading, onClick }: { loading: boolean; onClick: () => void }) {
  const { t } = useTranslation();
  return (
    <Button type="button" variant="outline" className="w-full" disabled={loading} onClick={onClick}>
      {loading ? t("app.loading") : t("expenses.loadMore")}
    </Button>
  );
}
