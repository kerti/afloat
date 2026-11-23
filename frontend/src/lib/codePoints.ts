// The server counts a name's length in code points, so the client does too:
// maxLength counts UTF-16 units and would cut an emoji name at 30.
export const NAME_MAX = 60;

export function clampName(value: string, max = NAME_MAX): string {
  const points = Array.from(value);
  return points.length <= max ? value : points.slice(0, max).join("");
}
