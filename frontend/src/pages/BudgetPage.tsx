import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Me } from "@/api/client";
import { useBudget, useCategories, useUpdateBudget } from "@/api/queries";
import { BackLink } from "@/components/BackLink";
import { CategoryCreateDialog } from "@/components/CategoryCreateDialog";
import { FigureRow } from "@/components/FigureRow";
import { Section } from "@/components/Form";
import { Badge } from "@/components/Badge";
import { ListCard } from "@/components/List";
import { MoneySetting } from "@/components/MoneySetting";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, MutedText, PageTitle } from "@/components/Text";
import { Button } from "@/components/ui/button";
import { formatDateRange } from "@/lib/dates";
import { formatMoney } from "@/lib/money";
import { routes } from "@/lib/routes";
import { useDialogParam } from "@/lib/useDialogParam";
import { uuidv7 } from "@/lib/uuid";

// The Budget screen. Every figure is the server's answer; the client adds none
// and derives no date (CLAUDE.md rule 5, PRD N5a). A Category's allocation is
// the Budget's, joined to the Category by id.
// An allocation of zero is still an allocation; only a non-zero one keeps an
// inactive Category on screen (#130 R9). Tested on the digits, never parsed.
const nonZero = (amount: string) => /[1-9]/.test(amount);
export function BudgetPage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const budget = useBudget();
  const categories = useCategories();
  const newParam = useDialogParam("new");
  // One id per create attempt, kept here so closing and reopening the dialog
  // after a failure still resends it.
  const [attemptId, setAttemptId] = useState(uuidv7);
  const update = useUpdateBudget();
  const currency = me.household.reporting_currency;
  const data = budget.data;
  const money = (v: string) => formatMoney(v, currency, i18n.language);
  const allocationOf = new Map(data?.allocations.map((a) => [a.category_id, a.allocation]));
  const listed = categories.data?.items.filter(
    (c) => c.active || nonZero(allocationOf.get(c.id) ?? "0"),
  );
  const buckets = [
    { bucket: "commitment", id: "commitments", empty: "categories.commitmentsEmpty" },
    { bucket: "daily", id: "daily", empty: "categories.dailyEmpty" },
  ] as const;

  return (
    <>
      <BackLink to={routes.home} label={t("home.title")} />
      <PageTitle>{t("budget.title")}</PageTitle>
      {budget.error ? (
        <ErrorState error={budget.error} onRetry={() => void budget.refetch()} />
      ) : !data ? (
        <Loading />
      ) : (
        <>
          <MutedText>
            {t(
              "budget.window",
              formatDateRange(data.period.starts_on, data.period.ends_on, i18n.language),
            )}
          </MutedText>
          <Section>
            <MoneySetting
              id="monthly-budget"
              label={t("fields.monthlyBudget")}
              currency={currency}
              stored={data.monthly_budget}
              save={(next) => update.mutateAsync({ monthly_budget: next })}
              pending={update.isPending}
              error={update.error}
              clearError={update.reset}
            />
          </Section>
          {data.daily_pool === undefined || data.unallocated === undefined ? (
            <EmptyState>{t("budget.empty")}</EmptyState>
          ) : (
            <ListCard>
              <FigureRow label={t("budget.commitmentsBudget")}>
                {money(data.commitments_budget)}
              </FigureRow>
              <FigureRow label={t("budget.dailyPool")}>{money(data.daily_pool)}</FigureRow>
              <FigureRow label={t("budget.unallocated")}>{money(data.unallocated)}</FigureRow>
            </ListCard>
          )}
          {data.unallocated?.startsWith("-") && <MutedText>{t("budget.overAllocated")}</MutedText>}
          {categories.error ? (
            <ErrorState error={categories.error} onRetry={() => void categories.refetch()} />
          ) : !listed ? (
            <Loading />
          ) : (
            buckets.map(({ bucket, id, empty }) => {
              const rows = listed.filter((c) => c.bucket === bucket);
              return (
                <Section key={bucket} id={id} title={t(`categories.${id}Heading`)}>
                  {rows.length === 0 ? (
                    <EmptyState>{t(empty)}</EmptyState>
                  ) : (
                    <ListCard>
                      {rows.map((c) => {
                        const amount = allocationOf.get(c.id);
                        return (
                          <FigureRow
                            key={c.id}
                            label={c.category_display_name}
                            badge={c.active ? undefined : <Badge>{t("pockets.inactive")}</Badge>}
                            muted={amount === undefined}
                          >
                            {amount === undefined ? t("categories.noAllocation") : money(amount)}
                          </FigureRow>
                        );
                      })}
                    </ListCard>
                  )}
                </Section>
              );
            })
          )}
          <Button className="w-full" onClick={() => newParam.open("category")}>
            {t("categories.addHeading")}
          </Button>
        </>
      )}
      {newParam.value === "category" && (
        <CategoryCreateDialog
          currency={currency}
          attemptId={attemptId}
          newAttemptId={() => setAttemptId(uuidv7())}
          close={newParam.close}
        />
      )}
    </>
  );
}
