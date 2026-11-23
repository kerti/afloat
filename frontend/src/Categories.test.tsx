import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
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

const v7 = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$/;
const c = enGB.categories;
const cat = (id: string, name: string, bucket: "daily" | "commitment", active = true) => ({
  id,
  category_display_name: name,
  bucket,
  active,
});
const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });
const planned = {
  ...emptyBudget,
  monthly_budget: "9000000.0000",
  daily_pool: "9000000.0000",
  unallocated: "9000000.0000",
};

async function openCreate(user: ReturnType<typeof userEvent.setup>, name = c.addHeading) {
  await user.click(await screen.findByRole("button", { name }));
  return screen.findByRole("dialog", { name });
}
async function chooseType(user: ReturnType<typeof userEvent.setup>, label: string, name: string) {
  await user.click(screen.getByRole("combobox", { name: label }));
  await user.click(await screen.findByRole("option", { name }));
}

describe("categories on the Budget screen", () => {
  it("lists Commitments and Daily Categories with their allocations, and none for a Category without one", async () => {
    const state = signedIn({
      categories: [
        cat("a", "Rent", "commitment"),
        cat("b", "Groceries", "daily"),
        cat("c", "Transport", "daily"),
      ],
      budget: {
        ...planned,
        allocations: [
          { category_id: "a", allocation: "3000000.0000" },
          { category_id: "b", allocation: "1500000.5000" },
        ],
        commitments_budget: "3000000.0000",
        daily_pool: "6000000.0000",
        unallocated: "4499999.5000",
      },
    });
    server.use(...defaultHandlers(state));
    renderApp("/budget");

    const commitments = within(await screen.findByRole("region", { name: c.commitmentsHeading }));
    expect(commitments.getAllByRole("listitem").map((r) => r.textContent)).toEqual([
      "RentRp3,000,000",
    ]);
    const daily = within(screen.getByRole("region", { name: c.dailyHeading }));
    expect(daily.getAllByRole("listitem").map((r) => r.textContent)).toEqual([
      "GroceriesRp1,500,001",
      "TransportNo allocation",
    ]);
  });

  it("says what will appear when a bucket has nothing", async () => {
    server.use(...defaultHandlers(signedIn({ categories: [cat("b", "Groceries", "daily")] })));
    renderApp("/budget");
    expect(await screen.findByText(c.commitmentsEmpty)).toBeInTheDocument();
    expect(screen.queryByText(c.dailyEmpty)).not.toBeInTheDocument();
  });

  it("lists an inactive Category only while it holds a non-zero allocation", async () => {
    const state = signedIn({
      categories: [
        cat("a", "Old rent", "commitment", false),
        cat("b", "Old gym", "commitment", false),
        cat("c", "Zeroed", "commitment", false),
        cat("d", "Old snacks", "daily", false),
      ],
      budget: {
        ...planned,
        allocations: [
          { category_id: "a", allocation: "500000.0000" },
          { category_id: "c", allocation: "0.0000" },
        ],
        commitments_budget: "500000.0000",
      },
    });
    server.use(...defaultHandlers(state));
    renderApp("/budget");

    const row = (await screen.findByText("Old rent")).parentElement!;
    expect(row).toHaveTextContent("Old rentInactiveRp500,000");
    expect(within(row).getByText(enGB.pockets.inactive)).toBeInTheDocument();
    expect(screen.queryByText("Old gym")).not.toBeInTheDocument();
    expect(screen.queryByText("Zeroed")).not.toBeInTheDocument();
    expect(screen.queryByText("Old snacks")).not.toBeInTheDocument();
  });

  it("shows a categories load failure with a Retry", async () => {
    let fail = true;
    server.use(
      http.get("/api/categories", () =>
        fail ? HttpResponse.json(envelope("INTERNAL"), { status: 500 }) : undefined,
      ),
      ...defaultHandlers(signedIn({ categories: [cat("b", "Groceries", "daily")] })),
    );
    const user = userEvent.setup();
    renderApp("/budget");
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole("button", { name: enGB.app.retry }));
    expect(await screen.findByText("Groceries")).toBeInTheDocument();
  });
});

