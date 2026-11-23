// The one money formatter and parser (docs/agents/ui-conventions.md, Money and
// dates; Uruni and Balances). Hand-built, never Intl's currency style: browsers
// disagree on it. Money is a decimal string on the wire (CLAUDE.md rule 1) and
// is never turned into a float here; every step works on its digits.
//
// PROPOSAL, not yet a house rule (#130 R15, pending a phone preview): the
// per-currency format. IDR shows no decimals and an `Rp` prefix with no space
// (CONTEXT.md writes "Rp9.000.000"); a currency without a symbol of its own
// here shows its code and a no-break space; decimals are 0, 2 or 3 by currency.

const SEPARATORS: Record<string, { group: string; decimal: string }> = {
  "en-GB": { group: ",", decimal: "." },
  "id-ID": { group: ".", decimal: "," },
};

function separators(locale: string) {
  return SEPARATORS[locale] ?? SEPARATORS["en-GB"]!;
}

const ZERO_DECIMALS = new Set([
  "IDR",
  "BIF",
  "CLP",
  "DJF",
  "GNF",
  "ISK",
  "JPY",
  "KMF",
  "KRW",
  "PYG",
  "RWF",
  "UGX",
  "UYI",
  "VND",
  "VUV",
  "XAF",
  "XOF",
  "XPF",
]);
const THREE_DECIMALS = new Set(["BHD", "IQD", "JOD", "KWD", "LYD", "OMR", "TND"]);

/** How many decimals a currency shows and accepts. */
export function currencyDecimals(currency: string): number {
  if (ZERO_DECIMALS.has(currency)) return 0;
  if (THREE_DECIMALS.has(currency)) return 3;
  return 2;
}

// Built, not written, so no editor or formatter turns it into a plain space.
const NBSP = String.fromCharCode(0xa0);

/** What stands before an amount: Rp for IDR, the code otherwise. */
export function currencySymbol(currency: string): string {
  return currency === "IDR" ? "Rp" : currency;
}

function prefix(currency: string): string {
  return currency === "IDR" ? "Rp" : currency + NBSP;
}

const SIGNED = /^(-?)(\d+)(?:\.(\d+))?$/;

function group(int: string, sep: string): string {
  return int.replace(/\B(?=(\d{3})+$)/g, sep);
}

function stripLeadingZeros(int: string): string {
  return int.replace(/^0+(?=\d)/, "");
}

function increment(digits: string): string {
  const out = digits.split("");
  for (let i = out.length - 1; i >= 0; i--) {
    if (out[i] === "9") out[i] = "0";
    else {
      out[i] = String(Number(out[i]) + 1);
      return out.join("");
    }
  }
  return "1" + out.join("");
}

// The digits of an amount rounded half up to `decimals`, shared by what the
// figure rows show and what the money field loads, so the two never differ.
function roundedDigits(m: RegExpExecArray, decimals: number) {
  const frac = m[3] ?? "";
  let digits = stripLeadingZeros(m[2]!) + frac.slice(0, decimals).padEnd(decimals, "0");
  if (frac.length > decimals && frac.charCodeAt(decimals) >= 53) digits = increment(digits);
  digits = digits.padStart(decimals + 1, "0");
  const cut = digits.length - decimals;
  return {
    int: stripLeadingZeros(digits.slice(0, cut)),
    fraction: digits.slice(cut),
    negative: m[1] === "-" && /[1-9]/.test(digits),
  };
}

/** A wire amount for the screen: grouped in the locale, the currency's own
 * decimals, half-up on the digits. Anything that is not a plain decimal comes
 * back as given, never NaN. */
export function formatMoney(value: string, currency: string, locale: string): string {
  const m = SIGNED.exec(value);
  if (!m) return value;
  const decimals = currencyDecimals(currency);
  const sep = separators(locale);
  const { int, fraction, negative } = roundedDigits(m, decimals);
  return (
    (negative ? "-" : "") +
    prefix(currency) +
    group(int, sep.group) +
    (decimals > 0 ? sep.decimal + fraction : "")
  );
}

/** A wire amount rounded half up to the currency's decimals, unpadded
 * ("1000.5" is "1001" in IDR): what a money field loads, so it shows what the
 * figures show. Anything that is not a plain decimal comes back as given. */
export function roundMoney(value: string, currency: string): string {
  const m = SIGNED.exec(value);
  if (!m) return value;
  const { int, fraction, negative } = roundedDigits(m, currencyDecimals(currency));
  return canonicalMoney((negative ? "-" : "") + int + (fraction ? "." + fraction : ""));
}

/** The same amount without padding: "9000000.0000" and "9000000" are one. Not a
 * plain decimal comes back as given. */
