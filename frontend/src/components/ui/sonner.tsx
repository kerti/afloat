import type { CSSProperties } from "react";
import { Toaster as Sonner, toast, type ToasterProps } from "sonner";
import { useAppearance } from "@/lib/useAppearance";
import { tappedToastId } from "@/lib/tapToDismiss";

// The one toast viewport, mounted once at the app root so a toast outlives the
// screen that raised it. Sonner follows the person's appearance, and its colours
// are mapped onto theme.css tokens.
export function Toaster(props: ToasterProps) {
  const { appearance } = useAppearance();
  // display: contents, so the wrapper adds no box; it only hears the tap that
  // bubbles up from a toast (lib/tapToDismiss.ts).
  return (
    <div
      className="contents"
      onClick={(e) => {
        const id = tappedToastId(e.target);
        if (id) toast.dismiss(id);
      }}
    >
      <Sonner
        theme={appearance}
        className="toaster group"
        position="bottom-center"
        toastOptions={{
          classNames: { error: "bg-card! text-attention! border-attention!" },
        }}
        style={
          {
            "--normal-bg": "var(--card)",
            "--normal-text": "var(--card-foreground)",
            "--normal-border": "var(--border)",
          } as CSSProperties
        }
        {...props}
      />
    </div>
  );
}
