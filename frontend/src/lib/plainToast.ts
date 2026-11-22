import { toast } from "sonner";
import { i18n } from "@/i18n";
import { tapToDismiss } from "@/lib/tapToDismiss";

// A plain toast lasts 4 s; one with an Undo lasts 8 s (lib/undoToast.ts).
export const PLAIN_TOAST_MS = 4000;

// The one way to say a write went through. Raised from mutateAsync().then,
// never from mutate's per-call onSuccess (TanStack drops those on unmount).
export function toastPlain(message: string) {
  toast(message, { duration: PLAIN_TOAST_MS, ...tapToDismiss() });
}

// A failure with no form open (§ Errors go where the user is looking).
export function toastError(message: string) {
  toast.error(message, tapToDismiss());
}

export function toastSaved() {
  toastPlain(i18n.t("toast.saved"));
}
