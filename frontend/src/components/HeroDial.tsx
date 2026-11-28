import { useId, useLayoutEffect, useRef } from "react";
import { heroSize } from "@/lib/heroSize";
import { cn } from "@/lib/utils";

export type DialState = "afloat" | "drifting" | "taking_on_water";

/** The ring round the disc: the Budget Period as a bezel, one tick a day. The
 * thin outer arc is the Period gone by (the today marker at its end), the thick
 * inner arc is how much of the Daily Pool is spent, and the part past all of it
 * laps the ring again in --attention, as a ShareBar does past 100%. */
export type DialRing = {
  days: number;
  daysRemaining: number;
  /** spent ÷ pool, for drawing only; null when there is no pool to read against */
  spent: number | null;
  daysLeftLabel: string;
  spentLabel: string | null;
};

// The sea holds the bottom third of the disc whatever the State, so the top
// two thirds are the figure's. The State moves the boat, in three fixed steps,
// never a slider: a step is the State's and no figure's, so the picture cannot
// become a gauge (D9). `waterline` is where the sea meets the boat, in the
// boat's own units (its deck at -8, its cabin roof at -24): under the deck when
// Afloat, nearly over it when Drifting, and only the cabin and mast above it
// when Taking on water. `chop` is the wave height.
const SEA: Record<DialState, { chop: number; waterline: number }> = {
  afloat: { chop: 2, waterline: 0 },
  drifting: { chop: 3.5, waterline: -6 },
  taking_on_water: { chop: 5, waterline: -11 },
};

// Geometry in a 300-unit square.
const C = 150;
const DISC = 108;
const TICKS_OUT = 138;
const TIME_R = 130;
const SPENT_R = 119;
// The sea's surface at rest: the bottom third of the disc.
const SEA_TOP = C + DISC - (2 * DISC) / 3;

const rad = (deg: number) => (deg * Math.PI) / 180;
const at = (r: number, deg: number) =>
  `${C + r * Math.cos(rad(deg))},${C + r * Math.sin(rad(deg))}`;
// A share of the ring as an angle, clockwise from the top.
const angleOf = (share: number) => -90 + 360 * share;

// How long the sea takes to settle as Home opens, then it is still, so nothing
// moves behind the figure while it is read.
const SETTLE_MS = 3200;

// A swell is two sines of different lengths; each layer's pair runs at its own
// speeds, so the surface changes shape as it moves instead of sliding whole.
// `run` is how far each sine travels, in wavelengths, while the sea settles.
const SWELLS = {
  front: { lengths: [48, 29], weights: [1, 0.35], run: [1.1, 1.8] },
  back: { lengths: [62, 37], weights: [0.8, 0.3], run: [-0.8, -1.3] },
} as const;

// The surface at `p` (0 as Home opens, 1 settled): it arrives a little high
// and rough, bobs down to its level and calms; the waves slow to a stop.
type Swell = (typeof SWELLS)[keyof typeof SWELLS];

function heightAt(swell: Swell, y: number, chop: number, p: number, x: number): number {
  const left = 1 - (1 - p) ** 3;
  const amp = chop * (1 + 0.8 * (1 - left));
  const lift = -5 * Math.exp(-3.5 * p) * Math.cos(2.5 * Math.PI * p);
  let h = 0;
  for (let i = 0; i < 2; i++) {
    const phase = 2 * Math.PI * (x / swell.lengths[i]! - swell.run[i]! * (1 - left));
    h += swell.weights[i]! * Math.sin(phase);
  }
  return y + lift + amp * h;
}

function surface(swell: Swell, y: number, chop: number, p: number): string {
  let d = "";
  for (let x = 38; x <= 262; x += 2) {
    d += `${d ? " L" : "M"}${x} ${heightAt(swell, y, chop, p, x).toFixed(2)}`;
  }
  return d;
}

// The boat rides the front swell at the disc's centre: it sits at the
// surface's height there, its waterline on it, and leans with the surface's
// slope, eased so it rocks rather than tips.
const BOAT_SCALE = 1.25;

