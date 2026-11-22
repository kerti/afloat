import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Toaster } from "@/components/ui/sonner";
import { toastError, toastPlain } from "@/lib/plainToast";
import { toastUndoable } from "@/lib/undoToast";

// The real sonner and the real <Toaster>: a tap on a toast hides it, as
// Uruni's do, instead of focusing it and pausing its timer.
afterEach(() => {
  act(() => {
    toast.dismiss();
  });
});

describe("tap to dismiss", () => {
  it("hides a plain toast when it is tapped", async () => {
    const user = userEvent.setup();
    render(<Toaster />);
    act(() => toastPlain("Saved."));
    await user.click(await screen.findByText("Saved."));
    await waitFor(() => expect(screen.queryByText("Saved.")).not.toBeInTheDocument());
  });

  it("hides an error toast when it is tapped", async () => {
    const user = userEvent.setup();
    render(<Toaster />);
    act(() => toastError("Afloat couldn't complete that."));
    await user.click(await screen.findByText("Afloat couldn't complete that."));
    await waitFor(() =>
      expect(screen.queryByText("Afloat couldn't complete that.")).not.toBeInTheDocument(),
    );
  });

  it("hides only the toast tapped, not one beside it", async () => {
    const user = userEvent.setup();
    render(<Toaster expand />);
    act(() => {
      toastPlain("First.");
      toastPlain("Second.");
    });
    await user.click(await screen.findByText("Second."));
    await waitFor(() => expect(screen.queryByText("Second.")).not.toBeInTheDocument());
    expect(screen.getByText("First.")).toBeInTheDocument();
  });

  it("leaves a tap on Undo to the action, which runs", async () => {
    const user = userEvent.setup();
    const undo = vi.fn(() => Promise.resolve());
    render(<Toaster />);
    act(() =>
      toastUndoable({
        message: "Deactivated OVO.",
        undo,
        qc: new QueryClient(),
        invalidate: ["pockets"],
      }),
    );
    await user.click(await screen.findByRole("button", { name: "Undo" }));
    expect(undo).toHaveBeenCalledTimes(1);
  });
});
