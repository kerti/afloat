import { useTranslation } from "react-i18next";
import type { Me } from "@/api/client";
import { useCategories, useExpenses, usePockets } from "@/api/queries";
import { DayGroup, ExpenseRow, LoadMore } from "@/components/ExpenseList";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, PageTitle } from "@/components/Text";
import { formatWeekdayDate, previousDay, yearOf } from "@/lib/dates";
import { formatMoney } from "@/lib/money";

// The Household's Expenses, newest first, a page at a time behind a Load more
// button (never infinite scroll). Every day heading is worked out from the
// server's Today, never the device's (CLAUDE.md rule 5).
export function ExpensesPage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const expenses = useExpenses();
  const pockets = usePockets();
  const categories = useCategories();

  // A failed refetch behind data already on screen keeps the screen.
  const failed =
    (!expenses.data && expenses.error) ||
    (!pockets.data && pockets.error) ||
    (!categories.data && categories.error) ||
    null;
  if (failed) {
    return (
      <>
        <PageTitle>{t("expenses.title")}</PageTitle>
        <ErrorState
          error={failed}
          onRetry={() => {
            void expenses.refetch();
            void pockets.refetch();
            void categories.refetch();
          }}
        />
      </>
    );
  }
  if (!expenses.data || !pockets.data || !categories.data) {
    return (
      <>
        <PageTitle>{t("expenses.title")}</PageTitle>
        <Loading />
      </>
    );
  }

  const pocketName = new Map(pockets.data.items.map((p) => [p.id, p.pocket_display_name]));
  const categoryName = new Map(categories.data.items.map((c) => [c.id, c.category_display_name]));
  const items = expenses.data.pages.flatMap((page) => page.items);

  const yesterday = previousDay(me.today);
  const heading = (day: string) =>
    day === me.today
      ? t("dates.today")
      : day === yesterday
        ? t("expenses.yesterday")
        : formatWeekdayDate(day, i18n.language, yearOf(day) !== yearOf(me.today));

  // The list is ordered by day, so a day is one run of rows, across pages too.
  const days: { day: string; rows: typeof items }[] = [];
  for (const item of items) {
    const last = days[days.length - 1];
    if (last?.day === item.occurred_on) last.rows.push(item);
    else days.push({ day: item.occurred_on, rows: [item] });
  }

  return (
    <>
      <PageTitle>{t("expenses.title")}</PageTitle>
      {items.length === 0 && <EmptyState>{t("expenses.empty")}</EmptyState>}
      {days.map(({ day, rows }) => (
        <DayGroup key={day} heading={heading(day)}>
          {rows.map((e) => {
            const category = e.category_id ? categoryName.get(e.category_id) : undefined;
            const title = e.description ?? category;
            const detail = [
              pocketName.get(e.pocket_id),
              e.description !== undefined ? category : undefined,
              e.occurred_time,
            ]
              .filter((part): part is string => part !== undefined && part !== "")
              .join(" · ");
            return (
              <ExpenseRow
                key={e.id}
                title={title ?? t("expenses.uncategorised")}
                muted={title === undefined}
                detail={detail}
                amount={formatMoney(e.amount, e.currency, i18n.language)}
              />
            );
          })}
        </DayGroup>
      ))}
      {expenses.isFetchNextPageError && expenses.error ? (
        <ErrorState error={expenses.error} onRetry={() => void expenses.fetchNextPage()} />
      ) : (
        expenses.hasNextPage && (
          <LoadMore
            loading={expenses.isFetchingNextPage}
            onClick={() => void expenses.fetchNextPage()}
          />
        )
      )}
    </>
  );
}