export function canonicalMoney(value: string): string {
  const m = SIGNED.exec(value);
  if (!m) return value;
  const frac = (m[3] ?? "").replace(/0+$/, "");
  return m[1] + stripLeadingZeros(m[2]!) + (frac ? "." + frac : "");
}

const MAX_INT_DIGITS = 16;
const MAX_DECIMALS = 4;

function maxDecimals(currency: string): number {
  return Math.min(currencyDecimals(currency), MAX_DECIMALS);
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** Strict parse of pasted text in the locale: optional correctly grouped
 * integer, then an optional decimal part within the currency's decimals.
 * Returns the wire string, or null when the text is anything else. */
export function parseMoney(text: string, locale: string, currency: string): string | null {
  const sep = separators(locale);
  const g = escapeRe(sep.group);
  const d = escapeRe(sep.decimal);
  const re = new RegExp(`^(\\d{1,3}(?:${g}\\d{3})+|\\d+)(?:${d}(\\d+))?$`);
  const m = re.exec(text.trim());
  if (!m) return null;
  const int = stripLeadingZeros(m[1]!.split(sep.group).join(""));
  const frac = (m[2] ?? "").replace(/0+$/, "");
  if (int.length > MAX_INT_DIGITS || (m[2] ?? "").length > maxDecimals(currency)) return null;
  return int + (frac ? "." + frac : "");
}

// The draft is what a MoneyInput holds while someone types: `digits[.digits]`,
// with a trailing "." while the decimal point has been typed and nothing after
// it. Its wire form is the draft without that point.

/** A wire amount as a draft; "" when it is not a plain unsigned decimal. */
export function draftFromWire(value: string): string {
  const c = canonicalMoney(value);
  return /^\d+(\.\d+)?$/.test(c) ? c : "";
}

export function draftToWire(draft: string): string {
  return draft.endsWith(".") ? draft.slice(0, -1) : draft;
}

/** What the field shows for a draft: the integer part grouped, the decimal
 * point in the locale's own. */
export function displayDraft(draft: string, locale: string): string {
  const sep = separators(locale);
  const dot = draft.indexOf(".");
  if (dot < 0) return group(draft, sep.group);
  return group(draft.slice(0, dot), sep.group) + sep.decimal + draft.slice(dot + 1);
}

/** Where the caret goes in the displayed text: after the same number of digits
 * and decimal points it was after before the text was regrouped. */
export function caretAfter(display: string, significant: number, locale: string): number {
  const sep = separators(locale);
  if (significant <= 0) return 0;
  let seen = 0;
  for (let i = 0; i < display.length; i++) {
    if (display[i] !== sep.group && ++seen === significant) return i + 1;
  }
  return display.length;
}

/** One edit of the field. `prev` is what was displayed, `raw` what the field
 * holds now and `caret` where the caret is. A typed "." or "," is the decimal
 * point; separators already on screen are the locale's own. Anything but a digit
 * and one decimal point is dropped, as are digits past the currency's decimals. */
export function typeInto(
  prev: string,
  raw: string,
  caret: number,
  locale: string,
  currency: string,
): { draft: string; significant: number } {
  const sep = separators(locale);
  let head = 0;
  while (head < prev.length && head < raw.length && prev[head] === raw[head]) head++;
  let tail = 0;
  while (
    tail < prev.length - head &&
    tail < raw.length - head &&
    prev[prev.length - 1 - tail] === raw[raw.length - 1 - tail]
  )
    tail++;
  const insertedEnd = raw.length - tail;
  const decimals = maxDecimals(currency);

  let draft = "";
  let significant = 0;
  let point = -1;
  for (let i = 0; i < raw.length; i++) {
    const ch = raw[i]!;
    const typed = i >= head && i < insertedEnd;
    let out = "";
    if (ch >= "0" && ch <= "9") out = ch;
    else if (typed ? ch === "." || ch === "," : ch === sep.decimal) out = ".";
    if (out === "") continue;
    if (out === ".") {
      if (decimals === 0 || point >= 0) continue;
      if (draft === "") draft = "0";
      point = draft.length;
    } else if (point >= 0 ? draft.length - point > decimals : draft.length >= MAX_INT_DIGITS) {
      continue;
    }
    draft += out;
    if (i < caret) significant = draft.length;
  }
  // Leading zeros collapse ("007" is 7); the caret loses the ones it had passed.
  const int = point >= 0 ? draft.slice(0, point) : draft;
  const trimmed = stripLeadingZeros(int);
  const removed = int.length - trimmed.length;
  if (removed > 0) {
    draft = trimmed + draft.slice(int.length);
    significant = Math.max(0, significant - removed);
  }
  return { draft, significant };
}
