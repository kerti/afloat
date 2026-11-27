import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import type { CommitmentFigure, Dashboard } from "@/api/client";
import { alice, defaultHandlers, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

// Home's Commitments panel (#155 PR 9, D6, R16): the server's figures per
// Budget Period, as what is still due and, under it, what was paid of what was
// set aside. Nothing is derived, drawn or coloured.

const rent: CommitmentFigure = {
  category_id: "0192f0a0-0000-7000-8000-0000000000c1",
  category_display_name: "Rent",
  active: true,
  allocation: "2000000.0000",
  paid: "1500000.0000",
  still_due: "500000.0000",
};
const school: CommitmentFigure = {
  category_id: "0192f0a0-0000-7000-8000-0000000000c2",
  category_display_name: "School",
  active: true,
  allocation: "1000000.0000",
  paid: "1000000.0000",
  still_due: "0.0000",
};
const tax: CommitmentFigure = {
  category_id: "0192f0a0-0000-7000-8000-0000000000c3",
  category_display_name: "Tax",
  active: false,
  allocation: "1000000.0000",
  paid: "1200000.0000",
  still_due: "-200000.0000",
};

const commitmentsOf = (items: CommitmentFigure[]): NonNullable<Dashboard["commitments"]> => ({
  commitments_budget: "4000000.0000",
  paid: "3700000.0000",
  still_due: "300000.0000",
  items,
});

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
    name: locale === "id-ID" ? idID.dashboard.commitments.title : enGB.dashboard.commitments.title,
  });
  return view;
}

const section = (name = "Commitments") => within(screen.getByRole("region", { name }));
const rowsOf = (name = "Commitments") => section(name).queryAllByRole("listitem");

