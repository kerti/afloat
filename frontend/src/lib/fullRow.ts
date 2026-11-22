// Whether the action at `index` takes a whole row: an odd one out, left without
// a partner, is the last of an odd count. (Uruni's fullRow, without its `wide`
// flag, which Afloat has no label long enough to need.)
export function fullRow(count: number, index: number): boolean {
  return index === count - 1 && index % 2 === 0;
}
