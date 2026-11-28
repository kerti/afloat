import { useEffect, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router";
import type { CategoryFigure, Dashboard, Me, PocketFigure } from "@/api/client";
import { useDashboard } from "@/api/queries";
import { ChartCard } from "@/components/ChartCard";
import { FigurePair } from "@/components/FigurePair";
import { Badge } from "@/components/Badge";
import { FigureRow } from "@/components/FigureRow";
import { ShareBar } from "@/components/ShareBar";
import { Section } from "@/components/Form";
import { HeroFigure } from "@/components/HeroFigure";
import { ListCard } from "@/components/List";
import { NoticeCard } from "@/components/NoticeCard";
import { ErrorState, Loading } from "@/components/states";
import { BodyText, EmptyState, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { compareMoney, formatAllowance, formatMoney, formatPercent, percentOf } from "@/lib/money";
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
// exactly when the server sent it. The Shell around it carries the app name.
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
      {/* The Home tab is lit and the hero says what the screen is, so the
          title is there for screen readers only (ruled 27 Nov 2026). */}
      <PageTitle visuallyHidden>{t("home.title")}</PageTitle>
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
              currency={currency}
              locale={locale}
            />
            {commitments.items.filter(shown).map((c) => (
              <CommitmentRow
                key={c.category_id}
                label={c.category_display_name}
                inactive={!c.active}
                allocation={c.allocation}
                paid={c.paid}
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

// What has gone against what was set aside, as "spent / allocation (pct)", and
// its bar: one form for a Commitment, the Commitments Total and a Category with
// an allocation (ruled 28 Nov 2026, replacing D6's still-due words and D7's
// "of <allocation>"). The percent is whole, toward zero, and absent against an
// allocation of zero. The figures are the wire's; nothing else is shown.
function Share({
  spent,
  allocation,
  currency,
  locale,
}: {
  spent: string;
  allocation: string;
  currency: string;
  locale: string;
}) {
  const { t } = useTranslation();
  const percent = percentOf(spent, allocation);
  const amounts = {
    spent: formatMoney(spent, currency, locale),
    allocation: formatMoney(allocation, currency, locale),
  };
  return percent === null
    ? t("dashboard.shareNoPercent", amounts)
    : t("dashboard.share", { ...amounts, percent: formatPercent(percent, locale) });
}

function CommitmentRow({
  label,
  inactive,
  allocation,
  paid,
  currency,
  locale,
}: {
  label: string;
  inactive?: boolean;
  allocation: string;
  paid: string;
  currency: string;
  locale: string;
}) {
  const { t } = useTranslation();
  return (
    <FigureRow
      label={label}
      labelMuted={inactive}
      badge={inactive && <Badge>{t("pockets.inactive")}</Badge>}
      bar={<ShareBar spent={paid} allocation={allocation} />}
      small
    >
      <Share spent={paid} allocation={allocation} currency={currency} locale={locale} />
    </FigureRow>
  );
}

// An inactive Commitment with nothing set aside and nothing paid says nothing,
// so it is left out, as /budget leaves out an inactive Category without an
// allocation (ruled 27 Nov 2026). The Total still counts every row.
function shown(c: NonNullable<Dashboard["commitments"]>["items"][number]): boolean {
  return c.active || compareMoney(c.allocation, "0") !== 0 || compareMoney(c.paid, "0") !== 0;
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

// Spent, or its Share where the Category has an allocation; no difference
// (R15). The rows come in the order the server sent. The row for spending with no Category has no id and
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
      small={figure.allocation !== undefined}
      bar={
        figure.allocation !== undefined && (
          <ShareBar spent={figure.spent} allocation={figure.allocation} />
        )
      }
    >
      {figure.allocation === undefined ? (
        formatMoney(figure.spent, currency, locale)
      ) : (
        <Share
          spent={figure.spent}
          allocation={figure.allocation}
          currency={currency}
          locale={locale}
        />
      )}
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
