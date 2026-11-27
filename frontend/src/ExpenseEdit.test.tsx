import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import { focusManager } from "@tanstack/react-query";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { expensesKey, retryUnlessGone } from "@/api/queries";
import { ApiError } from "@/api/client";
import { selectPick } from "@/test/pickers";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);
beforeEach(() => {
  vi.clearAllMocks();
});

type Expense = NonNullable<State["expenses"]>[number];

const expense = (id: string, extra: Partial<Expense> = {}): Expense => ({
  id,
  occurred_on: "2026-11-25",
  captured_at: "2026-11-25T03:00:00Z",
  amount: "45000",
  currency: "IDR",
  pocket_id: "p-cash",
  ownership: "joint",
  ...extra,
});

const signedIn = (extra: Partial<State> = {}): State => ({
  signedIn: true,
  me: alice,
  pockets: [
    { id: "p-cash", pocket_display_name: "Cash", active: true },
    { id: "p-old", pocket_display_name: "Old wallet", active: false },
    { id: "p-gone", pocket_display_name: "Lost wallet", active: false },
  ],
  categories: [
    { id: "c-food", category_display_name: "Eating out", bucket: "daily", active: true },
    { id: "c-old", category_display_name: "Retired", bucket: "daily", active: false },
    { id: "c-gone", category_display_name: "Forgotten", bucket: "daily", active: false },
  ],
  defaultPocketId: "p-cash",
  ...extra,
});

const e = enGB.expenses;
// The title shows while loading, so wait for the form.
const title = async () => {
  await screen.findByRole("heading", { level: 1, name: e.editTitle });
  await screen.findByRole("textbox", { name: enGB.fields.amount });
};
const amountBox = () => screen.getByRole("textbox", { name: enGB.fields.amount });
const pocketBox = () => screen.getByRole("combobox", { name: enGB.fields.pocket });
const categoryBox = () => screen.getByRole("combobox", { name: enGB.fields.category });
const descriptionBox = () => screen.getByRole("textbox", { name: enGB.fields.description });
const save = () => screen.getByRole("button", { name: e.save });
const back = () => within(screen.getByRole("main")).getByRole("link", { name: e.title });
const path = () => screen.getByTestId("location").textContent;

