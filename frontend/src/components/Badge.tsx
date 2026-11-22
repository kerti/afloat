import type { ReactNode } from "react";

// The one status pill: a word on a soft fill ("Inactive"). Uruni
// web/src/components/Badge.tsx (github.com/kerti/uruni), without its tones:
// Afloat has only the neutral one.
export function Badge({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground">
      {children}
    </span>
  );
}
