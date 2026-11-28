// The hero figure's type size, stepped down by how long the formatted figure
// is, so it always fits on one line inside the dial's disc. A step is chosen
// from the string that is shown (formatting, not derivation: nothing is
// computed from the amount). Sizes are in cqw, a share of the dial's width, so
// the figure shrinks with the dial on a narrow phone. The room is the disc's
// chord at the figure's line, about 63% of the dial. The bounds assume the
// widest glyphs: tabular bold digits at about 0.62em, a separator at 0.28em,
// "Rp" at 1.25em and a minus at 0.4em. So `Rp999.999.999` (13 characters) is
// about 7.4em, and 8.2cqw makes it 61% of the dial. The worst case,
// `-Rp9.999.999.999.999.999` (24), is about 13em at 4.6cqw: 60%. Past it, a
// figure no rupiah amount reaches (another currency's code and decimals) falls
// to 4cqw. A figure that still does not fit is cut at the disc, never wrapped.
// At a 304px dial, 9.9cqw is 30px and 4.6cqw is 14px (ruled 28 Nov 2026,
// replacing #155 R18's 36px to 24px steps for the card).
const STEPS: readonly { upTo: number; className: string }[] = [
  { upTo: 10, className: "text-[9.9cqw]" }, // up to Rp999.999 and -Rp99.999
  { upTo: 13, className: "text-[8.2cqw]" }, // up to Rp999.999.999
  { upTo: 16, className: "text-[7cqw]" }, // up to Rp99.999.999.999
  { upTo: 19, className: "text-[5.6cqw]" }, // up to Rp9.999.999.999.999
  { upTo: 24, className: "text-[4.6cqw]" }, // the worst case, and its negative
];

export function heroSize(figure: string): string {
  return STEPS.find((s) => figure.length <= s.upTo)?.className ?? "text-[4cqw]";
}
