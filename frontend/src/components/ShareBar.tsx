// How much of an allocation has gone, drawn under a FigureRow. The track runs
// to 125%, with a tick at 100%, so going over has room to show; past 125% the
// fill stops at the end. The share past 100% wears --attention. Ruled by the
// maintainer 28 Nov 2026 (polish pass), from Balances' DashboardScreen bar.
// Drawing only: the width is read from the two wire strings for the picture,
// never shown or kept as a figure (N5a), and the figures beside it say it all,
// so the bar is hidden from assistive technology.
const TRACK = 1.25;

export function ShareBar({ spent, allocation }: { spent: string; allocation: string }) {
  const of = Number(allocation);
  if (!(of > 0)) return null;
  const share = Math.min(Math.max(Number(spent) / of, 0), TRACK);
  const pct = (n: number) => `${(n / TRACK) * 100}%`;
  return (
    <div aria-hidden="true" data-testid="share-bar" className="relative h-2 rounded-full bg-muted">
      <div
        data-testid="share-bar-fill"
        className="absolute inset-y-0 left-0 rounded-full bg-primary"
        style={{ width: pct(Math.min(share, 1)) }}
      />
      {share > 1 && (
        <div
          data-testid="share-bar-over"
          className="absolute inset-y-0 rounded-r-full bg-attention"
          style={{ left: pct(1), width: pct(share - 1) }}
        />
      )}
      <div className="absolute -inset-y-0.5 w-px bg-foreground/60" style={{ left: pct(1) }} />
    </div>
  );
}
