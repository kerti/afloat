import { createContext, useContext } from "react";
import type { Me } from "@/api/client";

export type Appearance = Me["appearance"];

export type AppearanceCtx = {
  appearance: Appearance;
  // Applies and caches at once (optimistic); returns the write's number.
  choose: (v: Appearance) => number;
  // Reports the write's outcome. Only the latest write's failure rolls back.
  settle: (seq: number, ok: boolean) => void;
};

export const AppearanceContext = createContext<AppearanceCtx>({
  appearance: "system",
  choose: () => 0,
  settle: () => {},
});

export const useAppearance = () => useContext(AppearanceContext);
