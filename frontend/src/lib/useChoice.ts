import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { toastError, toastSaved } from "@/lib/plainToast";

// A choice saves the moment it changes, shows the new value at once, and rolls
// back with an error toast if the write fails (docs/agents/ui-conventions.md,
// Settings save by control kind). No form is open, so the failure is a toast.
export function useChoice<T extends string | number>(
  stored: T,
  write: (v: T) => Promise<unknown>,
): [T, (v: T) => void] {
  const { t } = useTranslation();
  const [pending, setPending] = useState<T | null>(null);
  // Only the latest choice may settle the control: with two quick choices, A
  // then B, A's late answer must neither clear B's pending value (a flicker
  // back to the stored one) nor raise a toast about a choice already replaced.
  const latest = useRef(0);
  function choose(v: T) {
    if (v === (pending ?? stored)) return;
    const seq = ++latest.current;
    setPending(v);
    write(v)
      .then(() => {
        if (seq === latest.current) toastSaved();
      })
      .catch((err: unknown) => {
        if (seq !== latest.current) return;
        const e = err instanceof ApiError ? err : new ApiError(0, "UNKNOWN");
        toastError(errorMessage(t, e));
      })
      .finally(() => {
        if (seq === latest.current) setPending(null);
      });
  }
  return [pending ?? stored, choose];
}
