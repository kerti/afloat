import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import type { CategoryFigure, Dashboard, PocketFigure } from "@/api/client";
import { alice, defaultHandlers, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

// Home's By Category and By Pocket (#155 PR 7, D7): the server's two lists,
// shown in the order they came, as spent or, where there is an allocation,
// "spent / allocation (pct)" over a share bar (ruled 28 Nov 2026).

const food: CategoryFigure = {
  category_id: "0192f0a0-0000-7000-8000-0000000000c1",
  category_display_name: "Food",
  active: true,
  spent: "40000.0000",
  allocation: "1500000.0000",
};
const snacks: CategoryFigure = {
  category_id: "0192f0a0-0000-7000-8000-0000000000c2",
  category_display_name: "Snacks",
  active: false,
  spent: "16000.0000",
};
const uncategorised: CategoryFigure = { spent: "45000.0000" };
const wallet: PocketFigure = {
  pocket_id: "0192f0a0-0000-7000-8000-0000000000b1",
  pocket_display_name: "Wallet",
  active: true,
  spent: "3107000.0000",
};
const oldCard: PocketFigure = {
  pocket_id: "0192f0a0-0000-7000-8000-0000000000b2",
  pocket_display_name: "Old card",
  active: false,
  spent: "4000.0000",
};

const dashboardOf = (over: Partial<Dashboard> = {}): Dashboard => ({
  currency: "IDR",
  trajectory: {
    left_to_spend_today: "203448.2758",
    todays_allowance: "203448.2758",
    days_remaining: 17,
    remaining_daily_pool: "3458620.7586",
    baseline_daily_budget: "100000.0000",
  },
  ...over,
});

async function home(dashboard: Dashboard, locale: "en-GB" | "id-ID" = "en-GB") {
  const state: State = {
    signedIn: true,
    me: { ...alice, locale, household: { ...alice.household, allowance_mode: "adaptive" } },
    dashboard,
  };
  server.use(...defaultHandlers(state));
  const view = renderApp("/");
  await screen.findByRole("heading", { name: locale === "id-ID" ? "Beranda" : "Home" });
  await screen.findByRole("region", {
    name: locale === "id-ID" ? idID.dashboard.byCategory : enGB.dashboard.byCategory,
  });
  return view;
}

const section = (name: string) => within(screen.getByRole("region", { name }));
const rowsOf = (name: string) => section(name).queryAllByRole("listitem");

describe("By Category", () => {
  it("is a section titled By Category after the State, with one row per figure, in the order sent", async () => {
    await home(dashboardOf({ categories: [uncategorised, food, snacks] }));
    expect(screen.getByRole("heading", { level: 2, name: "By Category" })).toBeInTheDocument();
    const rows = rowsOf("By Category");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("Uncategorised");
    expect(rows[1]).toHaveTextContent("Food");
    expect(rows[2]).toHaveTextContent("Snacks");
    // After the supporting figures, the last thing on Home for now.
    const main = document.querySelector("main")!.textContent!;
    expect(main.indexOf("Remaining Daily Pool")).toBeGreaterThanOrEqual(0);
    expect(main.indexOf("Remaining Daily Pool")).toBeLessThan(main.indexOf("By Category"));
    expect(main.indexOf("By Category")).toBeLessThan(main.indexOf("By Pocket"));
  });

  it("keeps the server's order, whatever the figures are", async () => {
    await home(
      dashboardOf({ categories: [snacks, food] }), // the larger is second: not re-sorted here
    );
    const rows = rowsOf("By Category");
    expect(rows[0]).toHaveTextContent("Snacks");
    expect(rows[1]).toHaveTextContent("Food");
  });

  it("shows spent / allocation (percent) where there is an allocation, and spent alone where not", async () => {
    await home(dashboardOf({ categories: [food, snacks] }));
    const [foodRow, snacksRow] = rowsOf("By Category");
    const figure = within(foodRow!).getByText("Rp40,000 / Rp1,500,000 (2%)");
    expect(figure.className).toContain("tabular-nums");
    expect(figure).not.toHaveTextContent("Food");
    expect(within(snacksRow!).getByText("Rp16,000")).toBeInTheDocument();
    expect(snacksRow!.textContent).not.toMatch(/\/|%|No allocation|Rp0/);
  });

  it("draws a share bar only under a Category with an allocation above zero", async () => {
    await home(
      dashboardOf({
        categories: [
          food,
          snacks,
          uncategorised,
          { ...food, category_id: "z", allocation: "0.0000" },
        ],
        pockets: [wallet],
      }),
    );
    const [foodRow, snacksRow, uncatRow, zeroRow] = rowsOf("By Category");
    expect(parseFloat(within(foodRow!).getByTestId("share-bar-fill").style.width)).toBeCloseTo(
      (40000 / 1500000 / 1.25) * 100,
    );
    for (const row of [snacksRow, uncatRow, zeroRow]) {
      expect(within(row!).queryByTestId("share-bar")).not.toBeInTheDocument();
    }
    expect(section("By Pocket").queryByTestId("share-bar")).not.toBeInTheDocument();
  });

  it("shows an allocation of zero as an allocation, and a refund net as negative", async () => {
    await home(
      dashboardOf({
        categories: [{ ...food, spent: "-5000.0000", allocation: "0.0000" }],
      }),
    );
    const [row] = rowsOf("By Category");
    expect(within(row!).getByText("-Rp5,000 / Rp0")).toBeInTheDocument();
  });

  it("formats spent half-up, as every figure but the allowances, and never abbreviates", async () => {
    await home(
      dashboardOf({
        categories: [{ ...food, spent: "9999999.5000", allocation: "1500000.4999" }],
      }),
    );
    const [row] = rowsOf("By Category");
    expect(within(row!).getByText("Rp10,000,000 / Rp1,500,000 (666%)")).toBeInTheDocument();
  });

  it("wears an inactive Category as a muted title and the Inactive badge, as every list row does", async () => {
    await home(dashboardOf({ categories: [food, snacks] }));
    const [foodRow, snacksRow] = rowsOf("By Category");
    const title = within(snacksRow!).getByText("Snacks");
    expect(title.className).toContain("text-muted-foreground");
    expect(within(snacksRow!).getByText("Inactive")).toBeInTheDocument();
    expect(within(foodRow!).getByText("Food").className).not.toContain("text-muted-foreground");
    expect(within(foodRow!).queryByText("Inactive")).not.toBeInTheDocument();
  });

  it("shows spending with no Category as Uncategorised, in the muted title of an Expense row, never as a link", async () => {
    await home(dashboardOf({ categories: [uncategorised] }));
    const [row] = rowsOf("By Category");
    expect(row).toHaveTextContent("Uncategorised");
    expect(row).toHaveTextContent("Rp45,000");
    expect(within(row!).getByText("Uncategorised").className).toContain("text-muted-foreground");
    expect(within(row!).queryByText("Inactive")).not.toBeInTheDocument();
    expect(row!.querySelector("a,button")).toBeNull();
    expect(screen.queryByText(/needs a category/i)).not.toBeInTheDocument();
  });

  it("says what will appear when the server sent no row", async () => {
    await home(dashboardOf({ pockets: [wallet] }));
    expect(
      section("By Category").getByText("Nothing recorded yet this Period."),
    ).toBeInTheDocument();
    expect(rowsOf("By Category")).toHaveLength(0);
    expect(rowsOf("By Pocket")).toHaveLength(1);
  });
});

describe("By Pocket", () => {
  it("is a section titled By Pocket with one row per figure, in the order sent", async () => {
    await home(dashboardOf({ pockets: [wallet, oldCard] }));
    expect(screen.getByRole("heading", { level: 2, name: "By Pocket" })).toBeInTheDocument();
    const rows = rowsOf("By Pocket");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Wallet");
    expect(rows[0]).toHaveTextContent("Rp3,107,000");
    expect(rows[1]).toHaveTextContent("Old card");
  });

  it("has no detail line: a Pocket has no allocation", async () => {
    await home(dashboardOf({ pockets: [wallet] }));
    expect(section("By Pocket").queryByText(/\//)).not.toBeInTheDocument();
  });

  it("wears an inactive Pocket as a muted title and the Inactive badge", async () => {
    await home(dashboardOf({ pockets: [wallet, oldCard] }));
    const [walletRow, cardRow] = rowsOf("By Pocket");
    expect(within(cardRow!).getByText("Old card").className).toContain("text-muted-foreground");
    expect(within(cardRow!).getByText("Inactive")).toBeInTheDocument();
    expect(within(walletRow!).queryByText("Inactive")).not.toBeInTheDocument();
  });

  it("says what will appear when the server sent no row", async () => {
    await home(dashboardOf({ categories: [food] }));
    expect(section("By Pocket").getByText("Nothing recorded yet this Period.")).toBeInTheDocument();
  });
});

describe("both sections", () => {
  it("say so, each in its own place, on a Period with no spending at all", async () => {
    await home(dashboardOf());
    expect(screen.getAllByText("Nothing recorded yet this Period.")).toHaveLength(2);
    expect(screen.getByRole("heading", { name: "By Category" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "By Pocket" })).toBeInTheDocument();
  });

  it("are not on Home while there is no Monthly Budget: the server sent nothing, and Home asks for one", async () => {
    const state: State = { signedIn: true, me: alice, dashboard: { currency: "IDR" } };
    server.use(...defaultHandlers(state));
    renderApp("/");
    await screen.findByText(enGB.dashboard.noBudget);
    expect(screen.queryByText("By Category")).not.toBeInTheDocument();
    expect(screen.queryByText("By Pocket")).not.toBeInTheDocument();
  });

  it("derive nothing but the percent: no meter or difference, colour only past 100% of a bar, and no word of fault", async () => {
    await home(
      dashboardOf({
        categories: [food, snacks, uncategorised],
        pockets: [wallet, oldCard],
      }),
    );
    const main = document.querySelector("main")!;
    expect(
      main.querySelector("meter,progress,[role=progressbar],[role=meter],svg[role=img]"),
    ).toBeNull();
    // The share bar (ruled 28 Nov 2026) is a picture the figures already say:
    // hidden from assistive technology, its fill the one bg-primary and the
    // share past 100% the one attention colour.
    // The hero's card is --primary whatever the figures say (ruled 28 Nov 2026):
    // a surface, not a tone, so it is out of this audit.
    screen.getByRole("region", { name: "Left to spend today" }).remove();
    for (const bar of main.querySelectorAll("[data-testid=share-bar]")) {
      expect(bar).toHaveAttribute("aria-hidden", "true");
      bar.remove();
    }
    expect(
      main.querySelector(
        "[class*=attention],[class*=destructive],[class*=success],[class*=bg-primary]",
      ),
    ).toBeNull();
    expect(main.textContent).not.toMatch(
      /overspen|over budget|under budget|left of|remaining of|failed|should|careful|warning|streak|score/i,
    );
    // The figures shown are the wire's and no other: Food's 40,000 of 1,500,000
    // leaves no 1,460,000 anywhere.
    expect(main.textContent).not.toContain("1,460,000");
    expect(main.textContent).not.toContain("Rp61,000");
  });

  it("are no live region: a refetch announces nothing", async () => {
    await home(dashboardOf({ categories: [food], pockets: [wallet] }));
    const main = document.querySelector("main")!;
    expect(main.querySelector("[aria-live],[role=status],[role=alert]")).toBeNull();
  });
});

describe("in Indonesian", () => {
  it("words the sections, groups with dots, and names Uncategorised", async () => {
    await home(
      dashboardOf({ categories: [food, uncategorised, snacks], pockets: [wallet, oldCard] }),
      "id-ID",
    );
    expect(screen.getByRole("heading", { name: "Per kategori" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Per kantong" })).toBeInTheDocument();
    const rows = within(
      screen.getByRole("region", { name: idID.dashboard.byCategory }),
    ).getAllByRole("listitem");
    expect(within(rows[0]!).getByText("Rp40.000 / Rp1.500.000 (2%)")).toBeInTheDocument();
    expect(rows[1]).toHaveTextContent("Tanpa kategori");
    expect(within(rows[2]!).getByText(idID.pockets.inactive)).toBeInTheDocument();
  });

  it("says what will appear in Indonesian when a list is empty", async () => {
    await home(dashboardOf(), "id-ID");
    expect(screen.getAllByText(idID.dashboard.breakdownEmpty)).toHaveLength(2);
  });
});
