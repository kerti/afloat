import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, emptyBudget, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);
beforeEach(() => {
  vi.clearAllMocks();
});

const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });

const planned = {
  ...emptyBudget,
  monthly_budget: "9000000.0000",
  daily_pool: "9000000.0000",
  unallocated: "9000000.0000",
};

describe("budget", () => {
  it("is reached from Home", async () => {
    server.use(...defaultHandlers(signedIn({ budget: planned })));
    const user = userEvent.setup();
    renderApp("/");
    await user.click(await screen.findByRole("link", { name: enGB.home.budgetLink }));
    expect(await screen.findByRole("heading", { name: enGB.budget.title })).toBeInTheDocument();
  });

  it("shows a loading state, then the Period as dates and the saved figures", async () => {
    server.use(...defaultHandlers(signedIn({ budget: planned })));
    renderApp("/budget");
    expect(await screen.findByRole("status")).toHaveTextContent("Loading…");

    expect(await screen.findByText("1 November to 30 November 2026")).toBeInTheDocument();
    expect(screen.getByLabelText("Monthly Budget")).toHaveValue("9,000,000");
    expect(screen.getByText("Commitments Budget").nextSibling).toHaveTextContent("Rp0");
    expect(screen.getByText("Daily Pool").nextSibling).toHaveTextContent("Rp9,000,000");
    expect(screen.getByText("Unallocated").nextSibling).toHaveTextContent("Rp9,000,000");
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("asks for a Monthly Budget on an empty first Period, and shows no figures", async () => {
    server.use(...defaultHandlers(signedIn()));
    renderApp("/budget");
    expect(await screen.findByText(enGB.budget.empty)).toBeInTheDocument();
    expect(screen.getByLabelText("Monthly Budget")).toHaveValue("");
    expect(screen.queryByText("Daily Pool")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("sends the Monthly Budget as a canonical string, shows the answer's figures and raises one toast", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    await user.type(await screen.findByLabelText("Monthly Budget"), "9000000");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.objectContaining({ duration: 4000 }));
    expect(state.budgetPatches).toEqual([{ monthly_budget: "9000000" }]);
    expect(JSON.stringify(state.budgetPatches)).not.toMatch(/e\+?\d/i);
    expect(await screen.findByText("Daily Pool")).toBeInTheDocument();
    expect(screen.getByText("Daily Pool").nextSibling).toHaveTextContent("Rp9,000,000");
    expect(screen.queryByText(enGB.budget.empty)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("computes no figure itself: it shows whatever the server sent", async () => {
    const odd = { ...planned, daily_pool: "1234.0000", unallocated: "-500.0000" };
    server.use(...defaultHandlers(signedIn({ budget: odd })));
    renderApp("/budget");
    expect((await screen.findByText("Daily Pool")).nextSibling).toHaveTextContent("Rp1,234");
    expect(screen.getByText("Unallocated").nextSibling).toHaveTextContent("-Rp500");
  });

  it("loads a stored amount rounded half up to the currency, so typing continues from what is shown", async () => {
    const me = { ...alice, locale: "id-ID" as const };
    const state = signedIn({
      me,
      budget: { ...planned, monthly_budget: "1000.5000", daily_pool: "1000.5000" },
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    const field = await screen.findByLabelText(idID.fields.monthlyBudget);
    expect(field).toHaveValue("1.001");
    await user.type(field, "0");
    expect(field).toHaveValue("10.010");
    await user.click(screen.getByRole("button", { name: idID.settings.save }));
    await waitFor(() => expect(state.budgetPatches).toEqual([{ monthly_budget: "10010" }]));
  });

  it("rounds a stored amount down under IDR, and to two decimals under USD", async () => {
    const usd = { ...alice, household: { ...alice.household, reporting_currency: "USD" } };
    const down = signedIn({ budget: { ...planned, monthly_budget: "1000.4000" } });
    server.use(...defaultHandlers(down));
    const first = renderApp("/budget");
    expect(await screen.findByLabelText("Monthly Budget")).toHaveValue("1,000");
    first.unmount();
    server.resetHandlers();

    server.use(
      ...defaultHandlers(signedIn({ me: usd, budget: { ...planned, monthly_budget: "12.3450" } })),
    );
    renderApp("/budget");
    expect(await screen.findByLabelText("Monthly Budget")).toHaveValue("12.35");
  });

  it("puts the currency inside the field as an adornment that is not the value", async () => {
    server.use(...defaultHandlers(signedIn({ budget: planned })));
    renderApp("/budget");
    const field = await screen.findByLabelText("Monthly Budget");
    expect(field).toHaveValue("9,000,000");
    expect(field).toHaveAccessibleDescription("Rp");
    expect(screen.getByText("Rp")).toHaveAttribute("aria-hidden", "true");
  });

  it("saves on Enter and reverts on Esc", async () => {
    const state = signedIn({ budget: planned });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    const field = await screen.findByLabelText("Monthly Budget");
    await user.type(field, "5{Escape}");
    expect(field).toHaveValue("9,000,000");
    await user.type(field, "0{Enter}");
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.budgetPatches).toEqual([{ monthly_budget: "90000000" }]);
  });

  it("shows a refused save inline in the code's copy, and typing clears it", async () => {
    server.use(
      http.patch("/api/budget", () =>
        HttpResponse.json(envelope("VALIDATION", { field: "monthly_budget", rule: "pattern" }), {
          status: 400,
        }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/budget");

    const field = await screen.findByLabelText("Monthly Budget");
    await user.type(field, "5");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Check the Monthly Budget field.");
    expect(toastMock).not.toHaveBeenCalled();
    await user.type(field, "0");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows a failed load in the code's copy, with Retry", async () => {
    server.use(
      http.get("/api/budget", () => HttpResponse.json(envelope("INTERNAL"), { status: 500 })),
      ...defaultHandlers(signedIn()),
    );
    renderApp("/budget");
    expect(await screen.findByRole("alert")).toHaveTextContent(enGB.errors.INTERNAL);
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("renders in id-ID: Indonesian words, dates and grouping", async () => {
    const me = { ...alice, locale: "id-ID" as const };
    server.use(...defaultHandlers(signedIn({ me, budget: planned })));
    renderApp("/budget");
    expect(await screen.findByText("1 November sampai 30 November 2026")).toBeInTheDocument();
    expect(screen.getByLabelText(idID.fields.monthlyBudget)).toHaveValue("9.000.000");
    expect(screen.getByText(idID.budget.dailyPool).nextSibling).toHaveTextContent("Rp9.000.000");
    expect(screen.getByText(idID.budget.commitmentsBudget)).toBeInTheDocument();
  });
});
