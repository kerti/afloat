import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { UNDO_TOAST_MS } from "@/lib/undoToast";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());

const pocket = (id: string, name: string, active = true) => ({
  id,
  pocket_display_name: name,
  active,
});
const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });
const toastMock = vi.mocked(toast);
// The edit form sits in the list; the Add form below it has the same label.
const editor = () => within(screen.getByRole("list")).getByLabelText("Pocket name");
const action = (call: number) =>
  (toastMock.mock.calls[call]![1]!.action as unknown as { onClick: () => void }).onClick();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("renaming a Pocket", () => {
  it("sends only the changed name, closes the form and says Saved. in place, with no toast", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByRole("button", { name: "Edit Cash" }));
    const field = editor();
    expect(field).toHaveValue("Cash");
    await user.clear(field);
    await user.type(field, "Wallet");
    await user.click(screen.getByRole("button", { name: "Save" }));

    const row = (await screen.findByText("Wallet")).closest("li")!;
    expect(within(row).getByRole("status")).toHaveTextContent("Saved.");
    expect(state.pocketPatches).toEqual([{ pocket_display_name: "Wallet" }]);
    expect(
      screen.queryByRole("list") &&
        within(screen.getByRole("list")).queryByLabelText("Pocket name"),
    ).not.toBeInTheDocument();
    expect(toastMock).not.toHaveBeenCalled();
    expect(toastMock.success).not.toHaveBeenCalled();
  });

  it("sends nothing when the name is unchanged", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByRole("button", { name: "Edit Cash" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("button", { name: "Edit Cash" })).toBeInTheDocument();
    expect(state.pocketPatches).toBeUndefined();
  });

  it("Cancel discards the draft", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByRole("button", { name: "Edit Cash" }));
    await user.type(editor(), "XYZ");
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(await screen.findByText("Cash")).toBeInTheDocument();
    expect(state.pocketPatches).toBeUndefined();
  });

  it("keeps NAME_TAKEN on the input and the form open", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByRole("button", { name: "Edit Cash" }));
    const field = editor();
    await user.clear(field);
    await user.type(field, "ovo");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("That name is already in use.");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveValue("ovo");
  });

  it("shows NOT_FOUND and refetches the list", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByRole("button", { name: "Edit Cash" }));
    const field = editor();
    await user.type(field, "x");
    // Another member deletes it meanwhile.
    state.pockets = [];
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Afloat couldn't find that. It may have been removed.",
    );
    // The refetch empties the list, which unmounts the row and its form.
    expect(await screen.findByText("No Pockets yet.")).toBeInTheDocument();
  });
});

describe("deactivating and reactivating", () => {
  it("deactivates at once, hides the row, and raises one 8 s Undo toast", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByRole("button", { name: "Deactivate Cash" }));

    await waitFor(() => expect(screen.queryByText("Cash")).not.toBeInTheDocument());
    expect(state.pocketPatches).toEqual([{ active: false }]);
    expect(toastMock).toHaveBeenCalledTimes(1);
    const [message, options] = toastMock.mock.calls[0]!;
    expect(message).toBe("Deactivated.");
    expect(options?.duration).toBe(UNDO_TOAST_MS);
    expect(UNDO_TOAST_MS).toBe(8000);
    expect(options?.action).toMatchObject({ label: "Undo" });
  });

  it("Undo sends active:true and the row comes back", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByRole("button", { name: "Deactivate Cash" }));
    await screen.findByText("No active Pockets. 1 inactive Pocket hidden.", { exact: false });
    action(0);

    expect(await screen.findByText("Cash")).toBeInTheDocument();
    expect(state.pocketPatches).toEqual([{ active: false }, { active: true }]);
  });

  it("a failed Undo raises its own error toast with no Undo", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByRole("button", { name: "Deactivate Cash" }));
    await screen.findByText("No active Pockets.", { exact: false });
    server.use(
      http.patch("/api/pockets/:id", () =>
        HttpResponse.json(envelope("INTERNAL"), { status: 500 }),
      ),
    );
    action(0);

    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toMatch(/Try again in a moment\./);
    expect(toastMock).toHaveBeenCalledTimes(1);
  });

  it("an inactive Pocket shows Inactive and Reactivate, which sends active:true with no toast", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash", false), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByLabelText("Show inactive Pockets (1)"));
    const row = screen.getByText("Cash").closest("li")!;
    expect(within(row).getByText("Inactive")).toBeInTheDocument();
    await user.click(within(row).getByRole("button", { name: "Reactivate Cash" }));

    await waitFor(() => expect(within(row).queryByText("Inactive")).not.toBeInTheDocument());
    expect(state.pocketPatches).toEqual([{ active: true }]);
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("with the box ticked, deactivating mutes the row in place", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO", false)] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByLabelText("Show inactive Pockets (1)"));
    await user.click(screen.getByRole("button", { name: "Deactivate Cash" }));

    const row = (await screen.findByText("Cash")).closest("li")!;
    expect(await within(row).findByText("Inactive")).toBeInTheDocument();
    expect(screen.getByLabelText("Show inactive Pockets (2)")).toBeChecked();
  });

  it("shows NOT_FOUND from a deactivation and refetches", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    const button = await screen.findByRole("button", { name: "Deactivate Cash" });
    state.pockets = [];
    await user.click(button);

    expect(await screen.findByText("No Pockets yet.")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Afloat couldn't find that.");
    expect(toastMock).not.toHaveBeenCalled();
  });
});

describe("Show inactive Pockets", () => {
  it("is absent when nothing is inactive", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })));
    renderApp("/pockets");
    await screen.findByText("Cash");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("hides inactive Pockets, counts them, and reveals them when ticked", async () => {
    server.use(
      ...defaultHandlers(
        signedIn({
          pockets: [pocket("a", "Cash"), pocket("b", "Old", false), pocket("c", "OVO", false)],
        }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/pockets");

    const box = await screen.findByRole("checkbox", { name: "Show inactive Pockets (2)" });
    expect(box).not.toBeChecked();
    expect(screen.queryByText("Old")).not.toBeInTheDocument();

    await user.click(box);
    const rows = screen.getAllByRole("listitem");
    expect(rows.map((r) => r.firstElementChild?.textContent)).toEqual(["Cash", "Old", "OVO"]);
  });

  it("is unticked again after a remount", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("b", "Old", false)] })));
    const user = userEvent.setup();
    const view = renderApp("/pockets");

    await user.click(await screen.findByRole("checkbox"));
    expect(screen.getByText("Old")).toBeInTheDocument();
    view.unmount();
    renderApp("/pockets");

    expect(await screen.findByRole("checkbox")).not.toBeChecked();
    expect(screen.queryByText("Old")).not.toBeInTheDocument();
  });

  it("says so when every Pocket is inactive and the box is unticked", async () => {
    server.use(
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "A", false), pocket("b", "B", false)] })),
    );
    renderApp("/pockets");
    expect(
      await screen.findByText(/No active Pockets\. 2 inactive Pockets hidden\./),
    ).toBeInTheDocument();
    expect(screen.queryByText("No Pockets yet.")).not.toBeInTheDocument();
  });
});
