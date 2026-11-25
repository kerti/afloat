import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { toast } from "sonner";
import { alice, defaultHandlers, envelope, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import { UNDO_TOAST_MS } from "@/lib/undoToast";
import enGB from "@/locales/en-GB.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());

const pocket = (id: string, name: string, active = true) => ({
  id,
  pocket_display_name: name,
  active,
});
const signedIn = (extra: Partial<State> = {}): State => ({ signedIn: true, me: alice, ...extra });
const toastMock = vi.mocked(toast);
const p = enGB.pockets;
const nameField = (dialog: HTMLElement) =>
  within(dialog).getByLabelText(enGB.fields.pocketDisplayName);
const action = (call: number) =>
  (toastMock.mock.calls[call]![1]!.action as unknown as { onClick: () => void }).onClick();

async function openEdit(user: ReturnType<typeof userEvent.setup>, name: string, inactive = false) {
  const label = inactive ? `Edit ${name}, inactive` : `Edit ${name}`;
  await user.click(await screen.findByRole("button", { name: label }));
  return screen.findByRole("dialog", { name: p.editHeading });
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("the edit dialog", () => {
  it("opens when a row is tapped, sets ?edit=pocket:<id>, and holds the Pocket's name", async () => {
    server.use(
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] })),
    );
    const user = userEvent.setup();
    const view = renderApp("/settings");

    const dialog = await openEdit(user, "OVO");

    expect(view.param("edit")).toBe("pocket:b");
    expect(nameField(dialog)).toHaveValue("OVO");
    expect(within(dialog).getByRole("button", { name: p.deactivate })).toBeInTheDocument();
  });

  it("opens from a link, and a link to a Pocket that is not there drops the param", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })));
    const view = renderApp("/settings?edit=pocket:a");
    expect(await screen.findByRole("dialog", { name: p.editHeading })).toBeInTheDocument();
    view.unmount();

    const stale = renderApp("/settings?edit=pocket:zzz");
    await screen.findByRole("button", { name: "Edit Cash" });
    await waitFor(() => expect(stale.param("edit")).toBeNull());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("closes by a backdrop tap, back, Esc and the X, clearing the param", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })));
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const view = renderApp("/settings");

    await openEdit(user, "Cash");
    await user.click(document.querySelector('[data-slot="dialog-overlay"]')!);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("edit")).toBeNull();

    await openEdit(user, "Cash");

    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("edit")).toBeNull();

    await openEdit(user, "Cash");
    act(() => view.navigate(-1));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("edit")).toBeNull();

    const again = await openEdit(user, "Cash");
    await user.click(within(again).getByRole("button", { name: enGB.dialog.close }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("edit")).toBeNull();
  });
});

