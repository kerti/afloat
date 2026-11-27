// The hero's type size, stepped down by how long the formatted figure is, so it
// always fits one line at a phone's 343px column and never wraps or clips the
// number. A step is chosen from the string that is shown (formatting, not
// derivation: nothing is computed from the amount). The bounds assume the
// widest glyphs, tabular bold digits at about 0.62em, a separator at 0.28em and
// "Rp" at 1.25em: the worst case, `Rp9.999.999.999.999.999` (16 integer digits,
// 23 characters), is about 12.5em, so 24px is 301px of the 343 available. A
// negative sign counts as a character, and a figure longer than any IDR one
// (another currency's code and decimals) falls to the title's own 20px.
// Confirmed on the phone preview (#155 R18). Uruni's `heroSize` in
// web/src/screens/Home.tsx steps the same way, from 36px to 24px.
const STEPS: readonly { upTo: number; className: string }[] = [
  { upTo: 13, className: "text-4xl" }, // 36px: up to Rp999.999.999 (13)
  { upTo: 16, className: "text-[32px]" }, // up to Rp99.999.999.999
  { upTo: 19, className: "text-[28px]" }, // up to Rp9.999.999.999.999 (19)
  { upTo: 23, className: "text-2xl" }, // 24px: 14 to 16 digits, the worst case at 23
];

export function heroSize(figure: string): string {
  return STEPS.find((s) => figure.length <= s.upTo)?.className ?? "text-xl";
}
