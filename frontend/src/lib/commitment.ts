import { formatMoney } from "@/lib/money";

// A Commitment's still due as the Household will read it (#155 R16, PRD D6):
// "Paid" when nothing is still due, the amount when something is, and "more than
// you set aside" when more was paid than the allocation. The wire string is
// signed money at four decimals and is never clamped by the server; the words
// are chosen from what the one formatter would show, never from the raw sign
// alone (as paceOf does), so a still due that shows as zero says "Paid" and
// never "Rp0 still due". The amount is the magnitude, half-up and unabbreviated,
// with no sign character: the words carry the direction. Nothing is derived
// here: the sign of a figure the server owns picks a word.
export type StillDue = { kind: "paid" } | { kind: "due" | "over"; amount: string };

const PLAIN = /^(-?)(\d+)(?:\.(\d+))?$/;

/** Positive: still due. Negative: more than was set aside. Null when the figure
 * is not a plain decimal, so the screen says nothing rather than a guess. */
export function stillDueOf(stillDue: string, currency: string, locale: string): StillDue | null {
  const m = PLAIN.exec(stillDue);
  if (!m) return null;
  const amount = formatMoney(`${m[2]}${m[3] ? "." + m[3] : ""}`, currency, locale);
  // A currency symbol or code has no digit 1 to 9, so any one is the amount's.
  if (!/[1-9]/.test(amount)) return { kind: "paid" };
  return { kind: m[1] === "-" ? "over" : "due", amount };
}
