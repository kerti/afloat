import type { ComponentProps, ReactNode } from "react";
import { SectionDescription, SectionTitle, SubsectionLabel } from "@/components/Text";
import { cn } from "@/lib/utils";

// Form layout, written once so the spacing steps change in one place
// (docs/agents/ui-conventions.md, Typography and spacing).

/** A form: fields and its submit stacked at the form step. Always noValidate:
 * Afloat renders its own errors. */
export function Form({ className, ...props }: ComponentProps<"form">) {
  return <form noValidate className={cn("flex flex-col gap-4", className)} {...props} />;
}

/** One field: its label, control, hint and error stacked at the field step. */
export function Field({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-2", className)} {...props} />;
}

/** A section of a screen: an optional title (its `id` names the region) and
 * description, then its content at the section step. */
export function Section({
  id,
  title,
  description,
  subsection,
  children,
}: {
  id?: string;
  title?: string;
  description?: string;
  // A stretch inside another section: its title is an h3 SubsectionLabel.
  subsection?: boolean;
  children: ReactNode;
}) {
  const Title = subsection ? SubsectionLabel : SectionTitle;
  const headingId = id && title ? `${id}-heading` : undefined;
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      {title && <Title id={headingId}>{title}</Title>}
      {description && <SectionDescription>{description}</SectionDescription>}
      {children}
    </section>
  );
}