function boatAt(y: number, chop: number, waterline: number, p: number): string {
  const h = heightAt(SWELLS.front, y, chop, p, C);
  const slope =
    (heightAt(SWELLS.front, y, chop, p, C + 12) - heightAt(SWELLS.front, y, chop, p, C - 12)) / 24;
  const lean = ((Math.atan(slope) * 180) / Math.PI) * 0.35;
  const w = waterline * BOAT_SCALE;
  return `translate(${C} ${(h - w).toFixed(2)}) rotate(${lean.toFixed(2)} 0 ${w.toFixed(2)}) scale(${BOAT_SCALE})`;
}

/** A small motor boat side on, bow to the right, its waterline at y = 0 when
 * light: the hull in --marker, the cabin and mast in --foreground, the windows
 * the sky. Drawing only. */
function Boat() {
  return (
    <>
      <line x1={-3} y1={-24} x2={-3} y2={-30} stroke="var(--foreground)" strokeWidth={1.6} />
      <path d="M-3 -30 L5.5 -27.75 L-3 -25.5 Z" fill="var(--marker)" />
      <path d="M-16 -8 L-16 -21 Q-16 -24 -13 -24 L5 -24 L12 -8 Z" fill="var(--foreground)" />
      <path
        d="M-12 -20 L-6 -20 L-6 -14 L-12 -14 Z M-3 -20 L4 -20 L6.5 -14 L-3 -14 Z"
        fill="var(--sky)"
      />
      <path d="M-36 -9 L40 -11 L31 7 Q2 13 -29 9 Z" fill="var(--marker)" />
      <path
        d="M-35 -4 L38 -6"
        stroke="var(--sky)"
        strokeOpacity={0.6}
        strokeWidth={1.2}
        fill="none"
      />
    </>
  );
}

const filled = (line: string) => `${line} L262 300 L38 300 Z`;