describe("renaming a Pocket", () => {
  it("sends only the changed name, closes the dialog and raises exactly one toast", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    const save = within(dialog).getByRole("button", { name: p.save });
    expect(save).toBeDisabled();
    const field = nameField(dialog);
    await user.clear(field);
    await user.type(field, "Wallet");
    expect(save).toBeEnabled();
    await user.click(save);

    expect(await screen.findByRole("button", { name: "Edit Wallet" })).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(state.pocketPatches).toEqual([{ pocket_display_name: "Wallet" }]);
    expect(toastMock).toHaveBeenCalledTimes(1);
    expect(toastMock.mock.calls[0]![0]).toBe("Saved.");
    expect(toastMock.mock.calls[0]![1]).toMatchObject({ duration: 4000 });
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("disables Save again when the name is typed back to the original", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    const save = within(dialog).getByRole("button", { name: p.save });
    await user.type(nameField(dialog), "x");
    expect(save).toBeEnabled();
    await user.type(nameField(dialog), "{Backspace}");
    expect(save).toBeDisabled();
    expect(state.pocketPatches).toBeUndefined();
  });

  it("leaves Save enabled when the name is blanked: submitting shows the error and sends nothing", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    await user.clear(nameField(dialog));
    const save = within(dialog).getByRole("button", { name: p.save });
    expect(save).toBeEnabled();
    await user.click(save);

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(p.nameRequired);
    expect(state.pocketPatches).toBeUndefined();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("keeps NAME_TAKEN under the field with the dialog open and no toast", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    const field = nameField(dialog);
    await user.clear(field);
    await user.type(field, "ovo");
    await user.click(within(dialog).getByRole("button", { name: p.save }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(enGB.errors.NAME_TAKEN);
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveValue("ovo");
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("shows another failure above the footer, and keeps the dialog open", async () => {
    server.use(
      http.patch("/api/pockets/:id", () =>
        HttpResponse.json(envelope("INTERNAL"), { status: 500 }),
      ),
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })),
    );
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    await user.type(nameField(dialog), "x");
    await user.click(within(dialog).getByRole("button", { name: p.save }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(enGB.errors.INTERNAL);
    expect(nameField(dialog)).not.toHaveAttribute("aria-invalid");
    expect(dialog).toBeInTheDocument();
  });

  it("says Saving… and disables the field while in flight", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    server.use(
      http.patch("/api/pockets/:id", async () => {
        await gate;
        return undefined;
      }),
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })),
    );
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    await user.type(nameField(dialog), "x");
    await user.click(within(dialog).getByRole("button", { name: p.save }));

    expect(await within(dialog).findByRole("button", { name: p.saving })).toBeDisabled();
    expect(nameField(dialog)).toBeDisabled();
    release();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("on NOT_FOUND refetches the list, which drops the Pocket and the dialog", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    await user.type(nameField(dialog), "x");
    // Another member deletes it meanwhile.
    state.pockets = [];
    await user.click(within(dialog).getByRole("button", { name: p.save }));

    expect(await screen.findByText(p.empty)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("edit")).toBeNull();
  });
});

