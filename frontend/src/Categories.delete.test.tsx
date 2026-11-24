import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, emptyBudget, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { UNDO_TOAST_MS } from "@/lib/undoToast";
import enGB from "@/locales/en-GB.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);
const action = (call: number) =>
  (toastMock.mock.calls[call]![1]!.action as unknown as { onClick: () => void }).onClick();
beforeEach(() => {
  vi.clearAllMocks();
});

const c = enGB.categories;
const cat = (id: string, name: string, bucket: "daily" | "commitment", active = true) => ({
  id,
  category_display_name: name,
  bucket,
  active,
});
const planned = {
  ...emptyBudget,
  monthly_budget: "9000000.0000",
  daily_pool: "6000000.0000",
  unallocated: "4500000.0000",
  commitments_budget: "3000000.0000",
};
const setup = (extra: Partial<State> = {}): State => ({
  signedIn: true,
  me: alice,
  categories: [cat("a", "Rent", "commitment"), cat("b", "Groceries", "daily")],
  budget: {
    ...planned,
    allocations: [
      { category_id: "a", allocation: "3000000.0000" },
      { category_id: "b", allocation: "1500000.0000" },
    ],
  },
  ...extra,
});

async function openEdit(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(await screen.findByRole("button", { name: new RegExp(`^Edit ${name},`) }));
  return screen.findByRole("dialog", { name: c.editHeading });
}
const figure = (label: string) => screen.getByText(label).parentElement as HTMLElement;

describe("the Category dialog's actions", () => {
  it("puts Deactivate and Delete above a Save-only footer, Delete last", async () => {
    server.use(...defaultHandlers(setup()));
    const user = userEvent.setup();
    renderApp("/budget");

    const dialog = await openEdit(user, "Rent");
    const footer = dialog.querySelector('[data-slot="dialog-footer"]') as HTMLElement;
    expect(
      within(footer)
        .getAllByRole("button")
        .map((b) => b.textContent),
    ).toEqual([c.save]);
    const actions = within(dialog)
      .getAllByRole("button")
      .filter(
        (b) => !footer.contains(b) && b.textContent !== "" && b.getAttribute("role") !== "combobox",
      );
    expect(actions.map((b) => b.textContent)).toEqual([c.deactivate, c.delete]);
    expect(actions[1]).toHaveClass("text-destructive");
  });
});

