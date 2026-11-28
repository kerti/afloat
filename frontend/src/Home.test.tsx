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
// The supporting figures sit at the card's foot: a dt and its dd.
const figureOf = (label: string) => screen.getByText(label).closest("div")!;

// A Period of `days` days with Today at `today` (0-based), the target line
// ending at `pool` and `actual` spent through Today.
const chartOf = (days: number, today: number, actual: string, pool: string) => ({
  points: Array.from({ length: days }, (_, i) => ({
    date: `2026-11-${String(i + 1).padStart(2, "0")}`,
    target: i === days - 1 ? pool : "0.0000",
    ...(i < today ? { actual: "0.0000" } : {}),
    ...(i === today ? { actual, in_progress: true as const } : {}),
  })),
});

async function home(state: State, path = "/") {
  server.use(...defaultHandlers(state));
  const view = renderApp(path);
  await screen.findByRole("heading", { name: state.me.locale === "id-ID" ? "Beranda" : "Home" });
  return view;
}

describe("the title", () => {
  it("is there for screen readers only: the lit tab and the hero say what the screen is", async () => {
    await home(signedIn());
    const title = screen.getByRole("heading", { level: 1, name: "Home" });
    expect(title).toHaveClass("sr-only");
  });
});

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
    expect(figure.className).toContain("text-[4.6cqw]");
    expect(figure.className).toContain("tabular-nums");
  });
});

describe("Today's allowance, under the hero, and the Baseline", () => {
  it("reads as what the hero is out of, each cut toward minus infinity", async () => {
    await home(
      signedIn(
        dashboardOf({
          left_to_spend_today: "150000.9999",
          todays_allowance: "203448.9999",
          baseline_daily_budget: "100000.9999",
        }),
      ),
    );
    const of = (await hero()).getByText("of Rp203,448");
    expect(of.className).toContain("tabular-nums");
    expect(of.previousElementSibling).toHaveTextContent("Rp150,000");
    expect(screen.queryByText("Today's allowance")).not.toBeInTheDocument();
    expect(figureOf("Baseline")).toHaveTextContent("Rp100,000");
  });

  it("keeps the Baseline row in adaptive mode when the amounts are equal", async () => {
    await home(signedIn(dashboardOf({ todays_allowance: "100000.0000" })));
    expect((await hero()).getByText("of Rp100,000")).toBeInTheDocument();
    expect(figureOf("Baseline")).toHaveTextContent("Rp100,000");
  });

  it("has no Baseline row in fixed mode, even when the amounts differ", async () => {
    await home(signedIn(dashboardOf({ todays_allowance: "100000.0000" }), "fixed"));
    expect((await hero()).getByText("of Rp100,000")).toBeInTheDocument();
    // The wire still carries a different baseline; mode, not amounts, decides.
    expect(screen.queryByText("Baseline")).not.toBeInTheDocument();
  });

  it("has no Baseline row when fully committed, with no word about it", async () => {
    const full = dashboardOf({ todays_allowance: "100000.0000" });
    delete full.trajectory!.baseline_daily_budget;
    await home(signedIn(full));
    expect((await hero()).getByText("of Rp100,000")).toBeInTheDocument();
    expect(screen.queryByText("Baseline")).not.toBeInTheDocument();
    expect(screen.queryByText(/committed|plan|used/i)).not.toBeInTheDocument();
  });
});

