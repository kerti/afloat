import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";
import idID from "@/locales/id-ID.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());

const v7 = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$/;
const pocket = (id: string, name: string) => ({ id, pocket_display_name: name, active: true });
const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });
const toastMock = vi.mocked(toast);
const p = enGB.pockets;

// The page's Add button opens the dialog; its submit button is named "Add".
async function openCreate(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: p.addHeading }));
  return screen.findByRole("dialog", { name: p.addHeading });
}
const submit = () => screen.getByRole("button", { name: p.add });

beforeEach(() => {
  vi.clearAllMocks();
});

describe("pockets list", () => {
  it("is reached from the shell and lists Pockets in server order, with no chevron or row buttons", async () => {
    const state = signedIn({
      pockets: [pocket("a", "Cash"), pocket("b", "BCA debit"), pocket("c", "OVO")],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/");

    await user.click(await screen.findByRole("link", { name: "Pockets" }));

    const rows = await screen.findAllByRole("listitem");
    expect(rows.map((r) => r.textContent)).toEqual(["Cash", "BCA debit", "OVO"]);
    // One button per row, named for the Pocket: no Edit/Deactivate pair.
    expect(screen.getAllByRole("button", { name: /^(Edit|Deactivate|Reactivate) / })).toHaveLength(
      3,
    );
    expect(screen.getByRole("button", { name: "Edit OVO" })).toBeInTheDocument();
  });

  it("shows a spinner with text while loading", async () => {
    server.use(
      http.get("/api/pockets", async () => {
        await new Promise((r) => setTimeout(r, 50));
        return HttpResponse.json({ items: [] });
      }),
      ...defaultHandlers(signedIn()),
    );
    renderApp("/pockets");
    expect(await screen.findByRole("status")).toHaveTextContent(enGB.app.loading);
    expect(await screen.findByText(p.empty)).toBeInTheDocument();
  });

  it("shows the empty state with the create action when there are none", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    const view = renderApp("/pockets");
    expect(await screen.findByText(p.empty)).toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: p.addHeading }));
    expect(await screen.findByRole("dialog", { name: p.addHeading })).toBeInTheDocument();
    expect(view.param("new")).toBe("pocket");
  });

  it("shows a load failure as an alert with a full-width Retry that refetches", async () => {
    let fail = true;
    server.use(
      http.get("/api/pockets", () =>
        fail ? HttpResponse.json(envelope("INTERNAL"), { status: 500 }) : undefined,
      ),
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })),
    );
    const user = userEvent.setup();
    renderApp("/pockets");

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(enGB.errors.INTERNAL);
    const retry = within(alert).getByRole("button", { name: enGB.app.retry });
    expect(retry).toHaveClass("w-full");

    fail = false;
    await user.click(retry);
    expect(await screen.findByRole("button", { name: "Edit Cash" })).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("creating a Pocket", () => {
  it("sends exactly {id, pocket_display_name} with a v7 id, closes the dialog, and raises one toast naming it", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/pockets");

    const dialog = await openCreate(user);
    expect(view.param("new")).toBe("pocket");
    const field = within(dialog).getByLabelText(enGB.fields.pocketDisplayName);
    expect(field).toHaveAttribute("maxlength", "60");
    expect(field).toHaveFocus();
    await user.type(field, "Cash");
    await user.click(submit());

    expect(await screen.findByRole("button", { name: "Edit Cash" })).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(view.param("new")).toBeNull();
    expect(state.pocketPosts).toHaveLength(1);
    expect(Object.keys(state.pocketPosts![0]!).sort()).toEqual(["id", "pocket_display_name"]);
    expect(state.pocketPosts![0]!.pocket_display_name).toBe("Cash");
    expect(state.pocketPosts![0]!.id).toMatch(v7);
    expect(toastMock).toHaveBeenCalledTimes(1);
    expect(toastMock.mock.calls[0]![0]).toBe("Added Cash.");
    expect(toastMock.mock.calls[0]![1]).toMatchObject({ duration: 4000 });
  });

  it("sends a different id for the next Add", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    for (const name of ["Cash", "OVO"]) {
      const dialog = await openCreate(user);
      await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), name);
      await user.click(submit());
      await screen.findByRole("button", { name: `Edit ${name}` });
    }

    const [first, second] = state.pocketPosts!;
    expect(first!.id).not.toBe(second!.id);
    expect(second!.id).toMatch(v7);
  });

  it("resends the same id when a retry follows a network failure, even after closing and reopening", async () => {
    const state = signedIn();
    let fail = true;
    server.use(
      http.post("/api/pockets", async ({ request }) => {
        if (fail) {
          (state.pocketPosts ??= []).push((await request.json()) as never);
          return HttpResponse.error();
        }
        return undefined;
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openCreate(user);
    await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), "Cash");
    await user.click(submit());
    // Not a field's code, so it sits in the form above the footer.
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Can't reach Afloat.");
    expect(within(dialog).getByLabelText(enGB.fields.pocketDisplayName)).not.toHaveAttribute(
      "aria-invalid",
    );
    expect(within(dialog).getByLabelText(enGB.fields.pocketDisplayName)).toHaveValue("Cash");
    expect(toastMock).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: p.cancel }));
    fail = false;
    const again = await openCreate(user);
    await user.type(within(again).getByLabelText(enGB.fields.pocketDisplayName), "Cash");
    await user.click(submit());
    expect(await screen.findByRole("button", { name: "Edit Cash" })).toBeInTheDocument();

    expect(state.pocketPosts).toHaveLength(2);
    expect(state.pocketPosts![0]!.id).toBe(state.pocketPosts![1]!.id);
  });

  it("sends a fresh id on the retry after an ID_TAKEN", async () => {
    const state = signedIn();
    let taken = true;
    server.use(
      http.post("/api/pockets", async ({ request }) => {
        if (taken) {
          taken = false;
          (state.pocketPosts ??= []).push((await request.json()) as never);
          return HttpResponse.json(envelope("ID_TAKEN"), { status: 409 });
        }
        return undefined;
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openCreate(user);
    await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), "Cash");
    await user.click(submit());
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(/Try again in a moment\./);

    await user.click(submit());
    expect(await screen.findByRole("button", { name: "Edit Cash" })).toBeInTheDocument();

    expect(state.pocketPosts).toHaveLength(2);
    expect(state.pocketPosts![1]!.id).not.toBe(state.pocketPosts![0]!.id);
    expect(state.pocketPosts![1]!.id).toMatch(v7);
  });

  it("treats a 200 replay as a success", async () => {
    const state = signedIn();
    server.use(
      http.post("/api/pockets", async ({ request }) => {
        const body = (await request.json()) as { id: string; pocket_display_name: string };
        state.pockets = [pocket(body.id, body.pocket_display_name)];
        return HttpResponse.json(state.pockets[0], { status: 200 });
      }),
      ...defaultHandlers(state),
    );
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openCreate(user);
    await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), "Cash");
    await user.click(submit());

    expect(await screen.findByRole("button", { name: "Edit Cash" })).toBeInTheDocument();
    expect(toastMock).toHaveBeenCalledTimes(1);
  });

  it("disables the inputs and says Adding… while the write is in flight", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    server.use(
      http.post("/api/pockets", async () => {
        await gate;
        return undefined;
      }),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openCreate(user);
    await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), "Cash");
    await user.click(submit());

    const pending = await within(dialog).findByRole("button", { name: p.adding });
    expect(pending).toBeDisabled();
    expect(within(dialog).getByLabelText(enGB.fields.pocketDisplayName)).toBeDisabled();
    release();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("stays on /pockets when the dialog is dismissed while the write is in flight", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    server.use(
      http.post("/api/pockets", async () => {
        await gate;
        return undefined;
      }),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    // Arrive from Home, so there is an entry behind /pockets for a stray
    // second back to land on.
    renderApp("/");
    await user.click(await screen.findByRole("link", { name: "Pockets" }));

    const dialog = await openCreate(user);
    await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), "Cash");
    await user.click(submit());
    await within(dialog).findByRole("button", { name: p.adding });
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    release();
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    // The success must not close the dialog a second time and go back past /pockets.
    expect(screen.getByTestId("location")).toHaveTextContent(/^\/pockets$/);
  });

  it("keeps NAME_TAKEN under the field, the dialog open, and raises no toast", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })));
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openCreate(user);
    const field = within(dialog).getByLabelText(enGB.fields.pocketDisplayName);
    await user.type(field, "cash");
    await user.click(submit());

    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent(enGB.errors.NAME_TAKEN);
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription(enGB.errors.NAME_TAKEN);
    expect(field).toHaveValue("cash");
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("keeps a server VALIDATION on pocket_display_name under the field", async () => {
    server.use(
      http.post("/api/pockets", () =>
        HttpResponse.json(envelope("VALIDATION", { field: "pocket_display_name", rule: "max" }), {
          status: 400,
        }),
      ),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openCreate(user);
    const field = within(dialog).getByLabelText(enGB.fields.pocketDisplayName);
    await user.type(field, "Cash");
    await user.click(submit());

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Check the Pocket name field.",
    );
    expect(field).toHaveAttribute("aria-invalid", "true");
  });

  it("leaves Add enabled when blank: submitting shows the error and sends nothing", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openCreate(user);
    expect(submit()).toBeEnabled();
    await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), "   ");
    await user.click(submit());

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(p.nameRequired);
    expect(within(dialog).getByLabelText(enGB.fields.pocketDisplayName)).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(state.pocketPosts).toBeUndefined();
    expect(submit()).toBeEnabled();
  });

  it("shows the blank error on blur once the name was edited, before any submit", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openCreate(user);
    const field = within(dialog).getByLabelText(enGB.fields.pocketDisplayName);
    await user.tab();
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();

    await user.type(field, "x");
    await user.clear(field);
    await user.tab();
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(p.nameRequired);
  });

  it.each(["INTERNAL", "NETWORK"] as const)(
    "shows %s inside the dialog above the footer, not on the field",
    async (code) => {
      server.use(
        http.post("/api/pockets", () =>
          code === "NETWORK"
            ? HttpResponse.error()
            : HttpResponse.json(envelope(code), { status: 500 }),
        ),
        ...defaultHandlers(signedIn()),
      );
      const user = userEvent.setup();
      renderApp("/pockets");

      const dialog = await openCreate(user);
      await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), "Cash");
      await user.click(submit());

      const alert = await within(dialog).findByRole("alert");
      expect(alert).toHaveTextContent(enGB.errors[code]);
      expect(within(dialog).getByLabelText(enGB.fields.pocketDisplayName)).not.toHaveAttribute(
        "aria-invalid",
      );
      expect(toastMock.error).not.toHaveBeenCalled();
    },
  );

  it("sends the person to sign in when the session has ended", async () => {
    server.use(
      http.post("/api/pockets", () => HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 })),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    const { client } = renderApp("/pockets");

    const dialog = await openCreate(user);
    await user.type(within(dialog).getByLabelText(enGB.fields.pocketDisplayName), "Cash");
    await user.click(submit());

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(client.getQueryData(["me"])).toBeNull();
  });
});

