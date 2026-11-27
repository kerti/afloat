import { describe, expect, it } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { dashboardKey } from "@/api/queries";
import type { Dashboard } from "@/api/client";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

// Home answers "how much is left to spend today" (#155 PR 5, D1, D4, D8). Every
// figure is the server's: the screen formats and derives nothing.

type Trajectory = NonNullable<Dashboard["trajectory"]>;

const trajectory: Trajectory = {
  left_to_spend_today: "203448.2758",
  todays_allowance: "203448.2758",
  days_remaining: 17,
  remaining_daily_pool: "3458620.7586",
  baseline_daily_budget: "100000.0000",
};

const dashboardOf = (over: Partial<Trajectory> = {}): Dashboard => ({
  currency: "IDR",
  trajectory: { ...trajectory, ...over },
});

const signedIn = (dashboard?: Dashboard, mode: "adaptive" | "fixed" = "adaptive"): State => ({
  signedIn: true,
  me: { ...alice, household: { ...alice.household, allowance_mode: mode } },
  dashboard,
});

const hero = async () =>
  within(await screen.findByRole("region", { name: enGB.dashboard.leftToSpendToday }));
const figureOf = (label: string) => screen.getByText(label).closest("div")!;

async function home(state: State, path = "/") {
  server.use(...defaultHandlers(state));
  const view = renderApp(path);
  await screen.findByRole("heading", { name: state.me.locale === "id-ID" ? "Beranda" : "Home" });
  return view;
}

describe("the hero", () => {
  it("is the server's Left to Spend Today, cut toward minus infinity at the rupiah", async () => {
    await home(signedIn(dashboardOf({ left_to_spend_today: "203448.9999" })));
    expect(await screen.findByRole("region", { name: "Left to spend today" })).toBeInTheDocument();
    expect((await hero()).getByText("Rp203,448")).toBeInTheDocument();
  });

  it("shows a negative as negative, grown away from zero, never clamped or tinted", async () => {
    await home(signedIn(dashboardOf({ left_to_spend_today: "-0.5000" })));
    const figure = await (await hero()).findByText("-Rp1");
    expect(figure.className).not.toMatch(/attention|destructive|red|green/);
    expect((await hero()).queryByText(/^Rp0$/)).not.toBeInTheDocument();
  });

  it("never prints a negative zero", async () => {
    await home(signedIn(dashboardOf({ left_to_spend_today: "-0.0000" })));
    expect(await (await hero()).findByText("Rp0")).toBeInTheDocument();
  });

  it("is a labelled figure and not a live region", async () => {
    await home(signedIn(dashboardOf()));
    const region = await screen.findByRole("region", { name: "Left to spend today" });
    expect(region.closest("[aria-live]")).toBeNull();
    expect(region.querySelector("[aria-live],[role=status],[role=alert]")).toBeNull();
  });

  it("steps its size down for the 16-digit worst case so it never wraps", async () => {
    await home(signedIn(dashboardOf({ left_to_spend_today: "9999999999999999.9999" })));
    const figure = await (await hero()).findByText("Rp9,999,999,999,999,999");
    expect(figure.className).toContain("text-2xl");
    expect(figure.className).toContain("tabular-nums");
  });
});

describe("Today's allowance and the Baseline", () => {
  it("are a pair in adaptive mode, each cut toward minus infinity", async () => {
    await home(signedIn(dashboardOf({ baseline_daily_budget: "100000.9999" })));
    expect(await screen.findByText("Today's allowance")).toBeInTheDocument();
    expect(figureOf("Today's allowance")).toHaveTextContent("Rp203,448");
    expect(figureOf("Baseline")).toHaveTextContent("Rp100,000");
  });

  it("are still a pair in adaptive mode when the amounts are equal", async () => {
    await home(signedIn(dashboardOf({ todays_allowance: "100000.0000" })));
    expect(await screen.findByText("Baseline")).toBeInTheDocument();
    expect(figureOf("Today's allowance")).toHaveTextContent("Rp100,000");
    expect(figureOf("Baseline")).toHaveTextContent("Rp100,000");
  });

  it("are the allowance alone in fixed mode, even when the amounts differ", async () => {
    await home(signedIn(dashboardOf({ todays_allowance: "100000.0000" }), "fixed"));
    expect(await screen.findByText("Today's allowance")).toBeInTheDocument();
    expect(figureOf("Today's allowance")).toHaveTextContent("Rp100,000");
    // The wire still carries a different baseline; mode, not amounts, decides.
    expect(screen.queryByText("Baseline")).not.toBeInTheDocument();
    expect(screen.queryByText("Rp100,000", { selector: "dd" })).toBeInTheDocument();
  });

  it("are the allowance alone when fully committed, with no word about the Baseline", async () => {
    const full = dashboardOf({ todays_allowance: "100000.0000" });
    delete full.trajectory!.baseline_daily_budget;
    await home(signedIn(full));
    expect(await screen.findByText("Today's allowance")).toBeInTheDocument();
    expect(screen.queryByText("Baseline")).not.toBeInTheDocument();
    expect(screen.queryByText(/committed|plan|used/i)).not.toBeInTheDocument();
  });
});

