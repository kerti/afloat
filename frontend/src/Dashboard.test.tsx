import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { QueryClientProvider, focusManager, type QueryClient } from "@tanstack/react-query";
import { createQueryClient } from "@/api/queryClient";
import {
  dashboardKey,
  useCreateCategory,
  useCreateExpense,
  useDashboard,
  useDeleteCategory,
  useDeleteExpense,
  useRestoreExpense,
  useUpdateBudget,
  useUpdateCategory,
  useUpdateExpense,
  useUpdateHousehold,
  useUpdateMe,
} from "@/api/queries";
import type { Dashboard } from "@/api/client";
import { alice, defaultHandlers, server, type State } from "@/test/server";

// The dashboard's data layer (#155 R5): one read, never patched by hand, and
// invalidated by every write that can move a figure. The screen is PR 5; these
// tests mount the hook beside the write, as a screen would.

const trajectory: Dashboard = {
  currency: "IDR",
  trajectory: {
    left_to_spend_today: "176470.5882",
    todays_allowance: "176470.5882",
    days_remaining: 17,
    remaining_daily_pool: "3000000.0000",
    baseline_daily_budget: "100000.0000",
  },
};

const id = "0192f0a0-0000-7000-8000-0000000000e1";
const expense = {
  id,
  occurred_on: "2026-11-25",
  captured_at: "2026-11-25T03:00:00Z",
  amount: "15000.0000",
  currency: "IDR",
  pocket_id: "p-cash",
  ownership: "joint" as const,
};
const signedIn = (extra: Partial<State> = {}): State => ({
  signedIn: true,
  me: alice,
  pockets: [{ id: "p-cash", pocket_display_name: "Cash", active: true }],
  categories: [{ id: "c-food", category_display_name: "Food", bucket: "daily", active: true }],
  expenses: [expense],
  deletedExpenses: [{ ...expense, id: "0192f0a0-0000-7000-8000-0000000000e2" }],
  ...extra,
});

function mount<T>(use: () => T, state: State) {
  server.use(...defaultHandlers(state));
  const client: QueryClient = createQueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  // Spread, so the result is read whole and every change of it re-renders (a
  // query's result re-renders only for the properties something has read).
  const view = renderHook(() => ({ dashboard: { ...useDashboard() }, write: use() }), { wrapper });
  return { ...view, client };
}

describe("useDashboard", () => {
  it("answers the figures as the server sent them, untouched", async () => {
    const { result } = mount(() => null, signedIn({ dashboard: trajectory }));
    await waitFor(() => expect(result.current.dashboard.isSuccess).toBe(true));
    expect(result.current.dashboard.data).toEqual(trajectory);
  });

  it("answers the currency alone while there is no Monthly Budget, with no trajectory", async () => {
    const { result } = mount(() => null, signedIn());
    await waitFor(() => expect(result.current.dashboard.isSuccess).toBe(true));
    expect(result.current.dashboard.data).toEqual({ currency: "IDR" });
    expect(result.current.dashboard.data).not.toHaveProperty("trajectory");
  });

  it("keeps the previous figures in place while a refetch runs, so there is never a spinner to come back to", async () => {
    const state = signedIn({ dashboard: trajectory });
    const { result, client } = mount(() => null, state);
    await waitFor(() => expect(result.current.dashboard.isSuccess).toBe(true));

    // The next read is held until the test lets it go, so "while it runs" is a
    // state the test can stand in.
    const fresh: Dashboard = {
      currency: "IDR",
      trajectory: { ...trajectory.trajectory!, left_to_spend_today: "90000.0000" },
    };
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    server.use(
      http.get("/api/dashboard", async () => {
        await held;
        return HttpResponse.json(fresh);
      }),
    );
    let refetch!: Promise<unknown>;
    act(() => {
      refetch = client.invalidateQueries({ queryKey: dashboardKey });
    });
    await waitFor(() => expect(result.current.dashboard.isFetching).toBe(true));
    expect(result.current.dashboard.isPending).toBe(false);
    expect(result.current.dashboard.data).toEqual(trajectory);

    release();
    await act(async () => {
      await refetch;
    });
    await waitFor(() => expect(result.current.dashboard.data).toEqual(fresh));
  });

  it("refetches when the app regains focus, which is how a day rolling over reaches Home", async () => {
    const state = signedIn({ dashboard: trajectory });
    const { result } = mount(() => null, state);
    await waitFor(() => expect(result.current.dashboard.isSuccess).toBe(true));
    expect(state.dashboardReads).toBe(1);

    act(() => {
      focusManager.setFocused(false);
      focusManager.setFocused(true);
    });

    await waitFor(() => expect(state.dashboardReads).toBe(2));
  });

  it("is dropped when the session ends, with everything else of the last User's", async () => {
    const { result, client } = mount(() => null, signedIn({ dashboard: trajectory }));
    await waitFor(() => expect(result.current.dashboard.isSuccess).toBe(true));
    const { clearSession } = await import("@/api/queryClient");

    await act(async () => {
      await clearSession(client);
    });

    expect(client.getQueryData(dashboardKey)).toBeUndefined();
  });
});

