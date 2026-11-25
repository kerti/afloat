import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { SubsectionLabel } from "@/components/Text";
import { Button } from "@/components/ui/button";

// The day heading is the SubsectionLabel over a bordered divided list; the row
// is two lines (Uruni web/src/screens/Home.tsx purpose row); Load more is Uruni
// web/src/components/LoadMore.tsx. Ruled 25 Nov 2026, #140 PR 3b.

export function DayGroup({ heading, children }: { heading: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <SubsectionLabel>{heading}</SubsectionLabel>
      <ul className="flex flex-col divide-y rounded-md border">{children}</ul>
    </section>
  );
}

/** Read-only until PR 4 makes it open the edit route (then it gains a chevron). */
export function ExpenseRow({
  title,
  detail,
  amount,
  muted,
}: {
  title: string;
  detail: string;
  amount: string;
  muted?: boolean;
}) {
  return (
    <li className="flex min-h-11 items-center gap-3 px-3 py-2">
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={muted ? "truncate text-muted-foreground" : "truncate"}>{title}</span>
        <span className="truncate text-sm text-muted-foreground">{detail}</span>
      </span>
      <span className="shrink-0 font-medium tabular-nums">{amount}</span>
    </li>
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