describe("the card's foot: the Baseline and the Remaining Daily Pool", () => {
  it("shows the server's pool, half-up where the allowance is cut", async () => {
    await home(signedIn(dashboardOf({ remaining_daily_pool: "2999999.5000" })));
    expect(await (await hero()).findByText("Remaining Daily Pool")).toBeInTheDocument();
    expect(figureOf("Remaining Daily Pool")).toHaveTextContent("Rp3,000,000");
  });

  it("has the pool alone in fixed mode, with no sum implied", async () => {
    await home(signedIn(dashboardOf(), "fixed"));
    const pool = await screen.findByText("Remaining Daily Pool");
    expect(document.body.textContent).not.toMatch(/[×*]/);
    expect(within(pool.closest("dl")!).getAllByRole("term")).toHaveLength(1);
  });

  it("is in order, the Baseline first in adaptive mode", async () => {
    await home(signedIn(dashboardOf()));
    const pool = await screen.findByText("Remaining Daily Pool");
    expect(
      within(pool.closest("dl")!)
        .getAllByRole("term")
        .map((d) => d.textContent),
    ).toEqual(["Baseline", "Remaining Daily Pool"]);
  });

  it("shows a negative pool as it is", async () => {
    await home(signedIn(dashboardOf({ remaining_daily_pool: "-1500.0000" })));
    await screen.findByText("Remaining Daily Pool");
    expect(figureOf("Remaining Daily Pool")).toHaveTextContent("-Rp1,500");
  });

  it("has no disclosure and no Days remaining row: the ring says it", async () => {
    await home(signedIn(dashboardOf()));
    await screen.findByText("Remaining Daily Pool");
    expect(screen.queryByRole("button", { name: "Details" })).not.toBeInTheDocument();
    expect(screen.queryByText("Days remaining")).not.toBeInTheDocument();
  });
});

describe('the hero\'s "of" line', () => {
  it.each(["0.0000", "-45416.0000"])("is left out when Today's allowance is %s", async (a) => {
    await home(signedIn(dashboardOf({ left_to_spend_today: a, todays_allowance: a })));
    await screen.findByText("Remaining Daily Pool");
    expect((await hero()).queryByText(/^of /)).not.toBeInTheDocument();
  });
});

