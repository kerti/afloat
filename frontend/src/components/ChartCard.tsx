import { Component, lazy, Suspense, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { ChartPoint } from "@/api/client";
import { Loading } from "@/components/states";
import { MutedText } from "@/components/Text";
import { formatDate } from "@/lib/dates";
import { formatMoney } from "@/lib/money";

// The Period's cumulative target line against its actual line (PRD D5, #155 T8).
// Everything here is `points` from the server, shown and never derived: the
// drawing (ChartCardImpl) and the visually hidden table below both read the one
// `points` prop, so what a screen reader is told is what the picture is made of.
//
// The drawing is a lazy chunk: Recharts is imported by ChartCardImpl.tsx alone
// (a test holds that), so Home's first paint never waits for it and its entry
// chunk does not grow. The card is the one `role="img"` and carries the pace
// sentence as its name, or a neutral one when there is no pace. The table is
// its sibling, not its child: an image's children are presentational, and a
// table inside one would be read by no one. Recharts' keyboard layer is off in
// the Impl, because a focusable SVG inside an image is nested-interactive; the
// chart is supplementary, and the hero, the State and the pace carry the reading.
//
// Confirmed on the phone preview, #155 PR 8 (R17 approved the card and
// recharts): NoticeCard's card surface, rounded-lg border bg-card, at p-3 (Uruni cardClasses.ts card), the
// Impl's lines and legend as ChartCardImpl says. Precedent for the shape:
// Balances components/ui/chart.tsx and components/charts/*Impl.tsx.
const ChartCardImpl = lazy(() => import("./ChartCardImpl"));

export type ChartCardProps = {
  points: ChartPoint[];
  currency: string;
  locale: string;
  // The pace sentence when there is one; the card names itself otherwise.
  paceSentence?: string;
};

export function ChartCard({ points, currency, locale, paceSentence }: ChartCardProps) {
  const { t } = useTranslation();
  return (
    <>
      <div
        role="img"
        aria-label={paceSentence ?? t("dashboard.chart.label")}
        className="min-h-48 rounded-lg border bg-card p-3 text-card-foreground"
      >
        <ChartBoundary>
          <Suspense fallback={<Loading />}>
            <ChartCardImpl points={points} currency={currency} locale={locale} />
          </Suspense>
        </ChartBoundary>
      </div>
      <ChartTable points={points} currency={currency} locale={locale} />
    </>
  );
}

// The same points as a table, for anyone the picture does not reach. Visually
// hidden, never display:none, so it stays in the accessibility tree. An absent
// actual (a day after Today) is an empty cell; Today's row says it is in progress.
// The hiding is on a wrapping div, never on the table: a table ignores the 1px
// width and hidden overflow, so it keeps its full width (with sr-only's nowrap,
// wider than a phone) and the page scrolls sideways.
function ChartTable({ points, currency, locale }: ChartCardProps) {
  const { t } = useTranslation();
  return (
    <div className="sr-only">
      <table>
        <caption>{t("dashboard.chart.label")}</caption>
        <thead>
          <tr>
            <th scope="col">{t("dashboard.chart.date")}</th>
            <th scope="col">{t("dashboard.chart.target")}</th>
            <th scope="col">{t("dashboard.chart.actual")}</th>
          </tr>
        </thead>
        <tbody>
          {points.map((p) => (
            <tr key={p.date}>
              <th scope="row">
                {formatDate(p.date, locale)}
                {p.in_progress ? `, ${t("dashboard.chart.inProgress")}` : ""}
              </th>
              <td>{formatMoney(p.target, currency, locale)}</td>
              <td>{p.actual === undefined ? "" : formatMoney(p.actual, currency, locale)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// A chunk that fails to load (offline on a first visit, a deploy that retired the
// file the page asks for) must not take Home with it: React rethrows a rejected
// lazy import to the nearest boundary, and the app has none. The table still
// says everything the picture would.
class ChartBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    return this.state.failed ? <ChartUnavailable /> : this.props.children;
  }
}

function ChartUnavailable() {
  const { t } = useTranslation();
  return <MutedText>{t("dashboard.chart.unavailable")}</MutedText>;
}