describe("deactivating and reactivating, from the dialog", () => {
  it("deactivates at once: the dialog closes, the row hides, and one 8 s Undo toast names it", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    await user.click(within(dialog).getByRole("button", { name: p.deactivate }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("edit")).toBeNull();
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Edit Cash" })).not.toBeInTheDocument(),
    );
    expect(state.pocketPatches).toEqual([{ active: false }]);
    expect(toastMock).toHaveBeenCalledTimes(1);
    const [message, options] = toastMock.mock.calls[0]!;
    expect(message).toBe("Deactivated Cash.");
    expect(options?.duration).toBe(UNDO_TOAST_MS);
    expect(UNDO_TOAST_MS).toBe(8000);
    expect(options?.action).toMatchObject({ label: "Undo" });
  });

  it("Undo sends active:true and the row comes back", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    await user.click(within(dialog).getByRole("button", { name: p.deactivate }));
    await screen.findByText("No active Pockets. 1 inactive Pocket hidden.", { exact: false });
    action(0);

    expect(await screen.findByRole("button", { name: "Edit Cash" })).toBeInTheDocument();
    expect(state.pocketPatches).toEqual([{ active: false }, { active: true }]);
  });

  it("a failed Undo raises its own error toast with no Undo", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    await user.click(within(dialog).getByRole("button", { name: p.deactivate }));
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

  it("a failed deactivation stays in the dialog as a form error, with no toast", async () => {
    server.use(
      http.patch("/api/pockets/:id", () =>
        HttpResponse.json(envelope("INTERNAL"), { status: 500 }),
      ),
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })),
    );
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    await user.click(within(dialog).getByRole("button", { name: p.deactivate }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(enGB.errors.INTERNAL);
    expect(toastMock.error).not.toHaveBeenCalled();
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("shows NOT_FOUND from a deactivation as a toast and refetches", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    const dialog = await openEdit(user, "Cash");
    state.pockets = [];
    await user.click(within(dialog).getByRole("button", { name: p.deactivate }));

    expect(await screen.findByText(p.empty)).toBeInTheDocument();
    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toBe(enGB.errors.NOT_FOUND);
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("an inactive Pocket offers Reactivate, which sends active:true with one 8 s Undo toast", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash", false), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    await user.click(await screen.findByLabelText("Show inactive Pockets (1)"));
    // The row's accessible name carries the Inactive badge too.
    const row = screen.getByRole("button", { name: "Edit Cash, inactive" });
    expect(within(row).getByText(p.inactive)).toBeInTheDocument();
    const dialog = await openEdit(user, "Cash", true);
    expect(within(dialog).queryByRole("button", { name: p.deactivate })).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: p.reactivate }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    await waitFor(() =>
      expect(
        within(screen.getByRole("button", { name: "Edit Cash" })).queryByText(p.inactive),
      ).not.toBeInTheDocument(),
    );
    expect(state.pocketPatches).toEqual([{ active: true }]);
    expect(toastMock).toHaveBeenCalledTimes(1);
    const [message, options] = toastMock.mock.calls[0]!;
    expect(message).toBe("Reactivated Cash.");
    expect(options?.duration).toBe(UNDO_TOAST_MS);
    expect(options?.action).toMatchObject({ label: "Undo" });
  });

  it("Undo after reactivating sends active:false and the row is inactive again", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash", false), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    await user.click(await screen.findByLabelText("Show inactive Pockets (1)"));
    const dialog = await openEdit(user, "Cash", true);
    await user.click(within(dialog).getByRole("button", { name: p.reactivate }));
    await waitFor(() => expect(state.pocketPatches).toEqual([{ active: true }]));
    action(0);

    await waitFor(() => expect(state.pocketPatches).toEqual([{ active: true }, { active: false }]));
    await waitFor(() =>
      expect(
        within(screen.getByRole("button", { name: "Edit Cash, inactive" })).getByText(p.inactive),
      ).toBeInTheDocument(),
    );
  });

  it("with the box ticked, deactivating mutes the row in place", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO", false)] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/settings");

    await user.click(await screen.findByLabelText("Show inactive Pockets (1)"));
    const dialog = await openEdit(user, "Cash");
    await user.click(within(dialog).getByRole("button", { name: p.deactivate }));

    // The dialog waits for the answer, so the refetched row is already muted.
    const row = await screen.findByRole("button", { name: "Edit Cash, inactive" });
    expect(await within(row).findByText(p.inactive)).toBeInTheDocument();
    expect(screen.getByLabelText("Show inactive Pockets (2)")).toBeChecked();
  });
});

describe("Show inactive Pockets", () => {
  it("is absent when nothing is inactive", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })));
    renderApp("/settings");
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
    renderApp("/settings");

    const box = await screen.findByRole("checkbox", { name: "Show inactive Pockets (2)" });
    expect(box).not.toBeChecked();
    expect(screen.queryByText("Old")).not.toBeInTheDocument();

    await user.click(box);
    const rows = screen.getAllByRole("listitem");
    expect(rows.map((r) => r.firstElementChild?.firstElementChild?.textContent)).toEqual([
      "Cash",
      "Old",
      "OVO",
    ]);
  });

  it("is unticked again after a remount", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("b", "Old", false)] })));
    const user = userEvent.setup();
    const view = renderApp("/settings");

    await user.click(await screen.findByRole("checkbox"));
    expect(screen.getByText("Old")).toBeInTheDocument();
    view.unmount();
    renderApp("/settings");

    expect(await screen.findByRole("checkbox")).not.toBeChecked();
    expect(screen.queryByText("Old")).not.toBeInTheDocument();
  });

  it("says so when every Pocket is inactive and the box is unticked", async () => {
    server.use(
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "A", false), pocket("b", "B", false)] })),
    );
    renderApp("/settings");
    expect(
      await screen.findByText(/No active Pockets\. 2 inactive Pockets hidden\./),
    ).toBeInTheDocument();
    expect(screen.queryByText(p.empty)).not.toBeInTheDocument();
  });
});
