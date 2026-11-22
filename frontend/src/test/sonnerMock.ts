// A recording stand-in for sonner, so a component test can read back the toasts
// a write raised without a viewport or timers. Use it as
//   vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
// and read the calls through `toast` imported from "sonner". The Toaster is a
// no-op: the real one is exercised in Pockets.toast.test.tsx.
import { vi } from "vitest";

export function sonnerMock() {
  const toast = Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn(), dismiss: vi.fn() });
  return { toast, Toaster: () => null };
}
