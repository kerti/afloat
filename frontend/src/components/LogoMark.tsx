import { cn } from "@/lib/utils";

// The app's mark, drawn from the one file the favicon and the PWA manifest use
// (public/icon.svg), so a new mark changes all three at once. A placeholder
// until the brand session (PRD §9). Decorative: callers give it no name.
export function LogoMark({ className }: { className?: string }) {
  return <img src="/icon.svg" alt="" className={cn("shrink-0 rounded-md", className)} />;
}
