import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import type { components } from "@/api/schema.gen";
import { renderApp } from "@/test/render";
import { selectPick } from "@/test/pickers";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);
beforeEach(() => {
  vi.clearAllMocks();
});

const v7 = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$/;
type CreateBody = components["schemas"]["CreateExpenseRequest"];
const c = enGB.capture;
const pocket = (id: string, name: string, active = true) => ({
  id,
  pocket_display_name: name,
  active,
});
const cat = (id: string, name: string, active = true) => ({
  id,
  category_display_name: name,
  bucket: "daily" as const,
  active,
});
const signedIn = (extra: Partial<State> = {}): State => ({
  signedIn: true,
  me: alice,
  pockets: [pocket("p-cash", "Cash"), pocket("p-bca", "BCA")],
  categories: [cat("c-food", "Eating out"), cat("c-old", "Old", false), cat("c-fuel", "Fuel")],
  defaultPocketId: "p-cash",
  ...extra,
});

const amount = () => screen.findByRole("textbox", { name: enGB.fields.amount });
const add = () => screen.getByRole("button", { name: c.add });
const pocketBox = () => screen.getByRole("combobox", { name: enGB.fields.pocket });
const categoryBox = () => screen.getByRole("combobox", { name: enGB.fields.category });

describe("Capture", () => {
  it("is a full screen with the amount focused and no back link", async () => {
    server.use(...defaultHandlers(signedIn()));
    renderApp("/capture");

    expect(await screen.findByRole("heading", { level: 1, name: c.title })).toBeInTheDocument();
    expect(await amount()).toHaveFocus();
    expect(within(screen.getByRole("main")).queryByRole("link")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
    // The refund checkbox sits directly under the amount (R15).
    expect(screen.getByRole("checkbox", { name: c.refund })).toBeInTheDocument();
  });

  it("adds an Expense with only an amount, sends no date or time, toasts once and goes Home", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());

    expect(await screen.findByRole("heading", { level: 1, name: enGB.home.title })).toBeVisible();
    expect(state.expensePosts).toHaveLength(1);
    const body = state.expensePosts![0]!;
    expect(body).toEqual({ id: expect.stringMatching(v7), amount: "25000", pocket_id: "p-cash" });
    expect(toastMock).toHaveBeenCalledTimes(1);
    // The toast lasts 8 s and carries an Edit action, never an Undo (R16).
    expect(toastMock).toHaveBeenCalledWith(
      "Added Rp25,000.",
      expect.objectContaining({
        duration: 8000,
        action: expect.objectContaining({ label: enGB.toast.edit }),
      }),
    );
  });

  it("sends the Category, a trimmed description, the picked date and time", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "45000");
    await selectPick(user, pocketBox(), "BCA");
    await selectPick(user, categoryBox(), "Eating out");
    await user.type(screen.getByLabelText(enGB.fields.description), "  Nasi padang  ");
    await user.click(screen.getByRole("button", { name: /^Date, / }));
    await user.click(await screen.findByRole("button", { name: /20(th)? November/ }));
    await user.click(screen.getByRole("button", { name: enGB.dates.addTime }));
    await user.click(screen.getByRole("button", { name: "19" }));
    await user.click(screen.getByRole("button", { name: ":05" }));
    await user.click(add());

    await waitFor(() => expect(state.expensePosts).toHaveLength(1));
    expect(state.expensePosts![0]).toEqual({
      id: expect.stringMatching(v7),
      amount: "45000",
      pocket_id: "p-bca",
      category_id: "c-food",
      description: "Nasi padang",
      occurred_on: "2026-11-20",
      occurred_time: "19:05",
    });
  });

  it("sends a refund as the negative amount and says so in the toast", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "450000");
    await user.click(screen.getByRole("checkbox", { name: c.refund }));
    await user.click(add());

    await waitFor(() => expect(state.expensePosts).toHaveLength(1));
    expect(state.expensePosts![0]!.amount).toBe("-450000");
    expect(toastMock).toHaveBeenCalledWith("Added -Rp450,000.", expect.anything());
  });

  it("offers active Pockets and Categories only, with a way back to no Category", async () => {
    const state = signedIn({
      pockets: [pocket("p-cash", "Cash"), pocket("p-gone", "Gone", false)],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");
    await amount();

    await user.click(pocketBox());
    expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual(["Cash"]);
    await user.keyboard("{Escape}");

    await user.click(categoryBox());
    expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual([
      c.noCategory,
      "Eating out",
      "Fuel",
    ]);
    await user.click(screen.getByRole("option", { name: "Fuel" }));
    await selectPick(user, categoryBox(), c.noCategory);
    await user.type(await amount(), "1000");
    await user.click(add());

    await waitFor(() => expect(state.expensePosts).toHaveLength(1));
    expect(state.expensePosts![0]).not.toHaveProperty("category_id");
  });

  it("preselects the default Pocket, else the only active one, else starts blank", async () => {
    server.use(...defaultHandlers(signedIn({ defaultPocketId: "p-bca" })));
    const first = renderApp("/capture");
    await amount();
    expect(pocketBox()).toHaveTextContent("BCA");
    first.unmount();

    server.use(
      ...defaultHandlers(
        signedIn({ pockets: [pocket("p-only", "Only")], defaultPocketId: undefined }),
      ),
    );
    const second = renderApp("/capture");
    await amount();
    expect(pocketBox()).toHaveTextContent("Only");
    second.unmount();

    const state = signedIn({ defaultPocketId: undefined });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");
    await user.type(await amount(), "1000");
    expect(pocketBox()).toHaveTextContent("");
    await user.click(add());
    expect(await screen.findByText(c.pocketRequired)).toBeInTheDocument();
    expect(state.expensePosts).toBeUndefined();
  });

  it("asks for an amount on submit and posts nothing", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");
    await amount();

    await user.click(add());

    expect(await screen.findByText(c.amountRequired)).toBeInTheDocument();
    expect(state.expensePosts).toBeUndefined();
  });

  it("clamps a description to 200 code points", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/capture");
    await amount();

    const field = screen.getByLabelText(enGB.fields.description);
    await user.click(field);
    await user.paste("🙂".repeat(205));

    expect(Array.from((field as HTMLInputElement).value)).toHaveLength(200);
  });

  it("omits a blank description", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "1000");
    await user.type(screen.getByLabelText(enGB.fields.description), "   ");
    await user.click(add());

    await waitFor(() => expect(state.expensePosts).toHaveLength(1));
    expect(state.expensePosts![0]).not.toHaveProperty("description");
  });

  it("retries a lost response with the same id, so the server's replay answers", async () => {
    const state = signedIn();
    let first = true;
    server.use(
      http.post("/api/expenses", () => {
        if (first) {
          first = false;
          return HttpResponse.error();
        }
        return undefined;
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());
    expect(await screen.findByRole("alert")).toHaveTextContent(enGB.errors.NETWORK);
    expect(toastMock).not.toHaveBeenCalled();
    await user.click(add());

    expect(await screen.findByRole("heading", { level: 1, name: enGB.home.title })).toBeVisible();
    expect(state.expensePosts).toHaveLength(1);
  });

  // A reply lost after the server stored the Expense: the server replays what it
  // stored, whatever the retry's body says.
  function loseFirstReply(state: State) {
    let first = true;
    return http.post("/api/expenses", async ({ request }) => {
      if (!first) return undefined;
      first = false;
      const body = (await request.clone().json()) as CreateBody;
      (state.expensePosts ??= []).push(body);
      // Store it as the server would, then drop the answer.
      (state.expenses ??= []).unshift({
        id: body.id,
        occurred_on: state.me.today,
        captured_at: "2026-11-25T03:00:00Z",
        amount: body.amount,
        currency: "IDR",
        pocket_id: body.pocket_id,
        ownership: "joint",
      });
      return HttpResponse.error();
    });
  }

  it("mends an Expense stored by a lost reply with one PATCH, and toasts what the server holds", async () => {
    const state = signedIn();
    server.use(loseFirstReply(state), ...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());
    expect(await screen.findByRole("alert")).toHaveTextContent(enGB.errors.NETWORK);
    await user.clear(await amount());
    await user.type(await amount(), "30000");
    await selectPick(user, categoryBox(), "Fuel");
    await user.type(screen.getByLabelText(enGB.fields.description), "Pertamax");
    await user.click(add());

    expect(await screen.findByRole("heading", { level: 1, name: enGB.home.title })).toBeVisible();
    expect(state.expensePosts).toHaveLength(2);
    expect(state.expensePosts![1]!.id).toBe(state.expensePosts![0]!.id);
    expect(state.expensePatches).toEqual([
      {
        id: state.expensePosts![0]!.id,
        body: { amount: "30000", category_id: "c-fuel", description: "Pertamax" },
      },
    ]);
    expect(toastMock).toHaveBeenCalledTimes(1);
    expect(toastMock).toHaveBeenCalledWith("Added Rp30,000.", expect.anything());
  });

  it("clears the time and Category the replayed Expense had with null, and sends no date nobody picked", async () => {
    const state = signedIn();
    let first = true;
    server.use(
      http.post("/api/expenses", async ({ request }) => {
        if (!first) return undefined;
        first = false;
        const body = (await request.clone().json()) as CreateBody;
        (state.expensePosts ??= []).push(body);
        (state.expenses ??= []).unshift({
          id: body.id,
          occurred_on: "2026-11-20",
          occurred_time: "19:05",
          captured_at: "2026-11-25T03:00:00Z",
          amount: "25000",
          currency: "IDR",
          pocket_id: body.pocket_id,
          category_id: "c-food",
          ownership: "joint",
        });
        return HttpResponse.error();
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());
    await screen.findByText(enGB.errors.NETWORK);
    await selectPick(user, categoryBox(), enGB.capture.noCategory);
    await user.click(add());

    await screen.findByRole("heading", { level: 1, name: enGB.home.title });
    expect(state.expensePatches).toEqual([
      {
        id: state.expensePosts![0]!.id,
        body: { category_id: null, occurred_time: null },
      },
    ]);
  });

  it("starts over with a fresh id when the mending PATCH finds the Expense gone, on the same tap", async () => {
    const state = signedIn();
    let patches = 0;
    server.use(
      loseFirstReply(state),
      http.patch("/api/expenses/:id", () => {
        patches += 1;
        return HttpResponse.json(envelope("NOT_FOUND"), { status: 404 });
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());
    await screen.findByText(enGB.errors.NETWORK);
    await user.clear(await amount());
    await user.type(await amount(), "30000");
    await user.click(add());
    expect(await screen.findByText(enGB.errors.NOT_FOUND)).toBeInTheDocument();
    expect(toastMock).not.toHaveBeenCalled();

    await user.click(add());
    await screen.findByRole("heading", { level: 1, name: enGB.home.title });
    expect(state.expensePosts).toHaveLength(3);
    expect(state.expensePosts![2]!.id).not.toBe(state.expensePosts![0]!.id);
    expect(patches).toBe(1);
    expect(toastMock).toHaveBeenCalledWith("Added Rp30,000.", expect.anything());
  });

  it("toasts but does not go Home when the person left Capture while Add was pending", async () => {
    const state = signedIn();
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    server.use(
      http.post("/api/expenses", async () => {
        await gate;
        return undefined;
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());
    expect(screen.getByRole("button", { name: c.adding })).toBeDisabled();
    expect(screen.getByRole("button", { name: /^Date, / })).toBeDisabled();
    expect(screen.getByRole("button", { name: enGB.dates.addTime })).toBeDisabled();
    await user.click(screen.getByRole("link", { name: enGB.shell.tabs.settings }));
    release();

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    await screen.findByRole("heading", { level: 2, name: enGB.settings.household.title });
    expect(
      screen.queryByRole("heading", { level: 1, name: enGB.home.title }),
    ).not.toBeInTheDocument();
    expect(toastMock).toHaveBeenCalledTimes(1);
  });

  it("sends no PATCH when the replay matches the form", async () => {
    const state = signedIn();
    server.use(loseFirstReply(state), ...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());
    await screen.findByText(enGB.errors.NETWORK);
    await user.click(add());

    await screen.findByRole("heading", { level: 1, name: enGB.home.title });
    expect(state.expensePatches).toBeUndefined();
    expect(toastMock).toHaveBeenCalledWith("Added Rp25,000.", expect.anything());
  });

  it("keeps the form when the mending PATCH fails, and Add retries the PATCH, not the POST", async () => {
    const state = signedIn();
    let failPatch = true;
    server.use(
      loseFirstReply(state),
      http.patch("/api/expenses/:id", () => {
        if (!failPatch) return undefined;
        failPatch = false;
        return HttpResponse.json(
          envelope("VALIDATION", { field: "category_id", rule: "inactive" }),
          {
            status: 400,
          },
        );
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());
    await screen.findByText(enGB.errors.NETWORK);
    await selectPick(user, categoryBox(), "Fuel");
    await user.click(add());

    expect(await screen.findByText(c.categoryUnusable)).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: c.title })).toBeVisible();
    expect(toastMock).not.toHaveBeenCalled();

    await user.click(add());
    await screen.findByRole("heading", { level: 1, name: enGB.home.title });
    expect(state.expensePosts).toHaveLength(2);
    expect(state.expensePatches).toHaveLength(1);
    expect(toastMock).toHaveBeenCalledTimes(1);
  });

  it("sends the same id again after a network failure, and a fresh one after ID_TAKEN", async () => {
    const ids: string[] = [];
    const answers = ["network", "taken", "ok"];
    server.use(
      http.post("/api/expenses", async ({ request }) => {
        ids.push(((await request.clone().json()) as { id: string }).id);
        const answer = answers.shift();
        if (answer === "network") return HttpResponse.error();
        if (answer === "taken") return HttpResponse.json(envelope("ID_TAKEN"), { status: 409 });
        return undefined;
      }),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());
    await screen.findByText(enGB.errors.NETWORK);
    await user.click(add());
    await screen.findByText(enGB.errors.ID_TAKEN);
    await user.click(add());
    await screen.findByRole("heading", { level: 1, name: enGB.home.title });

    expect(ids).toHaveLength(3);
    expect(ids[1]).toBe(ids[0]);
    expect(ids[2]).not.toBe(ids[1]);
  });

  it("keeps the form when a background refetch of the Pockets fails", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    const { client } = renderApp("/capture");
    await user.type(await amount(), "25000");

    server.use(
      http.get("/api/pockets", () => HttpResponse.json(envelope("INTERNAL"), { status: 500 })),
    );
    await client.refetchQueries({ queryKey: ["pockets"] });

    expect(await amount()).toHaveValue("25,000");
    expect(screen.queryByRole("button", { name: enGB.app.retry })).not.toBeInTheDocument();
  });

  it("clears a server refusal when the time changes", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "0");
    await user.click(add());
    expect(await screen.findByText(c.amountNonzero)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: enGB.dates.addTime }));
    await user.click(screen.getByRole("button", { name: "19" }));
    await user.click(screen.getByRole("button", { name: ":05" }));

    expect(screen.queryByText(c.amountNonzero)).not.toBeInTheDocument();
  });

  it("shows a zero amount's refusal under the amount", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "0");
    await user.click(add());

    const message = await screen.findByText(c.amountNonzero);
    expect(message).toHaveAttribute("role", "alert");
    expect(await amount()).toHaveAttribute("aria-invalid", "true");
    expect(toastMock).not.toHaveBeenCalled();
    await user.type(await amount(), "5");
    expect(screen.queryByText(c.amountNonzero)).not.toBeInTheDocument();
  });

  it.each([
    ["pocket_id", "inactive", c.pocketUnusable],
    ["pocket_id", "invalid", c.pocketUnusable],
    ["category_id", "inactive", c.categoryUnusable],
    ["category_id", "invalid", c.categoryUnusable],
    ["occurred_on", "invalid", c.dateInvalid],
    ["description", "required", "Check the description field."],
  ])("shows VALIDATION %s %s under its field", async (field, rule, text) => {
    server.use(
      http.post("/api/expenses", () =>
        HttpResponse.json(envelope("VALIDATION", { field, rule }), { status: 400 }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());

    expect(await screen.findByText(text)).toHaveAttribute("role", "alert");
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("refetches the Pockets when the chosen one is no longer in use, and blanks the choice", async () => {
    const state = signedIn({
      pockets: [pocket("p-cash", "Cash"), pocket("p-bca", "BCA"), pocket("p-ovo", "OVO")],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/capture");
    await user.type(await amount(), "25000");
    // Another member deactivates Cash after this screen loaded its Pockets.
    state.pockets = [
      pocket("p-cash", "Cash", false),
      pocket("p-bca", "BCA"),
      pocket("p-ovo", "OVO"),
    ];
    await user.click(add());

    expect(await screen.findByText(c.pocketUnusable)).toBeInTheDocument();
    await waitFor(() => expect(pocketBox()).toHaveTextContent(/^$/));
    await selectPick(user, pocketBox(), "BCA");
    await user.click(add());
    await screen.findByRole("heading", { level: 1, name: enGB.home.title });
    expect(state.expensePosts![1]!.pocket_id).toBe("p-bca");
  });

  it("shows any other failure in the form, above Add", async () => {
    server.use(
      http.post("/api/expenses", () => HttpResponse.json(envelope("INTERNAL"), { status: 500 })),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/capture");

    await user.type(await amount(), "25000");
    await user.click(add());

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(enGB.errors.INTERNAL);
    expect(alert.closest("form")).not.toBeNull();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("offers Settings instead of the form when there is no active Pocket", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("p-gone", "Gone", false)] })));
    renderApp("/capture");

    expect(await screen.findByText(c.noPocket)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: c.noPocketLink })).toHaveAttribute("href", "/settings");
    expect(screen.queryByRole("button", { name: c.add })).not.toBeInTheDocument();
  });

  it("is worded in Indonesian, with the refund checkbox", async () => {
    server.use(...defaultHandlers(signedIn({ me: { ...alice, locale: "id-ID" } })));
    const user = userEvent.setup();
    renderApp("/capture");

    expect(
      await screen.findByRole("heading", { level: 1, name: idID.capture.title }),
    ).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Ini pengembalian dana" })).toBeInTheDocument();
    await user.type(screen.getByRole("textbox", { name: idID.fields.amount }), "25000");
    await user.click(screen.getByRole("button", { name: idID.capture.add }));
    await waitFor(() =>
      expect(toastMock).toHaveBeenCalledWith("Rp25.000 ditambahkan.", expect.anything()),
    );
  });
});
