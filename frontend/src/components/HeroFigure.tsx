import { useId } from "react";
import { heroSize } from "@/lib/heroSize";
import { cn } from "@/lib/utils";

/** The screen's one headline figure: a muted label over the figure in the
 * display role, centred, neutral whatever its sign. A labelled figure, not a
 * live region: a refetch announces nothing. The size steps with the length of
 * the formatted `figure` (`lib/heroSize.ts`) so it never wraps. `className` is
 * for layout only. Confirmed on the phone preview (#155 R18); Uruni
 * `web/src/screens/Home.tsx` hero, 36px bold tabular, stepped by length. */
export function HeroFigure({
  label,
  figure,
  className,
}: {
  label: string;
  figure: string;
  className?: string;
}) {
  const labelId = useId();
  return (
    <section
      aria-labelledby={labelId}
      className={cn("flex flex-col items-center gap-1 py-2 text-center", className)}
    >
      <p id={labelId} className="text-muted-foreground">
        {label}
      </p>
      <p
        className={cn(
          "max-w-full break-all font-bold leading-tight tabular-nums",
          heroSize(figure),
        )}
      >
        {figure}
      </p>
    </section>
  );
}