// Every write that can move a figure refetches the mounted dashboard (R5): the
// Expenses, the Budget, the Categories (an allocation may ride with one), the
// Household (the Period's window, the allowance mode) and the User's own
// settings (`time_zone` moves Today). Removing the invalidation from any one
// of these fails its row.
describe("writes that move a figure invalidate the dashboard", () => {
  const rows: {
    name: string;
    use: () => { mutateAsync: (v: never) => Promise<unknown> };
    vars: unknown;
  }[] = [
    {
      name: "a new Expense",
      use: useCreateExpense as never,
      vars: { id: "0192f0a0-0000-7000-8000-0000000000e3", amount: "5000", pocket_id: "p-cash" },
    },
    {
      name: "a corrected Expense",
      use: useUpdateExpense as never,
      vars: { id, body: { amount: "20000" } },
    },
    { name: "a deleted Expense", use: useDeleteExpense as never, vars: id },
    {
      name: "a restored Expense",
      use: useRestoreExpense as never,
      vars: "0192f0a0-0000-7000-8000-0000000000e2",
    },
    {
      name: "a Budget change",
      use: useUpdateBudget as never,
      vars: { monthly_budget: "9000000.0000" },
    },
    {
      name: "a new Category",
      use: useCreateCategory as never,
      vars: {
        id: "0192f0a0-0000-7000-8000-0000000000c9",
        category_display_name: "Rent",
        bucket: "commitment",
        allocation: "2000000",
      },
    },
    {
      name: "an updated Category",
      use: useUpdateCategory as never,
      vars: { id: "c-food", body: { active: false } },
    },
    { name: "a deleted Category", use: useDeleteCategory as never, vars: "c-food" },
    {
      name: "a Household setting",
      use: useUpdateHousehold as never,
      vars: { period_start_day: 5 },
    },
    {
      name: "the User's own settings (time_zone moves Today)",
      use: useUpdateMe as never,
      vars: { time_zone: "Asia/Makassar" },
    },
  ];

  it.each(rows)("$name", async ({ use, vars }) => {
    const state = signedIn({ dashboard: trajectory });
    const { result } = mount(use, state);
    await waitFor(() => expect(result.current.dashboard.isSuccess).toBe(true));
    expect(state.dashboardReads).toBe(1);

    await act(async () => {
      await result.current.write.mutateAsync(vars as never);
    });

    await waitFor(() => expect(state.dashboardReads).toBeGreaterThan(1));
  });

  it("a write that fails moves no figure and refetches nothing", async () => {
    const state = signedIn({ dashboard: trajectory });
    const { result } = mount(useCreateExpense, state);
    await waitFor(() => expect(result.current.dashboard.isSuccess).toBe(true));

    await act(async () => {
      await expect(
        result.current.write.mutateAsync({
          id: "0192f0a0-0000-7000-8000-0000000000e4",
          amount: "0",
          pocket_id: "p-cash",
        }),
      ).rejects.toBeDefined();
    });

    expect(state.dashboardReads).toBe(1);
  });
});
