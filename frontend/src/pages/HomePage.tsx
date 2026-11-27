import { useEffect, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router";
import type { Dashboard, Me } from "@/api/client";
import { useDashboard } from "@/api/queries";
import { FigurePair } from "@/components/FigurePair";
import { FigureRow } from "@/components/FigureRow";
import { HeroFigure } from "@/components/HeroFigure";
import { ListCard } from "@/components/List";
import { NoticeCard } from "@/components/NoticeCard";
import { ErrorState, Loading } from "@/components/states";
import { BodyText, EmptyState, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { formatAllowance, formatMoney } from "@/lib/money";
import { paceOf } from "@/lib/pace";
import { routes } from "@/lib/routes";

// What silent founding hands Home through router state (R2): the name of the
// Pocket it made. The Household and currency are read live from `me`.
export type FoundedState = { founded: true; pocket: string };

function foundedPocket(state: unknown): string | null {
  const s = state as Partial<FoundedState> | null;
  return s?.founded === true && typeof s.pocket === "string" ? s.pocket : null;
}

// Home answers one question: how much is left to spend today. Every figure is
// the server's, shown and never derived (PRD N5a); the allowance figures are
// cut toward minus infinity at the currency's minor unit and the rest are
// half-up (#155 R11). Whether the allowance stands alone or beside the Baseline
// is `allowance_mode`, never a comparison of the amounts (R10). The State and
// the pace sentence come last, as words only (R12): each is there exactly when
// the server sent it. The Shell around it carries the app name and sign-out.
export function HomePage({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const dashboard = useDashboard();
  const location = useLocation();
  const navigate = useNavigate();
  // Taken from the history entry once, then the entry is cleared: leaving Home,
  // coming back, or a refresh finds no notice. Nothing is stored (R2).
  const [pocket, setPocket] = useState(() => foundedPocket(location.state));

  useEffect(() => {
    if (foundedPocket(location.state) !== null) {
      void navigate(location.pathname + location.search, { replace: true, state: null });
    }
  }, [location, navigate]);

  return (
    <>
      <PageTitle>{t("home.title")}</PageTitle>
      {pocket !== null && (
        <NoticeCard dismissLabel={t("home.founded.dismiss")} onDismiss={() => setPocket(null)}>
          <BodyText>
            <Trans
              i18nKey="home.founded.named"
              values={{
                household: me.household.household_display_name,
                pocket,
                currency: me.household.reporting_currency,
              }}
              components={{ strong: <strong /> }}
            />
          </BodyText>
          <MutedText>{t("home.founded.hint")}</MutedText>
          <TextLink to={routes.settings}>{t("home.founded.settings")}</TextLink>
        </NoticeCard>
      )}
      {/* A failed refresh keeps the figures already shown; the error is for a
          Home with none yet (ruled 27 Nov 2026). */}
      {!dashboard.data ? (
        dashboard.error ? (
          <ErrorState error={dashboard.error} onRetry={() => void dashboard.refetch()} />
        ) : (
          <Loading />
        )
      ) : !dashboard.data.trajectory ? (
        <>
          <EmptyState>{t("dashboard.noBudget")}</EmptyState>
          <TextLink to={routes.budget}>{t("dashboard.noBudgetLink")}</TextLink>
        </>
      ) : (
        <Figures
          currency={dashboard.data.currency}
          locale={i18n.language}
          mode={me.household.allowance_mode}
          trajectory={dashboard.data.trajectory}
        />
      )}
    </>
  );
}

function Figures({
  currency,
  locale,
  mode,
  trajectory,
}: {
  currency: string;
  locale: string;
  mode: Me["household"]["allowance_mode"];
  trajectory: NonNullable<Dashboard["trajectory"]>;
}) {
  const { t } = useTranslation();
  // Present only while the server has a pace to read (fully committed has none;
  // day one has a State and no variance), so absence is never decided here.
  const pace =
    trajectory.trajectory_variance !== undefined
      ? paceOf(trajectory.trajectory_variance, currency, locale)
      : null;
  const allowance = {
    label: t("dashboard.todaysAllowance"),
    value: formatAllowance(trajectory.todays_allowance, currency, locale),
  };
  // Fixed mode has one allowance, so one figure. In adaptive mode the Baseline
  // sits beside it, unless there is none (fully committed): then nothing says so.
  const pair =
    mode === "adaptive" && trajectory.baseline_daily_budget !== undefined
      ? [
          allowance,
          {
            label: t("dashboard.baseline"),
            value: formatAllowance(trajectory.baseline_daily_budget, currency, locale),
          },
        ]
      : [allowance];
  return (
    <>
      <HeroFigure
        label={t("dashboard.leftToSpendToday")}
        figure={formatAllowance(trajectory.left_to_spend_today, currency, locale)}
      />
      <FigurePair figures={pair} />
      <ListCard>
        <FigureRow label={t("dashboard.daysRemaining")}>{trajectory.days_remaining}</FigureRow>
        <FigureRow label={t("dashboard.remainingDailyPool")}>
          {formatMoney(trajectory.remaining_daily_pool, currency, locale)}
        </FigureRow>
      </ListCard>
      {(trajectory.state !== undefined || pace) && (
        <div className="flex flex-col gap-1">
          {trajectory.state !== undefined && (
            <BodyText>{t(`dashboard.state.${trajectory.state}`)}</BodyText>
          )}
          {pace && (
            <MutedText>
              {pace.direction === "on"
                ? t("dashboard.pace.on")
                : t(`dashboard.pace.${pace.direction}`, { amount: pace.amount })}
            </MutedText>
          )}
        </div>
      )}
    </>
  );
}
