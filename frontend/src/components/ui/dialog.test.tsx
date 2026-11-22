import { useState } from "react";
import { describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";

function Harness({ posts }: { posts?: boolean }) {
  const [open, setOpen] = useState(true);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent closeLabel="Close" posts={posts}>
        <DialogTitle>Title</DialogTitle>
      </DialogContent>
    </Dialog>
  );
}

const tapBackdrop = (user: ReturnType<typeof userEvent.setup>) =>
  user.click(document.querySelector('[data-slot="dialog-overlay"]')!);

// Uruni ADR-046: only a dialog that records money ignores an outside tap.
describe("DialogContent outside tap", () => {
  it("closes a dialog that records no money", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<Harness />);
    await tapBackdrop(user);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("ignores it on a dialog that posts, which Esc still closes", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<Harness posts />);
    await tapBackdrop(user);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});
