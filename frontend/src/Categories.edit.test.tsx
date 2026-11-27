import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { toast } from "sonner";
import { alice, defaultHandlers, emptyBudget, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { dashboardKey } from "@/api/queries";
import enGB from "@/locales/en-GB.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);
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
  unallocated: "6000000.0000",
  commitments_budget: "3000000.0000",
};
const rentAndGroceries = (extra: Partial<State> = {}): State => ({
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
const nameField = () => screen.getByLabelText(enGB.fields.categoryDisplayName);
const allocationField = () => screen.getByLabelText(enGB.fields.allocation);
const save = () => screen.getByRole("button", { name: c.save });

async function openEdit(user: ReturnType<typeof userEvent.setup>, name: string) {
  const inactive = name.endsWith(", inactive");
  const base = name.replace(", inactive", "");
  const accessible = new RegExp(`^Edit ${base}, ${inactive ? ".*, inactive$" : ""}`);
  await user.click(await screen.findByRole("button", { name: accessible }));
  return screen.findByRole("dialog", { name: c.editHeading });
}

describe("editing a Category", () => {
  it("names each row with its figure, 'No allocation' when it has none, and 'inactive' when it is", async () => {
    server.use(
      ...defaultHandlers(
        rentAndGroceries({
          categories: [
            cat("a", "Rent", "commitment"),
            cat("c", "Transport", "daily"),
            cat("z", "Old rent", "commitment", false),
          ],
          budget: {
            ...planned,
            allocations: [
              { category_id: "a", allocation: "3000000.0000" },
              { category_id: "z", allocation: "500000.0000" },
            ],
          },
        }),
      ),
    );
    renderApp("/budget");
    expect(await screen.findByRole("button", { name: "Edit Rent, Rp3,000,000" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Edit Transport, No allocation" })).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Edit Old rent, Rp500,000, inactive" }),
    ).toBeVisible();
  });

  it("treats a Category deleted meanwhile, answered 400 {category_id, invalid} on an allocation-only save, as gone: a toast, a refetch and a close", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    state.categories = [cat("a", "Rent", "commitment")];
    await user.clear(allocationField());
    await user.type(allocationField(), "5");
    await user.click(save());
    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toBe(enGB.errors.NOT_FOUND);
    expect(state.budgetPatches).toHaveLength(1);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /^Edit Groceries, / })).not.toBeInTheDocument(),
    );
  });

  it("counts a name in code points, so an emoji name is not cut at 30", async () => {
    server.use(...defaultHandlers(rentAndGroceries()));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    await user.clear(nameField());
    await user.click(nameField());
    await user.paste("😀".repeat(70));
    expect(Array.from((nameField() as HTMLInputElement).value)).toHaveLength(60);
  });

  it("opens on a tap of the row with its name, fixed Kind and allocation, and Save is disabled until something changes", async () => {
    server.use(...defaultHandlers(rentAndGroceries()));
    const user = userEvent.setup();
    const view = renderApp("/budget");
    await openEdit(user, "Rent");
    expect(view.param("edit")).toBe("category:a");
    expect(nameField()).toHaveValue("Rent");
    expect(allocationField()).toHaveValue("3,000,000");
    expect(screen.getByRole("combobox", { name: enGB.fields.categoryBucket })).toBeDisabled();
    expect(screen.getByRole("combobox", { name: enGB.fields.categoryBucket })).toHaveTextContent(
      c.kindCommitment,
    );
    expect(save()).toBeDisabled();
    await userEvent.type(nameField(), " ");
    expect(save()).toBeDisabled();
  });

  it("renames with one PATCH /categories and no budget PATCH, toasting Saved once", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Rent");
    await user.clear(nameField());
    await user.type(nameField(), "Housing");
    await user.click(save());

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith("Saved.", expect.objectContaining({ duration: 4000 }));
    expect(state.categoryPatches).toEqual([
      { id: "a", body: { category_display_name: "Housing" } },
    ]);
    expect(state.budgetPatches).toBeUndefined();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(await screen.findByRole("button", { name: /^Edit Housing, / })).toBeInTheDocument();
  });

  it("changes an allocation with one PATCH /budget, sends the string, refreshes the figures and not the name", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    await user.clear(allocationField());
    await user.type(allocationField(), "2000000");
    await user.click(save());

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.budgetPatches).toEqual([
      { allocations: [{ category_id: "b", allocation: "2000000" }] },
    ]);
    expect(state.categoryPatches).toBeUndefined();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const daily = within(screen.getByRole("region", { name: c.dailyHeading }));
    expect(await daily.findByRole("button", { name: /^Edit Groceries, / })).toHaveTextContent(
      "Rp2,000,000",
    );
  });

  it("sends both parts when both changed, the name first, and one toast", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    await user.clear(nameField());
    await user.type(nameField(), "Food");
    await user.clear(allocationField());
    await user.type(allocationField(), "1");
    await user.click(save());
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.categoryPatches).toHaveLength(1);
    expect(state.budgetPatches).toEqual([{ allocations: [{ category_id: "b", allocation: "1" }] }]);
  });

  it('sends "0" as an allocation, and null only when a Daily allocation is cleared', async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    await openEdit(user, "Groceries");
    await user.clear(allocationField());
    await user.type(allocationField(), "0");
    await user.click(save());
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.budgetPatches![0]).toEqual({
      allocations: [{ category_id: "b", allocation: "0" }],
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(await screen.findByRole("button", { name: /^Edit Groceries, / })).toHaveTextContent(
      "Rp0",
    );

    await openEdit(user, "Groceries");
    await user.clear(allocationField());
    await user.click(save());
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(2));
    expect(state.budgetPatches![1]).toEqual({
      allocations: [{ category_id: "b", allocation: null }],
    });
    expect(await screen.findByRole("button", { name: /^Edit Groceries, / })).toHaveTextContent(
      c.noAllocation,
    );
  });

  it("never sends null for a Commitment: a cleared allocation is a client error and nothing is sent", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Rent");
    await user.clear(allocationField());
    await user.click(save());
    expect(await screen.findByText(c.allocationRequired)).toBeInTheDocument();
    expect(state.budgetPatches).toBeUndefined();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("gives a Daily Category with no allocation a blank field, and sending nothing when left blank", async () => {
    const state = rentAndGroceries({
      categories: [cat("b", "Groceries", "daily")],
      budget: planned,
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    expect(allocationField()).toHaveValue("");
    expect(save()).toBeDisabled();
  });

  it("shows an error on blur only after the field was edited", async () => {
    server.use(...defaultHandlers(rentAndGroceries()));
    const user = userEvent.setup();
    const dialog = await (async () => {
      renderApp("/budget");
      return openEdit(user, "Rent");
    })();
    nameField().focus();
    await user.tab();
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
    await user.clear(nameField());
    await user.tab();
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(c.nameRequired);
  });

  it("maps NAME_TAKEN to the name field and keeps the dialog open without a toast", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    await user.clear(nameField());
    await user.type(nameField(), "rent");
    await user.click(save());
    expect(await screen.findByText(enGB.errors.NAME_TAKEN)).toBeInTheDocument();
    expect(nameField()).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("maps NOT_FOUND to an error toast and closes the dialog, as the Category is gone", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    state.categories = [cat("a", "Rent", "commitment")];
    await user.clear(nameField());
    await user.type(nameField(), "Food");
    await user.click(save());
    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toBe(enGB.errors.NOT_FOUND);
    expect(toastMock).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /^Edit Groceries, / })).not.toBeInTheDocument();
  });

  it("a Category found gone on an allocation-only save also invalidates the dashboard, which counted it", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const { client } = renderApp("/budget");
    await openEdit(user, "Groceries");
    state.categories = [cat("a", "Rent", "commitment")];
    // A fresh entry, so only the gone path can mark it stale.
    client.setQueryData(dashboardKey, { currency: "IDR" });
    await user.clear(allocationField());
    await user.type(allocationField(), "5");
    await user.click(save());

    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(state.budgetPatches).toHaveLength(1);
    await waitFor(() => expect(client.getQueryState(dashboardKey)?.isInvalidated).toBe(true));
  });

  it("maps a nested allocation VALIDATION to the allocation field", async () => {
    const state = rentAndGroceries();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    const { http, HttpResponse } = await import("msw");
    server.use(
      http.patch("/api/budget", () =>
        HttpResponse.json(
          { code: "VALIDATION", args: { field: "allocations.0.allocation", rule: "pattern" } },
          { status: 400 },
        ),
      ),
    );
    await openEdit(user, "Groceries");
    await user.clear(allocationField());
    await user.type(allocationField(), "5");
    await user.click(save());
    expect(await screen.findByText("Check the allocation field.")).toBeInTheDocument();
    expect(allocationField()).toHaveAttribute("aria-invalid", "true");
  });

  it("keeps a saved name when the allocation then fails, and a retry sends the allocation alone", async () => {
    const state = rentAndGroceries({ budgetPatchFails: "INTERNAL" });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    await user.clear(nameField());
    await user.type(nameField(), "Food");
    await user.clear(allocationField());
    await user.type(allocationField(), "7");
    await user.click(save());

    expect(await screen.findByText(enGB.errors.INTERNAL)).toBeInTheDocument();
    expect(toastMock).not.toHaveBeenCalled();
    expect(state.categoryPatches).toHaveLength(1);
    await user.click(save());
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.categoryPatches).toHaveLength(1);
    expect(state.budgetPatches).toHaveLength(2);
    expect(state.budgetPatches![1]).toEqual({
      allocations: [{ category_id: "b", allocation: "7" }],
    });
  });
});