describe("an over-allocated Budget", () => {
  const over = { ...planned, unallocated: "-500000.0000" };

  it("shows a negative Unallocated with a muted explanatory line, and none otherwise", async () => {
    server.use(...defaultHandlers(signedIn({ budget: over })));
    const first = renderApp("/budget");
    expect((await screen.findByText("Unallocated")).nextSibling).toHaveTextContent("-Rp500,000");
    const line = screen.getByText(enGB.budget.overAllocated);
    expect(line).toHaveClass("text-muted-foreground");
    expect(line).not.toHaveAttribute("role", "alert");
    first.unmount();
    server.resetHandlers();

    server.use(...defaultHandlers(signedIn({ budget: planned })));
    renderApp("/budget");
    await screen.findByText("Unallocated");
    expect(screen.queryByText(enGB.budget.overAllocated)).not.toBeInTheDocument();
  });
});

describe("adding a Category", () => {
  it("creates a Commitment with a client UUIDv7 and a canonical allocation, then refreshes the figures and toasts once", async () => {
    const state = signedIn({ budget: planned });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/budget");
    await openCreate(user);
    expect(view.param("new")).toBe("category");

    await user.type(screen.getByLabelText(enGB.fields.categoryDisplayName), "Rent");
    await chooseType(user, enGB.fields.categoryBucket, c.kindCommitment);
    await user.type(screen.getByLabelText(enGB.fields.allocation), "3000000");
    await user.click(screen.getByRole("button", { name: c.add }));

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(toastMock).toHaveBeenCalledWith(
      "Added Rent.",
      expect.objectContaining({ duration: 4000 }),
    );
    expect(state.categoryPosts).toHaveLength(1);
    expect(state.categoryPosts![0]).toEqual({
      id: expect.stringMatching(v7),
      category_display_name: "Rent",
      bucket: "commitment",
      allocation: "3000000",
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const commitments = within(screen.getByRole("region", { name: c.commitmentsHeading }));
    expect(await commitments.findByText("Rent")).toBeInTheDocument();
    expect(screen.getByText("Commitments Budget").nextSibling).toHaveTextContent("Rp3,000,000");
    expect(screen.getByText("Daily Pool").nextSibling).toHaveTextContent("Rp6,000,000");
    expect(screen.getByText("Unallocated").nextSibling).toHaveTextContent("Rp6,000,000");
  });

  it("requires an allocation for a Commitment and sends nothing without it", async () => {
    const state = signedIn({ budget: planned });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user);

    await user.type(screen.getByLabelText(enGB.fields.categoryDisplayName), "Rent");
    await chooseType(user, enGB.fields.categoryBucket, c.kindCommitment);
    await user.click(screen.getByRole("button", { name: c.add }));

    expect(await screen.findByText(c.allocationRequired)).toBeInTheDocument();
    expect(state.categoryPosts).toBeUndefined();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("asks for a name and a type, and sends nothing", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user);
    await user.click(screen.getByRole("button", { name: c.add }));
    expect(await screen.findByText(c.nameRequired)).toBeInTheDocument();
    expect(screen.getByText(c.kindRequired)).toBeInTheDocument();
    expect(state.categoryPosts).toBeUndefined();
  });

  it("creates a Daily Category with no allocation, omitting the field", async () => {
    const state = signedIn({ budget: planned });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user);

    await user.type(screen.getByLabelText(enGB.fields.categoryDisplayName), "Groceries");
    await chooseType(user, enGB.fields.categoryBucket, c.kindDaily);
    await user.click(screen.getByRole("button", { name: c.add }));

    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    expect(state.categoryPosts![0]).toEqual({
      id: expect.stringMatching(v7),
      category_display_name: "Groceries",
      bucket: "daily",
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const daily = within(screen.getByRole("region", { name: c.dailyHeading }));
    expect((await daily.findByText("Groceries")).parentElement).toHaveTextContent(
      "GroceriesNo allocation",
    );
  });

  it("moves only Unallocated for a Daily Category with an allocation", async () => {
    const state = signedIn({ budget: planned });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user);

    await user.type(screen.getByLabelText(enGB.fields.categoryDisplayName), "Groceries");
    await chooseType(user, enGB.fields.categoryBucket, c.kindDaily);
    await user.type(screen.getByLabelText(enGB.fields.allocation), "1000000");
    await user.click(screen.getByRole("button", { name: c.add }));

    await waitFor(() =>
      expect(screen.getByText("Unallocated").nextSibling).toHaveTextContent("Rp8,000,000"),
    );
    expect(screen.getByText("Daily Pool").nextSibling).toHaveTextContent("Rp9,000,000");
  });

  it("shows NAME_TAKEN under the name, keeps the dialog open and raises no toast", async () => {
    const state = signedIn({ categories: [cat("a", "Rent", "commitment")], budget: planned });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user);

    await user.type(screen.getByLabelText(enGB.fields.categoryDisplayName), "rent");
    await chooseType(user, enGB.fields.categoryBucket, c.kindDaily);
    await user.click(screen.getByRole("button", { name: c.add }));

    const field = screen.getByLabelText(enGB.fields.categoryDisplayName);
    expect(await screen.findByText(enGB.errors.NAME_TAKEN)).toBeInTheDocument();
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(toastMock).not.toHaveBeenCalled();
    await user.type(field, "s");
    expect(screen.queryByText(enGB.errors.NAME_TAKEN)).not.toBeInTheDocument();
  });

  it("resends the same id after a failure, and takes a new one after ID_TAKEN", async () => {
    let calls = 0;
    server.use(
      http.post("/api/categories", async ({ request }) => {
        calls += 1;
        seen.push(((await request.json()) as { id: string }).id);
        return calls === 1
          ? HttpResponse.json(envelope("INTERNAL"), { status: 500 })
          : HttpResponse.json(envelope("ID_TAKEN"), { status: 409 });
      }),
      ...defaultHandlers(signedIn()),
    );
    const seen: string[] = [];
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user);
    await user.type(screen.getByLabelText(enGB.fields.categoryDisplayName), "Groceries");
    await chooseType(user, enGB.fields.categoryBucket, c.kindDaily);
    await user.click(screen.getByRole("button", { name: c.add }));
    expect(await screen.findByText(enGB.errors.INTERNAL)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: c.add }));
    expect(await screen.findByText(enGB.errors.ID_TAKEN)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: c.add }));
    await waitFor(() => expect(seen).toHaveLength(3));
    expect(seen[1]).toBe(seen[0]);
    expect(seen[2]).not.toBe(seen[1]);
  });

  it("closes on a tap outside the dialog, since an allocation is a plan, and on Cancel", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user);
    await user.click(document.querySelector('[data-slot="dialog-overlay"]')!);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    await openCreate(user);
    await user.click(screen.getByRole("button", { name: c.cancel }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("shows a field's error on blur only once it was edited, so leaving untouched flashes nothing", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/budget");
    const dialog = await openCreate(user);
    const field = within(dialog).getByLabelText(enGB.fields.categoryDisplayName);
    field.focus();
    await user.tab();
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();

    await user.type(field, "x");
    await user.clear(field);
    await user.tab();
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(c.nameRequired);
  });

  it("offers the kinds as Daily, then Commitment", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user);
    await user.click(screen.getByRole("combobox", { name: enGB.fields.categoryBucket }));
    expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual([
      c.kindDaily,
      c.kindCommitment,
    ]);
  });
});

