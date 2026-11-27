import { useEffect, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router";
import type { CategoryFigure, Dashboard, Me, PocketFigure } from "@/api/client";
import { useDashboard } from "@/api/queries";
import { ChartCard } from "@/components/ChartCard";
import { FigurePair } from "@/components/FigurePair";
import { Badge } from "@/components/Badge";
import { FigureRow } from "@/components/FigureRow";
import { Section } from "@/components/Form";
import { HeroFigure } from "@/components/HeroFigure";
import { ListCard } from "@/components/List";
import { NoticeCard } from "@/components/NoticeCard";
import { ErrorState, Loading } from "@/components/states";
import { BodyText, EmptyState, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { formatAllowance, formatMoney } from "@/lib/money";
import { stillDueOf } from "@/lib/commitment";
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
// the pace sentence are words only (R12), then the chart (D5: the server's
// points, drawn, with the pace sentence as its name), then Commitments (R16,
// per Budget Period), then By Category and By Pocket (R15); each is there
// exactly when the server sent it. The Shell around it carries the app name and sign-out.
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
          chart={dashboard.data.chart}
          commitments={dashboard.data.commitments}
          categories={dashboard.data.categories}
          pockets={dashboard.data.pockets}
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
  chart,
  commitments,
  categories,
  pockets,
}: {
  currency: string;
  locale: string;
  mode: Me["household"]["allowance_mode"];
  trajectory: NonNullable<Dashboard["trajectory"]>;
  chart: Dashboard["chart"];
  commitments: Dashboard["commitments"];
  categories: Dashboard["categories"];
  pockets: Dashboard["pockets"];
}) {
  const { t } = useTranslation();
  // Present only while the server has a pace to read (fully committed has none;
  // day one has a State and no variance), so absence is never decided here.
  const pace =
    trajectory.trajectory_variance !== undefined
      ? paceOf(trajectory.trajectory_variance, currency, locale)
      : null;
  const paceSentence = pace
    ? pace.direction === "on"
      ? t("dashboard.pace.on")
      : t(`dashboard.pace.${pace.direction}`, { amount: pace.amount })
    : undefined;
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
          {paceSentence !== undefined && <MutedText>{paceSentence}</MutedText>}
        </div>
      )}
      {chart && (
        <Section id="chart" title={t("dashboard.chart.title")}>
          <ChartCard
            points={chart.points}
            currency={currency}
            locale={locale}
            paceSentence={paceSentence}
          />
        </Section>
      )}
      <Section id="commitments" title={t("dashboard.commitments.title")}>
        {commitments === undefined ? (
          <EmptyState>{t("dashboard.commitments.empty")}</EmptyState>
        ) : (
          <ListCard>
            <CommitmentRow
              label={t("dashboard.commitments.total")}
              allocation={commitments.commitments_budget}
              paid={commitments.paid}
              stillDue={commitments.still_due}
              currency={currency}
              locale={locale}
            />
            {commitments.items.map((c) => (
              <CommitmentRow
                key={c.category_id}
                label={c.category_display_name}
                inactive={!c.active}
                allocation={c.allocation}
                paid={c.paid}
                stillDue={c.still_due}
                currency={currency}
                locale={locale}
              />
            ))}
          </ListCard>
        )}
      </Section>
      <Section id="by-category" title={t("dashboard.byCategory")}>
        <Rows empty={categories === undefined}>
          {categories?.map((c) => (
            <CategoryRow
              key={c.category_id ?? "uncategorised"}
              figure={c}
              currency={currency}
              locale={locale}
            />
          ))}
        </Rows>
      </Section>
      <Section id="by-pocket" title={t("dashboard.byPocket")}>
        <Rows empty={pockets === undefined}>
          {pockets?.map((p) => (
            <PocketRow key={p.pocket_id} figure={p} currency={currency} locale={locale} />
          ))}
        </Rows>
      </Section>
    </>
  );
}

// One Commitment, or the totals of them: what is still due (or "Paid", or "more
// than you set aside"), and under it what has been paid of what was set aside.
// Information and no tone: no colour, bar or percentage, and a still due below
// zero is never clamped (D9, R16). The server sends the figures; the words are
// chosen from the still due as it shows.
function CommitmentRow({
  label,
  inactive,
  allocation,
  paid,
  stillDue,
  currency,
  locale,
}: {
  label: string;
  inactive?: boolean;
  allocation: string;
  paid: string;
  stillDue: string;
  currency: string;
  locale: string;
}) {
  const { t } = useTranslation();
  const due = stillDueOf(stillDue, currency, locale);
  const status =
    due === null
      ? undefined
      : due.kind === "paid"
        ? t("dashboard.commitments.paid")
        : due.kind === "due"
          ? t("dashboard.commitments.stillDue", { amount: due.amount })
          : t("dashboard.commitments.over", { amount: due.amount });
  return (
    <FigureRow
      label={label}
      labelMuted={inactive}
      badge={inactive && <Badge>{t("pockets.inactive")}</Badge>}
      muted={due?.kind === "paid"}
      detail={t("dashboard.commitments.detail", {
        paid: formatMoney(paid, currency, locale),
        allocation: formatMoney(allocation, currency, locale),
      })}
    >
      {status}
    </FigureRow>
  );
}

// A section's rows, or the sentence that says what will appear (T12): the
// server omits a list that has no row, so absence is its answer, not a guess.
function Rows({ empty, children }: { empty: boolean; children: ReactNode }) {
  const { t } = useTranslation();
  return empty ? (
    <EmptyState>{t("dashboard.breakdownEmpty")}</EmptyState>
  ) : (
    <ListCard>{children}</ListCard>
  );
}

// Spent, and "of <allocation>" where the Category has one, and nothing else:
// no bar, no difference, no share, no colour (D9, R15). The rows come in the
// order the server sent. The row for spending with no Category has no id and
// no name, and is not a link: Home never prompts about it (E4, T11).
function CategoryRow({
  figure,
  currency,
  locale,
}: {
  figure: CategoryFigure;
  currency: string;
  locale: string;
}) {
  const { t } = useTranslation();
  const inactive = figure.active === false;
  return (
    <FigureRow
      label={figure.category_display_name ?? t("expenses.uncategorised")}
      labelMuted={inactive || figure.category_display_name === undefined}
      badge={inactive && <Badge>{t("pockets.inactive")}</Badge>}
      detail={
        figure.allocation === undefined
          ? undefined
          : t("dashboard.ofAllocation", {
              amount: formatMoney(figure.allocation, currency, locale),
            })
      }
    >
      {formatMoney(figure.spent, currency, locale)}
    </FigureRow>
  );
}

function PocketRow({
  figure,
  currency,
  locale,
}: {
  figure: PocketFigure;
  currency: string;
  locale: string;
}) {
  const { t } = useTranslation();
  return (
    <FigureRow
      label={figure.pocket_display_name}
      labelMuted={!figure.active}
      badge={!figure.active && <Badge>{t("pockets.inactive")}</Badge>}
    >
      {formatMoney(figure.spent, currency, locale)}
    </FigureRow>
  );
}
