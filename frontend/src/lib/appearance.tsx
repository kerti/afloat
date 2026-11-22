import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { Appearance } from "@/lib/useAppearance";
import { useMe } from "@/api/queries";
import { AppearanceContext } from "@/lib/useAppearance";

// public/appearance-init.js is the engine: the boot script runs it before
// paint, and this provider, the one writer after boot, calls the same code, so
// the key table, the cache key and the theme-color rule live once.
type Engine = {
  read: (storage: Storage, name: string) => string | null;
  write: (storage: Storage, name: string, value: string) => void;
  apply: (doc: Document, name: string, value: string) => void;
};

declare global {
  interface Window {
    afloatAppearance?: Engine;
  }
}

const NAME = "appearance";
const known: readonly string[] = ["system", "light", "dark"];

let warned = false;
// The boot script is what defines the engine. Without it (blocked, failed to
// load) the picker still saves to the server but nothing reaches the page, so
// say so once in the console. Nothing leaves the browser.
function engine(): Engine | undefined {
  const e = window.afloatAppearance;
  if (!e && !warned) {
    warned = true;
    console.error("appearance-init.js did not load: appearance is not applied or cached");
  }
  return e;
}

function cached(): Appearance {
  try {
    const v = engine()?.read(window.localStorage, NAME);
    if (v && known.includes(v)) return v as Appearance;
  } catch {
    // Storage can throw; the cache is only a convenience.
  }
  return "system";
}

function put(value: Appearance) {
  const e = engine();
  if (!e) return;
  e.apply(document, NAME, value);
  try {
    e.write(window.localStorage, NAME, value);
  } catch {
    // Storage can throw; the cache is only a convenience.
  }
}

// The one writer of <html data-appearance> and its cache after boot (#124).
// The cache only makes the first paint right: once /me loads, the server's
// value wins and is written back to the cache. Signing out leaves it alone.
//
// Writes are numbered. While any is in flight the server's value is not
// applied (an earlier write's refetch would otherwise flip a newer choice),
// and only the latest write's failure rolls back, to the last server value.
export function AppearanceProvider({ children }: { children: React.ReactNode }) {
  const [appearance, setAppearance] = useState<Appearance>(cached);
  const [inFlight, setInFlight] = useState(0);
  const [confirmed, setConfirmed] = useState<Appearance | undefined>(undefined);
  const confirmedRef = useRef(confirmed);
  const latest = useRef(0);
  const server = useMe().data?.appearance;
  // A new server value (first load, or another User signing in) replaces the
  // cached one during render, not in an effect.
  if (server && server !== confirmed && inFlight === 0) {
    setConfirmed(server);
    setAppearance(server);
  }

  // The external system: the attribute, theme-color and the cache.
  useLayoutEffect(() => {
    confirmedRef.current = confirmed;
    put(appearance);
  }, [appearance, confirmed]);

  const choose = useCallback((v: Appearance) => {
    const seq = ++latest.current;
    setInFlight((n) => n + 1);
    setAppearance(v);
    return seq;
  }, []);

  const settle = useCallback((seq: number, ok: boolean) => {
    setInFlight((n) => n - 1);
    if (!ok && seq === latest.current) setAppearance(confirmedRef.current ?? "system");
  }, []);

  const value = useMemo(() => ({ appearance, choose, settle }), [appearance, choose, settle]);
  return <AppearanceContext value={value}>{children}</AppearanceContext>;
}
