import { useSyncExternalStore } from "react";

// How much of the bottom of the layout viewport the on-screen keyboard covers,
// and how tall the part still visible is, in CSS px. Ported from Uruni
// web/src/lib/useKeyboardInset.ts (github.com/kerti/uruni).
export type KeyboardInset = { inset: number; height: number };

const NO_KEYBOARD: KeyboardInset = { inset: 0, height: 0 };

// useSyncExternalStore compares snapshots by identity, so the object is only
// replaced when a value changes.
let cachedKey = "";
let cached: KeyboardInset = NO_KEYBOARD;

function subscribe(onChange: () => void): () => void {
  const viewport = window.visualViewport;
  if (!viewport) return () => {};
  viewport.addEventListener("resize", onChange);
  viewport.addEventListener("scroll", onChange);
  return () => {
    viewport.removeEventListener("resize", onChange);
    viewport.removeEventListener("scroll", onChange);
  };
}

function getSnapshot(): KeyboardInset {
  const viewport = window.visualViewport;
  if (!viewport) return NO_KEYBOARD;
  const inset = Math.max(0, Math.round(window.innerHeight - viewport.height - viewport.offsetTop));
  const height = Math.round(viewport.height);
  const key = `${inset}:${height}`;
  if (key !== cachedKey) {
    cachedKey = key;
    cached = { inset, height };
  }
  return cached;
}

// iOS and recent Android Chrome slide the keyboard over a fixed dialog instead
// of shrinking the layout viewport, so only the visual viewport tells where the
// visible area is. No visualViewport (and jsdom) reads as no keyboard.
export function useKeyboardInset(): KeyboardInset {
  return useSyncExternalStore(subscribe, getSnapshot, () => NO_KEYBOARD);
}