describe("closing a writing dialog", () => {
  it("closes by Cancel, the labelled X, Esc and back, and clears the param", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    const view = renderApp("/pockets");

    let dialog = await openCreate(user);
    await user.click(within(dialog).getByRole("button", { name: p.cancel }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("new")).toBeNull();

    dialog = await openCreate(user);
    await user.click(within(dialog).getByRole("button", { name: enGB.dialog.close }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("new")).toBeNull();

    await openCreate(user);
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("new")).toBeNull();

    await openCreate(user);
    expect(view.param("new")).toBe("pocket");
    act(() => view.navigate(-1));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("new")).toBeNull();
  });

  it("closes on a tap on the backdrop: it records no money (Uruni ADR-046)", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const view = renderApp("/pockets");

    await openCreate(user);
    await user.click(document.querySelector('[data-slot="dialog-overlay"]')!);

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("new")).toBeNull();
  });

  it("opens from a link to ?new=pocket", async () => {
    server.use(...defaultHandlers(signedIn()));
    renderApp("/pockets?new=pocket");
    expect(await screen.findByRole("dialog", { name: p.addHeading })).toBeInTheDocument();
  });
});

describe("session and locale", () => {
  it("does not show one User's Pockets to another on the same device after sign-out", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const { client } = renderApp("/");

    await user.click(await screen.findByRole("link", { name: "Pockets" }));
    expect(await screen.findByText("Cash")).toBeInTheDocument();
    await user.click(screen.getByRole("link", { name: "Home" }));
    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    await screen.findByRole("heading", { name: "Sign in" });
    expect(client.getQueryData(["pockets"])).toBeUndefined();

    // Bob, with his own Household and no Pockets, signs in on the same client.
    state.pockets = [];
    state.me = { ...alice, email: "bob@example.com", user_display_name: "Bob" };
    await user.type(screen.getByLabelText("Email"), "bob@example.com");
    await user.type(screen.getByLabelText("Password"), "correct-horse");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await user.click(await screen.findByRole("link", { name: "Pockets" }));

    expect(await screen.findByText(p.empty)).toBeInTheDocument();
    expect(screen.queryByText("Cash")).not.toBeInTheDocument();
  });

  it("renders in Indonesian for an Indonesian User", async () => {
    server.use(
      ...defaultHandlers(
        signedIn({ me: { ...alice, locale: "id-ID" }, pockets: [pocket("a", "Tunai")] }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/pockets");
    const ip = idID.pockets;

    expect(await screen.findByRole("heading", { name: "Kantong" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Ubah Tunai" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: idID.home.title })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: ip.addHeading }));
    const dialog = await screen.findByRole("dialog", { name: ip.addHeading });
    expect(within(dialog).getByLabelText("Nama kantong")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: idID.dialog.close })).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: ip.add })).toBeInTheDocument();
  });

  it("sends a signed-out visitor to sign in", async () => {
    server.use(...defaultHandlers());
    renderApp("/pockets");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });
});
