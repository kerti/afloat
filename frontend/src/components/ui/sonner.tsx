import type { CSSProperties } from "react";
import { Toaster as Sonner, type ToasterProps } from "sonner";

// The one toast viewport, mounted once at the app root so a toast outlives the
// screen that raised it. Afloat has no theme switch yet (the Shallows palette
// follows the OS), so sonner follows the OS too, and its colours are mapped
// onto theme.css tokens.
export function Toaster(props: ToasterProps) {
  return (
    <Sonner
      theme="system"
      className="toaster group"
      position="bottom-center"
      toastOptions={{
        classNames: { error: "bg-destructive! text-destructive-foreground! border-destructive!" },
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
  );
}
