import { cn } from "@/lib/utils";

/** Up to two labelled figures side by side in one bordered box, each a small
 * muted label over a medium tabular amount: Today's allowance beside the
 * Baseline, or the allowance alone. A figure wide enough to crowd its half
 * sends both to their own lines, never a clipped or wrapped number. Neutral
 * whatever the sign. `className` is for layout only. Confirmed on the
 * phone preview (#155 R18); Uruni `web/src/screens/Home.tsx` cash-flow card
 * (a two-column `dl`, one column when either figure is wide) and
 * `components/Figures.tsx` (grey label, tabular medium amount). */
export function FigurePair({
  figures,
  className,
}: {
  figures: readonly { label: string; value: string }[];
  className?: string;
}) {
  const sideBySide = figures.length === 2 && figures.every((f) => f.value.length <= 15);
  return (
    <dl
      className={cn(
        "grid gap-3 rounded-md border px-3 py-2",
        sideBySide ? "grid-cols-2" : "grid-cols-1",
        className,
      )}
    >
      {figures.map((f) => (
        <div key={f.label} className="flex flex-col">
          <dt className="text-sm text-muted-foreground">{f.label}</dt>
          <dd className="font-medium tabular-nums">{f.value}</dd>
        </div>
      ))}
    </dl>
  );
}
