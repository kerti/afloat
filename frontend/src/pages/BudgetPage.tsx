import { useTranslation } from "react-i18next";
import type { Me } from "@/api/client";
import { useBudget, useUpdateBudget } from "@/api/queries";
import { BackLink } from "@/components/BackLink";
import { FigureRow } from "@/components/FigureRow";
import { Section } from "@/components/Form";
import { ListCard } from "@/components/List";
import { MoneySetting } from "@/components/MoneySetting";
import { ErrorState, Loading } from "@/components/states";
import { EmptyState, MutedText, PageTitle } from "@/components/Text";
import { formatDateRange } from "@/lib/dates";
import { formatMoney } from "@/lib/money";
import { routes } from "@/lib/routes";

// The Budget screen. Every figure is the server's answer; the client adds none
// and derives no date (CLAUDE.md rule 5, PRD N5a). Categories and their
// allocations arrive with the next slice.
export function BudgetPage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const budget = useBudget();
  const update = useUpdateBudget();
  const currency = me.household.reporting_currency;
  const data = budget.data;
  const money = (v: string) => formatMoney(v, currency, i18n.language);

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
        </>
      )}
    </>
  );
}
