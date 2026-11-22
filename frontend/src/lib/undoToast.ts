import type { CSSProperties } from "react";
import { toast } from "sonner";
import type { QueryClient } from "@tanstack/react-query";
import { i18n } from "@/i18n";
import { ApiError } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { toastError } from "@/lib/plainToast";
import { tapToDismiss } from "@/lib/tapToDismiss";

// An undoable act raises one toast with an Undo action. sonner pauses the timer
// while the pointer is over it (R3).
export const UNDO_TOAST_MS = 8000;

type Options = {
  message: string;
  // Reverses the act. Runs from the toast, so it must not depend on the screen
  // that raised it still being mounted.
  undo: () => Promise<unknown>;
  qc: QueryClient;
  invalidate: readonly unknown[];
};

// A failed Undo (a name taken meanwhile, no network) is its own toast with no
// Undo; a successful one needs no words, the row is back.
export function toastUndoable({ message, undo, qc, invalidate }: Options) {
  toast(message, {
    duration: UNDO_TOAST_MS,
    // The countdown bar (index.css) runs for exactly the toast's duration.
    ...tapToDismiss("undo-countdown"),
    style: { "--undo-countdown-ms": `${UNDO_TOAST_MS}ms` } as CSSProperties,
    action: {
      label: i18n.t("toast.undo"),
      onClick: () => {
        undo()
          .then(() => qc.invalidateQueries({ queryKey: invalidate }))
          .catch((err: unknown) => {
            const e = err instanceof ApiError ? err : new ApiError(0, "UNKNOWN");
            toastError(errorMessage(i18n.t.bind(i18n), e));
            // NOT_FOUND means the list is stale.
            if (e.code === "NOT_FOUND") void qc.invalidateQueries({ queryKey: invalidate });
          });
      },
    },
  });
}
