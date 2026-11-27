import { formatMoney } from "@/lib/money";

// The pace sentence's words and amount, chosen from the server's Trajectory
// Variance as the Household will read it (#155 R8, PRD D3). The wire string is
// money at four decimals; the sentence is decided from what the one formatter
// would show, never from the raw sign alone, so a variance that shows as zero
// says "on pace" and never "Rp0 behind pace". The amount is the magnitude,
// half-up and unabbreviated, with no sign character: the words carry the
// direction. Nothing is derived here: the sign of a figure the server owns
// picks a word.
export type Pace = { direction: "on" } | { direction: "ahead" | "behind"; amount: string };

const PLAIN = /^(-?)(\d+)(?:\.(\d+))?$/;

/** Positive Trajectory Variance means less is spent than the target: ahead of
 * pace. Negative means more: behind pace. Null when the figure is not a plain
 * decimal, so the screen says nothing rather than a guess. */
export function paceOf(variance: string, currency: string, locale: string): Pace | null {
  const m = PLAIN.exec(variance);
  if (!m) return null;
  const amount = formatMoney(`${m[2]}${m[3] ? "." + m[3] : ""}`, currency, locale);
  // A currency symbol or code has no digit 1 to 9, so any one is the amount's.
  if (!/[1-9]/.test(amount)) return { direction: "on" };
  return { direction: m[1] === "-" ? "behind" : "ahead", amount };
}
