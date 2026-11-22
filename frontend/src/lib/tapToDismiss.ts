// Every toast hides when tapped, as Uruni's do (web/src/components/toast/
// useToast.ts). sonner has no tap-to-dismiss of its own: a tap only focuses
// the toast, and a focused toast pauses its timer, so it stayed up. Each toast
// therefore carries an id in its className, and the Toaster
// (components/ui/sonner.tsx) dismisses the toast a tap lands on. A tap on a
// button inside it (Undo) is left to sonner.
let seq = 0;
const PREFIX = "afloat-toast-";

export function tapToDismiss(extraClass?: string) {
  const id = `${PREFIX}${++seq}`;
  return { id, className: extraClass ? `${id} ${extraClass}` : id };
}

// The toast id a tap landed on, or null for a tap on a button or off a toast.
export function tappedToastId(target: EventTarget | null): string | null {
  if (!(target instanceof Element) || target.closest("button")) return null;
  const el = target.closest("[data-sonner-toast]");
  const match = el?.className.match(new RegExp(`${PREFIX}\\d+`));
  return match ? match[0] : null;
}
