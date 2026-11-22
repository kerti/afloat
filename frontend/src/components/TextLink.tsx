import type { ComponentProps } from "react";
import { Link } from "react-router";
import { cn } from "@/lib/utils";

// The one body link: small and underlined, standalone or inside a sentence.
// It is drawn small but tapped at 44px (touch-target). BackLink is the
// sub-screen's way home and is its own element.
export function TextLink({ className, ...props }: ComponentProps<typeof Link>) {
  return <Link className={cn("touch-target text-sm underline", className)} {...props} />;
}