function stillWanted(): boolean {
  return (
    typeof window.matchMedia !== "function" ||
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

/** The sea in the disc: a back swell, the front water and its crest. Drawn
 * settled; as it mounts it plays the settling once, frame by frame, unless
 * reduced motion is asked for. Drawing only. */
function Sea({ y, chop, waterline }: { y: number; chop: number; waterline: number }) {
  const boat = useRef<SVGGElement>(null);
  const back = useRef<SVGPathElement>(null);
  const front = useRef<SVGPathElement>(null);
  const crest = useRef<SVGPathElement>(null);

  useLayoutEffect(() => {
    if (stillWanted()) return;
    const draw = (p: number) => {
      const b = surface(SWELLS.back, y - chop * 0.7, chop * 0.8, p);
      const f = surface(SWELLS.front, y, chop, p);
      back.current?.setAttribute("d", filled(b));
      front.current?.setAttribute("d", filled(f));
      crest.current?.setAttribute("d", f);
      boat.current?.setAttribute("transform", boatAt(y, chop, waterline, p));
    };
    let frame = 0;
    const start = performance.now();
    const tick = (now: number) => {
      const p = Math.min((now - start) / SETTLE_MS, 1);
      draw(p);
      if (p < 1) frame = requestAnimationFrame(tick);
    };
    draw(0);
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [y, chop, waterline]);

  const b = surface(SWELLS.back, y - chop * 0.7, chop * 0.8, 1);
  const f = surface(SWELLS.front, y, chop, 1);
  return (
    <>
      <path ref={back} d={filled(b)} fill="var(--sea-back)" fillOpacity={0.55} />
      <g ref={boat} transform={boatAt(y, chop, waterline, 1)}>
        <Boat />
      </g>
      <path ref={front} d={filled(f)} fill="var(--sea)" />
      <path
        ref={crest}
        d={f}
        fill="none"
        stroke="var(--sea-crest)"
        strokeOpacity={0.7}
        strokeWidth={1.5}
      />
    </>
  );
}

function Arc({
  r,
  share,
  width,
  colour,
}: {
  r: number;
  share: number;
  width: number;
  colour: string;
}) {
  if (share <= 0) return null;
  const length = 2 * Math.PI * r;
  return (
    <circle
      cx={C}
      cy={C}
      r={r}
      fill="none"
      stroke={colour}
      strokeWidth={width}
      strokeLinecap="round"
      strokeDasharray={`${length * Math.min(share, 1)} ${length}`}
      transform={`rotate(-90 ${C} ${C})`}
    />
  );
}

/** A label curved along the outside of the ring, starting just clear of
 * `deg` and running away from it on `side`. Upright in both halves: over the
 * top it runs clockwise, under the bottom anticlockwise. */
function RingLabel({
  id,
  deg,
  side,
  colour,
  children,
}: {
  id: string;
  deg: number;
  side: "cw" | "ccw";
  colour: string;
  children: string;
}) {
  const bottom = Math.sin(rad(deg)) > 0.15;
  const r = bottom ? 152 : 143;
  const sweep = bottom ? 0 : 1;
  const from = at(r, deg + 180);
  const through = at(r, deg);
  // The path runs from the opposite side through `deg` and round again, so
  // `deg` sits at half its length, and the text can run either way from it.
  const d = `M${from} A${r} ${r} 0 1 ${sweep} ${through} A${r} ${r} 0 1 ${sweep} ${from}`;
  const ahead = (side === "cw") === (sweep === 1);
  const half = Math.PI * r;
  return (
    <>
      <path id={id} d={d} fill="none" />
      <text fontSize={11.5} fontWeight={600} fill={colour} letterSpacing={0.3}>
        <textPath
          href={`#${id}`}
          startOffset={half + (ahead ? 7 : -7)}
          textAnchor={ahead ? "start" : "end"}
        >
          {children}
        </textPath>
      </text>
    </>
  );
}

/** Home's hero: one card holding a dial and the figures that support it. The
 * disc's top two thirds carry the label, the figure in the display role and
 * what it is out of; its bottom third is the sea, with a boat riding it that
 * sits higher or lower by the State, in three steps. The State's name is for
 * screen readers; the eye reads the boat. The ring is the Period (DialRing).
 * Under the dial, the pace sentence, then the supporting figures in a row.
 * Nothing drawn reaches the words: the boat's mast stays under the "of" line.
 * Ruled 28 Nov 2026, replacing the card with a disclosure. A labelled figure,
 * not a live region: a refetch announces nothing. `className` is for layout
 * only. */
export function HeroDial({
  label,
  figure,
  of,
  state,
  stateLabel,
  pace,
  ring,
  figures,
  className,
}: {
  label: string;
  figure: string;
  of?: string;
  state?: DialState;
  stateLabel?: string;
  pace?: string;
  ring?: DialRing;
  figures: { label: string; value: string }[];
  className?: string;
}) {
  const labelId = useId();
  const uid = useId().replace(/[^a-zA-Z0-9]/g, "");
  const sea = state ? SEA[state] : undefined;

  const elapsed = ring ? (ring.days - ring.daysRemaining) / ring.days : 0;
  const spent = ring?.spent ?? null;
  // Where the spent arc ends, unwrapped (0 to 2), so the two labels can be
  // told apart even when one has lapped the ring.
  const spentEnd = spent === null ? null : Math.min(Math.max(spent, 0), 2);
  const todayFirst = spentEnd === null || elapsed <= spentEnd;

  return (
    <section
      aria-labelledby={labelId}
      className={cn(
        "relative flex flex-col rounded-2xl bg-card px-4 pt-3 pb-3.5 text-card-foreground hero-glass",
        className,
      )}
    >
      <div className="@container relative mx-auto aspect-square w-full max-w-[304px]">
        <svg viewBox="0 0 300 300" aria-hidden="true" className="absolute inset-0 overflow-visible">
          {ring &&
            Array.from({ length: ring.days }, (_, day) => {
              const deg = angleOf(day / ring.days);
              const major = day % 5 === 0;
              return (
                <line
                  key={day}
                  x1={C + TICKS_OUT * Math.cos(rad(deg))}
                  y1={C + TICKS_OUT * Math.sin(rad(deg))}
                  x2={C + (TICKS_OUT - (major ? 7 : 4)) * Math.cos(rad(deg))}
                  y2={C + (TICKS_OUT - (major ? 7 : 4)) * Math.sin(rad(deg))}
                  stroke="var(--muted-foreground)"
                  strokeOpacity={major ? 1 : 0.4}
                  strokeWidth={major ? 1.6 : 1}
                />
              );
            })}
          <circle cx={C} cy={C} r={SPENT_R} fill="none" stroke="var(--border)" strokeWidth={8} />
          {ring && (
            <>
              <Arc r={TIME_R} share={elapsed} width={2} colour="var(--muted-foreground)" />
              {spent !== null && (
                <>
                  <Arc r={SPENT_R} share={spent} width={8} colour="var(--primary)" />
                  <Arc r={SPENT_R} share={spent - 1} width={8} colour="var(--attention)" />
                </>
              )}
              <polygon
                points={[
                  at(TIME_R + 2, angleOf(elapsed)),
                  at(TICKS_OUT + 3, angleOf(elapsed) - 2.9),
                  at(TICKS_OUT + 3, angleOf(elapsed) + 2.9),
                ].join(" ")}
                fill="var(--marker)"
              />
              <RingLabel
                id={`${uid}-today`}
                deg={angleOf(elapsed)}
                side={todayFirst ? "ccw" : "cw"}
                colour="var(--marker)"
              >
                {ring.daysLeftLabel}
              </RingLabel>
              {spentEnd !== null && ring.spentLabel !== null && (
                <RingLabel
                  id={`${uid}-spent`}
                  deg={angleOf(spentEnd)}
                  side={todayFirst ? "cw" : "ccw"}
                  colour={spentEnd > 1 ? "var(--attention)" : "var(--primary)"}
                >
                  {ring.spentLabel}
                </RingLabel>
              )}
            </>
          )}
          <clipPath id={`${uid}-disc`}>
            <circle cx={C} cy={C} r={DISC} />
          </clipPath>
          <g clipPath={`url(#${uid}-disc)`}>
            <rect width={300} height={300} fill="var(--sky)" />
            {sea && <Sea y={SEA_TOP} chop={sea.chop} waterline={sea.waterline} />}
          </g>
          <circle cx={C} cy={C} r={DISC} fill="none" stroke="var(--border)" strokeWidth={1.5} />
        </svg>
        <p
          id={labelId}
          className="absolute inset-x-0 top-[21%] text-center text-[4.3cqw] font-medium"
        >
          {label}
        </p>
        <p
          className={cn(
            "absolute inset-x-[16%] top-[27%] overflow-hidden text-center leading-tight font-bold whitespace-nowrap tabular-nums",
            heroSize(figure),
          )}
        >
          {figure}
        </p>
        {of !== undefined && (
          <p className="absolute inset-x-0 top-[40%] text-center text-[4.3cqw] text-muted-foreground tabular-nums">
            {of}
          </p>
        )}
        {stateLabel !== undefined && <p className="sr-only">{stateLabel}</p>}
        {ring && (
          <p className="sr-only">
            {ring.spentLabel === null
              ? ring.daysLeftLabel
              : `${ring.daysLeftLabel}, ${ring.spentLabel}`}
          </p>
        )}
      </div>
      {pace !== undefined && (
        <p className="mt-1 text-center text-sm text-muted-foreground">{pace}</p>
      )}
      {figures.length > 0 && (
        <dl className="mt-3 flex justify-between gap-3 border-t border-border pt-2.5 text-sm">
          {figures.map((f, i) => (
            <div key={f.label} className={cn(i > 0 && "text-right")}>
              <dt className="text-muted-foreground">{f.label}</dt>
              <dd className="font-semibold tabular-nums">{f.value}</dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  );
}