describe("deactivating and reactivating a Category", () => {
  it("deactivates at once with no confirm, closes the dialog and raises one Undo toast that reverses it", async () => {
    const state = rentAndGroceries({
      categories: [cat("b", "Groceries", "daily")],
      budget: { ...planned, allocations: [] },
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Groceries");
    await user.click(screen.getByRole("button", { name: c.deactivate }));

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.categoryPatches).toEqual([{ id: "b", body: { active: false } }]);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    const [message, opts] = toastMock.mock.calls[0]!;
    expect(message).toBe("Deactivated Groceries.");
    expect(opts).toMatchObject({ duration: 8000, action: { label: enGB.toast.undo } });
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /^Edit Groceries, / })).not.toBeInTheDocument(),
    );

    (opts as unknown as { action: { onClick: () => void } }).action.onClick();
    await waitFor(() => expect(state.categories![0]!.active).toBe(true));
    expect(await screen.findByRole("button", { name: /^Edit Groceries, / })).toBeInTheDocument();
  });

  it("the Undo of a deactivate invalidates the dashboard, as the write it reverses did (#155 R5)", async () => {
    const state = rentAndGroceries({
      categories: [cat("b", "Groceries", "daily")],
      budget: { ...planned, allocations: [] },
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const { client } = renderApp("/budget");
    await openEdit(user, "Groceries");
    await user.click(screen.getByRole("button", { name: c.deactivate }));
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    // A fresh entry, so only the Undo can mark it stale.
    client.setQueryData(dashboardKey, { currency: "IDR" });

    (
      toastMock.mock.calls[0]![1] as unknown as { action: { onClick: () => void } }
    ).action.onClick();

    await waitFor(() => expect(client.getQueryState(dashboardKey)?.isInvalidated).toBe(true));
  });

  it("reactivates an inactive Category that holds an allocation, with its own Undo toast", async () => {
    const state = rentAndGroceries({
      categories: [cat("a", "Old rent", "commitment", false)],
      budget: { ...planned, allocations: [{ category_id: "a", allocation: "500000.0000" }] },
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openEdit(user, "Old rent, inactive");
    await user.click(screen.getByRole("button", { name: c.reactivate }));
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.categoryPatches).toEqual([{ id: "a", body: { active: true } }]);
    const [message, opts] = toastMock.mock.calls[0]!;
    expect(message).toBe("Reactivated Old rent.");
    expect(opts).toMatchObject({ duration: 8000 });
  });
});

describe("Show inactive Categories", () => {
  const withInactive = () =>
    rentAndGroceries({
      categories: [
        cat("a", "Rent", "commitment"),
        cat("x", "Old gym", "commitment", false),
        cat("y", "Old snacks", "daily", false),
        cat("z", "Old rent", "commitment", false),
      ],
      budget: { ...planned, allocations: [{ category_id: "z", allocation: "500000.0000" }] },
    });

  it("counts only the inactive Categories still hidden, lists them on tick, and keeps an allocated one listed throughout", async () => {
    server.use(...defaultHandlers(withInactive()));
    const user = userEvent.setup();
    renderApp("/budget");
    const toggle = await screen.findByRole("checkbox", { name: "Show inactive Categories (2)" });
    expect(toggle).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: /^Edit Old rent, .*, inactive$/ }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Old gym")).not.toBeInTheDocument();

    await user.click(toggle);
    expect(
      screen.getByRole("button", { name: /^Edit Old gym, .*, inactive$/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /^Edit Old snacks, .*, inactive$/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /^Edit Old rent, .*, inactive$/ }),
    ).toBeInTheDocument();
    await user.click(toggle);
    expect(screen.queryByText("Old gym")).not.toBeInTheDocument();
  });

  it("is absent when nothing is hidden", async () => {
    server.use(...defaultHandlers(rentAndGroceries()));
    renderApp("/budget");
    await screen.findByRole("button", { name: /^Edit Rent, / });
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });
});

describe("the trailing-amount row", () => {
  it("is a tappable row naming the Category, with its amount at the end, and muted words when it has none", async () => {
    server.use(
      ...defaultHandlers(
        rentAndGroceries({
          categories: [cat("a", "Rent", "commitment"), cat("c", "Transport", "daily")],
        }),
      ),
    );
    renderApp("/budget");
    const rent = await screen.findByRole("button", { name: /^Edit Rent, / });
    expect(rent).toHaveTextContent("RentRp3,000,000");
    expect(within(rent).getByText("Rp3,000,000")).toHaveClass("tabular-nums", "font-medium");
    const none = within(screen.getByRole("button", { name: /^Edit Transport, / })).getByText(
      c.noAllocation,
    );
    expect(none).toHaveClass("text-muted-foreground");
    expect(none).not.toHaveClass("font-medium");
  });
});
