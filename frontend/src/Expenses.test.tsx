import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
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

    // The "Needs a category" row leads the list while an Expense has none.
    const items = await list().findAllByRole("listitem");
    const rows = items.slice(1);
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

describe("The uncategorised surface", () => {
  const needs = (n: number) => `Needs a category (${n})`;
  const path = () => screen.getByTestId("location").textContent;
  const filtered = () =>
    screen.findByRole("heading", { level: 1, name: enGB.expenses.needsCategoryTitle });
  const withBoth = () =>
    signedIn({
      expenses: [
        expense("1", { occurred_on: "2026-11-25", description: "Coffee" }),
        expense("2", { occurred_on: "2026-11-24", category_id: "c-food", description: "Lunch" }),
        expense("3", { occurred_on: "2026-11-23", description: "Parking" }),
      ],
    });

  it("shows one 'Needs a category (N)' row only while N is above zero", async () => {
    server.use(...defaultHandlers(withBoth()));
    const view = renderApp("/expenses");
    await shown();

    expect(await list().findByRole("link", { name: needs(2) })).toHaveAttribute(
      "href",
      "/expenses?uncategorised=true",
    );
    view.unmount();

    server.use(
      ...defaultHandlers(
        signedIn({
          expenses: [expense("2", { occurred_on: "2026-11-24", category_id: "c-food" })],
        }),
      ),
    );
    renderApp("/expenses");
    await shown();
    await list().findByText("Eating out");
    expect(list().queryByRole("link", { name: /Needs a category/ })).not.toBeInTheDocument();
  });

  it("filters through the URL, with a way back, a line saying what it is, and no row", async () => {
    const state = withBoth();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses");
    await shown();

    await user.click(await list().findByRole("link", { name: needs(2) }));
    await list().findByText(enGB.expenses.uncategorisedFilter);
    await filtered();
    expect(path()).toBe("/expenses?uncategorised=true");
    expect(state.expenseListQueries).toContain("uncategorised=true");
    expect(list().getByRole("link", { name: enGB.expenses.allExpenses })).toBeInTheDocument();
    expect(list().queryByRole("link", { name: /Needs a category/ })).not.toBeInTheDocument();
    expect(await list().findByRole("link", { name: "Edit Coffee, Rp10,000" })).toBeVisible();
    expect(list().getByRole("link", { name: "Edit Parking, Rp10,000" })).toBeVisible();
    expect(list().queryByRole("link", { name: /Lunch/ })).not.toBeInTheDocument();

    await user.click(list().getByRole("link", { name: enGB.expenses.allExpenses }));
    expect(await list().findByRole("link", { name: "Edit Lunch, Rp10,000" })).toBeVisible();
    expect(path()).toBe("/expenses");
  });

  it("opens straight on the filter from a link, and says when nothing is missing a Category", async () => {
    server.use(
      ...defaultHandlers(
        signedIn({
          expenses: [expense("2", { occurred_on: "2026-11-24", category_id: "c-food" })],
        }),
      ),
    );
    renderApp("/expenses?uncategorised=true");
    await filtered();

    expect(await screen.findByText(enGB.expenses.uncategorisedEmpty)).toBeInTheDocument();
  });

  it("drops an Expense from the filter once a Category is chosen on its edit route", async () => {
    const state = withBoth();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/expenses?uncategorised=true");
    await filtered();

    await user.click(await list().findByRole("link", { name: "Edit Coffee, Rp10,000" }));
    // Its way back names the filtered view it came from, not the whole list.
    expect(
      await screen.findByRole("link", { name: enGB.expenses.needsCategoryTitle }),
    ).toHaveAttribute("href", "/expenses?uncategorised=true");
    await user.click(await screen.findByRole("combobox", { name: enGB.fields.category }));
    await user.click(await screen.findByRole("option", { name: "Eating out" }));
    await user.click(screen.getByRole("button", { name: enGB.expenses.save }));

    await waitFor(() => expect(path()).toBe("/expenses?uncategorised=true"));
    await list().findByRole("link", { name: "Edit Parking, Rp10,000" });
    await waitFor(() =>
      expect(list().queryByRole("link", { name: "Edit Coffee, Rp10,000" })).not.toBeInTheDocument(),
    );
  });

  it("words the row in Indonesian", async () => {
    server.use(...defaultHandlers({ ...withBoth(), me: { ...alice, locale: "id-ID" } }));
    renderApp("/expenses");
    await screen.findByRole("heading", { level: 1, name: idID.expenses.title });

    expect(await list().findByRole("link", { name: "Perlu kategori (2)" })).toBeInTheDocument();
  });
});
