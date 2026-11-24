import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** A one-time notice in the page flow: the card surface with a 44px dismiss
 * at its top right and its children in the body. Carried by router state, never
 * stored. `className` is for layout only. (Uruni `cardClasses.ts` card; ruled
 * 24 Nov 2026, #140 R2.) */
export function NoticeCard({
  dismissLabel,
  onDismiss,
  className,
  children,
}: {
  dismissLabel: string;
  onDismiss: () => void;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section
      className={cn(
        "relative flex flex-col gap-2 rounded-lg border bg-card px-4 py-3 pr-12 text-sm text-card-foreground",
        className,
      )}
    >
      <Button
        type="button"
        variant="ghost"
        aria-label={dismissLabel}
        onClick={onDismiss}
        className="absolute right-0 top-0 w-11 rounded-full px-0 text-muted-foreground"
      >
        <svg
          aria-hidden="true"
          viewBox="0 0 24 24"
          className="size-4"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
        >
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </Button>
      {children}
    </section>
  );
}
