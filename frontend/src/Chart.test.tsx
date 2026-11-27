import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import type { ChartPoint, Dashboard } from "@/api/client";
import { alice, defaultHandlers, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

// Home's chart (#155 PR 8, D5): the server's `points`, drawn by one lazy chunk and
// listed in a visually hidden table. The tests read the data and the document's
// structure, not pixels: jsdom has no layout, so Recharts draws at its initial
// dimension and a ResizeObserver stub (test/setup.ts) keeps it quiet.

// A 30-day Period, Today the 14th, a target of 100.000 a day, 40.000 spent on the
// 3rd and 7.000 on the 14th. Built here with plain numbers because it is test
// data; the app derives none of it.
const money = (n: number) => n.toFixed(4);
function pointsOf(today = 14): ChartPoint[] {
  const spend: Record<number, number> = { 3: 40000, 14: 7000 };
  let sum = 0;
  return Array.from({ length: 30 }, (_, i) => {
    const day = i + 1;
    sum += spend[day] ?? 0;
    const point: ChartPoint = {
      date: `2026-11-${String(day).padStart(2, "0")}`,
      target: money(100000 * day),
    };
    if (day <= today) point.actual = money(sum);
    if (day === today) point.in_progress = true;
    return point;
  });
}

const dashboardOf = (
  points: ChartPoint[] | undefined,
  variance: string | null = "420000.0000",
): Dashboard => ({
  currency: "IDR",
  trajectory: {
    left_to_spend_today: "203448.2758",
    todays_allowance: "203448.2758",
    days_remaining: 17,
    remaining_daily_pool: "3458620.7586",
    baseline_daily_budget: "100000.0000",
    state: "afloat",
    ...(variance === null ? {} : { trajectory_variance: variance }),
  },
  ...(points === undefined ? {} : { chart: { points } }),
});

const signedIn = (dashboard: Dashboard, locale: "en-GB" | "id-ID" = "en-GB"): State => ({
  signedIn: true,
  me: { ...alice, locale },
  dashboard,
});

async function home(state: State) {
  server.use(...defaultHandlers(state));
  renderApp("/");
  await screen.findByRole("heading", { name: state.me.locale === "id-ID" ? "Beranda" : "Home" });
}

const chartImage = () => screen.findByRole("img", { name: /./ });
// Recharts is a lazy chunk: wait for its legend, which it draws once it has.
const drawn = () =>
  screen.findByText(enGB.dashboard.chart.target, { selector: ".recharts-legend-item-text span" });

// The axes' tick text: Recharts draws its own in one layer (`orientation` says
// which axis), and the X ticks are ChartCardImpl's own `text` elements.
const yTicks = () => [
  ...document.querySelectorAll("text.recharts-cartesian-axis-tick-value[orientation='left']"),
];
const xTicks = () => [
  ...document.querySelectorAll("svg.recharts-surface > g text:not(.recharts-text)"),
];

describe("the chart card", () => {
  it("is one image named by the pace sentence, with the server's sentence and no other", async () => {
    await home(signedIn(dashboardOf(pointsOf())));
    const image = await chartImage();
    expect(image).toHaveAttribute("aria-label", "Rp420,000 ahead of pace");
    expect(screen.getAllByRole("img", { name: /./ })).toHaveLength(1);
  });

  it("is named for what it shows when there is no pace to read", async () => {
    await home(signedIn(dashboardOf(pointsOf(), null)));
    expect(await chartImage()).toHaveAttribute("aria-label", enGB.dashboard.chart.label);
  });

  it("reads 'On pace' from the displayed amount, as the pace line does", async () => {
    await home(signedIn(dashboardOf(pointsOf(), "-0.4000")));
    expect(await chartImage()).toHaveAttribute("aria-label", "On pace");
  });

  it("is named in Indonesian when the locale is", async () => {
    await home(signedIn(dashboardOf(pointsOf(), "-420000.0000"), "id-ID"));
    expect(await chartImage()).toHaveAttribute("aria-label", "Rp420.000 di belakang laju");
  });

  it("is a section titled This Period, after the State and the pace and before By Category", async () => {
    const dashboard = dashboardOf(pointsOf());
    dashboard.categories = [{ spent: "45000.0000" }];
    await home(signedIn(dashboard));
    const title = await screen.findByRole("heading", {
      level: 2,
      name: enGB.dashboard.chart.title,
    });
    const pace = screen.getByText("Rp420,000 ahead of pace");
    const byCategory = screen.getByRole("heading", { level: 2, name: "By Category" });
    expect(pace.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(
      title.compareDocumentPosition(byCategory) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("is absent when the server sent no chart, and nothing says so", async () => {
    await home(signedIn(dashboardOf(undefined)));
    await screen.findByText("Days remaining");
    expect(
      screen.queryByRole("heading", { name: enGB.dashboard.chart.title }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("img", { name: /./ })).not.toBeInTheDocument();
  });

  it("is absent while there is no Monthly Budget", async () => {
    await home(signedIn({ currency: "IDR" }));
    await screen.findByText(enGB.dashboard.noBudget);
    expect(screen.queryByRole("img", { name: /./ })).not.toBeInTheDocument();
  });
});

describe("the hidden table", () => {
  it("has one row for each of the server's points, with its figures as sent", async () => {
    const points = pointsOf();
    await home(signedIn(dashboardOf(points)));
    const table = await screen.findByRole("table", { name: enGB.dashboard.chart.label });
    const rows = within(table).getAllByRole("row").slice(1); // under the header row
    expect(rows).toHaveLength(points.length);

    const cells = (row: HTMLElement) =>
      within(row)
        .getAllByRole("cell")
        .map((c) => c.textContent);
    // The 1st: one day of target, nothing spent. The 3rd: 40.000 spent.
    expect(within(rows[0]!).getByRole("rowheader")).toHaveTextContent("1 November 2026");
    expect(cells(rows[0]!)).toEqual(["Rp100,000", "Rp0"]);
    expect(cells(rows[2]!)).toEqual(["Rp300,000", "Rp40,000"]);
    // Every row's figures are the point's, formatted: no figure is derived.
    points.forEach((p, i) => {
      expect(cells(rows[i]!)[0]).toBe(`Rp${Number(p.target).toLocaleString("en-GB")}`);
    });
  });

  it("leaves the actual empty after Today and says which row is Today, in progress", async () => {
    await home(signedIn(dashboardOf(pointsOf(14))));
    const table = await screen.findByRole("table", { name: enGB.dashboard.chart.label });
    const rows = within(table).getAllByRole("row").slice(1);
    const today = rows[13]!;
    expect(within(today).getByRole("rowheader")).toHaveTextContent(
      `14 November 2026, ${enGB.dashboard.chart.inProgress}`,
    );
    expect(within(today).getAllByRole("cell")[1]).toHaveTextContent("Rp47,000");
    // Only one row is in progress.
    expect(
      rows.filter((r) => r.textContent?.includes(enGB.dashboard.chart.inProgress)),
    ).toHaveLength(1);
    // The 15th onward has a target and no actual.
    for (const row of rows.slice(14)) {
      const [target, actual] = within(row).getAllByRole("cell");
      expect(target!.textContent).toMatch(/^Rp/);
      expect(actual!.textContent).toBe("");
    }
  });

  it("is visually hidden and not removed from the tree, and is not inside the image", async () => {
    await home(signedIn(dashboardOf(pointsOf())));
    const table = await screen.findByRole("table", { name: enGB.dashboard.chart.label });
    // Hidden by a wrapping div: sr-only on the table itself leaves it full width,
    // and the page scrolls sideways on a phone.
    expect(table.className).not.toContain("sr-only");
    expect(table.parentElement!.className).toContain("sr-only");
    expect(table.closest("[role=img]")).toBeNull();
  });

  it("is made of the same points the chart is drawn from", async () => {
    // Today the 20th: the table and the drawing both move together.
    await home(signedIn(dashboardOf(pointsOf(20))));
    const table = await screen.findByRole("table", { name: enGB.dashboard.chart.label });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(
      rows.filter((r) => r.textContent?.includes(enGB.dashboard.chart.inProgress)),
    ).toHaveLength(1);
    expect(rows[19]).toHaveTextContent(enGB.dashboard.chart.inProgress);
    await drawn();
    // One open dot, on Today: the drawing marks the point the server marked.
    expect(document.querySelectorAll(".recharts-reference-dot")).toHaveLength(1);
  });

  it("is worded in Indonesian with Indonesian separators", async () => {
    await home(signedIn(dashboardOf(pointsOf()), "id-ID"));
    const table = await screen.findByRole("table", { name: idID.dashboard.chart.label });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(within(rows[0]!).getByRole("rowheader")).toHaveTextContent("1 November 2026");
    expect(within(rows[13]!).getByRole("rowheader")).toHaveTextContent(
      idID.dashboard.chart.inProgress,
    );
    expect(within(rows[2]!).getAllByRole("cell")[0]).toHaveTextContent("Rp300.000");
  });
});

describe("the drawing", () => {
  it("draws two named lines and the Today stretch, and no keyboard layer", async () => {
    await home(signedIn(dashboardOf(pointsOf())));
    await drawn();
    const legend = document.querySelector(".recharts-legend-wrapper")!;
    expect(legend).toHaveTextContent(enGB.dashboard.chart.target);
    expect(legend).toHaveTextContent(enGB.dashboard.chart.actual);
    // The Today stretch is not a legend entry of its own.
    expect(legend.querySelectorAll(".recharts-legend-item")).toHaveLength(2);
    // accessibilityLayer is off: a focusable SVG inside role="img" is nested-interactive.
    const surface = document.querySelector("svg.recharts-surface")!;
    expect(surface).not.toHaveAttribute("tabindex");
    expect(surface).not.toHaveAttribute("role");
    expect(document.querySelector("[role=img] [tabindex]:not([tabindex='-1'])")).toBeNull();
  });

  it("tells the lines apart by style: the target dashed, the actual solid, Today's stretch dashed", async () => {
    await home(signedIn(dashboardOf(pointsOf())));
    await drawn();
    const dashes = [...document.querySelectorAll("path.recharts-line-curve")].map((p) => ({
      stroke: p.getAttribute("stroke"),
      dash: p.getAttribute("stroke-dasharray"),
    }));
    expect(dashes).toContainEqual({ stroke: "var(--muted-foreground)", dash: "6 4" });
    expect(dashes).toContainEqual({ stroke: "var(--primary)", dash: null });
    expect(dashes).toContainEqual({ stroke: "var(--primary)", dash: "2 3" });
    // No colour carries a meaning: nothing is red or green, and no fill.
    for (const el of document.querySelectorAll("svg.recharts-surface *")) {
      for (const attr of ["stroke", "fill"]) {
        expect(el.getAttribute(attr) ?? "").not.toMatch(/red|green|#[0-9a-f]{3,6}|rgb|hsl/i);
      }
    }
    expect(document.querySelector(".recharts-area")).toBeNull();
  });

  it("draws the actual only up to Today: no Today means no open dot, a first day has just the dot", async () => {
    await home(signedIn(dashboardOf(pointsOf(1))));
    await drawn();
    expect(document.querySelectorAll(".recharts-reference-dot")).toHaveLength(1);
  });

  it("marks no Today when the server marked none", async () => {
    const points = pointsOf().map((p) => ({ date: p.date, target: p.target, actual: p.actual }));
    await home(signedIn(dashboardOf(points)));
    await drawn();
    expect(document.querySelectorAll(".recharts-reference-dot")).toHaveLength(0);
  });

  it("labels the Y axis with digits and no currency symbol, and the X axis with the Period's two ends", async () => {
    await home(signedIn(dashboardOf(pointsOf())));
    await drawn();
    await waitFor(() => expect(yTicks().length).toBeGreaterThan(1));
    for (const tick of yTicks()) {
      expect(tick.textContent).toMatch(/^-?[\d,]+$/);
    }
    // The X ticks are the first and last day of the Period, as the server sent them.
    expect(xTicks().map((t) => t.textContent)).toEqual(["1 November", "30 November"]);
    // Axis text is 12px.
    for (const text of [...xTicks(), ...yTicks()]) {
      expect(text.getAttribute("font-size")).toBe("12");
    }
  });

  it("separates Y digits as the locale does", async () => {
    await home(signedIn(dashboardOf(pointsOf(), "-1.0000"), "id-ID"));
    await screen.findByText(idID.dashboard.chart.target, {
      selector: ".recharts-legend-item-text span",
    });
    await waitFor(() => expect(yTicks().length).toBeGreaterThan(1));
    const ticks = yTicks().map((t) => t.textContent ?? "");
    expect(ticks.some((t) => /^\d{1,3}(\.\d{3})+$/.test(t))).toBe(true);
    expect(ticks.join(" ")).not.toContain(",");
  });
});
