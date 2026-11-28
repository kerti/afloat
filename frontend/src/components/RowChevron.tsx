import { CaretRight } from "@phosphor-icons/react";

/** The chevron that ends a row which takes you to another screen. A row that
 * opens a dialog carries none, so the arrow keeps meaning "this goes
 * somewhere". One size and one ink everywhere. Uruni `components/RowChevron.tsx`. */
export function RowChevron() {
  return (
    <CaretRight
      aria-hidden="true"
      weight="regular"
      className="size-4 shrink-0 text-muted-foreground"
    />
  );
}