describe("deleting a Category", () => {
  it("closes the dialog, drops the row and the figures, and raises one 8 s Undo toast", async () => {
    const state = setup();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/budget");

    expect(await screen.findByText(enGB.budget.commitmentsBudget)).toBeInTheDocument();
    const dialog = await openEdit(user, "Rent");
    await user.click(within(dialog).getByRole("button", { name: c.delete }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("edit")).toBeNull();
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /^Edit Rent,/ })).not.toBeInTheDocument(),
    );
    // Both queries were refetched: the figures moved with the list.
    await waitFor(() => expect(figure(enGB.budget.commitmentsBudget)).toHaveTextContent("Rp0"));
    expect(figure(enGB.budget.dailyPool)).toHaveTextContent("Rp9,000,000");
    expect(state.categoryDeletes).toEqual(["a"]);
    expect(toastMock).toHaveBeenCalledTimes(1);
    const [message, options] = toastMock.mock.calls[0]!;
    expect(message).toBe("Deleted Rent.");
    expect(options?.duration).toBe(UNDO_TOAST_MS);
    expect(options?.action).toMatchObject({ label: "Undo" });
  });

  it("Undo restores the Category and its figures", async () => {
    const state = setup();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    const dialog = await openEdit(user, "Rent");
    await user.click(within(dialog).getByRole("button", { name: c.delete }));
    await waitFor(() => expect(figure(enGB.budget.commitmentsBudget)).toHaveTextContent("Rp0"));
    action(0);

    expect(await screen.findByRole("button", { name: /^Edit Rent,/ })).toBeInTheDocument();
    await waitFor(() =>
      expect(figure(enGB.budget.commitmentsBudget)).toHaveTextContent("Rp3,000,000"),
    );
    expect(state.categoryRestores).toEqual(["a"]);
  });

  it("a refused delete (CATEGORY_IN_USE) keeps the dialog open with the error and Deactivate", async () => {
    const state = setup({ categoriesInUse: ["a"] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    const dialog = await openEdit(user, "Rent");
    await user.click(within(dialog).getByRole("button", { name: c.delete }));

    expect(await within(dialog).findByText(enGB.errors.CATEGORY_IN_USE)).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: c.editHeading })).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: c.deactivate })).toBeEnabled();
    expect(within(dialog).getByRole("button", { name: c.delete })).toBeEnabled();
    expect(screen.getByRole("button", { name: /^Edit Rent,/, hidden: true })).toBeInTheDocument();
    expect(toastMock).not.toHaveBeenCalled();
    expect(toastMock.error).not.toHaveBeenCalled();

    // Deactivate is the way out: it takes the Category out of the active lists.
    await user.click(within(dialog).getByRole("button", { name: c.deactivate }));
    await waitFor(() => expect(state.categories![0]!.active).toBe(false));
    expect(toastMock.mock.calls[0]![0]).toBe("Deactivated Rent.");
  });

  it("a Category already gone is a NOT_FOUND toast, a refetch and a closed dialog", async () => {
    const state = setup();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    const dialog = await openEdit(user, "Rent");
    state.categories = [cat("b", "Groceries", "daily")];
    await user.click(within(dialog).getByRole("button", { name: c.delete }));

    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toBe(enGB.errors.NOT_FOUND);
    expect(toastMock).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /^Edit Rent,/ })).not.toBeInTheDocument();
  });

  it("any other failure stays in the dialog", async () => {
    server.use(
      http.delete("/api/categories/:id", () =>
        HttpResponse.json(envelope("INTERNAL"), { status: 500 }),
      ),
      ...defaultHandlers(setup()),
    );
    const user = userEvent.setup();
    renderApp("/budget");

    const dialog = await openEdit(user, "Rent");
    await user.click(within(dialog).getByRole("button", { name: c.delete }));

    expect(await within(dialog).findByText(enGB.errors.INTERNAL)).toBeInTheDocument();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("a failed Undo (NAME_TAKEN) raises its own error toast and the Category stays deleted", async () => {
    const state = setup();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    const dialog = await openEdit(user, "Groceries");
    await user.click(within(dialog).getByRole("button", { name: c.delete }));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /^Edit Groceries,/ })).not.toBeInTheDocument(),
    );
    // Someone adds a Category under the same name meanwhile.
    state.categories = [...state.categories!, cat("z", "Groceries", "daily")];
    action(0);

    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toBe(enGB.errors.NAME_TAKEN);
    expect(toastMock).toHaveBeenCalledTimes(1);
    expect(state.categoryRestores).toBeUndefined();
  });

  it("the refusal on an inactive Category shows Reactivate beside the alert", async () => {
    const state = setup({
      categories: [cat("a", "Rent", "commitment", false), cat("b", "Groceries", "daily")],
      categoriesInUse: ["a"],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    const dialog = await openEdit(user, "Rent");
    await user.click(within(dialog).getByRole("button", { name: c.delete }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(enGB.errors.CATEGORY_IN_USE);
    expect(within(dialog).getByRole("button", { name: c.reactivate })).toBeEnabled();
    expect(within(dialog).queryByRole("button", { name: c.deactivate })).not.toBeInTheDocument();
  });

  it("the dialog cannot be dismissed while the Delete is in flight", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    server.use(
      http.delete("/api/categories/:id", async () => {
        await gate;
        return new HttpResponse(null, { status: 204 });
      }),
      ...defaultHandlers(setup()),
    );
    const user = userEvent.setup();
    const view = renderApp("/budget");

    const dialog = await openEdit(user, "Rent");
    await user.click(within(dialog).getByRole("button", { name: c.delete }));
    await user.keyboard("{Escape}");
    await user.click(within(dialog).getByRole("button", { name: enGB.dialog.close }));

    expect(screen.getByRole("dialog", { name: c.editHeading })).toBeInTheDocument();
    expect(view.param("edit")).toBe("category:a");
    release();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});
