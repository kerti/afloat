import { toast } from "sonner";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { alice, defaultHandlers, server, type State } from "@/test/server";
import { renderApp } from "@/test/render";
import enGB from "@/locales/en-GB.json";

// The real <Toaster> and the real sonner, no mock: the viewport is mounted at
// the app root, so the toast outlives the page that raised it.
const state = (): State => ({
  signedIn: true,
  me: alice,
  pockets: [{ id: "a", pocket_display_name: "Cash", active: true }],
});

afterEach(() => {
  vi.useRealTimers();
  // sonner's toast store is module state: a toast left by one test is on screen in the next.
  toast.dismiss();
});

async function deactivateCash(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: "Edit Cash" }));
  const dialog = await screen.findByRole("dialog", { name: enGB.pockets.editHeading });
  await user.click(within(dialog).getByRole("button", { name: enGB.pockets.deactivate }));
}

describe("the Undo toast, for real", () => {
  it("survives navigating away from /settings, and its Undo still works there", async () => {
    const s = state();
    server.use(...defaultHandlers(s));
    const user = userEvent.setup();
    renderApp("/settings");

    await deactivateCash(user);
    expect(await screen.findByText("Deactivated Cash.")).toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "Home" }));
    expect(await screen.findByRole("link", { name: "Settings" })).toBeInTheDocument();
    expect(screen.getByText("Deactivated Cash.")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Undo" }));
    await waitFor(() => expect(s.pocketPatches).toEqual([{ active: false }, { active: true }]));
  });

  it("is gone after 8 s", async () => {
    server.use(...defaultHandlers(state()));
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    renderApp("/settings");

    await deactivateCash(user);
    expect(await screen.findByText("Deactivated Cash.")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(7000);
    });
    expect(screen.getByText("Deactivated Cash.")).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    await waitFor(() => expect(screen.queryByText("Deactivated Cash.")).not.toBeInTheDocument());
  });
});
