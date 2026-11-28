import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

// Every piece of text a screen renders is one of these (docs/agents/
// ui-conventions.md, Catalogue). Pages never set their own type size, weight or
// colour; changing a role here changes it everywhere. One component per role,
// as Uruni does (web/src/components/SectionLabel.tsx, SubsectionLabel.tsx,
// github.com/kerti/uruni). `className` is for layout only.

/** The screen title: the one h1 on every screen. `visuallyHidden` keeps it for
 * screen readers on a screen whose tab already names it (Home). */
export function PageTitle({
  className,
  visuallyHidden,
  ...props
}: ComponentProps<"h1"> & { visuallyHidden?: boolean }) {
  return (
    <h1
      className={cn(visuallyHidden ? "sr-only" : "text-xl font-semibold", className)}
      {...props}
    />
  );
}

/** The title that opens a section of a screen. */
export function SectionTitle({ className, ...props }: ComponentProps<"h2">) {
  return <h2 className={cn("text-lg font-semibold", className)} {...props} />;
}

/** The h3 that labels a stretch inside a section: one step under SectionTitle.
 * (Uruni SubsectionLabel; ruled 24 Nov 2026, #140 PR 1.) */
export function SubsectionLabel({ className, ...props }: ComponentProps<"h3">) {
  return <h3 className={cn("text-sm font-semibold text-muted-foreground", className)} {...props} />;
}

/** The sentence under a section title that says what the section is for. */
export function SectionDescription({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn("text-sm text-muted-foreground", className)} {...props} />;
}

/** The sentence under a field that says what it takes. */
export function FieldHint({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn("text-sm text-muted-foreground", className)} {...props} />;
}

/** A failure, under its field or above a form's footer: attention-coloured,
 * never red, and announced. */
export function FieldError({ className, ...props }: ComponentProps<"p">) {
  return <p role="alert" className={cn("text-sm text-attention", className)} {...props} />;
}

/** A plain sentence at body size. */
export function BodyText({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn(className)} {...props} />;
}

/** A plain sentence at body size, muted: an intro, a status message, a note. */
export function MutedText({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn("text-muted-foreground", className)} {...props} />;
}

/** The sentence a list shows when it has nothing, naming what will appear. */
export function EmptyState({ className, ...props }: ComponentProps<"p">) {
  return <p className={cn("text-muted-foreground", className)} {...props} />;
}
