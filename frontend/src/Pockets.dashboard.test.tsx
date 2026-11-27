import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { toast } from "sonner";
import { dashboardKey } from "@/api/queries";
import { alice, defaultHandlers, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());

// Home's By Pocket rows carry a Pocket's name and its Inactive flag, so every
// way a Pocket changes must mark the dashboard stale (#155 PR 7). The hooks'
// own are in Dashboard.test.tsx; these are the Undos, which call the client
// directly and so must invalidate for themselves, as the Category Undos do.

const pocket = (id: string, name: string, active = true) => ({
  id,
  pocket_display_name: name,
  active,
});
const state = (): State => ({
  signedIn: true,
  me: alice,
  pockets: [pocket("a", "Cash"), pocket("b", "OVO")],
});
const toastMock = vi.mocked(toast);
const p = enGB.pockets;
const action = (call: number) =>
  (toastMock.mock.calls[call]![1]!.action as unknown as { onClick: () => void }).onClick();

beforeEach(() => {
  vi.clearAllMocks();
});

async function openEdit(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(await screen.findByRole("button", { name: `Edit ${name}` }));
  return screen.findByRole("dialog", { name: p.editHeading });
}

describe("a Pocket's Undo refetches the dashboard", () => {
  it("the Undo of a deactivate does", async () => {
    server.use(...defaultHandlers(state()));
    const user = userEvent.setup();
    const { client } = renderApp("/settings");
    const dialog = await openEdit(user, "OVO");
    await user.click(within(dialog).getByRole("button", { name: p.deactivate }));
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    // A fresh entry, so only the Undo can mark it stale.
    client.setQueryData(dashboardKey, { currency: "IDR" });

    action(0);

    await waitFor(() => expect(client.getQueryState(dashboardKey)?.isInvalidated).toBe(true));
  });

  it("the Undo of a delete does", async () => {
    server.use(...defaultHandlers(state()));
    const user = userEvent.setup();
    const { client } = renderApp("/settings");
    const dialog = await openEdit(user, "OVO");
    await user.click(within(dialog).getByRole("button", { name: p.delete }));
    await waitFor(() => expect(toastMock).toHaveBeenCalledTimes(1));
    client.setQueryData(dashboardKey, { currency: "IDR" });

    action(0);

    await waitFor(() => expect(client.getQueryState(dashboardKey)?.isInvalidated).toBe(true));
  });
});

describe("a Pocket's own writes refetch the dashboard", () => {
  it("a deactivate marks it stale, before any Undo", async () => {
    server.use(...defaultHandlers(state()));
    const user = userEvent.setup();
    const { client } = renderApp("/settings");
    const dialog = await openEdit(user, "OVO");
    client.setQueryData(dashboardKey, { currency: "IDR" });

    await user.click(within(dialog).getByRole("button", { name: p.deactivate }));

    await waitFor(() => expect(client.getQueryState(dashboardKey)?.isInvalidated).toBe(true));
  });
});
