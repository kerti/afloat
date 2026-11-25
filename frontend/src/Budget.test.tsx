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
    await user.click(await screen.findByRole("link", { name: enGB.shell.tabs.budget }));
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
    expect(screen.getAllByRole("button", { name: "Save" })[0]!).toBeDisabled();
  });

  it("asks for a Monthly Budget on an empty first Period, and shows no figures", async () => {
    server.use(...defaultHandlers(signedIn()));
    renderApp("/budget");
    expect(await screen.findByText(enGB.budget.empty)).toBeInTheDocument();
    expect(screen.getByLabelText("Monthly Budget")).toHaveValue("");
    expect(screen.queryByText("Daily Pool")).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Save" })[0]!).toBeDisabled();
  });

  it("sends the Monthly Budget as a canonical string, shows the answer's figures and raises one toast", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    await user.type(await screen.findByLabelText("Monthly Budget"), "9000000");
    await user.click(screen.getAllByRole("button", { name: "Save" })[0]!);

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.objectContaining({ duration: 4000 }));
    expect(state.budgetPatches).toEqual([{ monthly_budget: "9000000" }]);
    expect(JSON.stringify(state.budgetPatches)).not.toMatch(/e\+?\d/i);
    expect(await screen.findByText("Daily Pool")).toBeInTheDocument();
    expect(screen.getByText("Daily Pool").nextSibling).toHaveTextContent("Rp9,000,000");
    expect(screen.queryByText(enGB.budget.empty)).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Save" })[0]!).toBeDisabled();
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
    await user.click(screen.getAllByRole("button", { name: idID.settings.save })[0]!);
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
    expect(screen.getAllByText("Rp")[0]).toHaveAttribute("aria-hidden", "true");
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
    await user.click(screen.getAllByRole("button", { name: "Save" })[0]!);

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

describe("expected monthly income", () => {
  const withIncome = (income: string | undefined, budget = "9000000.0000") =>
    signedIn({
      me: { ...alice, household: { ...alice.household, expected_monthly_income: income } },
      budget: { ...planned, monthly_budget: budget },
    });
  const notice = enGB.budget.exceedsIncomeSaved;
  const saveButtons = () => screen.getAllByRole("button", { name: "Save" });
  const income = async () => screen.findByLabelText(enGB.fields.expectedMonthlyIncome);

  it("is a labelled optional field under the Monthly Budget, with its currency", async () => {
    server.use(...defaultHandlers(signedIn({ budget: planned })));
    renderApp("/budget");
    const field = await income();
    expect(field).toHaveValue("");
    expect(field).toHaveAccessibleDescription(expect.stringContaining(enGB.budget.incomeHint));
    expect(field).toHaveAccessibleDescription(expect.stringContaining("Rp"));
    const budgetField = screen.getByLabelText("Monthly Budget");
    expect(
      budgetField.compareDocumentPosition(field) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("sets the income as a string with the ordinary toast when the Budget stays within it", async () => {
    const state = signedIn({ budget: planned });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await user.type(await income(), "12000000");
    await user.click(saveButtons()[1]!);
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.anything());
    expect(state.patches).toEqual([{ expected_monthly_income: "12000000" }]);
    await waitFor(() =>
      expect(screen.getByLabelText(enGB.fields.expectedMonthlyIncome)).toHaveValue("12,000,000"),
    );
  });

  it("sends null when the field is cleared and saved", async () => {
    const state = withIncome("12000000.0000");
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    const field = await income();
    expect(field).toHaveValue("12,000,000");
    await user.clear(field);
    await user.click(saveButtons()[1]!);
    await waitFor(() => expect(state.patches).toEqual([{ expected_monthly_income: null }]));
    await waitFor(() => expect(toastMock).toHaveBeenCalledWith("Saved.", expect.anything()));
    await waitFor(() =>
      expect(screen.getByLabelText(enGB.fields.expectedMonthlyIncome)).toHaveValue(""),
    );
  });

  it("says so in the save's toast when raising the Budget crosses the income", async () => {
    const state = withIncome("9000000.0000");
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    const field = await screen.findByLabelText("Monthly Budget");
    await user.type(field, "1");
    await user.click(saveButtons()[0]!);
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith(notice, expect.objectContaining({ duration: 4000 }));
    expect(state.budgetPatches).toEqual([{ monthly_budget: "90000001" }]);
  });

  it("says so when lowering the income crosses the Budget", async () => {
    const state = withIncome("12000000.0000");
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    const field = await income();
    await user.clear(field);
    await user.type(field, "8000000");
    await user.click(saveButtons()[1]!);
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith(notice, expect.anything());
  });

  it("is exact: a Budget equal to the income, however padded, says nothing", async () => {
    server.use(...defaultHandlers(withIncome("9000000.0000", "8000000.0000")));
    const user = userEvent.setup();
    renderApp("/budget");
    const field = await screen.findByLabelText("Monthly Budget");
    await user.clear(field);
    await user.type(field, "9000000");
    await user.click(saveButtons()[0]!);
    await waitFor(() => expect(toastMock).toHaveBeenCalledWith("Saved.", expect.anything()));
  });

  it("does not repeat when it was already true before the save", async () => {
    server.use(...defaultHandlers(withIncome("5000000.0000")));
    const user = userEvent.setup();
    renderApp("/budget");
    await user.type(await screen.findByLabelText("Monthly Budget"), "0");
    await user.click(saveButtons()[0]!);
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.anything());
  });

  it("is exact below display precision: a Budget a fraction above the income says so", async () => {
    server.use(...defaultHandlers(withIncome("8999999.9999", "8000000.0000")));
    const user = userEvent.setup();
    renderApp("/budget");
    const field = await screen.findByLabelText("Monthly Budget");
    await user.clear(field);
    await user.type(field, "9000000");
    await user.click(saveButtons()[0]!);
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith(notice, expect.anything());
  });

  it("says nothing when no income is set", async () => {
    server.use(...defaultHandlers(withIncome(undefined)));
    const user = userEvent.setup();
    renderApp("/budget");
    await user.type(await screen.findByLabelText("Monthly Budget"), "0");
    await user.click(saveButtons()[0]!);
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.anything());
  });

  it("has Indonesian copy for the label and the notice", async () => {
    const me = {
      ...alice,
      locale: "id-ID" as const,
      household: { ...alice.household, expected_monthly_income: "9000000.0000" },
    };
    server.use(...defaultHandlers(signedIn({ me, budget: planned })));
    const user = userEvent.setup();
    renderApp("/budget");
    await user.type(await screen.findByLabelText(idID.fields.monthlyBudget), "1");
    await user.click(screen.getAllByRole("button", { name: idID.settings.save })[0]!);
    await waitFor(() =>
      expect(toastMock).toHaveBeenCalledWith(idID.budget.exceedsIncomeSaved, expect.anything()),
    );
    expect(screen.getByLabelText(idID.fields.expectedMonthlyIncome)).toBeInTheDocument();
  });
});