describe("Edit an Expense", () => {
  it("loads the Expense by id into the Capture form, with a way back and a lone Save", async () => {
    const state = signedIn({
      expenses: [
        expense("e1", {
          description: "Nasi padang",
          category_id: "c-food",
          occurred_time: "12:30",
        }),
      ],
    });
    server.use(...defaultHandlers(state));
    renderApp("/expenses/e1");

    await title();
    expect(state.expenseGets).toEqual(["e1"]);
    expect(back()).toBeInTheDocument();
    expect(amountBox()).toHaveValue("45,000");
    expect(screen.getByRole("checkbox", { name: enGB.capture.refund })).not.toBeChecked();
    expect(pocketBox()).toHaveTextContent("Cash");
    expect(categoryBox()).toHaveTextContent("Eating out");
    expect(descriptionBox()).toHaveValue("Nasi padang");
    expect(screen.getByRole("button", { name: /^Date, Today$/ })).toBeInTheDocument();
    // Time is pre-filled on edit, with a way to clear it.
    expect(screen.getByRole("button", { name: "Time, 12:30" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: enGB.dates.clearTime })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
    // Below a hairline, Delete and Save share one row, Save last: never Delete
    // alone above Save, where a thumb reaching for Save lands on it.
    const del = screen.getByRole("button", { name: e.delete });
    const save = screen.getByRole("button", { name: e.save });
    expect(del.parentElement).toBe(save.parentElement);
    expect(del.parentElement).toHaveClass("grid-cols-2", "border-t");
    expect(del.compareDocumentPosition(save) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // Expenses stays lit.
    expect(
      screen.getAllByRole("link", { name: enGB.shell.tabs.expenses }).map((a) => a.ariaCurrent),
    ).toContain("page");
  });

  it("shows a stored refund as a positive amount with the box ticked", async () => {
    server.use(
      ...defaultHandlers(signedIn({ expenses: [expense("e1", { amount: "-450000.0000" })] })),
    );
    renderApp("/expenses/e1");

    await title();
    expect(amountBox()).toHaveValue("450,000");
    expect(screen.getByRole("checkbox", { name: enGB.capture.refund })).toBeChecked();
  });

  it("words the date row as the list does, from the server's Today", async () => {
    const state = signedIn({
      expenses: [
        expense("e1", { occurred_on: "2026-11-24" }),
        expense("e2", { occurred_on: "2026-11-22" }),
        expense("e3", { occurred_on: "2025-12-31" }),
      ],
    });
    server.use(...defaultHandlers(state));
    const view = renderApp("/expenses/e1");
    await title();
    expect(screen.getByRole("button", { name: "Date, Yesterday" })).toBeInTheDocument();
    view.unmount();

    renderApp("/expenses/e2");
    await title();
    expect(screen.getByRole("button", { name: "Date, Sunday, 22 November" })).toBeInTheDocument();
  });

  it("keeps an inactive current Pocket and Category as the marked selection, hiding other inactive ones", async () => {
    const state = signedIn({
      expenses: [expense("e1", { pocket_id: "p-old", category_id: "c-old" })],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    expect(pocketBox()).toHaveTextContent("Old wallet (inactive)");
    expect(categoryBox()).toHaveTextContent("Retired (inactive)");

    await user.click(pocketBox());
    expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual([
      "Cash",
      "Old wallet (inactive)",
    ]);
    await user.keyboard("{Escape}");
    await user.click(categoryBox());
    expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual([
      enGB.capture.noCategory,
      "Eating out",
      "Retired (inactive)",
    ]);
  });

  it("sends nothing for an inactive reference left alone, then toasts once and lands on the list from a direct link", async () => {
    const state = signedIn({
      expenses: [expense("e1", { pocket_id: "p-old", category_id: "c-old", description: "Hmm" })],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    await user.type(descriptionBox(), " more");
    await user.click(save());

    await waitFor(() => expect(path()).toBe("/expenses"));
    expect(state.expensePatches).toEqual([{ id: "e1", body: { description: "Hmm more" } }]);
    expect(toastMock).toHaveBeenCalledTimes(1);
    expect(toastMock).toHaveBeenCalledWith(
      enGB.toast.saved,
      expect.objectContaining({ duration: 4000 }),
    );
  });

  it("sends a no-change Save as no request, still toasting once and leaving", async () => {
    const state = signedIn({ expenses: [expense("e1", { amount: "45000.0000" })] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    await user.click(save());

    await waitFor(() => expect(path()).toBe("/expenses"));
    expect(state.expensePatches).toBeUndefined();
    expect(toastMock).toHaveBeenCalledTimes(1);
  });

  it("sends only the changed fields, and null for each field cleared", async () => {
    const state = signedIn({
      expenses: [
        expense("e1", { description: "Lunch", category_id: "c-food", occurred_time: "12:30" }),
      ],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    await selectPick(user, categoryBox(), enGB.capture.noCategory);
    await user.clear(descriptionBox());
    await user.click(screen.getByRole("button", { name: enGB.dates.clearTime }));
    await user.click(save());

    await waitFor(() => expect(state.expensePatches).toHaveLength(1));
    expect(state.expensePatches![0]!.body).toEqual({
      category_id: null,
      description: null,
      occurred_time: null,
    });
  });

  it("sends the amount, the Pocket and a refund as the negated string", async () => {
    const state = signedIn({
      pockets: [
        { id: "p-cash", pocket_display_name: "Cash", active: true },
        { id: "p-bca", pocket_display_name: "BCA", active: true },
      ],
      expenses: [expense("e1")],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    await user.click(screen.getByRole("checkbox", { name: enGB.capture.refund }));
    await selectPick(user, pocketBox(), "BCA");
    await user.click(save());

    await waitFor(() => expect(state.expensePatches).toHaveLength(1));
    expect(state.expensePatches![0]!.body).toEqual({ amount: "-45000", pocket_id: "p-bca" });
  });

  it("goes back to where the person came from, and the list shows the change", async () => {
    const state = signedIn({ expenses: [expense("e1", { description: "Lunch" })] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const { navigate } = renderApp("/expenses");

    await user.click(await screen.findByRole("link", { name: "Edit Lunch, Rp45,000" }));
    await title();
    expect(path()).toBe("/expenses/e1");
    await user.clear(descriptionBox());
    await user.type(descriptionBox(), "Dinner");
    await user.click(save());

    await waitFor(() => expect(path()).toBe("/expenses"));
    expect(await screen.findByRole("link", { name: "Edit Dinner, Rp45,000" })).toBeInTheDocument();
    expect(toastMock).toHaveBeenCalledTimes(1);
    // It went back, not forward to a new /expenses: the edit route is still the
    // entry ahead, where a push would have dropped it.
    await act(async () => navigate(1));
    expect(path()).toBe("/expenses/e1");
  });

  it("diffs against the Expense as it was opened, never a refetch, so another member's edit survives", async () => {
    const state = signedIn({ expenses: [expense("e1", { description: "Lunch" })] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    // Another member files it under a Category while this screen is open, and
    // the tab loses and regains focus.
    state.expenses = [expense("e1", { description: "Lunch", category_id: "c-food" })];
    act(() => {
      focusManager.setFocused(false);
      focusManager.setFocused(true);
    });
    await user.clear(descriptionBox());
    await user.type(descriptionBox(), "Dinner");
    await user.click(save());

    await waitFor(() => expect(state.expensePatches).toHaveLength(1));
    expect(state.expensePatches![0]!.body).toEqual({ description: "Dinner" });
    expect(state.expenseGets).toEqual(["e1"]);
    focusManager.setFocused(undefined);
  });

  it("never re-sends a stored amount with more decimals than the field shows, unless it is touched", async () => {
    const state = signedIn({ expenses: [expense("e1", { amount: "45000.5000" })] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    await user.type(descriptionBox(), "Lunch");
    await user.click(save());

    await waitFor(() => expect(state.expensePatches).toHaveLength(1));
    expect(state.expensePatches![0]!.body).toEqual({ description: "Lunch" });
  });

  it("re-sends such an amount once the Refund box is ticked", async () => {
    const state = signedIn({ expenses: [expense("e1", { amount: "45000.5000" })] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    await user.click(screen.getByRole("checkbox", { name: enGB.capture.refund }));
    await user.click(save());

    await waitFor(() => expect(state.expensePatches).toHaveLength(1));
    expect(state.expensePatches![0]!.body.amount).toMatch(/^-45/);
  });

  it("shows a refusal under its field and stays", async () => {
    const state = signedIn({ expenses: [expense("e1")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await title();
    await user.clear(amountBox());
    await user.type(amountBox(), "0");
    await user.click(save());

    expect(await screen.findByText(enGB.capture.amountNonzero)).toBeInTheDocument();
    expect(path()).toBe("/expenses/e1");
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("toasts, refetches and goes to the list when the Expense is gone at Save", async () => {
    const state = signedIn({
      expenses: [expense("e1", { description: "Lunch" }), expense("e2", { description: "Bus" })],
    });
    server.use(
      http.patch("/api/expenses/:id", () => {
        // Another member deleted it while this screen was open.
        state.expenses = state.expenses!.filter((e) => e.id !== "e1");
        return HttpResponse.json(envelope("NOT_FOUND"), { status: 404 });
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    const { client } = renderApp("/expenses");
    const invalidate = vi.spyOn(client, "invalidateQueries");

    await user.click(await screen.findByRole("link", { name: "Edit Lunch, Rp45,000" }));
    await title();
    const listReadsBeforeSave = state.expenseListQueries!.length;
    await user.type(descriptionBox(), "x");
    await user.click(save());

    await waitFor(() => expect(path()).toBe("/expenses"));
    expect(toastMock.error).toHaveBeenCalledWith(enGB.errors.NOT_FOUND, expect.anything());
    expect(invalidate).toHaveBeenCalledWith({ queryKey: expensesKey });
    // The list on screen is the server's, read after the refusal, not the cached one.
    expect(await screen.findByRole("link", { name: "Edit Bus, Rp45,000" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Edit Lunch, Rp45,000" })).not.toBeInTheDocument();
    expect(state.expenseListQueries!.length).toBeGreaterThan(listReadsBeforeSave);
  });

  it("toasts once and goes to the list when the Expense is gone on opening, as a Save that finds it gone does", async () => {
    server.use(...defaultHandlers(signedIn({ expenses: [] })));
    const { client } = renderApp("/expenses/nope");
    const invalidate = vi.spyOn(client, "invalidateQueries");

    await waitFor(() => expect(path()).toBe("/expenses"));
    expect(toastMock.error).toHaveBeenCalledTimes(1);
    expect(toastMock.error).toHaveBeenCalledWith(enGB.errors.NOT_FOUND, expect.anything());
    expect(invalidate).toHaveBeenCalledWith({ queryKey: expensesKey });
    expect(screen.queryByRole("button", { name: enGB.app.retry })).not.toBeInTheDocument();
  });

  it("keeps the error state with Try again when opening fails for another reason", async () => {
    const state = signedIn({ expenses: [expense("e1")] });
    let failing = true;
    server.use(
      http.get("/api/expenses/:id", ({ params }) => {
        if (failing) return HttpResponse.json(envelope("INTERNAL"), { status: 500 });
        return HttpResponse.json(state.expenses!.find((x) => x.id === params.id));
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    expect(await screen.findByRole("alert")).toHaveTextContent(enGB.errors.INTERNAL);
    expect(path()).toBe("/expenses/e1");
    expect(toastMock.error).not.toHaveBeenCalled();
    failing = false;
    await user.click(screen.getByRole("button", { name: enGB.app.retry }));
    expect(await screen.findByRole("button", { name: e.save })).toBeInTheDocument();
  });

  it("is worded in Indonesian", async () => {
    const state = signedIn({
      me: { ...alice, locale: "id-ID" },
      expenses: [expense("e1", { pocket_id: "p-old", occurred_on: "2026-11-24" })],
    });
    server.use(...defaultHandlers(state));
    renderApp("/expenses/e1");

    expect(
      await screen.findByRole("heading", { level: 1, name: idID.expenses.editTitle }),
    ).toBeInTheDocument();
    expect(await screen.findByRole("combobox", { name: idID.fields.pocket })).toHaveTextContent(
      "Old wallet (nonaktif)",
    );
    expect(
      screen.getByRole("button", { name: `${idID.fields.date}, ${idID.expenses.yesterday}` }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: idID.expenses.save })).toBeInTheDocument();
  });

  it("names a row by its title and amount and opens the Expense", async () => {
    const state = signedIn({
      expenses: [
        expense("e1", { description: "Lunch" }),
        expense("e2", { category_id: "c-food", amount: "-1000" }),
        expense("e3"),
      ],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses");

    expect(await screen.findByRole("link", { name: "Edit Eating out, -Rp1,000" })).toHaveAttribute(
      "href",
      "/expenses/e2",
    );
    expect(screen.getByRole("link", { name: `Edit ${e.uncategorised}, Rp45,000` })).toBeVisible();
    await user.click(screen.getByRole("link", { name: "Edit Lunch, Rp45,000" }));
    await title();
    expect(path()).toBe("/expenses/e1");
  });
});

describe("The Edit action on the Added toast", () => {
  it("lasts 8 s and opens the Expense just saved, after Capture has gone", async () => {
    const state = signedIn({
      pockets: [{ id: "p-cash", pocket_display_name: "Cash", active: true }],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await screen.findByRole("textbox", { name: enGB.fields.amount }), "25000");
    await user.click(screen.getByRole("button", { name: enGB.capture.add }));
    await screen.findByRole("heading", { level: 1, name: enGB.home.title });

    const options = toastMock.mock.calls[0]![1] as {
      duration: number;
      action: { label: string; onClick: () => void };
    };
    expect(options.duration).toBe(8000);
    expect(options.action.label).toBe(enGB.toast.edit);
    options.action.onClick();

    await title();
    expect(path()).toBe(`/expenses/${state.expensePosts![0]!.id}`);
    expect(amountBox()).toHaveValue("25,000");
  });
});

describe("Reading the Expense to edit", () => {
  it("does not retry a gone Expense, and retries anything else up to three times", () => {
    const gone = new ApiError(404, "NOT_FOUND");
    const down = new ApiError(500, "INTERNAL");
    expect(retryUnlessGone(0, gone)).toBe(false);
    expect(retryUnlessGone(0, down)).toBe(true);
    expect(retryUnlessGone(2, down)).toBe(true);
    expect(retryUnlessGone(3, down)).toBe(false);
  });
});