describe("the dial's ring", () => {
  const withChart = (chart: ReturnType<typeof chartOf>, days_remaining: number): Dashboard => ({
    ...dashboardOf({ days_remaining }),
    chart,
  });

  it("says the days left and the share of the Daily Pool spent, toward zero", async () => {
    await home(signedIn(withChart(chartOf(30, 13, "1999999.9999", "4000000.0000"), 17)));
    const region = await hero();
    expect(region.getAllByText("17 days left").length).toBeGreaterThan(0);
    expect(region.getAllByText("49% spent").length).toBeGreaterThan(0);
    // once for the eye, curved on the ring, and once for a screen reader
    expect(region.getByText("17 days left, 49% spent")).toHaveClass("sr-only");
  });

  it("says one day in the singular, and a share past the whole as it is", async () => {
    await home(signedIn(withChart(chartOf(30, 29, "4320000.0000", "4000000.0000"), 1)));
    expect((await hero()).getByText("1 day left, 108% spent")).toBeInTheDocument();
  });

  it("has no share when there is no pool to read against", async () => {
    await home(signedIn(withChart(chartOf(30, 13, "0.0000", "0.0000"), 17)));
    const region = await hero();
    expect(region.getAllByText("17 days left").length).toBeGreaterThan(0);
    expect(region.queryByText(/spent/)).not.toBeInTheDocument();
  });

  it("has no ring labels without a chart", async () => {
    await home(signedIn(dashboardOf()));
    await screen.findByText("Remaining Daily Pool");
    expect(screen.queryByText(/days? left|spent/)).not.toBeInTheDocument();
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
    expect(screen.queryByText("Remaining Daily Pool")).not.toBeInTheDocument();
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
    expect(within(region).getByText("dari Rp203.448")).toBeInTheDocument();
    expect(figureOf(idID.dashboard.baseline)).toHaveTextContent("Rp100.000");
    expect(figureOf(idID.dashboard.remainingDailyPool)).toHaveTextContent("Rp3.458.621");
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

const mainText = (text: string) => screen.queryByText(text, { selector: "main p" });
const STATES = [
  ["afloat", "Afloat"],
  ["drifting", "Drifting"],
  ["taking_on_water", "Taking on water"],
] as const;

describe("the State (D2): the name and nothing else", () => {
  it.each(STATES)("shows %s as the word %s, whatever the pace says", async (state, word) => {
    await home(signedIn(dashboardOf({ state, trajectory_variance: "420000.0000" })));
    expect(await screen.findByText(word, { selector: "main p" })).toBeInTheDocument();
    for (const [, other] of STATES) {
      if (other !== word) expect(mainText(other)).not.toBeInTheDocument();
    }
  });

  it("shows no State when the server sent none, as when fully committed", async () => {
    const full = dashboardOf({ todays_allowance: "100000.0000" });
    delete full.trajectory!.baseline_daily_budget;
    await home(signedIn(full));
    await screen.findByText("Remaining Daily Pool");
    for (const [, word] of STATES) expect(mainText(word)).not.toBeInTheDocument();
    expect(screen.queryByText(/pace/i)).not.toBeInTheDocument();
  });

  it("reads plain Afloat while the warm-up pins it, beside the pace as it is", async () => {
    await home(signedIn(dashboardOf({ state: "afloat", trajectory_variance: "-200000.0000" })));
    expect(await screen.findByText("Afloat", { selector: "main p" })).toBeInTheDocument();
    expect(screen.getByText("Rp200,000 behind pace")).toBeInTheDocument();
  });

  it("reads Afloat on day one with no pace sentence, because nothing has settled", async () => {
    await home(signedIn(dashboardOf({ state: "afloat" })));
    expect(await screen.findByText("Afloat", { selector: "main p" })).toBeInTheDocument();
    expect(screen.queryByText(/pace/i)).not.toBeInTheDocument();
  });

  it("is the name in the sea, uncoloured by verdict, with no sentence and no live region", async () => {
    await home(
      signedIn(dashboardOf({ state: "taking_on_water", trajectory_variance: "-350000.0000" })),
    );
    const word = await screen.findByText("Taking on water", { selector: "main p" });
    expect(word.className).not.toMatch(/attention|destructive|success|red|green|amber|orange/);
    expect(word.textContent).toBe("Taking on water");
    const region = screen.getByRole("region", { name: "Left to spend today" });
    expect(region.querySelector("[aria-live],[role=status],[role=alert]")).toBeNull();
    // The sea is drawing only: hidden from assistive technology.
    expect(region.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  });

  it("sits in the dial with the figure, beside the numbers and never instead of them", async () => {
    await home(signedIn(dashboardOf({ state: "drifting", trajectory_variance: "-100000.0000" })));
    const word = await screen.findByText("Drifting", { selector: "main p" });
    const region = screen.getByRole("region", { name: "Left to spend today" });
    expect(region).toContainElement(word);
    expect(within(region).getByText("Rp203,448")).toBeInTheDocument();
    expect(within(region).getByText("Remaining Daily Pool")).toBeInTheDocument();
  });
});

describe("the pace sentence (D3, R8)", () => {
  const pace = async (variance: string) => {
    await home(signedIn(dashboardOf({ state: "afloat", trajectory_variance: variance })));
    await screen.findByText("Remaining Daily Pool");
  };

  it("says ahead of pace for a variance above zero, which is less spent than the target", async () => {
    await pace("420000.0000");
    expect(screen.getByText("Rp420,000 ahead of pace")).toBeInTheDocument();
  });

  it("says behind pace for a variance below zero, with no sign character", async () => {
    await pace("-420000.0000");
    const sentence = screen.getByText("Rp420,000 behind pace");
    expect(sentence).toBeInTheDocument();
    expect(sentence.textContent).not.toMatch(/[-+\u2212]/);
  });

  it.each(["0.4999", "-0.4999", "0.0000", "-0.0000", "0"])(
    "says On pace when %s shows as zero, never Rp0 ahead or behind",
    async (variance) => {
      await pace(variance);
      expect(screen.getByText("On pace")).toBeInTheDocument();
      expect(screen.queryByText(/ahead|behind|Rp0/)).not.toBeInTheDocument();
    },
  );

  it.each([
    ["0.5000", "Rp1 ahead of pace"],
    ["-0.5000", "Rp1 behind pace"],
    ["-0.5", "Rp1 behind pace"],
  ])("rounds %s half away from zero, then reads %s", async (variance, sentence) => {
    await pace(variance);
    expect(screen.getByText(sentence)).toBeInTheDocument();
    expect(screen.queryByText("On pace")).not.toBeInTheDocument();
  });

  it("writes the whole amount, never abbreviated", async () => {
    await pace("-1234567.8900");
    expect(screen.getByText("Rp1,234,568 behind pace")).toBeInTheDocument();
    expect(screen.queryByText(/\d\s?(k|rb|jt|m)\b/i)).not.toBeInTheDocument();
  });

  it("is the muted sentence under the State, in no colour", async () => {
    await pace("420000.0000");
    const sentence = screen.getByText("Rp420,000 ahead of pace");
    expect(sentence.className).toMatch(/text-muted-foreground/);
    expect(sentence.className).not.toMatch(/attention|destructive|red|green/);
  });

  it("is absent with the State alone when the server sent no variance", async () => {
    await home(signedIn(dashboardOf({ state: "drifting" })));
    expect(await screen.findByText("Drifting", { selector: "main p" })).toBeInTheDocument();
    expect(screen.queryByText(/pace/i)).not.toBeInTheDocument();
  });

  it("is absent when the variance is not a plain decimal, not a guess", async () => {
    await home(signedIn(dashboardOf({ state: "afloat", trajectory_variance: "1e6" })));
    expect(await screen.findByText("Afloat", { selector: "main p" })).toBeInTheDocument();
    expect(screen.queryByText(/pace/i)).not.toBeInTheDocument();
  });

  it("follows the Household's currency", async () => {
    const state = signedIn({
      currency: "USD",
      trajectory: { ...trajectory, state: "afloat", trajectory_variance: "12.3450" },
    });
    await home(state);
    expect(await screen.findByText(/^USD\s12\.35 ahead of pace$/)).toBeInTheDocument();
  });
});

describe("the D9 audit of the State and the pace", () => {
  it.each([
    ["afloat", "420000.0000"],
    ["drifting", "-150000.0000"],
    ["taking_on_water", "-9999999.0000"],
  ] as const)(
    "%s says nothing of fault, advice or score, and colours nothing",
    async (state, v) => {
      await home(signedIn(dashboardOf({ state, trajectory_variance: v })));
      await screen.findByText("Remaining Daily Pool");
      const main = document.querySelector("main")!;
      expect(main.textContent).not.toMatch(
        /overspen|over budget|failed|should|must|careful|warning|streak|score|%/i,
      );
      expect(
        main.querySelector("[class*=attention],[class*=destructive],[class*=success]"),
      ).toBeNull();
      expect(main.querySelector("meter,progress,[role=progressbar],[role=meter]")).toBeNull();
    },
  );
});

describe("the State and the pace in Indonesian (Q-10, ruled 27 Nov 2026)", () => {
  const inIndonesian = async (over: Partial<Trajectory>) => {
    const state = signedIn(dashboardOf(over));
    state.me = { ...state.me, locale: "id-ID" };
    await home(state);
    await screen.findByText(idID.dashboard.remainingDailyPool);
  };

  it.each([
    ["afloat", "Aman"],
    ["drifting", "Bocor"],
    ["taking_on_water", "Nyaris tenggelam"],
  ] as const)("words %s as %s", async (state, word) => {
    await inIndonesian({ state });
    expect(screen.getByText(word, { selector: "main p" })).toBeInTheDocument();
  });

  it("words ahead, behind and on pace, with dots grouping the amount", async () => {
    await inIndonesian({ state: "afloat", trajectory_variance: "1420000.0000" });
    expect(screen.getByText("Rp1.420.000 di depan laju")).toBeInTheDocument();
  });

  it("words behind pace", async () => {
    await inIndonesian({ state: "drifting", trajectory_variance: "-420000.0000" });
    expect(screen.getByText("Rp420.000 di belakang laju")).toBeInTheDocument();
  });

  it("words on pace for an amount that shows as zero", async () => {
    await inIndonesian({ state: "afloat", trajectory_variance: "-0.4999" });
    expect(screen.getByText(idID.dashboard.pace.on)).toBeInTheDocument();
  });
});

describe("what Home does not show yet", () => {
  it("has no State or pace without the server's, no chart, and no placeholder lines", async () => {
    await home(signedIn(dashboardOf()));
    await screen.findByText("Remaining Daily Pool");
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
