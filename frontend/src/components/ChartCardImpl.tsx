import { useTranslation } from "react-i18next";
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ReferenceDot,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type TooltipContentProps,
  type XAxisTickContentProps,
} from "recharts";
import type { ChartPoint } from "@/api/client";
import { formatDate } from "@/lib/dates";
import { currencyDecimals, formatMoney, formatMoneyCompact } from "@/lib/money";

// The drawing of ChartCard, and the only file that imports Recharts: it is its
// own lazy chunk. It draws what `points` says and derives no figure. The numbers
// handed to Recharts are positions on a page, converted once from the wire's
// strings below; nothing on the screen is formatted from them except the Y
// axis's ticks, which are axis positions in compact words with the currency's
// symbol ("Rp2.4M"). The tooltip and the table show the server's strings.
//
// Line style, not colour, tells the lines apart (PRD §9, D9): the target is
// dashed and muted, the actual solid and primary, and Today's last stretch
// (the day that is not over) is dashed again, ending in an open dot. No red,
// no green, no fill. Axis text is 12px (the Badge's size). The keyboard layer is
// off (accessibilityLayer={false}): the card is a role="img" and a focusable
// SVG inside one is nested-interactive. A touch or hover still opens the
// tooltip. Colours are tokens, as SVG presentation attributes (CSSOM-safe under a
// style-src without 'unsafe-inline'; no <style> element is rendered, which is
// what Balances' shadcn ChartStyle does and this does not).
const TARGET_DASH = "6 4";
const TODAY_DASH = "2 3";

type Row = {
  date: string;
  target: number;
  // One series per stroke style: the days that are over, and the last stretch
  // into Today. They share Today's neighbour so the two meet.
  settled: number | null;
  today: number | null;
  point: ChartPoint;
};

// The plotted rows. `Number()` is for position only: no figure is shown from it.
function rowsOf(points: ChartPoint[]): Row[] {
  const todayAt = points.findIndex((p) => p.in_progress === true);
  return points.map((point, i) => {
    const actual = point.actual === undefined ? null : Number(point.actual);
    const overDay = todayAt < 0 || i < todayAt;
    return {
      date: point.date,
      target: Number(point.target),
      settled: overDay ? actual : null,
      today: todayAt >= 0 && (i === todayAt || i === todayAt - 1) ? actual : null,
      point,
    };
  });
}

export default function ChartCardImpl({
  points,
  currency,
  locale,
}: {
  points: ChartPoint[];
  currency: string;
  locale: string;
}) {
  const { t } = useTranslation();
  const rows = rowsOf(points);
  const today = rows.find((r) => r.point.in_progress === true);
  const decimals = currencyDecimals(currency);
  const edges = [points[0]?.date, points[points.length - 1]?.date].filter(
    (d): d is string => d !== undefined,
  );

  return (
    <ResponsiveContainer width="100%" height={220} initialDimension={{ width: 320, height: 220 }}>
      <LineChart
        data={rows}
        accessibilityLayer={false}
        margin={{ top: 8, right: 8, bottom: 0, left: 0 }}
      >
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis
          dataKey="date"
          ticks={edges}
          tickLine={false}
          axisLine={{ stroke: "var(--border)" }}
          tickMargin={8}
          tick={(props: XAxisTickContentProps) => (
            <text
              x={props.x}
              y={props.y}
              dy={12}
              fontSize={12}
              fill="var(--muted-foreground)"
              textAnchor={props.index === 0 ? "start" : "end"}
            >
              {formatDate(props.payload.value, locale, false)}
            </text>
          )}
        />
        <YAxis
          width="auto"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          tickCount={4}
          allowDecimals={decimals > 0}
          domain={[(min: number) => Math.min(0, min), "auto"]}
          tick={{ fontSize: 12, fill: "var(--muted-foreground)" }}
          tickFormatter={(v: number) => formatMoneyCompact(v, currency, locale)}
        />
        <Tooltip
          cursor={{ stroke: "var(--border)" }}
          content={(props) => <ChartTooltip {...props} currency={currency} locale={locale} />}
        />
        <Legend
          verticalAlign="bottom"
          align="left"
          iconType="plainline"
          itemSorter={null}
          wrapperStyle={{ fontSize: 12, paddingTop: 8 }}
          formatter={(value: string) => <span className="text-foreground">{value}</span>}
        />
        <Line
          dataKey="target"
          name={t("dashboard.chart.target")}
          type="linear"
          stroke="var(--muted-foreground)"
          strokeWidth={2}
          strokeDasharray={TARGET_DASH}
          dot={false}
          activeDot={false}
          isAnimationActive={false}
        />
        <Line
          dataKey="settled"
          name={t("dashboard.chart.actual")}
          type="linear"
          stroke="var(--primary)"
          strokeWidth={2}
          dot={false}
          activeDot={{ r: 4, fill: "var(--primary)", stroke: "var(--card)" }}
          connectNulls={false}
          isAnimationActive={false}
        />
        <Line
          dataKey="today"
          legendType="none"
          type="linear"
          stroke="var(--primary)"
          strokeWidth={2}
          strokeDasharray={TODAY_DASH}
          dot={false}
          activeDot={false}
          connectNulls={false}
          isAnimationActive={false}
        />
        {today && today.today !== null && (
          <ReferenceDot
            x={today.date}
            y={today.today}
            r={4}
            fill="var(--card)"
            stroke="var(--primary)"
            strokeWidth={2}
          />
        )}
      </LineChart>
    </ResponsiveContainer>
  );
}

// What a touch or a hover says: the day, and the server's two figures for it.
function ChartTooltip({
  active,
  payload,
  currency,
  locale,
}: TooltipContentProps & { currency: string; locale: string }) {
  const { t } = useTranslation();
  const row = active ? (payload?.[0]?.payload as Row | undefined) : undefined;
  if (!row) return null;
  const { point } = row;
  return (
    <div className="rounded-md border bg-card px-3 py-2 text-sm text-card-foreground shadow-lg">
      <p className="font-medium">
        {formatDate(point.date, locale)}
        {point.in_progress ? `, ${t("dashboard.chart.inProgress")}` : ""}
      </p>
      <p className="tabular-nums">
        {t("dashboard.chart.target")}: {formatMoney(point.target, currency, locale)}
      </p>
      {point.actual !== undefined && (
        <p className="tabular-nums">
          {t("dashboard.chart.actual")}: {formatMoney(point.actual, currency, locale)}
        </p>
      )}
    </div>
  );
}