describe("categories in Indonesian", () => {
  it("renders the headings, dialog and allocation in id-ID", async () => {
    const me = { ...alice, locale: "id-ID" as const };
    const state = signedIn({
      me,
      categories: [cat("a", "Sewa", "commitment")],
      budget: {
        ...planned,
        allocations: [{ category_id: "a", allocation: "3000000.0000" }],
        commitments_budget: "3000000.0000",
      },
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/budget");

    const section = within(
      await screen.findByRole("region", { name: idID.categories.commitmentsHeading }),
    );
    expect(section.getByText("Sewa").parentElement).toHaveTextContent("Rp3.000.000");
    expect(screen.getByRole("region", { name: idID.categories.dailyHeading })).toBeInTheDocument();

    await openCreate(user, idID.categories.addHeading);
    await user.click(screen.getByRole("button", { name: idID.categories.add }));
    expect(await screen.findByText(idID.categories.nameRequired)).toBeInTheDocument();
    expect(screen.getByText(idID.categories.kindRequired)).toBeInTheDocument();
  });

  it("words NAME_TAKEN in id-ID", async () => {
    const me = { ...alice, locale: "id-ID" as const };
    server.use(...defaultHandlers(signedIn({ me, categories: [cat("a", "Sewa", "commitment")] })));
    const user = userEvent.setup();
    renderApp("/budget");
    await openCreate(user, idID.categories.addHeading);
    await user.type(screen.getByLabelText(idID.fields.categoryDisplayName), "Sewa");
    await chooseType(user, idID.fields.categoryBucket, idID.categories.kindDaily);
    await user.click(screen.getByRole("button", { name: idID.categories.add }));
    expect(await screen.findByText(idID.errors.NAME_TAKEN)).toBeInTheDocument();
  });
});
