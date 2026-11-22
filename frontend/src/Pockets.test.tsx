import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";

const v7 = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$/;
const pocket = (id: string, name: string) => ({ id, pocket_display_name: name, active: true });

const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });

describe("pockets", () => {
  it("is reached from the shell and lists Pockets in server order", async () => {
    const state = signedIn({
      pockets: [pocket("a", "Cash"), pocket("b", "BCA debit"), pocket("c", "OVO")],
    });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/");

    await user.click(await screen.findByRole("link", { name: "Pockets" }));

    const rows = await screen.findAllByRole("listitem");
    expect(rows.map((r) => r.firstElementChild?.textContent)).toEqual(["Cash", "BCA debit", "OVO"]);
  });

  it("shows the empty state when there are none", async () => {
    server.use(...defaultHandlers(signedIn()));
    renderApp("/pockets");
    expect(await screen.findByText("No Pockets yet.")).toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("sends exactly {id, pocket_display_name} with a v7 id, then shows the new Pocket and clears the form", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    const field = await screen.findByLabelText("Pocket name");
    expect(field).toHaveAttribute("maxlength", "60");
    await user.type(field, "Cash");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByRole("listitem")).toHaveTextContent("Cash");
    expect(state.pocketPosts).toHaveLength(1);
    expect(Object.keys(state.pocketPosts![0]!).sort()).toEqual(["id", "pocket_display_name"]);
    expect(state.pocketPosts![0]!.pocket_display_name).toBe("Cash");
    expect(state.pocketPosts![0]!.id).toMatch(v7);
    expect(field).toHaveValue("");
  });

  it("sends a different id for the next Add", async () => {
    const state = signedIn();
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    const field = await screen.findByLabelText("Pocket name");
    await user.type(field, "Cash");
    await user.click(screen.getByRole("button", { name: "Add" }));
    await screen.findByText("Cash");
    await user.type(field, "OVO");
    await user.click(screen.getByRole("button", { name: "Add" }));
    await screen.findByText("OVO");

    const [first, second] = state.pocketPosts!;
    expect(first!.id).not.toBe(second!.id);
    expect(second!.id).toMatch(v7);
  });

  it("resends the same id when a retry follows a network failure", async () => {
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

    await user.type(await screen.findByLabelText("Pocket name"), "Cash");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Can't reach Afloat.");
    expect(screen.getByLabelText("Pocket name")).toHaveValue("Cash");

    fail = false;
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByRole("listitem")).toHaveTextContent("Cash");

    expect(state.pocketPosts).toHaveLength(2);
    expect(state.pocketPosts![0]!.id).toBe(state.pocketPosts![1]!.id);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
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

    await user.type(await screen.findByLabelText("Pocket name"), "Cash");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/Try again in a moment\./);

    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByRole("listitem")).toHaveTextContent("Cash");

    expect(state.pocketPosts).toHaveLength(2);
    expect(state.pocketPosts![1]!.id).not.toBe(state.pocketPosts![0]!.id);
    expect(state.pocketPosts![1]!.id).toMatch(v7);
  });

  it("treats a 200 replay as a success and clears the form", async () => {
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

    const field = await screen.findByLabelText("Pocket name");
    await user.type(field, "Cash");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByRole("listitem")).toHaveTextContent("Cash");
    expect(field).toHaveValue("");
  });

  it("keeps NAME_TAKEN on the input", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })));
    const user = userEvent.setup();
    renderApp("/pockets");

    const field = await screen.findByLabelText("Pocket name");
    await user.type(field, "cash");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("That name is already in use.");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveValue("cash");
  });

  it("keeps a VALIDATION on pocket_display_name on the input", async () => {
    server.use(...defaultHandlers(signedIn()));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.type(await screen.findByLabelText("Pocket name"), "   ");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Check the Pocket name field.");
    expect(screen.getByLabelText("Pocket name")).toHaveAttribute("aria-invalid", "true");
  });

  it.each(["ID_TAKEN", "INTERNAL"] as const)(
    "shows %s as the generic string, not on the input",
    async (code) => {
      server.use(
        http.post("/api/pockets", () =>
          HttpResponse.json(envelope(code), { status: code === "ID_TAKEN" ? 409 : 500 }),
        ),
        ...defaultHandlers(signedIn()),
      );
      const user = userEvent.setup();
      renderApp("/pockets");

      await user.type(await screen.findByLabelText("Pocket name"), "Cash");
      await user.click(screen.getByRole("button", { name: "Add" }));

      expect(await screen.findByRole("alert")).toHaveTextContent(/Try again in a moment\./);
      expect(screen.getByLabelText("Pocket name")).not.toHaveAttribute("aria-invalid");
    },
  );

  it("sends the person to sign in when the session has ended", async () => {
    server.use(
      http.post("/api/pockets", () => HttpResponse.json(envelope("UNAUTHORIZED"), { status: 401 })),
      ...defaultHandlers(signedIn()),
    );
    const user = userEvent.setup();
    const { client } = renderApp("/pockets");

    await user.type(await screen.findByLabelText("Pocket name"), "Cash");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(client.getQueryData(["me"])).toBeNull();
  });

  it("does not show one User's Pockets to another on the same device after sign-out", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const { client } = renderApp("/");

    await user.click(await screen.findByRole("link", { name: "Pockets" }));
    expect(await screen.findByText("Cash")).toBeInTheDocument();
    await user.click(screen.getByRole("link", { name: "Back" }));
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

    expect(await screen.findByText("No Pockets yet.")).toBeInTheDocument();
    expect(screen.queryByText("Cash")).not.toBeInTheDocument();
  });

  it("renders in Indonesian for an Indonesian User", async () => {
    server.use(
      ...defaultHandlers(
        signedIn({ me: { ...alice, locale: "id-ID" }, pockets: [pocket("a", "Tunai")] }),
      ),
    );
    renderApp("/pockets");

    expect(await screen.findByRole("heading", { name: "Kantong" })).toBeInTheDocument();
    expect(await within(await screen.findByRole("list")).findByText("Tunai")).toBeInTheDocument();
    expect(screen.getByLabelText("Nama Kantong")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Tambah" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Kembali" })).toBeInTheDocument();
  });

  it("sends a signed-out visitor to sign in", async () => {
    server.use(...defaultHandlers());
    renderApp("/pockets");
    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });
});
