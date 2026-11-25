import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import type { Me } from "@/api/client";
import { useCategories, useExpenses, usePockets } from "@/api/queries";
import { BackLink } from "@/components/BackLink";
import { DayGroup, ExpenseRow, FilterRow, LoadMore } from "@/components/ExpenseList";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, MutedText, PageTitle } from "@/components/Text";
import { formatMoney } from "@/lib/money";
import { routes } from "@/lib/routes";
import { useDayHeading } from "@/lib/useDayHeading";

// The Household's Expenses, newest first, a page at a time behind a Load more
// button (never infinite scroll). Every day heading is worked out from the
// server's Today, never the device's (CLAUDE.md rule 5). `?uncategorised=true`
// narrows it to the Expenses with no Category (#140 R8); filters live in the URL.
export function ExpensesPage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const [params] = useSearchParams();
  const uncategorised = params.get("uncategorised") === "true";
  const expenses = useExpenses(uncategorised);
  const heading = useDayHeading(me.today);
  const pockets = usePockets();
  const categories = useCategories();

  // The same head in every state, so the title is not remounted when data lands.
  const head = (
    <>
      {uncategorised && <BackLink to={routes.expenses} label={t("expenses.allExpenses")} />}
      <PageTitle>{t("expenses.title")}</PageTitle>
    </>
  );

  // A failed refetch behind data already on screen keeps the screen.
  const failed =
    (!expenses.data && expenses.error) ||
    (!pockets.data && pockets.error) ||
    (!categories.data && categories.error) ||
    null;
  if (failed) {
    return (
      <>
        {head}
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
        {head}
        <Loading />
      </>
    );
  }

  const pocketName = new Map(pockets.data.items.map((p) => [p.id, p.pocket_display_name]));
  const categoryName = new Map(categories.data.items.map((c) => [c.id, c.category_display_name]));
  const items = expenses.data.pages.flatMap((page) => page.items);

  // The list is ordered by day, so a day is one run of rows, across pages too.
  const days: { day: string; rows: typeof items }[] = [];
  for (const item of items) {
    const last = days[days.length - 1];
    if (last?.day === item.occurred_on) last.rows.push(item);
    else days.push({ day: item.occurred_on, rows: [item] });
  }

  return (
    <>
      {head}
      {uncategorised && <MutedText>{t("expenses.uncategorisedFilter")}</MutedText>}
      {!uncategorised && expenses.data.pages[0]!.uncategorised_count > 0 && (
        <FilterRow to={routes.uncategorisedExpenses}>
          {t("expenses.needsCategory", { count: expenses.data.pages[0]!.uncategorised_count })}
        </FilterRow>
      )}
      {items.length === 0 && (
        <EmptyState>
          {t(uncategorised ? "expenses.uncategorisedEmpty" : "expenses.empty")}
        </EmptyState>
      )}
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
            const amount = formatMoney(e.amount, e.currency, i18n.language);
            const name = title ?? t("expenses.uncategorised");
            return (
              <ExpenseRow
                key={e.id}
                to={routes.expense(e.id)}
                label={t("expenses.editRow", { name, amount })}
                title={name}
                muted={title === undefined}
                detail={detail}
                amount={amount}
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
