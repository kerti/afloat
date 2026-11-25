import * as React from "react";
import { X } from "@phosphor-icons/react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useKeyboardInset } from "@/lib/useKeyboardInset";
import { cn } from "@/lib/utils";

// The dialog primitive, after Uruni web/src/components/ui/dialog.tsx
// (github.com/kerti/uruni): a centred modal inset 1rem from each side, a
// header, the body, and a muted footer strip of two half-width buttons. Radix
// owns the focus trap, focus restore and background inerting; this file only
// styles it. The caller controls `open` from a search param (useDialogParam).
const Dialog = DialogPrimitive.Root;

// Focuses the first field the way Radix would, minus the select-all: a dialog
// that opens on a pre-filled field must not leave its value one keystroke from
// gone (Uruni dialog.tsx).
const FOCUSABLE =
  'input:not([type="hidden"]):not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])';

function focusFirstFieldWithoutSelecting(event: Event) {
  const content = event.currentTarget;
  if (!(content instanceof HTMLElement)) return;
  const first = content.querySelector<HTMLElement>(FOCUSABLE);
  if (first === null) return;
  event.preventDefault();
  first.focus({ preventScroll: true });
  if (first instanceof HTMLInputElement && typeof first.selectionStart === "number") {
    const end = first.value.length;
    first.setSelectionRange(end, end);
  }
}

type DialogContentProps = React.ComponentProps<typeof DialogPrimitive.Content> & {
  // Translated by the caller, so this file carries no copy of its own.
  closeLabel: string;
  // The dialog records money (Uruni ADR-046): a tap outside does nothing, so a
  // stray backdrop touch cannot dismiss it. The X, Esc and its own buttons
  // still close it. Every other dialog leaves this off and closes on an
  // outside tap.
  posts?: boolean;
};

function DialogContent({
  className,
  children,
  closeLabel,
  posts = false,
  style,
  onOpenAutoFocus,
  onInteractOutside,
  ...props
}: DialogContentProps) {
  const { inset, height } = useKeyboardInset();
  // The dialog centres itself in what the keyboard leaves visible.
  const keyboardStyle: React.CSSProperties | undefined =
    inset > 0
      ? { top: window.innerHeight - inset - height / 2, maxHeight: height - 32 }
      : undefined;

  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay
        data-slot="dialog-overlay"
        className="fixed inset-0 isolate z-50 animate-overlay-in bg-scrim supports-backdrop-filter:backdrop-blur-xs"
      />
      <DialogPrimitive.Content
        data-slot="dialog-content"
        // The title says it all; opt out of Radix's describedby warning.
        aria-describedby={undefined}
        className={cn(
          "fixed top-1/2 left-1/2 z-50 flex max-h-[90dvh] w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 animate-dialog-in flex-col overflow-hidden rounded-lg bg-card text-card-foreground ring-1 ring-border outline-none sm:max-w-sm",
          className,
        )}
        style={keyboardStyle ? { ...style, ...keyboardStyle } : style}
        onInteractOutside={(event) => {
          onInteractOutside?.(event);
          if (posts) event.preventDefault();
        }}
        onOpenAutoFocus={(event) => {
          onOpenAutoFocus?.(event);
          if (event.defaultPrevented) return;
          focusFirstFieldWithoutSelecting(event);
        }}
        {...props}
      >
        <div data-slot="dialog-body" className="grid min-h-0 grid-cols-1 gap-4 overflow-y-auto p-4">
          {children}
        </div>
        <DialogPrimitive.Close
          aria-label={closeLabel}
          className="absolute top-1 right-1 z-20 flex size-11 items-center justify-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <X aria-hidden="true" weight="regular" className="size-5" />
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

// Right padding keeps a long title clear of the close button.
function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex min-h-11 flex-col justify-center gap-1 pr-10", className)}
      {...props}
    />
  );
}

function DialogTitle({ className, ...props }: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn("text-lg font-semibold", className)}
      {...props}
    />
  );
}

// The button strip, bleeding to the body's edges (hence the negative margins
// matching its p-4), so it is the last thing in the dialog. Children go in
// reading order, secondary first and primary last, splitting the row equally.
function DialogFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn(
        "-mx-4 -mb-4 flex gap-2 border-t border-border bg-muted/50 p-4 *:min-w-0 *:flex-1 *:basis-0",
        className,
      )}
      {...props}
    />
  );
}

export { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle };
