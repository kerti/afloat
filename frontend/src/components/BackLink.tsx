import { ArrowLeft } from "@phosphor-icons/react";
import { Link } from "react-router";

// A sub-screen's one way back: a quiet link, top-left above the title, naming
// its destination (Uruni web/src/components/BackLink.tsx, github.com/kerti/uruni).
export function BackLink({ to, label }: { to: string; label: string }) {
  return (
    <Link
      to={to}
      className="-ml-1 inline-flex min-h-11 items-center gap-1.5 self-start rounded-md px-1 text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <ArrowLeft aria-hidden="true" weight="regular" className="size-4 shrink-0" />
      {label}
    </Link>
  );
}