describe("Commitments", () => {
  it("is a section titled Commitments with a totals row first, then one row per Commitment in the order sent", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([rent, school, tax]) }));
    expect(screen.getByRole("heading", { level: 2, name: "Commitments" })).toBeInTheDocument();
    const rows = rowsOf();
    expect(rows).toHaveLength(4);
    expect(rows[0]).toHaveTextContent("Total");
    expect(rows[1]).toHaveTextContent("Rent");
    expect(rows[2]).toHaveTextContent("School");
    expect(rows[3]).toHaveTextContent("Tax");
  });

  it("goes after the chart's place and before By Category, in the order a person asks the questions", async () => {
    await home(
      dashboardOf({
        commitments: commitmentsOf([rent]),
        categories: [{ spent: "1000.0000" }],
        chart: { points: [{ date: "2026-11-01", target: "100.0000", actual: "0.0000" }] },
      }),
    );
    const regions = screen
      .getAllByRole("region")
      .map((r) => r.querySelector("h2")?.textContent)
      .filter((name) => name !== undefined);
    expect(regions).toEqual(["This Period", "Commitments", "By Category", "By Pocket"]);
  });

  it("shows what is still due, and under it what was paid of what was set aside", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([rent]) }));
    const row = rowsOf()[1]!;
    expect(row).toHaveTextContent("Rp500,000 still due");
    const detail = within(row).getByText("Rp1,500,000 paid of Rp2,000,000");
    expect(detail.className).toContain("text-muted-foreground");
    // Under the amount, not the name (as D7's rows).
    expect(detail.parentElement).not.toHaveTextContent("Rent");
  });

  it("says Paid, in the muted words, where nothing is still due", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([school]) }));
    const row = rowsOf()[1]!;
    expect(within(row).getByText("Paid").className).toContain("text-muted-foreground");
    expect(row).not.toHaveTextContent(/still due/);
    expect(within(row).getByText("Rp1,000,000 paid of Rp1,000,000")).toBeInTheDocument();
  });

  it("says more than you set aside, as information, where more was paid than the allocation", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([tax]) }));
    const row = rowsOf()[1]!;
    expect(row).toHaveTextContent("Rp200,000 more than you set aside");
    // The signed figure is never shown as it is: no minus sign, no "still due".
    expect(row.textContent).not.toContain("-Rp");
    expect(row).not.toHaveTextContent(/still due|overspen|over budget|failed|should/i);
    expect(within(row).getByText("Rp1,200,000 paid of Rp1,000,000")).toBeInTheDocument();
  });

  it("reads the totals row as it reads a Commitment: still due, Paid, or more than set aside", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([rent, tax]) }));
    expect(rowsOf()[0]).toHaveTextContent("Rp300,000 still due");
    expect(within(rowsOf()[0]!).getByText("Rp3,700,000 paid of Rp4,000,000")).toBeInTheDocument();
  });

  it.each([
    ["0.0000", "-0.0000"],
    ["0.4999", "-0.4999"],
  ])(
    "chooses the words from the amount as shown: %s and %s are Paid, never Rp0 still due",
    async (a, b) => {
      await home(
        dashboardOf({
          commitments: {
            commitments_budget: "10.0000",
            paid: "10.0000",
            still_due: a,
            items: [
              { ...school, still_due: a },
              { ...school, category_id: "0192f0a0-0000-7000-8000-0000000000c9", still_due: b },
            ],
          },
        }),
      );
      const rows = rowsOf();
      for (const row of rows) {
        expect(within(row).getByText("Paid")).toBeInTheDocument();
        expect(row.textContent).not.toMatch(/Rp0\b/);
      }
    },
  );

  it("shows an allocation of zero as it is, with a refund as a negative paid", async () => {
    await home(
      dashboardOf({
        commitments: commitmentsOf([
          { ...school, allocation: "0.0000", paid: "-50000.0000", still_due: "50000.0000" },
        ]),
      }),
    );
    const row = rowsOf()[1]!;
    expect(row).toHaveTextContent("Rp50,000 still due");
    expect(within(row).getByText("-Rp50,000 paid of Rp0")).toBeInTheDocument();
  });

  it("wears an inactive Commitment as a muted title and the Inactive badge, as every list row does", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([rent, tax]) }));
    const [, rentRow, taxRow] = rowsOf();
    expect(within(taxRow!).getByText("Tax").className).toContain("text-muted-foreground");
    expect(within(taxRow!).getByText("Inactive")).toBeInTheDocument();
    expect(within(rentRow!).getByText("Rent").className).not.toContain("text-muted-foreground");
    expect(within(rentRow!).queryByText("Inactive")).not.toBeInTheDocument();
  });

  it("leaves out an inactive Commitment with nothing set aside and nothing paid, and keeps one with either", async () => {
    const empty = {
      ...tax,
      category_id: "0192f0a0-0000-7000-8000-0000000000c4",
      category_display_name: "Trip",
      allocation: "0.0000",
      paid: "0.0000",
      still_due: "0.0000",
    };
    const paidOnly = {
      ...empty,
      category_id: "0192f0a0-0000-7000-8000-0000000000c5",
      category_display_name: "Visa",
      paid: "50000.0000",
      still_due: "-50000.0000",
    };
    const activeEmpty = {
      ...empty,
      category_id: "0192f0a0-0000-7000-8000-0000000000c6",
      category_display_name: "Gift",
      active: true,
    };
    await home(dashboardOf({ commitments: commitmentsOf([rent, empty, paidOnly, activeEmpty]) }));
    const names = rowsOf().map((r) => r.textContent ?? "");
    expect(names).toHaveLength(4);
    expect(names.join(" ")).not.toContain("Trip");
    expect(names[2]).toContain("Visa");
    expect(names[3]).toContain("Gift");
  });

  it("formats half-up and never abbreviates, to sixteen digits", async () => {
    await home(
      dashboardOf({
        commitments: {
          commitments_budget: "9999999999999999.4999",
          paid: "1500000.9999",
          still_due: "9999999998499998.5000",
          items: [
            { ...rent, allocation: "1500000.4999", paid: "1500000.9999", still_due: "-0.5000" },
            {
              ...school,
              allocation: "9999999998499999.0000",
              paid: "0.0000",
              still_due: "9999999998499999.0000",
            },
          ],
        },
      }),
    );
    const total = rowsOf()[0]!;
    expect(total).toHaveTextContent("Rp9,999,999,998,499,999 still due");
    expect(
      within(total).getByText("Rp1,500,001 paid of Rp9,999,999,999,999,999"),
    ).toBeInTheDocument();
    const row = rowsOf()[1]!;
    expect(within(row).getByText("Rp1,500,001 paid of Rp1,500,000")).toBeInTheDocument();
    expect(row).toHaveTextContent("Rp1 more than you set aside");
  });

  it("says what will appear, and keeps its heading, when the server sent no Commitments", async () => {
    await home(dashboardOf());
    expect(screen.getByRole("heading", { level: 2, name: "Commitments" })).toBeInTheDocument();
    expect(section().getByText("Your Commitments will appear here.")).toBeInTheDocument();
    expect(rowsOf()).toHaveLength(0);
  });

  it("is not on Home while there is no Monthly Budget: the server sent nothing, and Home asks for one", async () => {
    const state: State = { signedIn: true, me: alice, dashboard: { currency: "IDR" } };
    server.use(...defaultHandlers(state));
    renderApp("/");
    await screen.findByText(enGB.dashboard.noBudget);
    expect(screen.queryByText("Commitments")).not.toBeInTheDocument();
  });

  it("derives nothing: no bar, meter, percentage, colour or word of fault, and no live region", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([rent, school, tax]) }));
    const main = document.querySelector("main")!;
    expect(
      main.querySelector("meter,progress,[role=progressbar],[role=meter],svg[role=img]"),
    ).toBeNull();
    expect(
      main.querySelector(
        "[class*=attention],[class*=destructive],[class*=success],[class*=bg-primary]",
      ),
    ).toBeNull();
    expect(main.querySelector("[aria-live],[role=status],[role=alert]")).toBeNull();
    expect(main.textContent).not.toMatch(/overspen|over budget|failed|should|warning|%/i);
    // The figures shown are the wire's and no other: no sum of a row's own.
    expect(main.textContent).not.toContain("500,000 of");
    expect(main.textContent).not.toContain("Rp3,000,000");
  });

  it("is no link and no button: nothing on the panel is tappable", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([rent, school, tax]) }));
    for (const row of rowsOf()) expect(row.querySelector("a,button")).toBeNull();
  });
});

describe("in Indonesian", () => {
  it("words the panel, groups with dots, and reads each of the three states", async () => {
    await home(dashboardOf({ commitments: commitmentsOf([rent, school, tax]) }), "id-ID");
    expect(screen.getByRole("heading", { level: 2, name: "Komitmen" })).toBeInTheDocument();
    const rows = section("Komitmen").getAllByRole("listitem");
    expect(rows[0]).toHaveTextContent("Total");
    expect(rows[1]).toHaveTextContent("Rp500.000 masih harus dibayar");
    expect(within(rows[1]!).getByText("Rp1.500.000 dibayar dari Rp2.000.000")).toBeInTheDocument();
    expect(within(rows[2]!).getByText("Lunas")).toBeInTheDocument();
    expect(rows[3]).toHaveTextContent("Rp200.000 lebih dari yang Anda sisihkan");
    expect(within(rows[3]!).getByText(idID.pockets.inactive)).toBeInTheDocument();
  });

  it("says what will appear in Indonesian when the server sent none", async () => {
    await home(dashboardOf(), "id-ID");
    expect(screen.getByText(idID.dashboard.commitments.empty)).toBeInTheDocument();
  });
});
