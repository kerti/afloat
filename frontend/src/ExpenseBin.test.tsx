import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { UNDO_TOAST_MS } from "@/lib/undoToast";
import { dashboardKey } from "@/api/queries";
import { PLAIN_TOAST_MS } from "@/lib/plainToast";
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
  amount: "25000",
  currency: "IDR",
  pocket_id: "p-cash",
  ownership: "joint",
  ...extra,
});

const signedIn = (extra: Partial<State> = {}): State => ({
  signedIn: true,
  me: alice,
  pockets: [{ id: "p-cash", pocket_display_name: "Cash", active: true }],
  categories: [
    { id: "c-food", category_display_name: "Eating out", bucket: "daily", active: true },
  ],
  defaultPocketId: "p-cash",
  ...extra,
});

const e = enGB.expenses;
const main = () => within(screen.getByRole("main"));
const path = () => screen.getByTestId("location").textContent;
const action = (call: number) =>
  (toastMock.mock.calls[call]![1]!.action as unknown as { onClick: () => void }).onClick();

describe("Delete on the edit route", () => {
  it("is immediate: goes to the list and raises one 8 s Undo toast naming the amount", async () => {
    const state = signedIn({ expenses: [expense("e1", { description: "Lunch" })] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    const del = await screen.findByRole("button", { name: e.delete });
    expect(del).toHaveClass("text-destructive");
    await user.click(del);

    await waitFor(() => expect(path()).toBe("/expenses"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(state.expenseDeletes).toEqual(["e1"]);
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    const [message, options] = toastMock.mock.calls[0]!;
    expect(message).toBe("Deleted Rp25,000.");
    expect(options?.duration).toBe(UNDO_TOAST_MS);
    expect(options?.action).toMatchObject({ label: "Undo" });
    await screen.findByText(enGB.expenses.empty);
  });

  it("reads a refund with its sign", async () => {
    server.use(...defaultHandlers(signedIn({ expenses: [expense("e1", { amount: "-450000" })] })));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await user.click(await screen.findByRole("button", { name: e.delete }));
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock.mock.calls[0]![0]).toBe("Deleted -Rp450,000.");
  });

  it("Undo refreshes the uncategorised count and the default Pocket on a screen that stays put", async () => {
    const state = signedIn({
      expenses: [expense("e1", { description: "Lunch" }), expense("e2", { description: "Tea" })],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await user.click(await screen.findByRole("button", { name: e.delete }));
    expect(
      await main().findByRole("link", { name: e.needsCategory.replace("{{count}}", "1") }),
    ).toBeInTheDocument();
    // Settle, then count the Pocket reads the Undo alone causes: /expenses stays mounted.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    const before = state.pocketReads ?? 0;
    action(0);

    expect(
      await main().findByRole("link", { name: e.needsCategory.replace("{{count}}", "2") }),
    ).toBeInTheDocument();
    await waitFor(() => expect(state.pocketReads).toBeGreaterThan(before));
  });

  it("Undo invalidates the dashboard, since the restored Expense moves its figures (#155 R5)", async () => {
    server.use(...defaultHandlers(signedIn({ expenses: [expense("e1")] })));
    const user = userEvent.setup();
    const { client } = renderApp("/expenses/e1");

    await user.click(await screen.findByRole("button", { name: e.delete }));
    await waitFor(() => expect(path()).toBe("/expenses"));
    // A fresh entry, so only the Undo can mark it stale.
    client.setQueryData(dashboardKey, { currency: "IDR" });
    action(0);

    await waitFor(() => expect(client.getQueryState(dashboardKey)?.isInvalidated).toBe(true));
  });

  it("Undo restores it and the list gets it back", async () => {
    const state = signedIn({ expenses: [expense("e1", { description: "Lunch" })] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await user.click(await screen.findByRole("button", { name: e.delete }));
    await screen.findByText(enGB.expenses.empty);
    action(0);

    expect(await screen.findByRole("link", { name: "Edit Lunch, Rp25,000" })).toBeInTheDocument();
    expect(state.expenseRestores).toEqual(["e1"]);
  });

  it("is gone already: an error toast and the list, not a stuck screen", async () => {
    const state = signedIn({ expenses: [expense("e1")] });
    server.use(
      http.delete("/api/expenses/:id", () =>
        HttpResponse.json(envelope("NOT_FOUND"), { status: 404 }),
      ),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await user.click(await screen.findByRole("button", { name: e.delete }));
    await waitFor(() => expect(path()).toBe("/expenses"));
    expect(toastMock.error).toHaveBeenCalledWith(enGB.errors.NOT_FOUND, expect.anything());
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("any other failure stays on the screen and says so", async () => {
    server.use(
      http.delete("/api/expenses/:id", () =>
        HttpResponse.json(envelope("INTERNAL"), { status: 500 }),
      ),
      ...defaultHandlers(signedIn({ expenses: [expense("e1")] })),
    );
    const user = userEvent.setup();
    renderApp("/expenses/e1");

    await user.click(await screen.findByRole("button", { name: e.delete }));
    expect(await screen.findByRole("alert")).toHaveTextContent(enGB.errors.INTERNAL);
    expect(path()).toBe("/expenses/e1");
    expect(toastMock).not.toHaveBeenCalled();
  });
});

describe("the Recycle Bin link", () => {
  it("sits in the title row, before the list, with no count", async () => {
    server.use(
      ...defaultHandlers(signedIn({ expenses: [expense("e1", { description: "Lunch" })] })),
    );
    renderApp("/expenses");

    await screen.findByRole("link", { name: "Edit Lunch, Rp25,000" });
    const link = main().getByRole("link", { name: e.recycleBin });
    expect(link).toHaveAttribute("href", "/expenses/recycle-bin");
    // First in the main area: on the title row, so Load more never moves it.
    expect(main().getAllByRole("link")[0]).toBe(link);
    expect(link).toHaveTextContent(e.recycleBin);
  });

  it("is there when the list is empty too", async () => {
    server.use(...defaultHandlers(signedIn({ expenses: [] })));
    renderApp("/expenses");

    await screen.findByText(e.empty);
    expect(main().getByRole("link", { name: e.recycleBin })).toBeInTheDocument();
  });

  it("is not on the uncategorised view", async () => {
    server.use(...defaultHandlers(signedIn({ expenses: [expense("e1")] })));
    renderApp("/expenses?uncategorised=true");

    await screen.findByRole("link", { name: /^Edit / });
    expect(main().queryByRole("link", { name: e.recycleBin })).not.toBeInTheDocument();
  });
});

describe("the Recycle Bin", () => {
  const bin = (...rows: Expense[]) =>
    signedIn({
      expenses: [],
      deletedExpenses: rows.map((r) => ({ ...r, deleted_at: "2026-11-25T05:00:00Z" })),
    });
  const shown = () => screen.findByRole("heading", { level: 1, name: e.recycleBin });

  it("has a way back, Expenses lit, and lists the bin as served with no day headings", async () => {
    const state = bin(
      expense("b1", { description: "Lunch", category_id: "c-food", occurred_time: "12:30" }),
      expense("b2", { amount: "-1000", occurred_on: "2026-11-22" }),
    );
    server.use(...defaultHandlers(state));
    renderApp("/expenses/recycle-bin");
    await shown();

    await shown();
    expect(main().getByRole("link", { name: e.title })).toHaveAttribute("href", "/expenses");
    expect(
      screen.getAllByRole("link", { name: enGB.shell.tabs.expenses }).map((a) => a.ariaCurrent),
    ).toContain("page");
    const rows = await main().findAllByRole("button", { name: /^View / });
    expect(rows.map((r) => r.getAttribute("aria-label"))).toEqual([
      "View Lunch, Rp25,000",
      "View Uncategorised, -Rp1,000",
    ]);
    expect(main().queryAllByRole("heading", { level: 3 })).toHaveLength(0);
    expect(state.expenseListQueries).toContain("deleted=true");
  });

  it("says so when the bin is empty", async () => {
    server.use(...defaultHandlers(bin()));
    renderApp("/expenses/recycle-bin");
    await shown();
    expect(await screen.findByText(e.binEmpty)).toBeInTheDocument();
  });

  it("pages on demand with Load more", async () => {
    const state = bin(
      expense("b1", { description: "One" }),
      expense("b2", { description: "Two" }),
      expense("b3", { description: "Three" }),
    );
    state.expensePageSize = 2;
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/recycle-bin");
    await shown();

    await main().findByRole("button", { name: "View One, Rp25,000" });
    expect(main().queryByRole("button", { name: /Three/ })).not.toBeInTheDocument();
    await user.click(main().getByRole("button", { name: e.loadMore }));
    expect(await main().findByRole("button", { name: "View Three, Rp25,000" })).toBeInTheDocument();
    expect(main().queryByRole("button", { name: e.loadMore })).not.toBeInTheDocument();
  });

  it("opens a read-only dialog whose one act is Restore", async () => {
    const state = bin(
      expense("b1", {
        description: "Lunch",
        category_id: "c-food",
        occurred_time: "12:30",
        occurred_on: "2026-11-24",
      }),
    );
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/expenses/recycle-bin");
    await shown();

    await user.click(await main().findByRole("button", { name: "View Lunch, Rp25,000" }));
    const dialog = await screen.findByRole("dialog", { name: e.binDialogTitle });
    expect(view.param("expense")).toBe("b1");
    for (const text of ["Rp25,000", "Cash", "Eating out", "Lunch", "Yesterday", "12:30"]) {
      expect(within(dialog).getByText(text)).toBeInTheDocument();
    }
    expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
    expect(within(dialog).queryByRole("combobox")).not.toBeInTheDocument();
    expect(
      within(dialog)
        .getAllByRole("button")
        .map((b) => b.getAttribute("aria-label") ?? b.textContent),
    ).toEqual([e.restore, enGB.dialog.close]);
  });

  it("closes on an outside tap", async () => {
    server.use(...defaultHandlers(bin(expense("b1", { description: "Lunch" }))));
    const user = userEvent.setup();
    const view = renderApp("/expenses/recycle-bin");
    await shown();

    await user.click(await main().findByRole("button", { name: "View Lunch, Rp25,000" }));
    await screen.findByRole("dialog");
    await user.click(document.querySelector('[data-slot="dialog-overlay"]')!);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("expense")).toBeNull();
    expect(main().getByRole("button", { name: "View Lunch, Rp25,000" })).toBeInTheDocument();
  });

  it("Restore closes the dialog, drops the row, raises a plain toast with no Undo and refreshes the list", async () => {
    const state = bin(
      expense("b1", { description: "Lunch" }),
      expense("b2", { description: "Tea" }),
    );
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/expenses/recycle-bin");
    await shown();

    await user.click(await main().findByRole("button", { name: "View Lunch, Rp25,000" }));
    const dialog = await screen.findByRole("dialog");
    const pocketReadsBeforeRestore = state.pocketReads ?? 0;
    await user.click(within(dialog).getByRole("button", { name: e.restore }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    await waitFor(() =>
      expect(main().queryByRole("button", { name: /Lunch/ })).not.toBeInTheDocument(),
    );
    expect(main().getByRole("button", { name: "View Tea, Rp25,000" })).toBeInTheDocument();
    expect(view.param("expense")).toBeNull();
    expect(state.expenseRestores).toEqual(["b1"]);
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(toastMock).toHaveBeenCalledTimes(1);
    const [message, options] = toastMock.mock.calls[0]!;
    expect(message).toBe("Restored Rp25,000.");
    expect(options?.duration).toBe(PLAIN_TOAST_MS);
    expect(options?.action).toBeUndefined();

    // The bin stayed mounted, so only the restore's invalidation reads the
    // Pockets again (the default Pocket may move back to this one).
    expect(state.pocketReads).toBeGreaterThan(pocketReadsBeforeRestore);

    // The live list has it again.
    await user.click(main().getByRole("link", { name: e.title }));
    expect(await screen.findByRole("link", { name: "Edit Lunch, Rp25,000" })).toBeInTheDocument();
  });

  it("a row gone meanwhile is an error toast, a closed dialog and a refetched bin", async () => {
    const state = bin(expense("b1", { description: "Lunch" }));
    server.use(
      http.post("/api/expenses/:id/restore", () => {
        state.deletedExpenses = [];
        return HttpResponse.json(envelope("NOT_FOUND"), { status: 404 });
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/expenses/recycle-bin");
    await shown();

    await user.click(await main().findByRole("button", { name: "View Lunch, Rp25,000" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: e.restore }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(toastMock.error).toHaveBeenCalledWith(enGB.errors.NOT_FOUND, expect.anything());
    expect(await screen.findByText(e.binEmpty)).toBeInTheDocument();
  });

  it("another failure keeps the dialog open and says so inside it", async () => {
    server.use(
      http.post("/api/expenses/:id/restore", () =>
        HttpResponse.json(envelope("INTERNAL"), { status: 500 }),
      ),
      ...defaultHandlers(bin(expense("b1", { description: "Lunch" }))),
    );
    const user = userEvent.setup();
    renderApp("/expenses/recycle-bin");
    await shown();

    await user.click(await main().findByRole("button", { name: "View Lunch, Rp25,000" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: e.restore }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(enGB.errors.INTERNAL);
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("clears a link to an Expense that is not in the bin", async () => {
    server.use(...defaultHandlers(bin(expense("b1", { description: "Lunch" }))));
    const view = renderApp("/expenses/recycle-bin?expense=nope");
    await shown();
    await main().findByRole("button", { name: "View Lunch, Rp25,000" });
    await waitFor(() => expect(view.param("expense")).toBeNull());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("is worded in Indonesian", async () => {
    const state = bin(expense("b1", { description: "Makan" }));
    state.me = { ...alice, locale: "id-ID" };
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses/recycle-bin");

    expect(
      await screen.findByRole("heading", { level: 1, name: idID.expenses.recycleBin }),
    ).toBeInTheDocument();
    await user.click(await main().findByRole("button", { name: "Lihat Makan, Rp25.000" }));
    const dialog = await screen.findByRole("dialog", { name: idID.expenses.binDialogTitle });
    expect(within(dialog).getByRole("button", { name: idID.expenses.restore })).toBeInTheDocument();
  });
});
