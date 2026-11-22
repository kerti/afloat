import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
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

describe("the edit dialog's actions", () => {
  it("puts Deactivate and Delete above a Save-only footer, Delete last", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })));
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openEdit(user, "Cash");
    const footer = dialog.querySelector('[data-slot="dialog-footer"]') as HTMLElement;
    expect(
      within(footer)
        .getAllByRole("button")
        .map((b) => b.textContent),
    ).toEqual([p.save]);
    const actions = within(dialog)
      .getAllByRole("button")
      .filter((b) => !footer.contains(b) && b.textContent !== "");
    expect(actions.map((b) => b.textContent)).toEqual([p.deactivate, p.delete]);
    expect(actions[1]).toHaveClass("text-destructive");
    expect(actions[0]!.parentElement).toHaveClass("border-t");
  });

  it("the create dialog keeps Cancel and Add, with no record actions", async () => {
    server.use(...defaultHandlers(signedIn({ pockets: [] })));
    renderApp("/pockets?new=pocket");

    const dialog = await screen.findByRole("dialog", { name: p.addHeading });
    expect(within(dialog).queryByRole("button", { name: p.delete })).not.toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: p.cancel })).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: p.add })).toBeInTheDocument();
  });
});

describe("deleting a Pocket", () => {
  it("is immediate: the dialog closes, the row goes, and one 8 s Undo toast names it", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash"), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    const view = renderApp("/pockets");

    const dialog = await openEdit(user, "OVO");
    await user.click(within(dialog).getByRole("button", { name: p.delete }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(view.param("edit")).toBeNull();
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Edit OVO" })).not.toBeInTheDocument(),
    );
    expect(state.pocketDeletes).toEqual(["b"]);
    expect(toastMock).toHaveBeenCalledTimes(1);
    const [message, options] = toastMock.mock.calls[0]!;
    expect(message).toBe("Deleted OVO.");
    expect(options?.duration).toBe(UNDO_TOAST_MS);
    expect(UNDO_TOAST_MS).toBe(8000);
    expect(options?.action).toMatchObject({ label: "Undo" });
  });

  it("Undo calls restore and the row comes back", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openEdit(user, "Cash");
    await user.click(within(dialog).getByRole("button", { name: p.delete }));
    await screen.findByText(p.empty);
    action(0);

    expect(await screen.findByRole("button", { name: "Edit Cash" })).toBeInTheDocument();
    expect(state.pocketRestores).toEqual(["a"]);
  });

  it("an inactive Pocket offers Reactivate and Delete, and can be deleted", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash", false), pocket("b", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    await user.click(await screen.findByLabelText("Show inactive Pockets (1)"));
    const dialog = await openEdit(user, "Cash", true);
    expect(within(dialog).getByRole("button", { name: p.reactivate })).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: p.delete }));

    await waitFor(() => expect(state.pocketDeletes).toEqual(["a"]));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /Edit Cash/ })).not.toBeInTheDocument(),
    );
    expect(toastMock.mock.calls[0]![0]).toBe("Deleted Cash.");
  });

  it("a failed delete, with the dialog already closed, is one error toast", async () => {
    server.use(
      http.delete("/api/pockets/:id", () =>
        HttpResponse.json(envelope("INTERNAL"), { status: 500 }),
      ),
      ...defaultHandlers(signedIn({ pockets: [pocket("a", "Cash")] })),
    );
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openEdit(user, "Cash");
    await user.click(within(dialog).getByRole("button", { name: p.delete }));

    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toBe(enGB.errors.INTERNAL);
    expect(toastMock).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit Cash" })).toBeInTheDocument();
  });

  it("deleting a Pocket already gone is a NOT_FOUND toast and a refetch", async () => {
    const state = signedIn({ pockets: [pocket("a", "Cash")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openEdit(user, "Cash");
    state.pockets = [];
    await user.click(within(dialog).getByRole("button", { name: p.delete }));

    expect(await screen.findByText(p.empty)).toBeInTheDocument();
    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toBe(enGB.errors.NOT_FOUND);
    expect(toastMock).not.toHaveBeenCalled();
  });

  it("a failed Undo (NAME_TAKEN) raises its own error toast with no Undo", async () => {
    const state = signedIn({ pockets: [pocket("a", "OVO")] });
    server.use(...defaultHandlers(state));
    const user = userEvent.setup();
    renderApp("/pockets");

    const dialog = await openEdit(user, "OVO");
    await user.click(within(dialog).getByRole("button", { name: p.delete }));
    await screen.findByText(p.empty);
    // Someone adds a Pocket under the same name meanwhile.
    state.pockets = [pocket("z", "OVO")];
    action(0);

    await waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(toastMock.error.mock.calls[0]![0]).toBe(enGB.errors.NAME_TAKEN);
    expect(toastMock).toHaveBeenCalledTimes(1);
  });
});