describe("Days remaining and the Remaining Daily Pool", () => {
  it("show the server's count and pool, the pool half-up where the allowance is cut", async () => {
    await home(signedIn(dashboardOf({ remaining_daily_pool: "2999999.5000" })));
    const days = (await screen.findByText("Days remaining")).closest("li")!;
    expect(days).toHaveTextContent("17");
    expect(screen.getByText("Remaining Daily Pool").closest("li")).toHaveTextContent("Rp3,000,000");
  });

  it("imply no sum in fixed mode: no multiplication and no sentence linking the figures", async () => {
    await home(signedIn(dashboardOf(), "fixed"));
    await screen.findByText("Days remaining");
    expect(document.body.textContent).not.toMatch(/[×*]/);
    // D8's list holds its two figures and nothing that links them.
    const d8 = screen.getByText("Days remaining").closest("ul")!;
    expect(within(d8).getAllByRole("listitem")).toHaveLength(2);
  });

  it("shows a negative pool as it is", async () => {
    await home(signedIn(dashboardOf({ remaining_daily_pool: "-1500.0000" })));
    expect((await screen.findByText("Remaining Daily Pool")).closest("li")).toHaveTextContent(
      "-Rp1,500",
    );
  });
});

describe("with no Monthly Budget", () => {
  it("says what to do and links to the Budget, with no figures", async () => {
    await home(signedIn());
    expect(await screen.findByText(enGB.dashboard.noBudget)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: enGB.dashboard.noBudgetLink })).toHaveAttribute(
      "href",
      "/budget",
    );
    expect(screen.queryByRole("region", { name: "Left to spend today" })).not.toBeInTheDocument();
    expect(screen.queryByText("Days remaining")).not.toBeInTheDocument();
  });
});

describe("loading, failure and refetch", () => {
  it("shows Loading while the first read runs", async () => {
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    server.use(
      http.get("/api/dashboard", async () => {
        await held;
        return HttpResponse.json(dashboardOf());
      }),
      ...defaultHandlers(signedIn()),
    );
    renderApp("/");
    expect(await screen.findByRole("status")).toHaveTextContent("Loading…");
    release();
    expect(await screen.findByRole("region", { name: "Left to spend today" })).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("shows the failure with Try again, and recovers on it", async () => {
    let fail = true;
    server.use(
      http.get("/api/dashboard", () =>
        fail
          ? HttpResponse.json(envelope("INTERNAL"), { status: 500 })
          : HttpResponse.json(dashboardOf()),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/");
    expect(await screen.findByRole("alert")).toHaveTextContent(enGB.errors.INTERNAL);
    expect(screen.queryByRole("region", { name: "Left to spend today" })).not.toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("region", { name: "Left to spend today" })).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("keeps the figures on screen while a refetch runs, with no spinner", async () => {
    const { client } = await home(signedIn(dashboardOf({ left_to_spend_today: "176470.5882" })));
    expect(await (await hero()).findByText("Rp176,470")).toBeInTheDocument();

    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    server.use(
      http.get("/api/dashboard", async () => {
        await held;
        return HttpResponse.json(dashboardOf({ left_to_spend_today: "90000.0000" }));
      }),
    );
    let refetch!: Promise<unknown>;
    act(() => {
      refetch = client.invalidateQueries({ queryKey: dashboardKey });
    });
    await waitFor(() => expect(client.isFetching({ queryKey: dashboardKey })).toBe(1));
    expect((await hero()).getByText("Rp176,470")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    release();
    await act(async () => {
      await refetch;
    });
    expect(await (await hero()).findByText("Rp90,000")).toBeInTheDocument();
  });

  it("keeps the figures on screen when a refetch fails, with no alert", async () => {
    const { client } = await home(signedIn(dashboardOf({ left_to_spend_today: "176470.5882" })));
    expect(await (await hero()).findByText("Rp176,470")).toBeInTheDocument();

    server.use(
      http.get("/api/dashboard", () => HttpResponse.json(envelope("INTERNAL"), { status: 500 })),
    );
    await act(async () => {
      await client.refetchQueries({ queryKey: dashboardKey });
    });
    await waitFor(() => expect(client.getQueryState(dashboardKey)?.status).toBe("error"));

    expect((await hero()).getByText("Rp176,470")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("in Indonesian", () => {
  it("words the figures in the ruled and drafted terms, and groups with dots", async () => {
    const state = signedIn(dashboardOf());
    state.me = { ...state.me, locale: "id-ID" };
    await home(state);
    const region = await screen.findByRole("region", { name: idID.dashboard.leftToSpendToday });
    expect(within(region).getByText("Rp203.448")).toBeInTheDocument();
    expect(figureOf(idID.dashboard.todaysAllowance)).toHaveTextContent("Rp203.448");
    expect(figureOf(idID.dashboard.baseline)).toHaveTextContent("Rp100.000");
    expect(screen.getByText(idID.dashboard.remainingDailyPool).closest("li")).toHaveTextContent(
      "Rp3.458.621",
    );
    expect(idID.dashboard.todaysAllowance).toMatch(/jatah harian/i);
    expect(idID.dashboard.baseline).toBe("Jatah dasar");
  });

  it("asks for a Monthly Budget in Indonesian when there is none", async () => {
    const state = signedIn();
    state.me = { ...state.me, locale: "id-ID" };
    await home(state);
    expect(await screen.findByText(idID.dashboard.noBudget)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: idID.dashboard.noBudgetLink })).toHaveAttribute(
      "href",
      "/budget",
    );
  });
});

describe("what Home does not show yet", () => {
  it("has no State, pace, chart or breakdowns, no colour and no placeholder lines", async () => {
    await home(signedIn(dashboardOf()));
    await screen.findByText("Days remaining");
    for (const word of [
      /Afloat$/,
      /Drifting/,
      /Taking on water/,
      /pace/i,
      /overspent/i,
      /should/i,
    ]) {
      expect(screen.queryByText(word, { selector: "main *" })).not.toBeInTheDocument();
    }
    expect(
      screen.queryByText(/Signed in as|Household:|dashboard will appear/),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });
});
