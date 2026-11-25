import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

type Expense = NonNullable<State["expenses"]>[number];

const expense = (id: string, extra: Partial<Expense> & { occurred_on: string }): Expense => ({
  id,
  captured_at: "2026-11-25T03:00:00Z",
  amount: "10000",
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
  ],
  categories: [
    { id: "c-food", category_display_name: "Eating out", bucket: "daily", active: true },
    { id: "c-old", category_display_name: "Retired", bucket: "daily", active: false },
  ],
  ...extra,
});

const list = () => within(screen.getByRole("main"));
// The Shell's main replaces the sign-in probe's, so wait for the title first.
const shown = () => screen.findByRole("heading", { level: 1, name: enGB.expenses.title });

describe("Expenses", () => {
  it("groups by day under headings worked out from the server's Today", async () => {
    // alice.today is 2026-11-25; nothing here reads the device clock.
    const state = signedIn({
      expenses: [
        expense("1", {
          occurred_on: "2026-11-25",
          description: "Nasi padang",
          category_id: "c-food",
          occurred_time: "12:30",
          amount: "45000",
        }),
        expense("2", { occurred_on: "2026-11-25", pocket_id: "p-old" }),
        expense("3", { occurred_on: "2026-11-24", category_id: "c-old", amount: "-450000" }),
        expense("4", { occurred_on: "2026-11-22", description: "Market" }),
        expense("5", { occurred_on: "2025-12-31", description: "Last year" }),
      ],
    });
    server.use(...defaultHandlers(state));
    renderApp("/expenses");
    await shown();

    expect(
      await screen.findByRole("heading", { level: 1, name: enGB.expenses.title }),
    ).toBeVisible();
    await list().findByText("Nasi padang");
    expect(
      list()
        .getAllByRole("heading", { level: 3 })
        .map((h) => h.textContent),
    ).toEqual(["Today", "Yesterday", "Sunday, 22 November", "Wednesday, 31 December 2025"]);
  });

  it("titles a row by its description, else its Category, else Uncategorised (muted), with the detail line", async () => {
    const state = signedIn({
      expenses: [
        expense("1", {
          occurred_on: "2026-11-25",
          description: "Nasi padang",
          category_id: "c-food",
          occurred_time: "12:30",
          amount: "45000",
        }),
        expense("2", { occurred_on: "2026-11-25", pocket_id: "p-old" }),
        expense("3", { occurred_on: "2026-11-25", category_id: "c-old", amount: "-450000" }),
      ],
    });
    server.use(...defaultHandlers(state));
    renderApp("/expenses");
    await shown();

    const rows = await list().findAllByRole("listitem");
    expect(rows).toHaveLength(3);
    // description: the Category and the time join the Pocket on the detail line
    expect(rows[0]).toHaveTextContent("Nasi padang");
    expect(rows[0]).toHaveTextContent("Cash · Eating out · 12:30");
    expect(rows[0]).toHaveTextContent("Rp45,000");
    // neither: muted, and an inactive Pocket still resolves
    expect(within(rows[1]!).getByText(enGB.expenses.uncategorised)).toHaveClass(
      "text-muted-foreground",
    );
    expect(rows[1]).toHaveTextContent("Old wallet");
    // a Category title is not repeated on the detail line; an inactive one still resolves; a refund is neutral
    expect(rows[2]).toHaveTextContent("Retired");
    expect(rows[2]).toHaveTextContent("Cash");
    expect(rows[2]).not.toHaveTextContent("Cash · Retired");
    expect(rows[2]).toHaveTextContent("-Rp450,000");
  });

  it("pages with a Load more button, never ahead, and the heading continues across pages", async () => {
    const all = [1, 2, 3, 4, 5].map((n) =>
      expense(String(n), {
        occurred_on: n <= 3 ? "2026-11-25" : "2026-11-24",
        description: `Row ${n}`,
      }),
    );
    const state = signedIn({ expenses: all, expensePageSize: 2 });
    const requests: (string | null)[] = [];
    server.use(
      http.get("/api/expenses", ({ request }) => {
        requests.push(new URL(request.url).searchParams.get("after"));
        return undefined;
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/expenses");
    await shown();

    await list().findByText("Row 2");
    expect(list().queryByText("Row 3")).not.toBeInTheDocument();
    expect(requests).toEqual([null]);

    await user.click(list().getByRole("button", { name: enGB.expenses.loadMore }));
    await list().findByText("Row 4");
    expect(requests).toEqual([null, "2"]);
    expect(
      list()
        .getAllByRole("heading", { level: 3 })
        .map((h) => h.textContent),
    ).toEqual(["Today", "Yesterday"]);
    expect(list().getByText("Row 3")).toBeInTheDocument();

    await user.click(list().getByRole("button", { name: enGB.expenses.loadMore }));
    await list().findByText("Row 5");
    expect(requests).toEqual([null, "2", "4"]);
    // The last page has no `next`, so no button.
    expect(list().queryByRole("button", { name: enGB.expenses.loadMore })).not.toBeInTheDocument();
  });

  it("replaces Load more with a retry when a page fails, and retries that page", async () => {
    const all = [1, 2, 3].map((n) =>
      expense(String(n), { occurred_on: "2026-11-25", description: `Row ${n}` }),
    );
    const state = signedIn({ expenses: all, expensePageSize: 2 });
    let fail = true;
    server.use(
      http.get("/api/expenses", ({ request }) =>
        fail && new URL(request.url).searchParams.has("after")
          ? HttpResponse.json(envelope("INTERNAL"), { status: 500 })
          : undefined,
      ),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/expenses");
    await shown();
    await list().findByText("Row 2");

    await user.click(list().getByRole("button", { name: enGB.expenses.loadMore }));

    const alert = await list().findByRole("alert");
    expect(alert).toHaveTextContent(enGB.errors.INTERNAL);
    expect(list().queryByRole("button", { name: enGB.expenses.loadMore })).not.toBeInTheDocument();
    // What loaded stays on screen.
    expect(list().getByText("Row 2")).toBeInTheDocument();

    fail = false;
    await user.click(within(alert).getByRole("button", { name: enGB.app.retry }));
    await list().findByText("Row 3");
    expect(list().queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows a first-page failure as an alert whose retry reloads", async () => {
    let fail = true;
    server.use(
      http.get("/api/expenses", () =>
        fail ? HttpResponse.json(envelope("INTERNAL"), { status: 500 }) : undefined,
      ),
      ...defaultHandlers(
        signedIn({ expenses: [expense("1", { occurred_on: "2026-11-25", description: "Back" })] }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/expenses");
    await shown();

    const alert = await screen.findByRole("alert");
    fail = false;
    await user.click(within(alert).getByRole("button", { name: enGB.app.retry }));
    expect(await screen.findByText("Back")).toBeInTheDocument();
  });

  it("keeps the list when a background refetch of the Pockets fails", async () => {
    server.use(
      ...defaultHandlers(
        signedIn({ expenses: [expense("1", { occurred_on: "2026-11-25", description: "Back" })] }),
      ),
    );
    const { client } = renderApp("/expenses");
    await shown();
    expect(await screen.findByText("Back")).toBeInTheDocument();

    server.use(
      http.get("/api/pockets", () => HttpResponse.json(envelope("INTERNAL"), { status: 500 })),
    );
    await client.refetchQueries({ queryKey: ["pockets"] });

    expect(screen.getByText("Back")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("says what will appear when there are no Expenses", async () => {
    server.use(...defaultHandlers(signedIn()));
    renderApp("/expenses");
    await shown();

    expect(await screen.findByText(enGB.expenses.empty)).toBeInTheDocument();
    expect(list().queryByRole("button", { name: enGB.expenses.loadMore })).not.toBeInTheDocument();
  });

  it("words the headings and the amounts in Indonesian", async () => {
    const state = signedIn({
      me: { ...alice, locale: "id-ID" },
      expenses: [
        expense("1", { occurred_on: "2026-11-25", description: "Nasi", amount: "25000" }),
        expense("2", { occurred_on: "2026-11-24", description: "Kopi" }),
        expense("3", { occurred_on: "2026-11-22", description: "Pasar" }),
      ],
    });
    server.use(...defaultHandlers(state));
    renderApp("/expenses");
    await screen.findByRole("heading", { level: 1, name: idID.expenses.title });

    await list().findByText("Nasi");
    expect(
      list()
        .getAllByRole("heading", { level: 3 })
        .map((h) => h.textContent),
    ).toEqual([idID.dates.today, idID.expenses.yesterday, "Minggu, 22 November"]);
    expect(list().getByText("Rp25.000")).toBeInTheDocument();
  });
});
